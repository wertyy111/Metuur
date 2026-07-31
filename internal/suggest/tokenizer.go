package suggest

import "unicode"

func parse(line string) parseResult {
	runes := []rune(line)
	var (
		tokens     []string
		current    []rune
		tokenStart = -1
		quote      rune
		escaped    bool
	)

	for i, r := range runes {
		if escaped {
			current = append(current, r)
			escaped = false
			continue
		}
		if r == '`' && quote != '\'' {
			escaped = true
			continue
		}
		if r == '"' || r == '\'' {
			if quote == 0 {
				quote = r
				if tokenStart < 0 {
					tokenStart = i
				}
				continue
			}
			if quote == r {
				quote = 0
				continue
			}
		}
		if unicode.IsSpace(r) && quote == 0 {
			if tokenStart >= 0 {
				tokens = append(tokens, string(current))
				current = current[:0]
				tokenStart = -1
			}
			continue
		}
		if tokenStart < 0 {
			tokenStart = i
		}
		current = append(current, r)
	}

	trailing := len(runes) > 0 && unicode.IsSpace(runes[len(runes)-1]) && quote == 0
	if tokenStart >= 0 {
		tokens = append(tokens, string(current))
	}
	if trailing || tokenStart < 0 {
		tokenStart = len(runes)
		current = nil
	}

	return parseResult{
		Tokens:       tokens,
		Current:      string(current),
		ReplaceStart: tokenStart,
		Trailing:     trailing,
	}
}

func replaceFromRune(line string, start int, value string) string {
	runes := []rune(line)
	if start < 0 || start > len(runes) {
		start = len(runes)
	}
	return string(runes[:start]) + value
}
