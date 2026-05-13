package ui

import "strings"

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}

	return prev[lb]
}

func SuggestMatch(input string, candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}

	input = strings.ToLower(input)
	bestDist := len(input) + 1
	bestMatch := ""

	for _, c := range candidates {
		lower := strings.ToLower(c)
		if strings.HasPrefix(lower, input) || strings.HasPrefix(input, lower) {
			d := levenshtein(input, lower)
			if d < bestDist {
				bestDist = d
				bestMatch = c
			}
		} else {
			d := levenshtein(input, lower)
			if d < bestDist {
				bestDist = d
				bestMatch = c
			}
		}
	}

	threshold := len(input) / 2
	if threshold < 2 {
		threshold = 2
	}
	if bestDist <= threshold {
		return bestMatch
	}
	return ""
}
