package suggest

import "strings"

// CanonicalCommandKey normalizes harmless Windows spelling differences so
// deterministic and learned suggestions for the same command are not shown
// twice. It intentionally does not treat a package target (.) as equivalent
// to a source file: those commands can compile different sets of files.
func CanonicalCommandKey(command string) string {
	tokens := parse(strings.TrimSpace(command)).Tokens
	for index, token := range tokens {
		token = strings.ReplaceAll(token, `\`, "/")
		if token != "./..." {
			token = strings.TrimPrefix(token, "./")
		}
		tokens[index] = strings.ToLower(token)
	}
	return strings.Join(tokens, " ")
}

func CommandsEquivalent(left, right string) bool {
	leftKey := CanonicalCommandKey(left)
	return leftKey != "" && leftKey == CanonicalCommandKey(right)
}
