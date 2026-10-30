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
	"testing"
	"time"

	"github.com/klauspost/compress/gzip"

	"github.com/colligo-go/colligo/internal/exttool"
)

const (
	perfTolerance = 1.05
	perfRounds    = 3
	perfTrials    = 3
)

type perfMeasure map[string]float64

type perfBaseline struct {
	Machine string                   `json:"machine"`
	Go      string                   `json:"go"`
	Runs    map[string][]perfMeasure `json:"runs"`
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

func bestRecorded(runs []perfMeasure, key string) float64 {
	best := runs[0][key]
	for _, run := range runs[1:] {
		best = min(best, run[key])
	}
	return best
}

func perfRegressions(runs []perfMeasure, now perfMeasure, gated []string) []string {
	var worse []string
	for _, key := range gated {
		base := bestRecorded(runs, key)
		if now[key] > base*perfTolerance {
			worse = append(worse, fmt.Sprintf("%s %.3f vs baseline %.3f (+%.1f%%)", key, now[key], base, 100*(now[key]/base-1)))
		}
	}
	return worse
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
	runs := readPerfBaseline(t).Runs[name]
	if len(runs) == 0 {
		t.Fatalf("no %q baseline for machine %q in %s: record one with COLLIGO_PERF_RECORD=1", name, machine, perfBaselinePath())
	}
	var m perfMeasure
	var worse []string
	for round := 1; round <= perfRounds; round++ {
		m = measure(t)
		worse = perfRegressions(runs, m, gated)
		if len(worse) == 0 {
			t.Logf("no regression against the baseline of %q (round %d of %d, tolerance %.0f%%)", machine, round, perfRounds, 100*(perfTolerance-1))
			return m
		}
		t.Logf("round %d of %d regressed: %v", round, perfRounds, worse)
	}
	t.Fatalf("every one of %d rounds is more than %.0f%% worse than the baseline of %q: %v", perfRounds, 100*(perfTolerance-1), machine, worse)
	return m
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
	path, size := perfFastq(t, 1024)
	m := runPerf(t, "hash", []string{"hash_s"}, func(t *testing.T) perfMeasure {
		run := func(args ...string) time.Duration {
			return bestOf(func() {
				if err := exec.Command(b3sum, append(args, path)...).Run(); err != nil {
					t.Fatal(err)
				}
			})
		}
		floor := run()
		floorOneThread := run("--num-threads", "1")
		ours := bestOf(func() {
			f := openOrFail(t, path)
			defer f.Close()
			if _, err := Compute(context.Background(), f, Options{Hashes: true}); err != nil {
				t.Fatal(err)
			}
		})
		return perfMeasure{
			"b3sum_s":               floor.Seconds(),
			"b3sum_1thread_s":       floorOneThread.Seconds(),
			"hash_s":                ours.Seconds(),
			"hash_vs_b3sum":         float64(ours) / float64(floor),
			"hash_vs_b3sum_1thread": float64(ours) / float64(floorOneThread),
		}
	})
	t.Logf("file %.0f MiB uncompressed: b3sum = %.2fs, b3sum --num-threads 1 = %.2fs, Compute = %.2fs", float64(size)/(1<<20), m["b3sum_s"], m["b3sum_1thread_s"], m["hash_s"])
	t.Logf("Compute = %.1f%% of b3sum (target <= 120%%, reported), %.1f%% of single-thread b3sum", 100*m["hash_vs_b3sum"], 100*m["hash_vs_b3sum_1thread"])
}
