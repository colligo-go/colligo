package text

import (
	"fmt"
	"slices"
	"strings"

	"github.com/colligo-go/colligo/render/theme"
)

func (v *view) allIssues() []*issueGroup {
	groups := append([]*issueGroup{}, v.general...)
	for _, rv := range v.runs {
		groups = append(groups, rv.issues...)
	}
	slices.SortStableFunc(groups, func(a, b *issueGroup) int { return b.severity.Rank() - a.severity.Rank() })
	return groups
}

func todoBlock(v *view) []string {
	var actions []string
	for _, g := range v.allIssues() {
		if !slices.Contains(actions, g.action) {
			actions = append(actions, g.action)
		}
	}
	if len(actions) == 0 {
		return nil
	}
	lines := []string{v.theme.Apply(theme.Strong, "What to do")}
	for i, a := range actions {
		prefix := fmt.Sprintf("  %d. ", i+1)
		for j, w := range v.wrapped(a, len(prefix)) {
			lead := prefix
			if j > 0 {
				lead = strings.Repeat(" ", len(prefix))
			}
			lines = append(lines, lead+w)
		}
	}
	return lines
}
