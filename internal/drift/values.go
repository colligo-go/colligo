package drift

import (
	"fmt"
	"strconv"

	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/source"
)

// DiffValues lists, one line each, how the values Colligo uses changed between
// what the adapter made of the recorded response and of today's: the runs,
// their layout, status and declared counts, and the name, size and MD5 of each
// file. These are public metadata of the recorded runs, the same values the
// fixtures already hold, so they are reported as they are.
func DiffValues(baseline, live source.Fetched) []string {
	var lines []string
	liveRuns := map[string]model.Sample{}
	for _, s := range live.Samples {
		liveRuns[s.Accession] = s
	}
	seen := map[string]bool{}
	for _, was := range baseline.Samples {
		seen[was.Accession] = true
		now, ok := liveRuns[was.Accession]
		if !ok {
			lines = append(lines, fmt.Sprintf("run %s: no longer returned", was.Accession))
			continue
		}
		lines = append(lines, diffRun(was, now)...)
	}
	for _, now := range live.Samples {
		if !seen[now.Accession] {
			lines = append(lines, fmt.Sprintf("run %s: newly returned", now.Accession))
		}
	}
	return lines
}

func diffRun(was, now model.Sample) []string {
	var lines []string
	field := func(label, before, after string) {
		if before != after {
			lines = append(lines, fmt.Sprintf("run %s: %s was %s, is now %s", was.Accession, label, shown(before), shown(after)))
		}
	}
	field("status", was.SourceStatus, now.SourceStatus)
	field("library_layout", was.Layout, now.Layout)
	field("read_count", count(was.Declared.ReadCount), count(now.Declared.ReadCount))
	field("base_count", count(was.Declared.BaseCount), count(now.Declared.BaseCount))

	liveFiles := map[string]model.Artifact{}
	for _, a := range now.Artifacts {
		liveFiles[a.Name.String()] = a
	}
	seen := map[string]bool{}
	for _, a := range was.Artifacts {
		name := a.Name.String()
		seen[name] = true
		b, ok := liveFiles[name]
		if !ok {
			lines = append(lines, fmt.Sprintf("run %s: file %s no longer listed", was.Accession, name))
			continue
		}
		field("size of "+name, strconv.FormatInt(a.Size, 10), strconv.FormatInt(b.Size, 10))
		field("md5 of "+name, a.MD5, b.MD5)
	}
	for _, b := range now.Artifacts {
		if name := b.Name.String(); !seen[name] {
			lines = append(lines, fmt.Sprintf("run %s: file %s newly listed", was.Accession, name))
		}
	}
	return lines
}

func count(n *int64) string {
	if n == nil {
		return ""
	}
	return strconv.FormatInt(*n, 10)
}

func shown(v string) string {
	if v == "" {
		return "empty"
	}
	return v
}
