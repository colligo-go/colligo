package verifier

import (
	"encoding/json"
	"os"
	"testing"
)

func countsOf(t *testing.T, rep *FileReport) (reads, bases *int64, keys map[string]json.RawMessage) {
	t.Helper()
	raw, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	return rep.Reads, rep.Bases, keys
}

func requireNoCounts(t *testing.T, label string, rep *FileReport) {
	t.Helper()
	reads, bases, keys := countsOf(t, rep)
	if reads != nil || bases != nil {
		t.Errorf("%s: counts %v %v on a file that was not read as FASTQ", label, reads, bases)
	}
	if _, ok := keys["reads"]; ok {
		t.Errorf("%s: reads is in the JSON", label)
	}
	if _, ok := keys["bases"]; ok {
		t.Errorf("%s: bases is in the JSON", label)
	}
}

func TestReadsAndBasesAreInTheJSONOfAFastqReadWhole(t *testing.T) {
	dir := t.TempDir()
	data := fastqText("SRR1", 25, "ACGTACGTAC")
	cases := map[string]Options{"check": {}, "validate": {Structure: true}}
	for label, opts := range cases {
		for name, content := range map[string][]byte{"plain": data, "gzip": gzipped(t, data)} {
			rep := run(t, opts, write(t, dir, label+"-"+name+".fastq", content)).Files[0]
			reads, bases, keys := countsOf(t, rep)
			if reads == nil || bases == nil || *reads != 25 || *bases != 250 {
				t.Errorf("%s %s: reads %v bases %v, want 25 and 250", label, name, reads, bases)
			}
			if string(keys["reads"]) != "25" || string(keys["bases"]) != "250" {
				t.Errorf("%s %s: JSON reads %s bases %s", label, name, keys["reads"], keys["bases"])
			}
		}
	}
}

func TestReadsAndBasesAreAbsentNeverZeroWhereTheyDoNotApply(t *testing.T) {
	dir := t.TempDir()
	good := fastqText("SRR1", 25, "ACGTACGTAC")
	fasta, err := os.ReadFile("../testdata/synthetic/genome.fa")
	if err != nil {
		t.Fatal(err)
	}
	gtf, err := os.ReadFile("../testdata/synthetic/annotation.gtf")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		opts Options
		data []byte
	}{
		"quick":              {Options{Quick: true}, good},
		"fasta":              {Options{}, fasta},
		"gtf":                {Options{}, gtf},
		"truncated fastq":    {Options{}, good[:len(good)-7]},
		"empty":              {Options{}, nil},
		"not a sequence":     {Options{}, []byte("hello\n")},
		"html as fastq":      {Options{}, []byte("<!DOCTYPE html><html></html>")},
		"fastq bad quality":  {Options{}, []byte("@r1\nACGT\n+\nFF\n")},
		"quick on a gzip":    {Options{Quick: true}, gzipped(t, good)},
		"truncated gzip":     {Options{}, gzipped(t, good)[:60]},
		"mismatched quality": {Options{}, []byte("@r1\nACGT\n+\nFFFFF\n")},
	}
	for label, tc := range cases {
		rep := run(t, tc.opts, write(t, dir, "f"+label+".dat", tc.data)).Files[0]
		requireNoCounts(t, label, rep)
	}
	missing := run(t, Options{}, write(t, dir, "x", nil)).Files[0]
	if err := os.Remove(string(missing.Name.String())); err != nil {
		t.Fatal(err)
	}
	rep := run(t, Options{}, missing.Name).Files[0]
	requireNoCounts(t, "missing", rep)
}

func TestReadsAndBasesAreAbsentWhenTheFileChangedDuringTheRead(t *testing.T) {
	rep, fired := checkWhileChanging(t, bigFastq(), func(path string) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		f.Write(fastqText("SRR2", 10, "ACGT"))
	})
	if !fired {
		t.Fatal("the file was never changed")
	}
	requireNoCounts(t, "changed during the read", rep)
}
