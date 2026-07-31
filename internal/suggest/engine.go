package suggest

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wertyy111/metuur/internal/history"
	"github.com/wertyy111/metuur/internal/localai"
	"github.com/wertyy111/metuur/specs"
)

type Engine struct {
	history    *history.Store
	localAI    *localai.Model
	root       []specItem
	contexts   map[string][]specItem
	recipes    []recipe
	showHidden bool
}

func New(store *history.Store, showHidden bool) (*Engine, error) {
	data, err := specs.Files.ReadFile("windows.json")
	if err != nil {
		return nil, fmt.Errorf("read embedded specs: %w", err)
	}
	var file specFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse embedded specs: %w", err)
	}
	recipeData, err := specs.Files.ReadFile("recipes.json")
	if err != nil {
		return nil, fmt.Errorf("read embedded recipes: %w", err)
	}
	var recipes recipeFile
	if err := json.Unmarshal(recipeData, &recipes); err != nil {
		return nil, fmt.Errorf("parse embedded recipes: %w", err)
	}
	return &Engine{
		history:    store,
		localAI:    localai.NewMemory(),
		root:       file.Root,
		contexts:   file.Contexts,
		recipes:    recipes.Recipes,
		showHidden: showHidden,
	}, nil
}

func (e *Engine) Suggest(line, cwd string, mode Mode, limit int) []Suggestion {
	if limit < 1 {
		return nil
	}
	if mode == ModeHistory {
		return e.historySuggestions(line, limit)
	}

	parsed := parse(line)
	if recipes := e.recipeSuggestions(line, cwd, parsed); len(recipes) > 0 {
		return rankAndLimit(recipes, limit)
	}
	result := make([]Suggestion, 0, limit*3)
	result = append(result, intentSuggestions(line, cwd)...)
	result = append(result, goStarterSuggestions(cwd, parsed)...)
	result = append(result, e.goRunSuggestions(line, cwd, parsed)...)
	result = append(result, e.goFormatSuggestions(cwd, parsed)...)
	result = append(result, e.goBuildSuggestions(cwd, parsed)...)
	result = append(result, e.workspaceSuggestions(cwd, parsed)...)
	result = append(result, filterWorkspaceSpecs(e.specSuggestions(line, parsed), cwd, parsed)...)
	if !hasWorkspaceTargetContext(parsed) {
		result = append(result, e.fileSuggestions(line, cwd, parsed)...)
	}
	if e.localAI != nil {
		for index := range result {
			result[index].Score += e.localAI.Score(result[index].Insert, cwd)
		}
		for _, prediction := range e.localAI.Predict(line, cwd, 8) {
			result = append(result, Suggestion{
				Label:       compactCommandLabel(prediction.Command),
				Insert:      prediction.Command,
				Description: "локальный AI · знакомая команда в этой папке",
				Kind:        "ai",
				Score:       470 + prediction.Score,
			})
		}
	}

	return rankAndLimit(result, limit)
}

func (e *Engine) SetLocalAI(model *localai.Model) {
	e.localAI = model
}

func (e *Engine) Learn(command, cwd string) {
	if e.localAI != nil {
		e.localAI.Learn(command, cwd)
	}
}

func hasWorkspaceTargetContext(parsed parseResult) bool {
	if _, ok := goRunQuery(parsed); ok {
		return true
	}
	if _, ok := goFormatQuery(parsed); ok {
		return true
	}
	if _, ok := goBuildQuery(parsed); ok {
		return true
	}
	return hasWorkspaceCommandContext(parsed)
}

func filterWorkspaceSpecs(items []Suggestion, cwd string, parsed parseResult) []Suggestion {
	if len(parsed.Tokens) < 2 || !strings.EqualFold(parsed.Tokens[0], "go") {
		return items
	}
	subcommand := strings.ToLower(parsed.Tokens[1])
	_, hasModule := regularFile(filepath.Join(cwd, "go.mod"))
	result := make([]Suggestion, 0, len(items))
	for _, item := range items {
		target := strings.ReplaceAll(strings.TrimSpace(item.Label), `\`, "/")
		switch subcommand {
		case "fmt":
			if !hasModule && (target == "." || target == "./...") {
				continue
			}
		case "run":
			if target == "./..." || (target == "." && (!hasModule || len(runnableGoFiles(cwd)) != 1)) {
				continue
			}
		case "build":
			if !hasModule && (target == "." || target == "./...") {
				continue
			}
		case "test", "vet", "generate", "fix", "list", "install", "doc":
			if !hasModule && (target == "." || target == "./...") {
				continue
			}
		case "mod":
			if !hasModule && len(parsed.Tokens) >= 3 && target != "init" {
				continue
			}
		}
		result = append(result, item)
	}
	return result
}

func (e *Engine) specSuggestions(line string, parsed parseResult) []Suggestion {
	exactContext := strings.ToLower(strings.TrimSpace(line))
	if items, ok := e.contexts[exactContext]; ok && exactContext != "" {
		base := strings.TrimRight(line, " \t\r\n")
		result := make([]Suggestion, 0, len(items))
		for index, item := range items {
			result = append(result, Suggestion{
				Label:       item.Value,
				Insert:      base + " " + item.Value + " ",
				Description: item.Description,
				Kind:        "spec",
				Score:       145 - float64(index)*0.01,
			})
		}
		return result
	}

	completed := parsed.Tokens
	if !parsed.Trailing && len(completed) > 0 {
		completed = completed[:len(completed)-1]
	}

	var items []specItem
	if len(completed) == 0 {
		root := make(map[string]string)
		for _, item := range e.root {
			root[item.Value] = item.Description
		}
		for contextName := range e.contexts {
			fields := strings.Fields(contextName)
			if len(fields) > 0 {
				name := fields[0]
				if _, exists := root[name]; !exists {
					root[name] = "встроенная спецификация"
				}
			}
		}
		items = make([]specItem, 0, len(root))
		for name, description := range root {
			items = append(items, specItem{Value: name, Description: description})
		}
	} else {
		for i := len(completed); i > 0; i-- {
			contextName := strings.ToLower(strings.Join(completed[:i], " "))
			if found, ok := e.contexts[contextName]; ok {
				items = found
				break
			}
		}
	}

	result := make([]Suggestion, 0, len(items))
	for _, item := range items {
		score := matchScore(item.Value, parsed.Current)
		if math.IsInf(score, -1) {
			continue
		}
		if item.Description != "команда из PATH" {
			score += 15
		}
		insert := replaceFromRune(line, parsed.ReplaceStart, item.Value+" ")
		result = append(result, Suggestion{
			Label:       item.Value,
			Insert:      insert,
			Description: item.Description,
			Kind:        "spec",
			Score:       score + 20,
		})
	}
	return result
}

func (e *Engine) goBuildSuggestions(cwd string, parsed parseResult) []Suggestion {
	query, ok := goBuildQuery(parsed)
	if !ok {
		return nil
	}
	targets := discoverGoBuildTargets(cwd)
	result := make([]Suggestion, 0, len(targets))
	for index, target := range targets {
		score := targetScore(target.search, query)
		if math.IsInf(score, -1) {
			continue
		}
		result = append(result, Suggestion{
			Label:       compactCommandLabel(target.command),
			Insert:      target.command,
			Description: target.description,
			Kind:        "build",
			Score:       420 + score - float64(index)*0.01,
		})
	}
	return result
}

func goBuildQuery(parsed parseResult) (string, bool) {
	if len(parsed.Tokens) < 2 || !strings.EqualFold(parsed.Tokens[0], "go") {
		return "", false
	}
	subcommand := strings.ToLower(parsed.Tokens[1])
	if subcommand != "build" {
		if len(parsed.Tokens) == 2 && !parsed.Trailing && strings.HasPrefix("build", subcommand) {
			return "", true
		}
		return "", false
	}
	if len(parsed.Tokens) <= 2 {
		return "", true
	}
	if strings.HasPrefix(parsed.Current, "-") {
		return "", false
	}
	return normalizeWorkspaceQuery(parsed.Current), true
}

func discoverGoBuildTargets(cwd string) []goRunTarget {
	rootMainFiles := runnableGoFiles(cwd)
	_, rootHasModule := regularFile(filepath.Join(cwd, "go.mod"))
	result := make([]goRunTarget, 0, len(rootMainFiles)+5)
	if target, ok := activeGoTarget(cwd, "build"); ok {
		result = append(result, target)
	}

	if rootHasModule {
		if len(rootMainFiles) == 1 {
			result = append(result, goRunTarget{
				command:     "go build .",
				search:      ". " + rootMainFiles[0],
				description: "собрать текущую программу",
			})
		}
		result = append(result, goRunTarget{
			command:     "go build ./...",
			search:      "./... module packages",
			description: "проверить сборку всего Go-модуля",
		})
	} else {
		for _, name := range rootMainFiles {
			path := `.\` + name
			result = append(result, goRunTarget{
				command:     "go build " + quotePowerShellPath(path),
				search:      path + " " + name,
				description: "собрать отдельный main-файл",
			})
		}
	}

	if rootHasModule {
		result = append(result, moduleBuildTargets(cwd, cwd, "")...)
	} else {
		for _, moduleDir := range nestedModules(cwd, 3) {
			relativeModule, err := filepath.Rel(cwd, moduleDir)
			if err != nil {
				continue
			}
			moduleTargets := moduleBuildTargets(cwd, moduleDir, relativeModule)
			if len(moduleTargets) == 0 {
				modulePath := `.\` + filepath.Clean(relativeModule)
				moduleTargets = append(moduleTargets, goRunTarget{
					command:     "go -C " + quotePowerShellPath(modulePath) + " build ./...",
					search:      modulePath + " module ./...",
					description: "проверить сборку найденного Go-модуля",
				})
			}
			result = append(result, moduleTargets...)
		}
	}
	return deduplicateRunTargets(result)
}

func moduleBuildTargets(workspace, moduleDir, relativeModule string) []goRunTarget {
	var result []goRunTarget
	_ = filepath.WalkDir(moduleDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != moduleDir && shouldSkipScanDir(entry.Name()) {
				return filepath.SkipDir
			}
			if path != moduleDir {
				if _, nested := regularFile(filepath.Join(path, "go.mod")); nested {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if filepath.Ext(entry.Name()) != ".go" {
			return nil
		}
		dir := filepath.Dir(path)
		files := runnableGoFiles(dir)
		if len(files) != 1 {
			return nil
		}
		packagePath, relErr := filepath.Rel(moduleDir, dir)
		if relErr != nil {
			return nil
		}
		target := "."
		if packagePath != "." {
			target = `.\` + filepath.Clean(packagePath)
		}
		command := "go build " + quotePowerShellPath(target)
		if relativeModule != "" {
			modulePath := `.\` + filepath.Clean(relativeModule)
			command = "go -C " + quotePowerShellPath(modulePath) + " build " + quotePowerShellPath(target)
		}
		displayPath, _ := filepath.Rel(workspace, dir)
		result = append(result, goRunTarget{
			command:     command,
			search:      command + " " + displayPath + " " + files[0],
			description: "собрать main package · открытая папка",
		})
		return filepath.SkipDir
	})
	return result
}

func (e *Engine) goFormatSuggestions(cwd string, parsed parseResult) []Suggestion {
	query, ok := goFormatQuery(parsed)
	if !ok {
		return nil
	}
	targets := discoverGoFormatTargets(cwd)
	result := make([]Suggestion, 0, len(targets))
	for index, target := range targets {
		score := targetScore(target.search, query)
		if math.IsInf(score, -1) {
			continue
		}
		result = append(result, Suggestion{
			Label:       compactCommandLabel(target.command),
			Insert:      target.command,
			Description: target.description,
			Kind:        "format",
			Score:       420 + score - float64(index)*0.01,
		})
	}
	return result
}

func goFormatQuery(parsed parseResult) (string, bool) {
	if len(parsed.Tokens) == 0 {
		return "", false
	}
	first := strings.ToLower(parsed.Tokens[0])
	if first == "gofmt" {
		if len(parsed.Tokens) == 1 {
			return "", true
		}
		if strings.HasPrefix(parsed.Current, "-") {
			return "", false
		}
		return normalizeWorkspaceQuery(parsed.Current), true
	}
	if first != "go" || len(parsed.Tokens) < 2 {
		return "", false
	}
	subcommand := strings.ToLower(parsed.Tokens[1])
	if subcommand != "fmt" {
		if len(parsed.Tokens) == 2 && !parsed.Trailing && strings.HasPrefix("fmt", subcommand) {
			return "", true
		}
		return "", false
	}
	if len(parsed.Tokens) <= 2 {
		return "", true
	}
	if strings.HasPrefix(parsed.Current, "-") {
		return "", false
	}
	return normalizeWorkspaceQuery(parsed.Current), true
}

func normalizeWorkspaceQuery(query string) string {
	switch strings.TrimSpace(strings.ReplaceAll(query, `\`, "/")) {
	case ".", "./", "./...":
		return ""
	default:
		return query
	}
}

func discoverGoFormatTargets(cwd string) []goRunTarget {
	files := goFiles(cwd)
	_, rootHasModule := regularFile(filepath.Join(cwd, "go.mod"))
	result := make([]goRunTarget, 0, len(files)+4)
	if target, ok := activeGoTarget(cwd, "format"); ok {
		result = append(result, target)
	}

	if rootHasModule {
		result = append(result,
			goRunTarget{
				command:     "go fmt ./...",
				search:      ". ./... module",
				description: "весь Go-модуль · открытая папка",
			},
			goRunTarget{
				command:     "go fmt .",
				search:      ". package",
				description: "текущий пакет · открытая папка",
			},
		)
	}
	for _, name := range files {
		path := `.\` + name
		result = append(result, goRunTarget{
			command:     "gofmt -w " + quotePowerShellPath(path),
			search:      path + " " + name,
			description: "форматировать файл · открытая папка",
		})
	}

	if !rootHasModule {
		for _, moduleDir := range nestedModules(cwd, 3) {
			relativeModule, err := filepath.Rel(cwd, moduleDir)
			if err != nil {
				continue
			}
			modulePath := `.\` + filepath.Clean(relativeModule)
			result = append(result, goRunTarget{
				command:     "go -C " + quotePowerShellPath(modulePath) + " fmt ./...",
				search:      modulePath + " module ./...",
				description: "форматировать найденный Go-модуль",
			})
		}
	}
	return deduplicateRunTargets(result)
}

func (e *Engine) goRunSuggestions(line, cwd string, parsed parseResult) []Suggestion {
	query, ok := goRunQuery(parsed)
	if !ok {
		return nil
	}

	targets := discoverGoRunTargets(cwd)
	result := make([]Suggestion, 0, len(targets))
	for index, target := range targets {
		score := targetScore(target.search, query)
		if math.IsInf(score, -1) {
			continue
		}
		result = append(result, Suggestion{
			Label:       compactCommandLabel(target.command),
			Insert:      target.command,
			Description: target.description,
			Kind:        "run",
			Score:       420 + score - float64(index)*0.01,
		})
	}
	return result
}

func goRunQuery(parsed parseResult) (string, bool) {
	if len(parsed.Tokens) < 2 || !strings.EqualFold(parsed.Tokens[0], "go") {
		return "", false
	}
	subcommand := strings.ToLower(parsed.Tokens[1])
	if subcommand != "run" {
		if len(parsed.Tokens) == 2 && !parsed.Trailing && strings.HasPrefix("run", subcommand) {
			return "", true
		}
		return "", false
	}
	if len(parsed.Tokens) <= 2 {
		return "", true
	}
	if strings.HasPrefix(parsed.Current, "-") {
		return "", false
	}
	return parsed.Current, true
}

type goRunTarget struct {
	command     string
	search      string
	description string
}

func discoverGoRunTargets(cwd string) []goRunTarget {
	rootFiles := runnableGoFiles(cwd)
	_, rootHasModule := regularFile(filepath.Join(cwd, "go.mod"))
	result := make([]goRunTarget, 0, len(rootFiles)+4)
	if target, ok := activeGoTarget(cwd, "run"); ok {
		result = append(result, target)
	}

	if rootHasModule && len(rootFiles) == 1 {
		result = append(result, goRunTarget{
			command:     "go run .",
			search:      ". " + rootFiles[0],
			description: "текущий main package · открытая папка",
		})
	}
	for _, name := range rootFiles {
		path := `.\` + name
		result = append(result, goRunTarget{
			command:     "go run " + quotePowerShellPath(path),
			search:      path + " " + name,
			description: "файл с func main · открытая папка",
		})
	}

	if rootHasModule {
		result = append(result, moduleMainPackages(cwd, cwd, "")...)
	} else {
		for _, moduleDir := range nestedModules(cwd, 3) {
			relativeModule, err := filepath.Rel(cwd, moduleDir)
			if err != nil {
				continue
			}
			result = append(result, moduleMainPackages(cwd, moduleDir, relativeModule)...)
		}
	}
	return deduplicateRunTargets(result)
}

func moduleMainPackages(workspace, moduleDir, relativeModule string) []goRunTarget {
	var result []goRunTarget
	_ = filepath.WalkDir(moduleDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != moduleDir && shouldSkipScanDir(entry.Name()) {
				return filepath.SkipDir
			}
			if path != moduleDir {
				if _, nested := regularFile(filepath.Join(path, "go.mod")); nested {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.EqualFold(entry.Name(), "go.mod") && filepath.Ext(entry.Name()) != ".go" {
			return nil
		}
		dir := filepath.Dir(path)
		files := runnableGoFiles(dir)
		if len(files) != 1 {
			return nil
		}

		packagePath, relErr := filepath.Rel(moduleDir, dir)
		if relErr != nil {
			return nil
		}
		var command string
		if relativeModule == "" {
			target := "."
			if packagePath != "." {
				target = `.\` + filepath.Clean(packagePath)
			}
			command = "go run " + quotePowerShellPath(target)
		} else {
			modulePath := `.\` + filepath.Clean(relativeModule)
			target := "."
			if packagePath != "." {
				target = `.\` + filepath.Clean(packagePath)
			}
			command = "go -C " + quotePowerShellPath(modulePath) + " run " + quotePowerShellPath(target)
		}
		displayPath, _ := filepath.Rel(workspace, dir)
		result = append(result, goRunTarget{
			command:     command,
			search:      command + " " + displayPath + " " + files[0],
			description: "main package · найден в открытой папке",
		})
		return filepath.SkipDir
	})
	return result
}

func nestedModules(root string, maxDepth int) []string {
	var result []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if path != root && shouldSkipScanDir(entry.Name()) {
			return filepath.SkipDir
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return filepath.SkipDir
		}
		if relative != "." && len(strings.Split(relative, string(filepath.Separator))) > maxDepth {
			return filepath.SkipDir
		}
		if path != root {
			if _, ok := regularFile(filepath.Join(path, "go.mod")); ok {
				result = append(result, path)
				return filepath.SkipDir
			}
		}
		return nil
	})
	sort.Strings(result)
	return result
}

func runnableGoFiles(dir string) []string {
	entries := goFiles(dir)
	var result []string
	for _, name := range entries {
		path := filepath.Join(dir, name)
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if parseErr != nil || file.Name == nil || file.Name.Name != "main" {
			continue
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && function.Name != nil && function.Name.Name == "main" {
				result = append(result, name)
				break
			}
		}
	}
	return result
}

func goFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var result []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(name), ".go") ||
			strings.HasSuffix(strings.ToLower(name), "_test.go") {
			continue
		}
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func regularFile(path string) (os.FileInfo, bool) {
	info, err := os.Stat(path)
	return info, err == nil && info.Mode().IsRegular()
}

func shouldSkipScanDir(name string) bool {
	switch strings.ToLower(name) {
	case ".git", ".idea", ".vscode", "bin", "dist", "node_modules", "tmp", "vendor":
		return true
	default:
		return strings.HasPrefix(name, ".")
	}
}

func deduplicateRunTargets(items []goRunTarget) []goRunTarget {
	seen := make(map[string]bool)
	result := make([]goRunTarget, 0, len(items))
	for _, item := range items {
		key := strings.ToLower(item.command)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, item)
	}
	return result
}

func (e *Engine) historySuggestions(query string, limit int) []Suggestion {
	matches := e.history.Search(query, limit*4)
	result := make([]Suggestion, 0, len(matches))
	for _, match := range matches {
		if !e.isGoCommand(match.Command) {
			continue
		}
		score := matchScore(match.Command, query)
		if math.IsInf(score, -1) {
			continue
		}
		result = append(result, Suggestion{
			Label:       match.Command,
			Insert:      match.Command,
			Description: fmt.Sprintf("история · %d×", match.Frequency),
			Kind:        "history",
			Score:       score + math.Log1p(float64(match.Frequency))*8 + float64(match.Recency)*0.0001,
		})
	}
	return rankAndLimit(result, limit)
}

func (e *Engine) fileSuggestions(line, cwd string, parsed parseResult) []Suggestion {
	if len(parsed.Tokens) == 0 {
		return nil
	}
	command := strings.ToLower(parsed.Tokens[0])
	if command == "go" && len(parsed.Tokens) == 1 {
		return nil
	}
	fileCommand := map[string]bool{
		"cd": true, "set-location": true, "sl": true,
		"code": true, "cat": true, "get-content": true, "gc": true,
		"remove-item": true, "rm": true, "copy-item": true, "cp": true,
		"move-item": true, "mv": true, "go": true,
		"gofmt": true, "goimports": true, "gopls": true, "dlv": true,
		"golangci-lint": true, "staticcheck": true, "gotestsum": true,
		"goreleaser": true, "goctl": true, "air": true, "mockgen": true,
		"govulncheck": true, "stringer": true,
	}
	fragment := parsed.Current
	if !fileCommand[command] &&
		!strings.ContainsAny(fragment, `./\`) &&
		!(parsed.Trailing && len(parsed.Tokens) > 0) {
		return nil
	}
	if strings.HasPrefix(fragment, "-") || strings.Contains(fragment, "$") {
		return nil
	}

	displayDir, leaf := splitPathFragment(fragment)
	searchDir := displayDir
	if searchDir == "" {
		searchDir = "."
	}
	if strings.HasPrefix(searchDir, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			searchDir = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(searchDir, "~"), `\`))
		}
	}
	if !filepath.IsAbs(searchDir) {
		searchDir = filepath.Join(cwd, searchDir)
	}

	entries, err := os.ReadDir(searchDir)
	if err != nil {
		return nil
	}
	onlyDirs := command == "cd" || command == "set-location" || command == "sl"
	result := make([]Suggestion, 0, len(entries))
	for _, entry := range entries {
		if onlyDirs && !entry.IsDir() {
			continue
		}
		if !e.showHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		score := matchScore(entry.Name(), leaf)
		if math.IsInf(score, -1) {
			continue
		}
		value := displayDir + entry.Name()
		description := "файл"
		trailing := " "
		if entry.IsDir() {
			value += `\`
			description = "папка"
			trailing = ""
			score += 5
		}
		insertValue := quotePowerShellPath(value) + trailing
		result = append(result, Suggestion{
			Label:       value,
			Insert:      replaceFromRune(line, parsed.ReplaceStart, insertValue),
			Description: description,
			Kind:        "file",
			Score:       score + 8,
		})
	}
	return result
}

func rankAndLimit(items []Suggestion, limit int) []Suggestion {
	best := make(map[string]Suggestion)
	for _, item := range items {
		key := strings.ToLower(item.Insert)
		if previous, ok := best[key]; !ok || item.Score > previous.Score {
			best[key] = item
		}
	}
	result := make([]Suggestion, 0, len(best))
	for _, item := range best {
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return strings.ToLower(result[i].Label) < strings.ToLower(result[j].Label)
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func (e *Engine) isGoCommand(commandLine string) bool {
	fields := strings.Fields(strings.TrimSpace(commandLine))
	if len(fields) == 0 {
		return false
	}
	command := strings.ToLower(strings.TrimSuffix(fields[0], ".exe"))
	for _, item := range e.root {
		if command == strings.ToLower(strings.TrimSuffix(item.Value, ".exe")) {
			return true
		}
	}
	return false
}

func splitPathFragment(fragment string) (dir, leaf string) {
	fragment = strings.ReplaceAll(fragment, "/", `\`)
	index := strings.LastIndex(fragment, `\`)
	if index < 0 {
		return "", fragment
	}
	return fragment[:index+1], fragment[index+1:]
}

func quotePowerShellPath(path string) string {
	if !strings.ContainsAny(path, " \t'") {
		return path
	}
	return "'" + strings.ReplaceAll(path, "'", "''") + "'"
}
