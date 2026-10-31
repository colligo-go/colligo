package text

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/colligo-go/colligo/render/format"
	"github.com/colligo-go/colligo/render/theme"
	"github.com/colligo-go/colligo/verifier"
)

func (v *view) wrapped(text string, prefix int) []string {
	width := 0
	if v.opts.Width > 0 {
		width = max(v.opts.Width-prefix, 20)
	}
	return format.Wrap(text, width)
}

func footerBlock(v *view) []string {
	r := v.report
	counts := map[string]int{}
	var size int64
	for _, f := range r.Files {
		counts[f.Status]++
		size += f.Size
	}
	var parts []string
	files := format.Plural(len(r.Files), "file", "files")
	var byStatus []string
	for _, status := range []string{verifier.StatusVerified, verifier.StatusValid, verifier.StatusQuick, verifier.StatusInvalid, verifier.StatusUnreadable} {
		if counts[status] > 0 {
			byStatus = append(byStatus, fmt.Sprintf("%d %s", counts[status], status))
		}
	}
	if len(byStatus) > 0 {
		files += ": " + strings.Join(byStatus, ", ")
	}
	parts = append(parts, files)
	if r.Source != nil && len(r.Source.Runs) > 0 {
		parts = append(parts, v.runCounts())
	}
	if n := len(v.visibleSkipped()); n > 0 {
		parts = append(parts, format.Plural(n, "other file skipped", "other files skipped")+" (--verbose lists them)")
	}
	if n := len(r.SkippedLinks); n > 0 {
		parts = append(parts, format.Plural(n, "directory link", "directory links")+" not followed (--verbose lists them)")
	}
	parts = append(parts, format.Size(size))
	if r.Elapsed > 0 {
		parts = append(parts, format.Duration(r.Elapsed))
	}
	return []string{v.theme.Apply(theme.Dim, strings.Join(parts, " · "))}
}

func (v *view) runCounts() string {
	failedRuns, advisory := 0, 0
	for _, rv := range v.runs {
		switch w := rv.worst(); {
		case w.Rank() >= 2:
			failedRuns++
		case w.Rank() == 1:
			advisory++
		}
	}
	out := format.Plural(len(v.runs), "run", "runs")
	var detail []string
	if failedRuns > 0 {
		detail = append(detail, fmt.Sprintf("%d fail", failedRuns))
	}
	if advisory > 0 {
		detail = append(detail, fmt.Sprintf("%d with a note", advisory))
	}
	if ok := len(v.runs) - failedRuns - advisory; ok > 0 {
		detail = append(detail, fmt.Sprintf("%d match", ok))
	}
	return out + ": " + strings.Join(detail, ", ")
}

func isOwnOutput(name string) bool {
	base := filepath.Base(name)
	return strings.HasSuffix(base, ".colligo-cert.json") || base == "colligo.sha256" || base == "colligo.b3"
}

func (v *view) visibleSkipped() []string {
	var out []string
	for _, s := range v.report.Skipped {
		if !isOwnOutput(s) {
			out = append(out, s)
		}
	}
	return out
}
