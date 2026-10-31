package format

import "strings"

// Cell is one value of a table row. Style, when set, is applied after the
// cell is padded, so colour codes never change the alignment.
type Cell struct {
	Text  string
	Right bool
	Style func(string) string
}

func Text(s string) Cell { return Cell{Text: s} }

func Num(s string) Cell { return Cell{Text: s, Right: true} }

func (c Cell) Styled(style func(string) string) Cell {
	c.Style = style
	return c
}

// Table aligns the rows in columns separated by gap spaces. The last column
// is never padded, so no line ends in spaces.
func Table(rows [][]Cell, indent string, gap int) []string {
	var widths []int
	for _, row := range rows {
		for i, c := range row {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], Width(c.Text))
		}
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		var b strings.Builder
		b.WriteString(indent)
		for i, c := range row {
			pad := strings.Repeat(" ", widths[i]-Width(c.Text))
			text := c.Text
			if c.Style != nil {
				text = c.Style(text)
			}
			last := i == len(row)-1
			switch {
			case c.Right:
				b.WriteString(pad + text)
			case last:
				b.WriteString(text)
			default:
				b.WriteString(text + pad)
			}
			if !last {
				b.WriteString(strings.Repeat(" ", gap))
			}
		}
		lines = append(lines, b.String())
	}
	return lines
}
