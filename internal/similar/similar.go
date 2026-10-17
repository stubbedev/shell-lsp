package similar

import (
	"iter"
	"strings"
)

func Closest(name string, candidates iter.Seq[string]) (string, bool) {
	best, bestDistance := "", 0
	for candidate := range candidates {
		distance, ok := Distance(name, candidate)
		if !ok {
			continue
		}
		if best == "" || distance < bestDistance || (distance == bestDistance && candidate < best) {
			best, bestDistance = candidate, distance
		}
	}
	return best, best != ""
}

func Distance(name, candidate string) (int, bool) {
	distance := 1
	if !strings.EqualFold(name, candidate) {
		distance = levenshtein(name, candidate)
	}
	switch {
	case len(candidate) > 7 && distance <= 2, len(candidate) > 3 && distance <= 1:
		return distance, distance > 0
	}
	return 0, false
}

func levenshtein(a, b string) int {
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i := 1; i <= len(a); i++ {
		diagonal := row[0]
		row[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			diagonal, row[j] = row[j], min(row[j]+1, row[j-1]+1, diagonal+cost)
		}
	}
	return row[len(b)]
}
