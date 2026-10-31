// Package drift compares a recorded source response with the response the
// source gives today: its structure, what the adapter makes of it, and the
// values Colligo uses.
package drift

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var (
	integerPattern = regexp.MustCompile(`^[0-9]+$`)
	md5Pattern     = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// Shape is the structure of a tab-separated response, without its values.
type Shape struct {
	Tabular bool
	Header  []string
	Ragged  bool
	Classes map[string][]string
}

// ShapeOf reads the structure of a tab-separated body. A body whose first line
// has no tab is not tabular and has no columns.
func ShapeOf(body []byte) Shape {
	lines := bytes.Split(body, []byte("\n"))
	header := strings.TrimRight(string(lines[0]), "\r")
	if !strings.Contains(header, "\t") {
		return Shape{}
	}
	shape := Shape{Tabular: true, Header: strings.Split(header, "\t"), Classes: map[string][]string{}}
	for _, name := range shape.Header {
		shape.Classes[name] = nil
	}
	for _, line := range lines[1:] {
		text := strings.TrimRight(string(line), "\r")
		if text == "" {
			continue
		}
		cells := strings.Split(text, "\t")
		if len(cells) != len(shape.Header) {
			shape.Ragged = true
			continue
		}
		for i, cell := range cells {
			name := shape.Header[i]
			class := classOf(cell)
			if !slices.Contains(shape.Classes[name], class) {
				shape.Classes[name] = append(shape.Classes[name], class)
				slices.Sort(shape.Classes[name])
			}
		}
	}
	return shape
}

func classOf(cell string) string {
	elements := strings.Split(cell, ";")
	seen := map[string]bool{}
	for _, element := range elements {
		seen[elementClass(element)] = true
	}
	if len(seen) > 1 {
		return "mixed-list"
	}
	class := elementClass(elements[0])
	if len(elements) > 1 {
		return class + "-list"
	}
	return class
}

func elementClass(element string) string {
	switch {
	case element == "":
		return "empty"
	case integerPattern.MatchString(element):
		return "integer"
	case md5Pattern.MatchString(element):
		return "md5"
	case strings.Contains(element, "/"):
		return "path"
	default:
		return "word"
	}
}

// Diff lists, one line each, how live differs structurally from baseline.
// drift holds what changes what Colligo reads; notes holds what does not, a
// column that moved or one that Colligo never reads, because the adapter
// reads the columns by name.
func Diff(baseline, live Shape) (drift, notes []string) {
	if baseline.Tabular != live.Tabular {
		return []string{fmt.Sprintf("table: tabular was %t, is now %t", baseline.Tabular, live.Tabular)}, nil
	}
	var lines []string
	for _, name := range baseline.Header {
		if !slices.Contains(live.Header, name) {
			lines = append(lines, "column disappeared: "+name)
		}
	}
	for _, name := range live.Header {
		if !slices.Contains(baseline.Header, name) {
			notes = append(notes, "column appeared: "+name)
		}
	}
	for _, name := range baseline.Header {
		was, now := slices.Index(baseline.Header, name), slices.Index(live.Header, name)
		if now >= 0 && was != now {
			notes = append(notes, fmt.Sprintf("column moved: %s from position %d to %d", name, was+1, now+1))
		}
	}
	for _, name := range baseline.Header {
		if !slices.Contains(live.Header, name) {
			continue
		}
		if !slices.Equal(baseline.Classes[name], live.Classes[name]) {
			lines = append(lines, fmt.Sprintf("column %s: cell classes were [%s], are now [%s]", name, strings.Join(baseline.Classes[name], " "), strings.Join(live.Classes[name], " ")))
		}
	}
	if baseline.Ragged != live.Ragged {
		lines = append(lines, fmt.Sprintf("rows: ragged was %t, is now %t", baseline.Ragged, live.Ragged))
	}
	return lines, notes
}
