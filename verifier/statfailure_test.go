package verifier

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/secret"
)

const statCanary = "STAT-PATH-CANARY"

type statFailing struct {
	io.Reader
	info      os.FileInfo
	okStats   int
	statCalls int
	reads     int
}

func (s *statFailing) Read(p []byte) (int, error) {
	s.reads++
	return s.Reader.Read(p)
}

func (s *statFailing) Stat() (os.FileInfo, error) {
	s.statCalls++
	if s.statCalls <= s.okStats {
		return s.info, nil
	}
	return nil, &os.PathError{Op: "stat", Path: "/data/" + statCanary + "/sample.fastq", Err: syscall.EIO}
}

func checkWithFailingStat(t *testing.T, okStats int) (*FileReport, *statFailing) {
	t.Helper()
	path := t.TempDir() + "/sample.fastq"
	data := fastqText("SRR1", 10, "ACGT")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	src := &statFailing{Reader: bytes.NewReader(data), info: info, okStats: okStats}
	rep, err := checkSource(context.Background(), newReport(secret.NewName(path), StatusVerified), src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return rep, src
}

func requireUnreadableByStat(t *testing.T, rep *FileReport) {
	t.Helper()
	if rep.Status != StatusUnreadable {
		t.Fatalf("status %s", rep.Status)
	}
	if rep.BLAKE3 != "" || rep.MD5 != "" || rep.SHA256 != "" {
		t.Errorf("hashes %q %q %q on a file whose stat failed", rep.BLAKE3, rep.MD5, rep.SHA256)
	}
	if got := failed(rep); len(got) != 1 || got[0] != diagnoses.ReadFailed {
		t.Fatalf("failed %v, want only read_failed", got)
	}
	all := ""
	for _, a := range rep.Assertions {
		all += a.Observed + "|" + a.Declared + "|" + a.Observation
	}
	if !strings.Contains(all, "stat: input/output error") {
		t.Errorf("assertion %q does not say that the stat failed", all)
	}
	if strings.Contains(all, statCanary) {
		t.Errorf("assertion leaks the path of the stat error: %q", all)
	}
	if code := (Report{Files: []*FileReport{rep}}).ExitCode(false); code != ExitFatal {
		t.Errorf("exit code %d", code)
	}
}

func TestStatFailingBeforeTheReadIsUnreadableAndReadsNothing(t *testing.T) {
	rep, src := checkWithFailingStat(t, 0)
	requireUnreadableByStat(t, rep)
	if src.reads != 0 {
		t.Errorf("%d reads on a file whose stat failed", src.reads)
	}
}

func TestStatFailingAfterTheReadIsUnreadableNotVerified(t *testing.T) {
	rep, src := checkWithFailingStat(t, 1)
	requireUnreadableByStat(t, rep)
	if src.reads == 0 {
		t.Error("the file was never read: the second stat was not the one that failed")
	}
}
