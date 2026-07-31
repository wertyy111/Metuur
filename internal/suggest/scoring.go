package suggest

import (
	"math"
	"strings"
)

func matchScore(candidate, query string) float64 {
	candidate = strings.ToLower(candidate)
	query = strings.ToLower(query)
	if query == "" {
		return 25
	}
	if candidate == query {
		return 150
	}
	if strings.HasPrefix(candidate, query) {
		return 110 - float64(len(candidate)-len(query))*0.15
	}
	if index := strings.Index(candidate, query); index >= 0 {
		return 75 - float64(index)
	}

	pos := -1
	gaps := 0
	for _, q := range query {
		next := strings.IndexRune(candidate[pos+1:], q)
		if next < 0 {
			return math.Inf(-1)
		}
		next += pos + 1
		if pos >= 0 {
			gaps += next - pos - 1
		}
		pos = next
	}
	return 45 - float64(gaps)
}

func targetScore(candidate, query string) float64 {
	query = strings.ToLower(strings.TrimSpace(strings.Trim(query, `"'`)))
	candidate = strings.ToLower(candidate)
	if query == "" {
		return 25
	}
	if len([]rune(query)) == 1 {
		return matchScore(candidate, query)
	}
	if !strings.Contains(candidate, query) {
		return math.Inf(-1)
	}
	return matchScore(candidate, query)
}
