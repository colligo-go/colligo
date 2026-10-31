package verifier

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/colligo-go/colligo/facts"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/secret"
)

const (
	StatusVerified   = "verified"
	StatusValid      = "valid"
	StatusQuick      = "quick"
	StatusInvalid    = "invalid"
	StatusUnreadable = "unreadable"

	ExitOK          = 0
	ExitUsage       = 1
	ExitFatal       = 2
	ExitIncomplete  = 3
	ExitInterrupted = 4
	ExitQuick       = 5
)

const SchemaVersion = 2

type FileReport struct {
	Name       secret.Name       `json:"path"`
	Status     string            `json:"status"`
	Container  string            `json:"container,omitempty"`
	Content    string            `json:"content,omitempty"`
	Size       int64             `json:"size"`
	BLAKE3     string            `json:"blake3_root,omitempty"`
	MD5        string            `json:"md5,omitempty"`
	SHA256     string            `json:"sha256,omitempty"`
	Assertions []model.Assertion `json:"assertions"`
	Result     facts.Result      `json:"-"`

	passStatus string
}

// String keeps fmt on the redacting path: a pointer printed under an
// unsupported verb is dumped without calling the methods of its fields.
func (r *FileReport) String() string { return fmt.Sprintf("%s: %s", r.Name, r.Status) }

func (r *FileReport) Worst() model.Severity { return worstOf(r.Assertions) }

// Records returns the reads and bases counted in the single read of the
// file. ok is false when the file has no FASTQ records to count.
func (r *FileReport) Records() (reads, bases int64, ok bool) {
	rec, ok := recordsOf(r.Result)
	return rec.Records, rec.Bases, ok
}

func worstOf(as []model.Assertion) model.Severity {
	var worst model.Severity
	for _, a := range as {
		if !a.Passed && !a.Skipped && a.Severity.Rank() > worst.Rank() {
			worst = a.Severity
		}
	}
	return worst
}

func (r *FileReport) finalize() {
	if r.Status == StatusUnreadable {
		return
	}
	if worst := r.Worst(); worst == model.Fatal || worst == model.Blocking {
		r.Status = StatusInvalid
		return
	}
	r.Status = r.passStatus
}

// Report is what a check found, with no presentation in it. The fields
// tagged "-" never reach the JSON document: they exist for the renderers.
type Report struct {
	Files           []*FileReport `json:"files"`
	Skipped         []string      `json:"skipped,omitempty"`
	SkippedLinks    []string      `json:"skipped_directory_links,omitempty"`
	Source          *SourceReport `json:"source,omitempty"`
	Quick           bool          `json:"-"`
	FromSamplesheet bool          `json:"-"`
	Elapsed         time.Duration `json:"-"`
}

func (r Report) ExitCode(permissive bool) int {
	var worst model.Severity
	for _, f := range r.Files {
		if w := f.Worst(); w.Rank() > worst.Rank() {
			worst = w
		}
	}
	if w := r.Source.Worst(); w.Rank() > worst.Rank() {
		worst = w
	}
	switch worst {
	case model.Fatal:
		return ExitFatal
	case model.Blocking:
		if !permissive {
			return ExitIncomplete
		}
	}
	if r.Quick {
		return ExitQuick
	}
	return ExitOK
}

func (r Report) WriteChecksums(sha256sum, b3sum io.Writer) error {
	for _, f := range r.Files {
		if f.SHA256 == "" || f.BLAKE3 == "" {
			continue
		}
		if _, err := io.WriteString(sha256sum, checksumLine(f.SHA256, f.Name.String())); err != nil {
			return err
		}
		if _, err := io.WriteString(b3sum, checksumLine(f.BLAKE3, f.Name.String())); err != nil {
			return err
		}
	}
	return nil
}

var checksumEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`)

func checksumLine(sum, shown string) string {
	if strings.ContainsAny(shown, "\\\n") {
		return `\` + sum + "  " + checksumEscaper.Replace(shown) + "\n"
	}
	return sum + "  " + shown + "\n"
}
