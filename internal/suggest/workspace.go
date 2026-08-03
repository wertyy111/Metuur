package suggest

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var workspaceGoSubcommands = []string{
	"clean",
	"test",
	"vet",
	"generate",
	"get",
	"fix",
	"list",
	"install",
	"doc",
}

func goStarterSuggestions(cwd string, parsed parseResult) []Suggestion {
	if len(parsed.Tokens) != 1 || !strings.EqualFold(parsed.Tokens[0], "go") {
		return nil
	}
	type starter struct {
		targets []goRunTarget
		kind    string
	}
	starters := []starter{
		{targets: discoverGoRunTargets(cwd), kind: "run"},
		{targets: discoverGoBuildTargets(cwd), kind: "build"},
		{targets: discoverGoFormatTargets(cwd), kind: "format"},
		{targets: discoverWorkspaceGoTargets(cwd, "test", ""), kind: "workspace"},
		{targets: activeFirstWorkspaceTargets(cwd, "vet"), kind: "workspace"},
	}
	result := make([]Suggestion, 0, len(starters))
	for index, item := range starters {
		if len(item.targets) == 0 {
			continue
		}
		target := item.targets[0]
		result = append(result, Suggestion{
			Label:       compactCommandLabel(target.command),
			Insert:      target.command,
			Description: structuredTargetDescription(strings.ToUpper(item.kind), target),
			Kind:        item.kind,
			Score:       600 + target.scoreBoost - float64(index),
		})
	}
	return result
}

func activeFirstWorkspaceTargets(cwd, command string) []goRunTarget {
	targets := discoverWorkspaceGoTargets(cwd, command, "")
	if target, ok := activeGoTarget(cwd, command); ok {
		targets = append([]goRunTarget{target}, targets...)
	}
	return deduplicateRunTargets(targets)
}

func (e *Engine) workspaceSuggestions(cwd string, parsed parseResult) []Suggestion {
	if command, query, ok := workspaceGoCommand(parsed); ok {
		return workspaceTargetsToSuggestions(discoverWorkspaceGoTargets(cwd, command, query), query)
	}
	if subcommand, ok := goModSubcommand(parsed); ok {
		return workspaceTargetsToSuggestions(discoverGoModTargets(cwd, subcommand), "")
	}
	if subcommand, ok := goWorkSubcommand(parsed); ok {
		return workspaceTargetsToSuggestions(discoverGoWorkTargets(cwd, subcommand), "")
	}
	if targets, query, ok := delveTargets(cwd, parsed); ok {
		return workspaceTargetsToSuggestions(targets, query)
	}
	if targets, ok := moduleToolTargets(cwd, parsed); ok {
		return workspaceTargetsToSuggestions(targets, "")
	}
	if targets, query, ok := fileToolTargets(cwd, parsed); ok {
		return workspaceTargetsToSuggestions(targets, query)
	}
	return nil
}

func workspaceTargetsToSuggestions(targets []goRunTarget, query string) []Suggestion {
	result := make([]Suggestion, 0, len(targets))
	query = strings.Trim(query, `"'`)
	for index, target := range targets {
		score := targetScore(target.search, query)
		if score < -1e100 {
			continue
		}
		result = append(result, Suggestion{
			Label:       compactCommandLabel(target.command),
			Insert:      target.command,
			Description: structuredTargetDescription("КОМАНДА", target),
			Kind:        "workspace",
			Score:       410 + score + target.scoreBoost - float64(index)*0.01,
		})
	}
	return result
}

func compactCommandLabel(command string) string {
	fields := strings.Fields(command)
	if len(fields) >= 4 && strings.EqualFold(fields[0], "go") && strings.EqualFold(fields[1], "-C") {
		module := strings.Trim(fields[2], `"'`)
		return strings.Join(fields[3:], " ") + " · " + filepath.Base(filepath.Clean(module))
	}
	if len(fields) >= 4 && strings.EqualFold(fields[0], "push-location") {
		module := strings.Trim(fields[1], `"'`)
		start := strings.Index(command, "{ ")
		end := strings.LastIndex(command, " } finally")
		if start >= 0 && end > start {
			inner := strings.TrimSpace(command[start+2 : end])
			return inner + " · " + filepath.Base(filepath.Clean(module))
		}
	}
	return command
}

func workspaceGoCommand(parsed parseResult) (command, query string, ok bool) {
	if len(parsed.Tokens) < 2 || !strings.EqualFold(parsed.Tokens[0], "go") {
		return "", "", false
	}
	command, ok = uniqueCommandPrefix(strings.ToLower(parsed.Tokens[1]), workspaceGoSubcommands)
	if !ok {
		return "", "", false
	}
	if len(parsed.Tokens) <= 2 {
		return command, "", true
	}
	if strings.HasPrefix(parsed.Current, "-") {
		return "", "", false
	}
	return command, normalizeWorkspaceQuery(parsed.Current), true
}

func goModSubcommand(parsed parseResult) (string, bool) {
	if len(parsed.Tokens) < 3 || !strings.EqualFold(parsed.Tokens[0], "go") ||
		!strings.EqualFold(parsed.Tokens[1], "mod") {
		return "", false
	}
	return uniqueCommandPrefix(strings.ToLower(parsed.Tokens[2]), []string{
		"download", "edit", "graph", "init", "tidy", "vendor", "verify", "why",
	})
}

func goWorkSubcommand(parsed parseResult) (string, bool) {
	if len(parsed.Tokens) < 2 || !strings.EqualFold(parsed.Tokens[0], "go") ||
		!strings.EqualFold(parsed.Tokens[1], "work") {
		return "", false
	}
	if len(parsed.Tokens) == 2 {
		return "", true
	}
	return uniqueCommandPrefix(strings.ToLower(parsed.Tokens[2]), []string{
		"edit", "init", "sync", "use", "vendor",
	})
}

func uniqueCommandPrefix(value string, commands []string) (string, bool) {
	for _, command := range commands {
		if value == command {
			return command, true
		}
	}
	var match string
	for _, command := range commands {
		if strings.HasPrefix(command, value) {
			if match != "" {
				return "", false
			}
			match = command
		}
	}
	return match, match != ""
}

func hasWorkspaceCommandContext(parsed parseResult) bool {
	if _, _, ok := workspaceGoCommand(parsed); ok {
		return true
	}
	if _, ok := goModSubcommand(parsed); ok {
		return true
	}
	if _, ok := goWorkSubcommand(parsed); ok {
		return true
	}
	if _, _, ok := delveTargets(".", parsed); ok {
		return true
	}
	if _, ok := moduleToolTargets(".", parsed); ok {
		return true
	}
	_, _, ok := fileToolTargets(".", parsed)
	return ok
}

type workspaceModule struct {
	dir      string
	relative string
}

func workspaceModules(cwd string) []workspaceModule {
	var result []workspaceModule
	if _, ok := regularFile(filepath.Join(cwd, "go.mod")); ok {
		result = append(result, workspaceModule{dir: cwd, relative: "."})
	}
	for _, moduleDir := range nestedModules(cwd, 3) {
		relative, err := filepath.Rel(cwd, moduleDir)
		if err == nil {
			result = append(result, workspaceModule{dir: moduleDir, relative: relative})
		}
	}
	return result
}

func discoverWorkspaceGoTargets(cwd, command, query string) []goRunTarget {
	var result []goRunTarget
	_, rootHasModule := regularFile(filepath.Join(cwd, "go.mod"))
	if !rootHasModule {
		result = append(result, standaloneGoTargets(cwd, command)...)
	}
	for _, module := range workspaceModules(cwd) {
		result = append(result, moduleGoTargets(module, command, query)...)
	}
	return deduplicateRunTargets(result)
}

func standaloneGoTargets(cwd, command string) []goRunTarget {
	var files []string
	switch command {
	case "test":
		files = testGoFiles(cwd)
	case "generate":
		files = generatedGoFiles(cwd)
	case "vet", "fix", "list":
		files = goFiles(cwd)
	case "install":
		files = runnableGoFiles(cwd)
	default:
		return nil
	}
	result := make([]goRunTarget, 0, len(files))
	for _, name := range files {
		path := `.\` + name
		result = append(result, goRunTarget{
			command:     "go " + command + " " + quotePowerShellPath(path),
			search:      path + " " + name,
			description: standaloneDescription(command),
		})
	}
	return result
}

func standaloneDescription(command string) string {
	switch command {
	case "test":
		return "запустить найденный тестовый файл"
	case "generate":
		return "файл с директивой go:generate"
	case "install":
		return "установить отдельную Go-программу"
	case "vet":
		return "проверить отдельный Go-файл"
	case "fix":
		return "обновить API в отдельном Go-файле"
	default:
		return "показать сведения о Go-файле"
	}
}

func moduleGoTargets(module workspaceModule, command, query string) []goRunTarget {
	prefix := "go "
	if module.relative != "." {
		prefix = "go -C " + quotePowerShellPath(`.\`+filepath.Clean(module.relative)) + " "
	}
	var result []goRunTarget
	switch command {
	case "clean":
		result = append(result, goRunTarget{
			command:     prefix + "clean",
			search:      module.relative + " clean",
			description: "очистить результаты найденного Go-модуля",
		})
	case "test":
		result = append(result, goRunTarget{
			command:     prefix + "test ./...",
			search:      module.relative + " ./... tests",
			description: "все тесты найденного Go-модуля",
		})
		for _, pkg := range modulePackageDirs(module.dir, packageTests) {
			result = append(result, packageCommandTarget(prefix+"test ", pkg, module.relative, "тесты найденного пакета"))
		}
	case "vet":
		result = append(result, goRunTarget{
			command:     prefix + "vet ./...",
			search:      module.relative + " ./... vet",
			description: "проверить найденный Go-модуль",
		})
	case "generate":
		packages := modulePackageDirs(module.dir, packageGenerate)
		if len(packages) == 0 {
			result = append(result, goRunTarget{
				command:     prefix + "generate ./...",
				search:      module.relative + " ./... generate",
				description: "выполнить go:generate в модуле",
			})
		}
		for _, pkg := range packages {
			result = append(result, packageCommandTarget(prefix+"generate ", pkg, module.relative, "пакет с go:generate"))
		}
	case "fix":
		result = append(result, goRunTarget{
			command:     prefix + "fix ./...",
			search:      module.relative + " ./... fix",
			description: "обновить API во всём Go-модуле",
		})
	case "list":
		result = append(result, goRunTarget{
			command:     prefix + "list ./...",
			search:      module.relative + " ./... list",
			description: "показать пакеты найденного модуля",
		})
	case "install":
		for _, pkg := range modulePackageDirs(module.dir, packageMain) {
			result = append(result, packageCommandTarget(prefix+"install ", pkg, module.relative, "установить найденную Go-программу"))
		}
	case "doc":
		for _, pkg := range modulePackageDirs(module.dir, packageAny) {
			result = append(result, packageCommandTarget(prefix+"doc ", pkg, module.relative, "документация найденного пакета"))
		}
	case "get":
		getCommand := prefix + "get "
		if query != "" {
			getCommand += query
		}
		result = append(result, goRunTarget{
			command:     getCommand,
			search:      module.relative + " get " + query,
			description: "добавить зависимость в найденный Go-модуль",
		})
	}
	return result
}

func packageCommandTarget(prefix, pkg, moduleRelative, description string) goRunTarget {
	target := "."
	if pkg != "." {
		target = `.\` + filepath.Clean(pkg)
	}
	command := prefix + quotePowerShellPath(target)
	return goRunTarget{
		command:     command,
		search:      moduleRelative + " " + pkg + " " + command,
		description: description,
	}
}

type packageFilter uint8

const (
	packageAny packageFilter = iota
	packageTests
	packageGenerate
	packageMain
)

func modulePackageDirs(moduleDir string, filter packageFilter) []string {
	var result []string
	_ = filepath.WalkDir(moduleDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if path != moduleDir && shouldSkipScanDir(entry.Name()) {
			return filepath.SkipDir
		}
		if path != moduleDir {
			if _, nested := regularFile(filepath.Join(path, "go.mod")); nested {
				return filepath.SkipDir
			}
		}
		entries, readErr := os.ReadDir(path)
		if readErr != nil {
			return nil
		}
		var (
			hasGo       bool
			hasTest     bool
			hasGenerate bool
		)
		for _, child := range entries {
			if child.IsDir() || !strings.EqualFold(filepath.Ext(child.Name()), ".go") {
				continue
			}
			hasGo = true
			if strings.HasSuffix(strings.ToLower(child.Name()), "_test.go") {
				hasTest = true
			}
			if data, readFileErr := os.ReadFile(filepath.Join(path, child.Name())); readFileErr == nil &&
				strings.Contains(string(data), "//go:generate") {
				hasGenerate = true
			}
		}
		include := hasGo
		switch filter {
		case packageTests:
			include = hasTest
		case packageGenerate:
			include = hasGenerate
		case packageMain:
			include = len(runnableGoFiles(path)) == 1
		}
		if include {
			relative, relErr := filepath.Rel(moduleDir, path)
			if relErr == nil {
				result = append(result, relative)
			}
		}
		return nil
	})
	sort.Strings(result)
	return result
}

func testGoFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var result []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), "_test.go") {
			result = append(result, entry.Name())
		}
	}
	sort.Strings(result)
	return result
}

func generatedGoFiles(dir string) []string {
	var result []string
	for _, name := range goFiles(dir) {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil && strings.Contains(string(data), "//go:generate") {
			result = append(result, name)
		}
	}
	return result
}

func delveTargets(cwd string, parsed parseResult) ([]goRunTarget, string, bool) {
	if len(parsed.Tokens) < 2 || !strings.HasPrefix("dlv", strings.ToLower(parsed.Tokens[0])) {
		return nil, "", false
	}
	subcommand, ok := uniqueCommandPrefix(strings.ToLower(parsed.Tokens[1]), []string{"debug", "test"})
	if !ok {
		return nil, "", false
	}
	query := ""
	if len(parsed.Tokens) > 2 {
		if strings.HasPrefix(parsed.Current, "-") {
			return nil, "", false
		}
		query = normalizeWorkspaceQuery(parsed.Current)
	}

	var result []goRunTarget
	_, rootHasModule := regularFile(filepath.Join(cwd, "go.mod"))
	if !rootHasModule && subcommand == "debug" {
		for _, name := range runnableGoFiles(cwd) {
			path := `.\` + name
			result = append(result, goRunTarget{
				command:     "dlv debug " + quotePowerShellPath(path),
				search:      path + " " + name,
				description: "отладить отдельную Go-программу",
			})
		}
	}
	for _, module := range workspaceModules(cwd) {
		filter := packageMain
		description := "отладить найденную Go-программу"
		if subcommand == "test" {
			filter = packageTests
			description = "отладить тесты найденного пакета"
		}
		for _, pkg := range modulePackageDirs(module.dir, filter) {
			target := "."
			if pkg != "." {
				target = `.\` + filepath.Clean(pkg)
			}
			inner := "dlv " + subcommand + " " + quotePowerShellPath(target)
			command := inner
			if module.relative != "." {
				modulePath := quotePowerShellPath(`.\` + filepath.Clean(module.relative))
				command = "Push-Location " + modulePath + "; try { " + inner + " } finally { Pop-Location }"
			}
			result = append(result, goRunTarget{
				command:     command,
				search:      module.relative + " " + pkg + " " + subcommand,
				description: description,
			})
		}
	}
	return deduplicateRunTargets(result), query, true
}

func moduleToolTargets(cwd string, parsed parseResult) ([]goRunTarget, bool) {
	if len(parsed.Tokens) == 0 {
		return nil, false
	}
	tool, ok := uniqueCommandPrefix(strings.ToLower(parsed.Tokens[0]), []string{
		"air", "golangci-lint", "goreleaser", "gotestsum", "govulncheck", "staticcheck",
	})
	if !ok {
		return nil, false
	}
	var inner string
	switch tool {
	case "air":
		inner = "air"
	case "golangci-lint":
		if len(parsed.Tokens) > 1 {
			subcommand, subOK := uniqueCommandPrefix(strings.ToLower(parsed.Tokens[1]), []string{"run"})
			if !subOK || subcommand != "run" {
				return nil, false
			}
		}
		inner = "golangci-lint run ./..."
	case "goreleaser":
		inner = "goreleaser check"
	case "gotestsum":
		inner = "gotestsum -- ./..."
	case "govulncheck":
		inner = "govulncheck ./..."
	case "staticcheck":
		inner = "staticcheck ./..."
	}

	var result []goRunTarget
	for _, module := range workspaceModules(cwd) {
		if tool == "goreleaser" && !hasReleaseConfig(module.dir) {
			continue
		}
		command := inner
		if module.relative != "." {
			path := quotePowerShellPath(`.\` + filepath.Clean(module.relative))
			command = "Push-Location " + path + "; try { " + inner + " } finally { Pop-Location }"
		}
		result = append(result, goRunTarget{
			command:     command,
			search:      module.relative + " " + tool,
			description: tool + " · найденный Go-модуль",
		})
	}
	return result, true
}

func hasReleaseConfig(dir string) bool {
	for _, name := range []string{".goreleaser.yml", ".goreleaser.yaml", "goreleaser.yml", "goreleaser.yaml"} {
		if _, ok := regularFile(filepath.Join(dir, name)); ok {
			return true
		}
	}
	return false
}

func fileToolTargets(cwd string, parsed parseResult) ([]goRunTarget, string, bool) {
	if len(parsed.Tokens) == 0 {
		return nil, "", false
	}
	first := strings.ToLower(parsed.Tokens[0])
	tool, ok := uniqueCommandPrefix(first, []string{"goimports", "gopls", "mockgen"})
	if !ok {
		return nil, "", false
	}
	var (
		prefix      string
		description string
		query       string
	)
	switch tool {
	case "goimports":
		prefix = "goimports -w "
		description = "обновить imports и форматирование"
		if len(parsed.Tokens) > 1 {
			query = normalizeWorkspaceQuery(parsed.Current)
		}
	case "mockgen":
		prefix = "mockgen -source "
		description = "создать mock из Go-файла"
		if len(parsed.Tokens) > 1 {
			query = normalizeWorkspaceQuery(parsed.Current)
		}
	case "gopls":
		if len(parsed.Tokens) < 2 {
			return nil, "", false
		}
		subcommand, subOK := uniqueCommandPrefix(strings.ToLower(parsed.Tokens[1]), []string{
			"check", "format", "imports", "symbols",
		})
		if !subOK {
			return nil, "", false
		}
		prefix = "gopls " + subcommand + " "
		description = subcommand + " · найденный Go-файл"
		if len(parsed.Tokens) > 2 {
			query = normalizeWorkspaceQuery(parsed.Current)
		}
	}
	if strings.HasPrefix(query, "-") {
		return nil, "", false
	}
	paths := workspaceGoFilePaths(cwd, 80)
	result := make([]goRunTarget, 0, len(paths))
	for _, path := range paths {
		result = append(result, goRunTarget{
			command:     prefix + quotePowerShellPath(path),
			search:      path + " " + filepath.Base(path),
			description: description,
		})
	}
	return result, query, true
}

func workspaceGoFilePaths(cwd string, limit int) []string {
	seen := make(map[string]bool)
	var result []string
	appendFile := func(path string) {
		if len(result) >= limit {
			return
		}
		key := strings.ToLower(path)
		if !seen[key] {
			seen[key] = true
			result = append(result, path)
		}
	}
	for _, name := range allGoFiles(cwd) {
		appendFile(`.\` + name)
	}
	for _, module := range workspaceModules(cwd) {
		_ = filepath.WalkDir(module.dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil || len(result) >= limit {
				return nil
			}
			if entry.IsDir() {
				if path != module.dir && shouldSkipScanDir(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.EqualFold(filepath.Ext(entry.Name()), ".go") {
				return nil
			}
			relative, relErr := filepath.Rel(cwd, path)
			if relErr == nil {
				appendFile(`.\` + filepath.Clean(relative))
			}
			return nil
		})
	}
	return result
}

func allGoFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var result []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".go") {
			result = append(result, entry.Name())
		}
	}
	sort.Strings(result)
	return result
}

func discoverGoModTargets(cwd, subcommand string) []goRunTarget {
	if subcommand == "init" {
		if _, exists := regularFile(filepath.Join(cwd, "go.mod")); exists {
			return nil
		}
		name := sanitizeModuleName(filepath.Base(cwd))
		return []goRunTarget{{
			command:     "go mod init " + name,
			search:      name + " init",
			description: "создать go.mod для открытой папки",
		}}
	}
	var result []goRunTarget
	for _, module := range workspaceModules(cwd) {
		command := "go mod " + subcommand
		if module.relative != "." {
			command = "go -C " + quotePowerShellPath(`.\`+filepath.Clean(module.relative)) + " mod " + subcommand
		}
		result = append(result, goRunTarget{
			command:     command,
			search:      module.relative + " " + subcommand,
			description: "команда для найденного Go-модуля",
		})
	}
	return result
}

func discoverGoWorkTargets(cwd, subcommand string) []goRunTarget {
	_, hasWork := regularFile(filepath.Join(cwd, "go.work"))
	modules := workspaceModules(cwd)
	if subcommand == "" {
		if hasWork {
			return []goRunTarget{
				{command: "go work sync", search: "sync", description: "синхронизировать workspace"},
				{command: "go work use -r .", search: "use modules", description: "найти и добавить модули"},
				{command: "go work vendor", search: "vendor", description: "создать vendor для workspace"},
			}
		}
		if len(modules) == 0 {
			return nil
		}
		return []goRunTarget{
			{command: "go work init " + joinModulePaths(modules), search: "init modules", description: "создать go.work из найденных модулей"},
			{command: "go work use " + joinModulePaths(modules), search: "use modules", description: "добавить найденные модули"},
		}
	}
	if subcommand == "init" && !hasWork && len(modules) > 0 {
		return []goRunTarget{{
			command:     "go work init " + joinModulePaths(modules),
			search:      "init modules",
			description: "создать go.work из найденных модулей",
		}}
	}
	if subcommand == "use" && len(modules) > 0 {
		return []goRunTarget{{
			command:     "go work use " + joinModulePaths(modules),
			search:      "use modules",
			description: "добавить найденные модули",
		}}
	}
	if hasWork && (subcommand == "sync" || subcommand == "vendor" || subcommand == "edit") {
		return []goRunTarget{{
			command:     "go work " + subcommand,
			search:      subcommand,
			description: "команда для найденного go.work",
		}}
	}
	return nil
}

func joinModulePaths(modules []workspaceModule) string {
	paths := make([]string, 0, len(modules))
	for _, module := range modules {
		if module.relative == "." {
			paths = append(paths, ".")
		} else {
			paths = append(paths, quotePowerShellPath(`.\`+filepath.Clean(module.relative)))
		}
	}
	return strings.Join(paths, " ")
}

func sanitizeModuleName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var result []rune
	lastDash := false
	for _, value := range name {
		if value >= 'a' && value <= 'z' || value >= '0' && value <= '9' {
			result = append(result, value)
			lastDash = false
		} else if !lastDash && len(result) > 0 {
			result = append(result, '-')
			lastDash = true
		}
	}
	name = strings.Trim(string(result), "-")
	if name == "" {
		return "go-project"
	}
	return name
}
