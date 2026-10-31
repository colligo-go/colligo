package request_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/request"
	"github.com/colligo-go/colligo/secret"
)

func resolveList(t *testing.T, list string) (request.Request, error) {
	t.Helper()
	name := secret.NewName(list)
	return request.Resolve(context.Background(), request.Inputs{ListFile: &name})
}

func TestAListSkipsBlankLinesAndCommentsAndReadsPathsRelativeToItself(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "data/a.fastq", fastqRead)
	put(t, dir, "data/b.fastq", fastqRead)
	abs := put(t, dir, "elsewhere/c.fastq", fastqRead)
	list := put(t, dir, "lists/files.txt", []byte("\xEF\xBB\xBF# the run\n\n../data/a.fastq\r\n   \n  ../data/b.fastq  \n"+abs+"\nfile://"+abs+"\n"))
	req, err := resolveList(t, list)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(bases(req.Files), ","); got != "a.fastq,b.fastq,c.fastq" {
		t.Fatalf("files %s", got)
	}
	if lines := req.Files[0].Lines(request.FromListLine); len(lines) != 1 || lines[0] != 3 {
		t.Errorf("a.fastq lines %v, want [3]: lines are counted as an editor shows them", lines)
	}
	if lines := req.Files[2].Lines(request.FromListLine); len(lines) != 2 || lines[0] != 6 || lines[1] != 7 {
		t.Errorf("c.fastq lines %v, want [6 7]", lines)
	}
	if want := filepath.Join(dir, "data", "a.fastq"); req.Files[0].Name.String() != want {
		t.Errorf("path %s, want %s", req.Files[0].Name, want)
	}
}

func TestAPathListedTwiceIsOneFileWithBothLines(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "a.fastq", fastqRead)
	list := put(t, dir, "list.txt", []byte("a.fastq\n./a.fastq\n"))
	req, err := resolveList(t, list)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Files) != 1 || len(req.Files[0].Origins) != 2 {
		t.Fatalf("files %+v", req.Files)
	}
}

func TestAListedFileThatDoesNotExistStaysInTheRequestWithItsLine(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "here.fastq", fastqRead)
	list := put(t, dir, "list.txt", []byte("here.fastq\ngone.fastq\n"))
	req, err := resolveList(t, list)
	if err != nil {
		t.Fatalf("a missing listed file is reported by the check, not refused here: %v", err)
	}
	if len(req.Files) != 2 || req.Files[1].Lines(request.FromListLine)[0] != 2 {
		t.Fatalf("files %+v", req.Files)
	}
}

func TestAListLineThatNamesAFolderIsAUsageErrorWithItsLine(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "folder/a.fastq", fastqRead)
	list := put(t, dir, "list.txt", []byte("# header\nfolder\n"))
	_, err := resolveList(t, list)
	if msg := usageMessage(t, err); !strings.Contains(msg, "line 2") || !strings.Contains(msg, "folder") {
		t.Errorf("message %q", msg)
	}
}

func TestADataFileGivenAsAListIsRefusedBeforeAnythingIsGuessed(t *testing.T) {
	dir := t.TempDir()
	list := put(t, dir, "reads.fastq.gz", []byte("a.fastq\n\x1f\x8b\x00\x00binary\n"))
	_, err := resolveList(t, list)
	if msg := usageMessage(t, err); !strings.Contains(msg, "line 2") || !strings.Contains(msg, "binary") {
		t.Errorf("message %q", msg)
	}
}

func TestAListThatDoesNotExistIsAUsageError(t *testing.T) {
	_, err := resolveList(t, filepath.Join(t.TempDir(), "no-list.txt"))
	if msg := usageMessage(t, err); !strings.Contains(msg, "--files-from") {
		t.Errorf("message %q", msg)
	}
}

func TestAListFromStandardInputIsRelativeToTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "reads/a.fastq", fastqRead)
	t.Chdir(dir)
	req, err := request.Resolve(context.Background(), request.Inputs{ListStdin: strings.NewReader("reads/a.fastq\nmissing.fastq\n")})
	if err != nil {
		t.Fatal(err)
	}
	if req.Files[0].Name.String() != filepath.Join("reads", "a.fastq") {
		t.Errorf("path %s", req.Files[0].Name)
	}
	if _, err := os.Stat(filepath.Join(dir, "reads", "a.fastq")); err != nil {
		t.Fatal(err)
	}
}

func TestAListAndArgumentsAreJoinedArgumentsFirst(t *testing.T) {
	dir := t.TempDir()
	first := put(t, dir, "first.fastq", fastqRead)
	put(t, dir, "second.fastq", fastqRead)
	list := secret.NewName(put(t, dir, "list.txt", []byte("second.fastq\n")))
	req, err := request.Resolve(context.Background(), request.Inputs{Paths: names(first), ListFile: &list})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(bases(req.Files), ","); got != "first.fastq,second.fastq" {
		t.Errorf("files %s", got)
	}
}

func TestAnEmptyListIsAUsageErrorNotAnEmptySuccess(t *testing.T) {
	list := put(t, t.TempDir(), "list.txt", []byte("# nothing yet\n\n"))
	_, err := resolveList(t, list)
	if msg := usageMessage(t, err); !strings.Contains(msg, "names no file") {
		t.Errorf("message %q", msg)
	}
	_, err = request.Resolve(context.Background(), request.Inputs{ListStdin: strings.NewReader("")})
	usageMessage(t, err)
}
