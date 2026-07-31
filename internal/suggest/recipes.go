package suggest

import (
	"strings"
)

func (e *Engine) recipeSuggestions(line, cwd string, parsed parseResult) []Suggestion {
	if len(parsed.Tokens) == 0 {
		return nil
	}
	first := strings.ToLower(parsed.Tokens[0])
	if first == "go" && len(parsed.Tokens) == 1 {
		return nil
	}
	if first != "go" && len(first) < 2 {
		return nil
	}
	if hasWorkspaceTargetContext(parsed) {
		return nil
	}

	var result []Suggestion
	for _, item := range e.recipes {
		words := strings.Fields(strings.ToLower(item.Phrase))
		if !recipeMatches(parsed.Tokens, words) {
			continue
		}
		if recipeExact(parsed.Tokens, words) {
			continue
		}

		contextual := e.contextSuggestions(item.Phrase, cwd)
		if len(contextual) > 0 {
			for index := range contextual {
				contextual[index].Score += 120
			}
			result = append(result, contextual...)
			continue
		}
		if item.Workspace {
			continue
		}
		result = append(result, Suggestion{
			Label:       item.Phrase,
			Insert:      item.Phrase,
			Description: item.Description,
			Kind:        "intent",
			Score:       380 - float64(len(words)-len(parsed.Tokens))*2,
		})
	}
	return result
}

func recipeExact(input, recipeWords []string) bool {
	if len(input) != len(recipeWords) {
		return false
	}
	for index, token := range input {
		if !strings.EqualFold(token, recipeWords[index]) {
			return false
		}
	}
	return true
}

func recipeMatches(input, recipeWords []string) bool {
	if len(input) == 0 || len(input) > len(recipeWords) {
		return false
	}
	if recipeWords[0] == "go" && !strings.EqualFold(input[0], "go") {
		return false
	}
	for index, token := range input {
		if !strings.HasPrefix(recipeWords[index], strings.ToLower(token)) {
			return false
		}
	}
	return true
}

func (e *Engine) contextSuggestions(phrase, cwd string) []Suggestion {
	parsed := parse(phrase)
	var result []Suggestion
	result = append(result, e.goRunSuggestions(phrase, cwd, parsed)...)
	result = append(result, e.goFormatSuggestions(cwd, parsed)...)
	result = append(result, e.goBuildSuggestions(cwd, parsed)...)
	result = append(result, e.workspaceSuggestions(cwd, parsed)...)
	return result
}
