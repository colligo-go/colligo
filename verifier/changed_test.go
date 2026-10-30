package verifier

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/secret"
)

type changingSource struct {
	*os.File
	total  int
	at     int
	change func()
	fired  bool
}

func (c *changingSource) Read(p []byte) (int, error) {
	n, err := c.File.Read(p)
	c.total += n
	if !c.fired && c.total >= c.at {
		c.fired = true
		c.change()
	}
	return n, err
}

func checkWhileChanging(t *testing.T, data []byte, change func(path string)) (*FileReport, bool) {
	t.Helper()
	path := t.TempDir() + "/sample.fastq"
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	src := &changingSource{File: f, at: len(data) / 2, change: func() { change(path) }}
	rep, err := checkSource(context.Background(), newReport(secret.NewName(path), StatusVerified), src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return rep, src.fired
}

func requireChanged(t *testing.T, rep *FileReport) {
	t.Helper()
	if got := failed(rep); len(got) != 1 || got[0] != diagnoses.ChangedDuringRead {
		t.Fatalf("failed %v, want only changed_during_read", got)
	}
	if rep.Status != StatusInvalid || rep.BLAKE3 != "" || rep.MD5 != "" || rep.SHA256 != "" {
		t.Fatalf("status %s with hashes %q %q %q", rep.Status, rep.BLAKE3, rep.MD5, rep.SHA256)
	}
	if code := (Report{Files: []*FileReport{rep}}).ExitCode(false); code != ExitIncomplete {
		t.Fatalf("exit code %d", code)
	}
	for _, a := range rep.Assertions {
		if a.ID != diagnoses.ChangedDuringRead && !a.Skipped {
			t.Errorf("%s was evaluated on bytes that changed", a.ID)
		}
	}
}

func bigFastq() []byte { return fastqText("SRR1", 80_000, "ACGTACGTACGTACGT") }

func TestFileOverwrittenInPlaceWithTheSameSizeDuringTheReadIsInvalidated(t *testing.T) {
	data := bigFastq()
	rep, fired := checkWhileChanging(t, data, func(path string) {
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		if _, err := f.WriteAt(bytes.Repeat([]byte("N"), 16), int64(len(data)-64)); err != nil {
			t.Error(err)
		}
	})
	if !fired {
		t.Fatal("the file was never changed")
	}
	requireChanged(t, rep)
}

func TestFileGrownDuringTheReadIsInvalidated(t *testing.T) {
	rep, fired := checkWhileChanging(t, bigFastq(), func(path string) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		if _, err := f.Write(fastqText("SRR2", 10, "ACGT")); err != nil {
			t.Error(err)
		}
	})
	if !fired {
		t.Fatal("the file was never changed")
	}
	requireChanged(t, rep)
}

func TestFileTruncatedDuringTheReadIsInvalidated(t *testing.T) {
	data := bigFastq()
	rep, fired := checkWhileChanging(t, data, func(path string) {
		if err := os.Truncate(path, int64(len(data)*3/4)); err != nil {
			t.Error(err)
		}
	})
	if !fired {
		t.Fatal("the file was never changed")
	}
	requireChanged(t, rep)
}

func requireChangedBecause(t *testing.T, rep *FileReport, observed string) {
	t.Helper()
	requireChanged(t, rep)
	for _, a := range rep.Assertions {
		if a.ID == diagnoses.ChangedDuringRead && a.Observed != observed {
			t.Errorf("observed %q, want %q", a.Observed, observed)
		}
	}
}

func TestFileReplacedByRenameDuringTheReadIsInvalidated(t *testing.T) {
	data := bigFastq()
	rep, fired := checkWhileChanging(t, data, func(path string) {
		replacement := path + ".new"
		if err := os.WriteFile(replacement, data, 0o644); err != nil {
			t.Error(err)
			return
		}
		if err := os.Rename(replacement, path); err != nil {
			t.Error(err)
		}
	})
	if !fired {
		t.Fatal("the file was never replaced")
	}
	requireChangedBecause(t, rep, "replaced")
}

func TestFileRemovedDuringTheReadIsInvalidated(t *testing.T) {
	rep, fired := checkWhileChanging(t, bigFastq(), func(path string) {
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
	})
	if !fired {
		t.Fatal("the file was never removed")
	}
	requireChangedBecause(t, rep, "removed")
}

func TestFileThatDoesNotChangeIsNeverInvalidated(t *testing.T) {
	for _, data := range [][]byte{nil, fastqText("SRR1", 3, "ACGT"), bigFastq()} {
		rep, fired := checkWhileChanging(t, data, func(string) {})
		if !fired && len(data) > 0 {
			t.Fatal("the hook never ran")
		}
		if has(failed(rep), diagnoses.ChangedDuringRead) || (len(data) > 0 && rep.Status != StatusVerified) {
			t.Errorf("%d bytes: status %s failed %v", len(data), rep.Status, failed(rep))
		}
	}
}
