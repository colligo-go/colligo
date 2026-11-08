package verifier

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/colligo-go/colligo/identity"
	"github.com/colligo-go/colligo/secret"
)

type cancellingSource struct {
	*os.File
	cancel func()
	reads  int
}

func (c *cancellingSource) Read(p []byte) (int, error) {
	c.reads++
	if c.reads == 2 {
		c.cancel()
	}
	return c.File.Read(p)
}

func openCancelling(t *testing.T, data []byte) (*cancellingSource, context.Context, secret.Name) {
	t.Helper()
	path := t.TempDir() + "/sample.fastq"
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &cancellingSource{File: f, cancel: cancel}, ctx, secret.NewName(path)
}

func TestCheckCancelledDuringTheReadReturnsTheCancellationAndNoReport(t *testing.T) {
	src, ctx, name := openCancelling(t, bigFastq())
	rep, err := checkSource(ctx, newReport(name, StatusVerified), src, Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v, want context.Canceled", err)
	}
	if rep != nil {
		t.Fatalf("a cancelled check produced a report with status %s", rep.Status)
	}
}

func TestIdentifyCancelledDuringTheReadReturnsTheCancellationAndNoAnswer(t *testing.T) {
	src, ctx, name := openCancelling(t, bigFastq())
	answer, err := identifySource(ctx, &Identification{Name: name}, src, identity.Table{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v, want context.Canceled", err)
	}
	if answer != nil {
		t.Fatalf("a cancelled identify produced an answer with status %s", answer.Status)
	}
}

func TestCheckAllAndIdentifyAllStopOnACancelledContext(t *testing.T) {
	path := t.TempDir() + "/sample.fastq"
	if err := os.WriteFile(path, fastqText("SRR1", 10, "ACGT"), 0o644); err != nil {
		t.Fatal(err)
	}
	names := []secret.Name{secret.NewName(path), secret.NewName(path)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if rep, err := CheckAll(ctx, names, Options{}); !errors.Is(err, context.Canceled) || len(rep.Files) != 0 {
		t.Errorf("CheckAll: %d files, err %v", len(rep.Files), err)
	}
	if rep, err := IdentifyAll(ctx, names, identity.Table{}); !errors.Is(err, context.Canceled) || len(rep.Files) != 0 {
		t.Errorf("IdentifyAll: %d files, err %v", len(rep.Files), err)
	}
}
