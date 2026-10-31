package request_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/request"
	"github.com/colligo-go/colligo/secret"
)

func errorsAs(err error, target any) bool { return errors.As(err, target) }

func gz(data []byte) []byte { return append([]byte{0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 0, 255}, data...) }

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "s_1.fastq.gz", gz(nil))
	put(t, dir, "genome.fa", []byte(">chr1\nACGT\n"))
	put(t, dir, "notes.txt", []byte("hello"))
	put(t, dir, "page.gtf.gz", []byte("<html>"))
	put(t, dir, ".hidden.fastq", fastqRead)
	put(t, dir, "sub/deep.fastq", fastqRead)

	found, skipped, _, err := request.Discover(context.Background(), secret.NewName(dir), false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range found {
		got = append(got, filepath.Base(n.String()))
	}
	if want := "genome.fa,page.gtf.gz,s_1.fastq.gz"; strings.Join(got, ",") != want {
		t.Fatalf("got %v want %s", got, want)
	}
	if len(skipped) != 1 || filepath.Base(skipped[0]) != "notes.txt" {
		t.Fatalf("skipped %v", skipped)
	}
	deep, _, _, err := request.Discover(context.Background(), secret.NewName(dir), true)
	if err != nil || len(deep) != 4 {
		t.Fatalf("recursive: %d %v", len(deep), err)
	}
}

func TestPathsRecordWhetherAFileOrAFolderGaveTheFile(t *testing.T) {
	dir := t.TempDir()
	alone := put(t, dir, "alone.fastq", fastqRead)
	put(t, dir, "folder/inside.fastq", fastqRead)
	req, err := request.Resolve(context.Background(), request.Inputs{Paths: names(alone, filepath.Join(dir, "folder"))})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(bases(req.Files), ","); got != "alone.fastq,inside.fastq" {
		t.Fatalf("files %s", got)
	}
	if req.Files[0].Origins[0].Kind != request.FromArgument || req.Files[1].Origins[0].Kind != request.FromFolder {
		t.Errorf("origins %+v %+v", req.Files[0].Origins, req.Files[1].Origins)
	}
}

func TestAMissingPathIsAnErrorThatNamesIt(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nowhere.fastq")
	_, err := request.Resolve(context.Background(), request.Inputs{Paths: names(missing)})
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), missing) {
		t.Fatalf("got %v", err)
	}
}

func TestNoPathIsAUsageErrorThatSuggestsTheCurrentFolder(t *testing.T) {
	_, err := request.Resolve(context.Background(), request.Inputs{})
	if msg := usageMessage(t, err); !strings.Contains(msg, "colligo check .") {
		t.Errorf("message %q does not suggest colligo check .", msg)
	}
}

func TestAFileURLIsReadAsAPath(t *testing.T) {
	dir := t.TempDir()
	real := put(t, dir, "a b.fastq", fastqRead)
	name, err := request.NameFromArgument("file://" + strings.ReplaceAll(real, " ", "%20"))
	if err != nil {
		t.Fatal(err)
	}
	if name.String() != real {
		t.Errorf("got %s want %s", name, real)
	}
	local, err := request.NameFromArgument("file://localhost" + real[:0] + strings.ReplaceAll(real, " ", "%20"))
	if err != nil || local.String() != real {
		t.Errorf("localhost: %v %v", local, err)
	}
	plain, _ := request.NameFromArgument("reads/a.fastq")
	if plain.String() != "reads/a.fastq" {
		t.Errorf("a plain path changed: %s", plain)
	}
}

func TestAFileURLThatIsNotLocalOrNotAPathIsRefused(t *testing.T) {
	for _, bad := range []string{"file://server/share/a.fastq", "file://", "file:///a.fastq?x=1", "file:///a.fastq#frag"} {
		_, err := request.NameFromArgument(bad)
		var u *request.UsageError
		if !errors.As(err, &u) {
			t.Errorf("%q: want a usage error, got %v", bad, err)
		}
	}
}

func TestIncompatibleFormsAreUsageErrors(t *testing.T) {
	sheet, list := secret.NewName("sheet.csv"), secret.NewName("list.txt")
	cases := map[string]request.Inputs{
		"samplesheet with a path":    {Samplesheet: &sheet, Paths: names("a.fastq")},
		"samplesheet with a list":    {Samplesheet: &sheet, ListFile: &list},
		"samplesheet with a source":  {Samplesheet: &sheet, Source: "ena:DRR021374"},
		"samplesheet with recursive": {Samplesheet: &sheet, Recursive: true},
		"two lists":                  {ListFile: &list, ListStdin: strings.NewReader("")},
		"recursive with only a list": {ListFile: &list, Recursive: true},
		"recursive with only stdin":  {ListStdin: strings.NewReader(""), Recursive: true},
	}
	for name, in := range cases {
		_, err := request.Resolve(context.Background(), in)
		var u *request.UsageError
		if !errors.As(err, &u) {
			t.Errorf("%s: want a usage error, got %v", name, err)
		}
	}
}
