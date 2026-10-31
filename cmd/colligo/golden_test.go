package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/identity"
	"github.com/colligo-go/colligo/transport"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files from the current output")

type goldenRun func(extra ...string) (code int, out string)

type goldenCase struct {
	name    string
	prepare func(t *testing.T, dir string) goldenRun
}

func checkRun(args []string, net transport.Transport) goldenRun {
	return func(extra ...string) (int, string) {
		var out, errb bytes.Buffer
		code := runCheck(append(append([]string{}, extra...), args...), &out, &errb, false, net)
		return code, out.String()
	}
}

func identifyRun(table identity.Table, args []string) goldenRun {
	return func(extra ...string) (int, string) {
		var out, errb bytes.Buffer
		code := runIdentify(append(append([]string{}, extra...), args...), &out, &errb, table)
		return code, out.String()
	}
}

func sourceCase(accession string, mutate func(spec runSpec, files map[string][]byte), edit func(t *testing.T, body []byte) []byte) func(t *testing.T, dir string) goldenRun {
	return sourceFlagCase(accession, "ena:"+accession, mutate, edit)
}

func sourceFlagCase(accession, flagValue string, mutate func(spec runSpec, files map[string][]byte), edit func(t *testing.T, body []byte) []byte) func(t *testing.T, dir string) goldenRun {
	return func(t *testing.T, dir string) goldenRun {
		spec := specOf(t, accession)
		files := syntheticRun(spec)
		if mutate != nil {
			mutate(spec, files)
		}
		writeDecompressed(t, dir, files)
		body := recorded(t, accession)
		if edit != nil {
			body = edit(t, body)
		}
		return checkRun([]string{"--source", flagValue, dir}, &fakeTransport{body: body})
	}
}

func shortenSecondMate(accession string) func(runSpec, map[string][]byte) {
	return func(_ runSpec, files map[string][]byte) {
		name := accession + "_2.fastq.gz"
		files[name] = shortenReads(files[name])
	}
}

func goldenCases() []goldenCase {
	return []goldenCase{
		{"verified", func(t *testing.T, dir string) goldenRun {
			put(t, dir, "sample_R1.fastq", fastq("S", 20))
			put(t, dir, "sample_R2.fastq", fastq("S", 20))
			return checkRun([]string{dir}, nil)
		}},
		{"source-verified", sourceCase("DRR021374", nil, nil)},
		{"source-link", sourceFlagCase("DRR021374", "https://www.ebi.ac.uk/ena/browser/view/DRR021374", nil, nil)},
		{"source-deduced", sourceFlagCase("DRR021374", "ena", nil, nil)},
		{"list-missing-line", func(t *testing.T, dir string) goldenRun {
			put(t, dir, "sample_R1.fastq", fastq("S", 20))
			put(t, dir, "sample_R2.fastq", fastq("S", 20))
			list := put(t, dir, "files.txt", []byte("# my run\nsample_R1.fastq\nsample_R3.fastq\nsample_R2.fastq\n"))
			return checkRun([]string{"--files-from", list}, nil)
		}},
		{"sra-archive", func(t *testing.T, dir string) goldenRun {
			path := put(t, dir, "SRR10663037.sra", append([]byte("NCBI.sra"), make([]byte, 64)...))
			return checkRun([]string{path}, nil)
		}},
		{"blocking", sourceCase("DRR021374", shortenSecondMate("DRR021374"), nil)},
		{"advisory", sourceCase("ERR5060520", nil, nil)},
		{"study", func(t *testing.T, dir string) goldenRun {
			first, second := specOf(t, "DRR021374"), specOf(t, "ERR12477684")
			files := syntheticRun(first)
			files["DRR021374_2.fastq.gz"] = shortenReads(files["DRR021374_2.fastq.gz"])
			for name, data := range syntheticRun(second) {
				files[name] = data
			}
			writeDecompressed(t, dir, files)
			rows := strings.SplitN(string(recorded(t, "ERR12477684")), "\n", 2)[1]
			body := append(recorded(t, "DRR021374"), []byte(rows)...)
			return checkRun([]string{"--source", "ena:PRJDB0001", dir}, &fakeTransport{body: body})
		}},
		{"not-declared", sourceCase("DRR021374", nil, func(t *testing.T, body []byte) []byte {
			return setField(t, body, "read_count", to(""))
		})},
		{"unreadable", func(t *testing.T, dir string) goldenRun {
			put(t, dir, "good.fastq", fastq("S", 10))
			locked := put(t, dir, "locked.fastq", fastq("L", 10))
			if err := os.Chmod(locked, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(locked, 0o644) })
			return checkRun([]string{dir}, nil)
		}},
		{"samplesheet-swapped", func(t *testing.T, dir string) goldenRun {
			putMates(t, dir, "a", "AAA", 10, ".fastq", false)
			putMates(t, dir, "b", "BBB", 10, ".fastq", false)
			sheet := put(t, dir, "samples.csv", []byte("sample,fastq_1,fastq_2,strandedness\n"+
				"A,a_R1.fastq,b_R2.fastq,auto\n"+
				"B,b_R1.fastq,a_R2.fastq,auto\n"))
			return checkRun([]string{"--samplesheet", sheet}, nil)
		}},
		{"identify-fasta", func(t *testing.T, dir string) goldenRun {
			known := put(t, dir, "known.fa", []byte(cliFasta))
			return identifyRun(cliReferences, []string{known})
		}},
		{"identify-gtf-declared", func(t *testing.T, dir string) goldenRun {
			declared := put(t, dir, "declared.gtf", []byte(cliGencodeHeader+"chr1"+cliGencodeGene))
			return identifyRun(identity.Table{}, []string{declared})
		}},
		{"identify-gtf-conflict", func(t *testing.T, dir string) goldenRun {
			conflict := put(t, dir, "conflict.gtf", []byte(cliGencodeHeader+"1"+cliGencodeGene))
			return identifyRun(identity.Table{}, []string{conflict})
		}},
		{"identify-gtf-checksum", func(t *testing.T, dir string) goldenRun {
			data := []byte(cliGencodeHeader + "chr1" + cliGencodeGene)
			path := put(t, dir, "annotation.gtf", data)
			sum := md5.Sum(data)
			table := identity.Table{Checksums: []identity.Checksum{{
				Label: "GENCODE human 44", Asset: "gencode.v44.annotation.gtf", MD5: hex.EncodeToString(sum[:]),
				Origin: "https://example.org/MD5SUMS", Retrieved: "2026-10-31",
			}}}
			return identifyRun(table, []string{path})
		}},
	}
}

func normalized(out, dir string) string {
	return strings.ReplaceAll(out, dir, "<dir>")
}

func goldenFile(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden file missing: %v", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the golden file; run with -update and review the diff\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

func TestJSONGolden(t *testing.T) {
	for _, c := range goldenCases() {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			code, out := c.prepare(t, dir)("--json")
			goldenFile(t, filepath.Join("json", c.name+".json"), normalized(out, dir))
			goldenFile(t, filepath.Join("json", c.name+".exit"), strconv.Itoa(code)+"\n")
		})
	}
}

var elapsedPattern = regexp.MustCompile(`(?m)( · )(\d+ ms|\d+\.\d s|\d+ m \d{2} s|\d+ h \d{2} m)(\x1b\[0m)?$`)

func normalizedText(out, dir string) string {
	return elapsedPattern.ReplaceAllString(normalized(out, dir), "${1}<time>${3}")
}

func TestTextGolden(t *testing.T) {
	for _, c := range goldenCases() {
		for _, mode := range []string{"never", "always"} {
			t.Run(c.name+"/"+mode, func(t *testing.T) {
				dir := t.TempDir()
				code, out := c.prepare(t, dir)("--color=" + mode)
				suffix := map[string]string{"never": "plain", "always": "color"}[mode]
				goldenFile(t, filepath.Join("text", c.name+"."+suffix+".txt"), normalizedText(out, dir))
				goldenFile(t, filepath.Join("text", c.name+".exit"), strconv.Itoa(code)+"\n")
			})
		}
	}
}

type usageCase struct {
	name    string
	prepare func(t *testing.T, dir string) []string
}

func usageCases() []usageCase {
	return []usageCase{
		{"deduce-two-runs", func(t *testing.T, dir string) []string {
			writeDecompressed(t, dir, syntheticRun(specOf(t, "DRR021374")))
			writeDecompressed(t, dir, syntheticRun(specOf(t, "ERR12477684")))
			return []string{"check", "--source", "ena", dir}
		}},
		{"deduce-from-a-study-file-name", func(t *testing.T, dir string) []string {
			path := put(t, dir, "PRJDB0001_1.fastq", fastq("S", 5))
			return []string{"check", "--source", "ena", path}
		}},
		{"link-not-ena", func(t *testing.T, dir string) []string {
			path := put(t, dir, "a.fastq", fastq("S", 5))
			return []string{"check", "--source", "https://www.ncbi.nlm.nih.gov/sra/SRR10663037", path}
		}},
		{"link-without-accession", func(t *testing.T, dir string) []string {
			path := put(t, dir, "a.fastq", fastq("S", 5))
			return []string{"check", "--source", "https://www.ebi.ac.uk/ena/browser/home", path}
		}},
		{"unknown-flag", func(t *testing.T, dir string) []string {
			return []string{"check", "--sorce", "ena", dir}
		}},
		{"no-path", func(t *testing.T, dir string) []string {
			return []string{"check"}
		}},
		{"xlsx-as-samplesheet", func(t *testing.T, dir string) []string {
			sheet := put(t, dir, "samples.xlsx", append([]byte("PK\x03\x04"), make([]byte, 64)...))
			return []string{"check", "--samplesheet", sheet}
		}},
		{"files-from-with-samplesheet", func(t *testing.T, dir string) []string {
			list := put(t, dir, "files.txt", []byte("a.fastq\n"))
			sheet := put(t, dir, "samples.csv", []byte("sample,fastq_1,fastq_2\nA,a.fastq,\n"))
			return []string{"check", "--files-from", list, "--samplesheet", sheet}
		}},
	}
}

func TestUsageGolden(t *testing.T) {
	for _, c := range usageCases() {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			args := c.prepare(t, dir)
			var out, errb bytes.Buffer
			code := run(args, &out, &errb)
			if out.Len() != 0 {
				t.Errorf("a usage error wrote to stdout: %q", out.String())
			}
			goldenFile(t, filepath.Join("usage", c.name+".txt"), normalized(errb.String(), dir))
			goldenFile(t, filepath.Join("usage", c.name+".exit"), strconv.Itoa(code)+"\n")
		})
	}
}
