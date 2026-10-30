package facts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/gzip"

	"github.com/colligo-go/colligo/internal/exttool"
)

const (
	perfDefaultTolerancePercent = 5
	perfRounds                  = 5
	perfTrials                  = 3
)

type perfMeasure map[string]float64

type perfBaseline struct {
	Machine          string                   `json:"machine"`
	Go               string                   `json:"go"`
	TolerancePercent float64                  `json:"tolerance_percent"`
	Conditions       map[string]string        `json:"conditions,omitempty"`
	Runs             map[string][]perfMeasure `json:"runs"`
}

func (b perfBaseline) tolerance() float64 {
	if b.TolerancePercent == 0 {
		return 1 + perfDefaultTolerancePercent/100.0
	}
	return 1 + b.TolerancePercent/100
}

func perfBaselinePath() string {
	return filepath.Join("..", "testdata", "perf", os.Getenv("COLLIGO_PERF_MACHINE"), "baseline.json")
}

func readPerfBaseline(t *testing.T) perfBaseline {
	t.Helper()
	b := perfBaseline{Machine: os.Getenv("COLLIGO_PERF_MACHINE"), Go: runtime.Version(), Runs: map[string][]perfMeasure{}}
	raw, err := os.ReadFile(perfBaselinePath())
	if errors.Is(err, os.ErrNotExist) {
		return b
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func recordPerfRun(t *testing.T, name string, m perfMeasure) {
	t.Helper()
	b := readPerfBaseline(t)
	b.Go = runtime.Version()
	now := readPerfConditions("/sys")
	if mismatches := perfConditionMismatches(b.Conditions, now); len(mismatches) > 0 {
		t.Fatalf("refusing to record: the conditions differ from the ones already in the baseline: %s", strings.Join(mismatches, "; "))
	}
	b.Conditions = now
	if v := os.Getenv("COLLIGO_PERF_TOLERANCE_PERCENT"); v != "" {
		if _, err := fmt.Sscan(v, &b.TolerancePercent); err != nil {
			t.Fatal(err)
		}
	}
	b.Runs[name] = append(b.Runs[name], m)
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(perfBaselinePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(perfBaselinePath(), append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func medianRecorded(runs []perfMeasure, key string) float64 {
	values := make([]float64, 0, len(runs))
	for _, run := range runs {
		values = append(values, run[key])
	}
	sort.Float64s(values)
	return values[len(values)/2]
}

func medianMeasure(rounds []perfMeasure) perfMeasure {
	median := perfMeasure{}
	for key := range rounds[0] {
		median[key] = medianRecorded(rounds, key)
	}
	return median
}

func perfRegressions(runs []perfMeasure, now perfMeasure, gated []string, tolerance float64) []string {
	var worse []string
	for _, key := range gated {
		base := medianRecorded(runs, key)
		if now[key] > base*tolerance {
			worse = append(worse, fmt.Sprintf("%s %.3f vs baseline %.3f (+%.1f%%)", key, now[key], base, 100*(now[key]/base-1)))
		}
	}
	return worse
}

func perfSpread(runs []perfMeasure, key string) float64 {
	values := make([]float64, 0, len(runs))
	for _, run := range runs {
		values = append(values, run[key])
	}
	sort.Float64s(values)
	return (values[len(values)-1] - values[0]) / values[len(values)/2]
}

func runPerf(t *testing.T, name string, gated []string, measure func(*testing.T) perfMeasure) perfMeasure {
	t.Helper()
	machine := os.Getenv("COLLIGO_PERF_MACHINE")
	record := os.Getenv("COLLIGO_PERF_RECORD") != ""
	if record && machine == "" {
		t.Fatal("COLLIGO_PERF_RECORD needs COLLIGO_PERF_MACHINE to say which machine the baseline belongs to")
	}
	if machine == "" {
		t.Log("COLLIGO_PERF_MACHINE is not set: measuring without comparing to a baseline")
		return measure(t)
	}
	if record {
		m := measure(t)
		recordPerfRun(t, name, m)
		return m
	}
	baseline := readPerfBaseline(t)
	runs := baseline.Runs[name]
	if len(runs) == 0 {
		t.Fatalf("no %q baseline for machine %q in %s: record one with COLLIGO_PERF_RECORD=1", name, machine, perfBaselinePath())
	}
	if len(baseline.Conditions) == 0 {
		t.Log("the baseline records no measurement conditions: nothing to check them against")
	}
	if mismatches := perfConditionMismatches(baseline.Conditions, readPerfConditions("/sys")); len(mismatches) > 0 {
		t.Fatalf("refusing to compare with the baseline of %q, the measurement conditions changed:\n  %s", machine, strings.Join(mismatches, "\n  "))
	}
	rounds := make([]perfMeasure, perfRounds)
	for i := range rounds {
		rounds[i] = measure(t)
	}
	median := medianMeasure(rounds)
	for _, key := range gated {
		t.Logf("%s: median of %d rounds %.3f, spread between rounds %.1f%%", key, perfRounds, median[key], 100*perfSpread(rounds, key))
	}
	if worse := perfRegressions(runs, median, gated, baseline.tolerance()); len(worse) > 0 {
		t.Fatalf("the median of %d rounds is more than %.0f%% worse than the baseline of %q: %v", perfRounds, 100*(baseline.tolerance()-1), machine, worse)
	}
	t.Logf("no regression against the baseline of %q (median of %d rounds, tolerance %.0f%%)", machine, perfRounds, 100*(baseline.tolerance()-1))
	return median
}

func bestOf(fn func()) time.Duration {
	d := timeIt(fn)
	for i := 1; i < perfTrials; i++ {
		d = min(d, timeIt(fn))
	}
	return d
}

func openOrFail(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPerformanceFloor(t *testing.T) {
	if os.Getenv("COLLIGO_PERF") == "" {
		t.Skip("set COLLIGO_PERF=1 to run the performance benchmark")
	}
	if _, err := exec.LookPath("zcat"); err != nil {
		t.Fatal("zcat not found: the reference floor cannot be measured")
	}
	path, size := perfFastqGz(t, 1024)
	m := runPerf(t, "gzip", []string{"check_s", "check_vs_gunzip", "check_vs_zcat"}, func(t *testing.T) perfMeasure {
		goFloor := bestOf(func() {
			f := openOrFail(t, path)
			defer f.Close()
			r, err := gzip.NewReader(f)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.Copy(io.Discard, r); err != nil {
				t.Fatal(err)
			}
		})
		zcat := bestOf(func() {
			if err := exec.Command("sh", "-c", fmt.Sprintf("zcat %q | wc -l >/dev/null", path)).Run(); err != nil {
				t.Fatal(err)
			}
		})
		ours := bestOf(func() {
			f := openOrFail(t, path)
			defer f.Close()
			if _, err := Compute(context.Background(), f, Options{Hashes: true}); err != nil {
				t.Fatal(err)
			}
		})
		return perfMeasure{
			"gunzip_s":        goFloor.Seconds(),
			"zcat_s":          zcat.Seconds(),
			"check_s":         ours.Seconds(),
			"check_vs_gunzip": float64(ours) / float64(goFloor),
			"check_vs_zcat":   float64(ours) / float64(zcat),
		}
	})
	t.Logf("file %.0f MiB compressed: Go gunzip = %.2fs, zcat|wc -l = %.2fs, check = %.2fs", float64(size)/(1<<20), m["gunzip_s"], m["zcat_s"], m["check_s"])
	t.Logf("check = %.1f%% of Go gunzip (target <= 110%%), %.1f%% of zcat|wc -l (reported)", 100*m["check_vs_gunzip"], 100*m["check_vs_zcat"])
	if m["check_vs_gunzip"] > 1.10 {
		t.Fatalf("check is %.1f%% of the Go gunzip floor, target is 110%%", 100*m["check_vs_gunzip"])
	}
}

func TestPerformanceHash(t *testing.T) {
	if os.Getenv("COLLIGO_PERF") == "" {
		t.Skip("set COLLIGO_PERF=1 to run the performance benchmark")
	}
	b3sum, ok := exttool.B3Sum(t)
	if !ok {
		t.Skip("b3sum not found: the hash floor cannot be measured")
	}
	for _, tool := range []string{"md5sum", "sha256sum"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s not found: the hash floor cannot be measured", tool)
		}
	}
	path, size := perfFastq(t, 1024)
	m := runPerf(t, "hash", []string{"hash_s", "hash_vs_floor"}, func(t *testing.T) perfMeasure {
		alone := func(tool string, args ...string) float64 {
			return bestOf(func() {
				if err := exec.Command(tool, append(args, path)...).Run(); err != nil {
					t.Fatal(err)
				}
			}).Seconds()
		}
		md5 := alone("md5sum")
		sha := alone("sha256sum")
		blake := alone(b3sum, "--num-threads", "1")
		b3 := alone(b3sum)
		floor := max(md5, sha, blake)
		ours := bestOf(func() {
			f := openOrFail(t, path)
			defer f.Close()
			if _, err := Compute(context.Background(), f, Options{Hashes: true}); err != nil {
				t.Fatal(err)
			}
		}).Seconds()
		return perfMeasure{
			"md5sum_s":        md5,
			"sha256sum_s":     sha,
			"b3sum_1thread_s": blake,
			"b3sum_s":         b3,
			"hash_floor_s":    floor,
			"hash_s":          ours,
			"hash_vs_floor":   ours / floor,
			"hash_vs_b3sum":   ours / b3,
		}
	})
	t.Logf("file %.0f MiB uncompressed: md5sum = %.2fs, sha256sum = %.2fs, b3sum --num-threads 1 = %.2fs, floor (slowest) = %.2fs, Compute = %.2fs", float64(size)/(1<<20), m["md5sum_s"], m["sha256sum_s"], m["b3sum_1thread_s"], m["hash_floor_s"], m["hash_s"])
	t.Logf("Compute = %.1f%% of the slowest single hash (target <= 120%%), %.1f%% of b3sum with all threads (reported)", 100*m["hash_vs_floor"], 100*m["hash_vs_b3sum"])
	if m["hash_vs_floor"] > 1.20 {
		t.Fatalf("Compute is %.1f%% of the slowest single hash, target is 120%%", 100*m["hash_vs_floor"])
	}
}
