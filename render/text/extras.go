package text

import (
	"fmt"
	"strings"

	"github.com/colligo-go/colligo/render/theme"
)

func skippedBlock(v *view) []string {
	if !v.opts.Verbose {
		return nil
	}
	var lines []string
	for _, s := range v.report.Skipped {
		lines = append(lines, fmt.Sprintf("  %s  %s", v.theme.Apply(theme.Dim, v.theme.NotChecked+" skipped"), s+": not a recognised data file"))
	}
	for _, s := range v.report.SkippedLinks {
		lines = append(lines, fmt.Sprintf("  %s  %s", v.theme.Apply(theme.Dim, v.theme.NotChecked+" skipped"), s+": symbolic link to a directory, not followed"))
	}
	if len(lines) == 0 {
		return nil
	}
	return append([]string{v.theme.Apply(theme.Strong, "Skipped")}, lines...)
}

func passedBlock(v *view) []string {
	if !v.opts.Verbose {
		return nil
	}
	lines := []string{v.theme.Apply(theme.Strong, "Assertions")}
	for _, f := range v.report.Files {
		lines = append(lines, "  "+v.short[f.Name.String()])
		for _, a := range f.Assertions {
			switch {
			case a.Skipped:
				lines = append(lines, fmt.Sprintf("    %s %s  skipped (%s)", v.theme.NotChecked, a.ID, strings.ReplaceAll(a.SkippedBy, "_", " ")))
			case a.Passed:
				lines = append(lines, fmt.Sprintf("    %s %s  %s", v.theme.Verified, a.ID, v.theme.Apply(theme.Dim, a.Observed)))
			default:
				lines = append(lines, fmt.Sprintf("    %s %s  %s", v.theme.Failed, a.ID, a.Severity))
			}
		}
	}
	if len(lines) == 1 {
		return nil
	}
	return lines
}

func explainBlock(v *view) []string {
	seen := map[string]bool{}
	var ids []string
	for _, g := range v.allIssues() {
		if !seen[g.id] {
			seen[g.id] = true
			ids = append(ids, g.id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	lines := []string{v.theme.Apply(theme.Strong, "Explain")}
	for _, id := range ids {
		lines = append(lines, "  colligo explain "+id)
	}
	return lines
}
