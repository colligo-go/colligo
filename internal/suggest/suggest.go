// Package suggest finds the known word nearest to a mistyped one.
package suggest

// Distance is the Levenshtein distance between a and b, counted in bytes.
func Distance(a, b string) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}

// Closest returns the candidate nearest to word, and false when none is near
// enough to be what the person meant. A tie goes to the earlier candidate, so
// the answer does not depend on map order when the caller sorts its list.
func Closest(word string, candidates []string) (string, bool) {
	best, bestDistance := "", -1
	for _, c := range candidates {
		d := Distance(word, c)
		if bestDistance < 0 || d < bestDistance {
			best, bestDistance = c, d
		}
	}
	if bestDistance < 0 || bestDistance > max(2, len(best)/3) {
		return "", false
	}
	return best, true
}
