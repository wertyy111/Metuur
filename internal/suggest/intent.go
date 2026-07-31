package suggest

import (
	"strings"
	"unicode"
)

type commandIntent struct {
	name     string
	keywords []string
}

var commandIntents = []commandIntent{
	{name: "run", keywords: []string{"запуст", "выполн", "старт", "run", "start", "execute", "launch"}},
	{name: "build", keywords: []string{"собер", "сборк", "компилир", "build", "compile"}},
	{name: "format", keywords: []string{"формат", "выровн", "gofmt", "format", "fmt"}},
	{name: "test", keywords: []string{"тест", "tests", "test"}},
	{name: "vet", keywords: []string{"проверь код", "проверить код", "ошибк", "анализ кода", "vet", "lint", "check code"}},
	{name: "tidy", keywords: []string{"зависимост", "модули", "mod tidy", "dependency", "dependencies", "tidy"}},
	{name: "clean", keywords: []string{"очист", "кэш", "мусор", "clean", "cache"}},
	{name: "generate", keywords: []string{"сгенер", "генерац", "generate"}},
	{name: "debug", keywords: []string{"отлад", "дебаг", "debug", "delve"}},
	{name: "vuln", keywords: []string{"уязв", "безопасност", "vuln", "security"}},
}

// intentSuggestions turns a short human request into commands for the actual
// workspace. It is deliberately local and deterministic: no prompt or file is
// sent outside the computer.
func intentSuggestions(line, cwd string) []Suggestion {
	normalized := normalizeIntentText(line)
	if normalized == "" || looksLikeCommand(normalized) {
		return nil
	}
	intent, confidence, ok := recognizeIntent(normalized)
	if !ok {
		return nil
	}
	targets := targetsForIntent(intent, cwd)
	result := make([]Suggestion, 0, min(5, len(targets)))
	for index, target := range targets {
		if index == 5 {
			break
		}
		result = append(result, Suggestion{
			Label:       compactCommandLabel(target.command),
			Insert:      target.command,
			Description: "AI: " + intentDescription(intent) + " · " + target.description,
			Kind:        "intent",
			Score:       700 + confidence - float64(index),
		})
	}
	return result
}

func recognizeIntent(text string) (string, float64, bool) {
	candidates := []string{text}
	if converted := englishKeysToRussian(text); converted != text {
		candidates = append(candidates, converted)
	}
	bestName := ""
	bestScore := 0.0
	for _, candidate := range candidates {
		for _, intent := range commandIntents {
			for _, keyword := range intent.keywords {
				score := intentMatchScore(candidate, keyword)
				if score > bestScore {
					bestName, bestScore = intent.name, score
				}
			}
		}
	}
	return bestName, bestScore, bestScore >= 34
}

func intentMatchScore(text, keyword string) float64 {
	if containsIntentKeyword(text, keyword) {
		return 90 + float64(len([]rune(keyword)))
	}
	best := 0.0
	keywordRunes := []rune(keyword)
	for _, token := range strings.Fields(text) {
		tokenRunes := []rune(token)
		if len(tokenRunes) < 3 || len(keywordRunes) < 3 {
			continue
		}
		distance := editDistance(tokenRunes, keywordRunes)
		longest := max(len(tokenRunes), len(keywordRunes))
		similarity := 1 - float64(distance)/float64(longest)
		if similarity >= 0.62 && similarity*70 > best {
			best = similarity * 70
		}
	}
	return best
}

func containsIntentKeyword(text, keyword string) bool {
	if strings.Contains(keyword, " ") || strings.IndexFunc(keyword, func(r rune) bool { return r > unicode.MaxASCII }) >= 0 {
		return strings.Contains(text, keyword)
	}
	for _, token := range strings.Fields(text) {
		if token == keyword {
			return true
		}
	}
	return false
}

func targetsForIntent(intent, cwd string) []goRunTarget {
	switch intent {
	case "run":
		return discoverGoRunTargets(cwd)
	case "build":
		return discoverGoBuildTargets(cwd)
	case "format":
		return discoverGoFormatTargets(cwd)
	case "test":
		return activeFirstWorkspaceTargets(cwd, "test")
	case "vet":
		return activeFirstWorkspaceTargets(cwd, "vet")
	case "tidy":
		return discoverGoModTargets(cwd, "tidy")
	case "generate":
		return activeFirstWorkspaceTargets(cwd, "generate")
	case "clean":
		return []goRunTarget{{command: "go clean -cache -testcache", description: "очистить кэш сборки и тестов"}}
	case "debug":
		if target, ok := activeGoTarget(cwd, "run"); ok {
			command := strings.Replace(target.command, "go run ", "dlv debug ", 1)
			return []goRunTarget{{command: command, description: "отладка активной программы"}}
		}
		return []goRunTarget{{command: "dlv debug .", description: "отладка текущего main package"}}
	case "vuln":
		if len(workspaceModules(cwd)) > 0 {
			return []goRunTarget{{command: "govulncheck ./...", description: "проверить модуль на известные уязвимости"}}
		}
	}
	return nil
}

func intentDescription(intent string) string {
	switch intent {
	case "run":
		return "запустить программу"
	case "build":
		return "собрать проект"
	case "format":
		return "отформатировать код"
	case "test":
		return "запустить тесты"
	case "vet":
		return "проверить код"
	case "tidy":
		return "привести зависимости в порядок"
	case "clean":
		return "очистить кэш"
	case "generate":
		return "запустить генерацию"
	case "debug":
		return "начать отладку"
	case "vuln":
		return "проверить уязвимости"
	default:
		return intent
	}
}

func normalizeIntentText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Join(strings.FieldsFunc(value, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_')
	}), " ")
}

func looksLikeCommand(value string) bool {
	first := strings.Fields(value)
	if len(first) == 0 {
		return false
	}
	switch first[0] {
	case "go", "gofmt", "goimports", "gopls", "dlv", "staticcheck", "golangci-lint", "gotestsum", "govulncheck", "air":
		return true
	default:
		return false
	}
}

func englishKeysToRussian(value string) string {
	const english = "qwertyuiop[]asdfghjkl;'zxcvbnm,."
	const russian = "йцукенгшщзхъфывапролджэячсмитьбю"
	translation := make(map[rune]rune, len([]rune(english)))
	englishRunes, russianRunes := []rune(english), []rune(russian)
	for index, source := range englishRunes {
		translation[source] = russianRunes[index]
	}
	return strings.Map(func(r rune) rune {
		if converted, ok := translation[r]; ok {
			return converted
		}
		return r
	}, value)
}

func editDistance(left, right []rune) int {
	previous := make([]int, len(right)+1)
	for index := range previous {
		previous[index] = index
	}
	for i, leftRune := range left {
		current := make([]int, len(right)+1)
		current[0] = i + 1
		for j, rightRune := range right {
			cost := 1
			if leftRune == rightRune {
				cost = 0
			}
			current[j+1] = min(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = current
	}
	return previous[len(right)]
}
