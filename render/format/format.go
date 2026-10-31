// Package format turns numbers, sizes, durations and text into the strings
// a person reads. It knows nothing about reports or colours.
package format

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func Number(n int64) string {
	magnitude := uint64(n)
	if n < 0 {
		magnitude = -magnitude
	}
	digits := strconv.FormatUint(magnitude, 10)
	var b strings.Builder
	if n < 0 {
		b.WriteByte('-')
	}
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	return b.String()
}

func Size(n int64) string {
	units := []string{"kB", "MB", "GB", "TB", "PB"}
	if n < 1000 {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	unit := -1
	for value >= 1000 && unit < len(units)-1 {
		value /= 1000
		unit++
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

func Duration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%d ms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1f s", d.Seconds())
	case d < time.Hour:
		whole := int(d.Round(time.Second).Seconds())
		return fmt.Sprintf("%d m %02d s", whole/60, whole%60)
	}
	whole := int(d.Round(time.Minute).Minutes())
	return fmt.Sprintf("%d h %02d m", whole/60, whole%60)
}

// Difference is how far observed is from declared, in percent of declared,
// with a sign. A difference too small to show at one decimal is never
// printed as zero: only equal values are.
func Difference(observed, declared int64) string {
	switch {
	case observed == declared:
		return "0%"
	case declared == 0:
		return "n/a"
	}
	percent := float64(observed-declared) / float64(declared) * 100
	sign := "+"
	if percent < 0 {
		sign = "-"
	}
	if math.Abs(percent) < 0.05 {
		return sign + "<0.1%"
	}
	return fmt.Sprintf("%s%.1f%%", sign, math.Abs(percent))
}

func PerRead(bases, reads int64) string {
	if reads <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", float64(bases)/float64(reads))
}

func Plural(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%s %s", Number(int64(n)), plural)
}

// Wrap breaks text into lines of at most width runes, never inside a word.
// A width of zero or less leaves the text on one line.
func Wrap(text string, width int) []string {
	words := strings.Fields(text)
	if width <= 0 || len(words) == 0 {
		return []string{strings.Join(words, " ")}
	}
	var lines []string
	line := ""
	for _, word := range words {
		switch {
		case line == "":
			line = word
		case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	return append(lines, line)
}

func Width(s string) int { return utf8.RuneCountInString(s) }
