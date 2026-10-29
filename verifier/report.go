package verifier

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

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
)

const SchemaVersion = 1

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

func (r *FileReport) Worst() model.Severity {
	var worst model.Severity
	for _, a := range r.Assertions {
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

type Report struct {
	Files        []*FileReport `json:"files"`
	Skipped      []string      `json:"skipped,omitempty"`
	SkippedLinks []string      `json:"skipped_directory_links,omitempty"`
}

func (r Report) ExitCode(permissive bool) int {
	var worst model.Severity
	for _, f := range r.Files {
		if w := f.Worst(); w.Rank() > worst.Rank() {
			worst = w
		}
	}
	switch worst {
	case model.Fatal:
		return ExitFatal
	case model.Blocking:
		if permissive {
			return ExitOK
		}
		return ExitIncomplete
	}
	return ExitOK
}

type jsonReport struct {
	SchemaVersion int           `json:"schema_version"`
	Files         []*FileReport `json:"files"`
	Skipped       []string      `json:"skipped,omitempty"`
	SkippedLinks  []string      `json:"skipped_directory_links,omitempty"`
	ExitCode      int           `json:"exit_code"`
}

func (r Report) WriteJSON(w io.Writer, exitCode int) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(jsonReport{SchemaVersion: SchemaVersion, Files: r.Files, Skipped: r.Skipped, SkippedLinks: r.SkippedLinks, ExitCode: exitCode})
}

func (r Report) WriteText(w io.Writer) {
	counts := map[string]int{}
	for _, f := range r.Files {
		counts[f.Status]++
		fmt.Fprintf(w, "%s\n  %s\n", f.Name, summaryLine(f))
		for _, a := range f.Assertions {
			if a.Passed || a.Skipped {
				continue
			}
			fmt.Fprintf(w, "  [%s] %s (%s): %s\n", upper(a.Severity), a.ID, a.Mode, a.Observation)
			fmt.Fprintf(w, "      probable cause: %s\n      suggested action: %s\n", a.Cause, a.Action)
		}
		for _, a := range f.Assertions {
			if a.Skipped {
				fmt.Fprintf(w, "  (%s not evaluated: %s failed, and everything below it depends on it)\n", a.ID, a.SkippedBy)
			}
		}
	}
	for _, s := range r.Skipped {
		fmt.Fprintf(w, "%s\n  skipped: not a recognised data file\n", s)
	}
	for _, s := range r.SkippedLinks {
		fmt.Fprintf(w, "%s\n  skipped: symbolic link to a directory, not followed\n", s)
	}
	fmt.Fprintf(w, "\n%d file(s): %d verified, %d valid, %d quick, %d invalid, %d unreadable\n",
		len(r.Files), counts[StatusVerified], counts[StatusValid], counts[StatusQuick], counts[StatusInvalid], counts[StatusUnreadable])
}

func upper(s model.Severity) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

func summaryLine(f *FileReport) string {
	kind := f.Content
	if kind == "" {
		kind = "unknown"
	}
	if f.Container != "" && f.Container != "none" {
		kind += " in " + f.Container
	}
	line := fmt.Sprintf("%s: %s, %d bytes", f.Status, kind, f.Size)
	if f.Status == StatusQuick {
		line += " (content was not read in full)"
	}
	return line
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
