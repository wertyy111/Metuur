package suggest

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type vscodeActiveFile struct {
	Path      string    `json:"path"`
	Workspace string    `json:"workspace"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func activeGoFile(cwd string) (string, bool) {
	if strings.TrimSpace(os.Getenv("METUUR_DISABLE_VSCODE_ACTIVE")) == "1" {
		return "", false
	}
	if path := strings.TrimSpace(os.Getenv("METUUR_ACTIVE_FILE")); path != "" {
		return validateActiveGoFile(cwd, path, "")
	}
	if state, ok := activeFileBridgeState(); ok {
		if path, valid := validateActiveGoFile(cwd, state.Path, state.Workspace); valid {
			return path, true
		}
	}
	if path, workspace, ok := activeFileFromVSCodeState(cwd); ok {
		return validateActiveGoFile(cwd, path, workspace)
	}
	return "", false
}

func validateActiveGoFile(cwd, path, workspace string) (string, bool) {
	path = strings.TrimSpace(path)
	workspace = strings.TrimSpace(workspace)
	absolute, err := filepath.Abs(path)
	if err != nil || !strings.EqualFold(filepath.Ext(absolute), ".go") ||
		strings.HasSuffix(strings.ToLower(absolute), "_test.go") {
		return "", false
	}
	if _, ok := regularFile(absolute); !ok {
		return "", false
	}
	if workspace != "" && !sameWorkspace(cwd, workspace) {
		return "", false
	}
	return absolute, true
}

func activeFileBridgeState() (vscodeActiveFile, bool) {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return vscodeActiveFile{}, false
	}
	data, err := os.ReadFile(filepath.Join(localAppData, "Metuur", "vscode-active-file.json"))
	if err != nil {
		return vscodeActiveFile{}, false
	}
	var state vscodeActiveFile
	if json.Unmarshal(data, &state) != nil || strings.TrimSpace(state.Path) == "" {
		return vscodeActiveFile{}, false
	}
	age := time.Since(state.UpdatedAt)
	if state.UpdatedAt.IsZero() || age > 20*time.Second || age < -time.Minute {
		return vscodeActiveFile{}, false
	}
	return state, true
}

func activeFileFromVSCodeState(cwd string) (string, string, bool) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", "", false
	}
	storageRoots := []string{
		filepath.Join(appData, "Code", "User", "workspaceStorage"),
		filepath.Join(appData, "Code - Insiders", "User", "workspaceStorage"),
	}
	type candidate struct {
		statePath string
		workspace string
		modTime   int64
	}
	var candidates []candidate
	for _, storageRoot := range storageRoots {
		entries, err := os.ReadDir(storageRoot)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dir := filepath.Join(storageRoot, entry.Name())
			workspace, ok := vscodeWorkspacePath(filepath.Join(dir, "workspace.json"))
			if !ok || !sameWorkspace(cwd, workspace) {
				continue
			}
			statePath := filepath.Join(dir, "state.vscdb")
			info, ok := regularFile(statePath)
			if !ok {
				continue
			}
			candidates = append(candidates, candidate{
				statePath: statePath,
				workspace: workspace,
				modTime:   info.ModTime().UnixNano(),
			})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].modTime > candidates[j].modTime
	})
	for _, item := range candidates {
		data, err := os.ReadFile(item.statePath)
		if err != nil {
			continue
		}
		if path, ok := recentEditorFromState(data); ok {
			return path, item.workspace, true
		}
	}
	return "", "", false
}

func vscodeWorkspacePath(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var state struct {
		Folder string `json:"folder"`
	}
	if json.Unmarshal(data, &state) != nil {
		return "", false
	}
	return fileURIPath(state.Folder)
}

func recentEditorFromState(data []byte) (string, bool) {
	const key = "history.entries"
	const resource = `"resource":"`
	searchFrom := 0
	for {
		keyOffset := strings.Index(string(data[searchFrom:]), key)
		if keyOffset < 0 {
			break
		}
		keyOffset += searchFrom
		end := min(len(data), keyOffset+4096)
		value := string(data[keyOffset:end])
		resourceOffset := strings.Index(value, resource)
		if resourceOffset >= 0 {
			start := resourceOffset + len(resource)
			if quote := strings.IndexByte(value[start:], '"'); quote >= 0 {
				if path, ok := fileURIPath(value[start : start+quote]); ok {
					if _, exists := regularFile(path); exists &&
						strings.EqualFold(filepath.Ext(path), ".go") &&
						!strings.HasSuffix(strings.ToLower(path), "_test.go") {
						return path, true
					}
				}
			}
		}
		searchFrom = keyOffset + len(key)
	}
	return "", false
}

func fileURIPath(raw string) (string, bool) {
	uri, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(uri.Scheme, "file") {
		return "", false
	}
	path, err := url.PathUnescape(uri.Path)
	if err != nil {
		return "", false
	}
	path = filepath.FromSlash(path)
	if len(path) >= 3 && path[0] == filepath.Separator && path[2] == ':' {
		path = path[1:]
	}
	if path == "" {
		return "", false
	}
	return filepath.Clean(path), true
}

func sameWorkspace(cwd, workspace string) bool {
	cwd, cwdErr := filepath.Abs(cwd)
	workspace, workspaceErr := filepath.Abs(workspace)
	if cwdErr != nil || workspaceErr != nil {
		return false
	}
	return isInside(cwd, workspace) || isInside(workspace, cwd)
}

func isInside(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, `..`+string(filepath.Separator))
}

func activeGoTarget(cwd, action string) (goRunTarget, bool) {
	path, ok := activeGoFile(cwd)
	if !ok {
		return goRunTarget{}, false
	}
	if (action == "run" || action == "build") && !isRunnableGoFile(path) {
		return goRunTarget{}, false
	}

	relative, err := filepath.Rel(cwd, path)
	if err != nil {
		return goRunTarget{}, false
	}
	displayPath := `.\` + filepath.Clean(relative)
	if strings.HasPrefix(relative, `..`+string(filepath.Separator)) {
		displayPath = filepath.Clean(relative)
	}
	quotedPath := quotePowerShellPath(displayPath)
	description := "открытый файл VS Code"
	command := ""
	switch action {
	case "run":
		command = activePackageCommand(cwd, path, "run")
		if command == "" {
			command = "go run " + quotedPath
		}
	case "build":
		command = activePackageCommand(cwd, path, "build")
		if command == "" {
			command = "go build " + quotedPath
		}
	case "format":
		command = "gofmt -w " + quotedPath
	case "vet":
		command = activePackageCommand(cwd, path, "vet")
		if command == "" {
			command = "go vet " + quotedPath
		}
	default:
		return goRunTarget{}, false
	}
	return goRunTarget{
		command:     command,
		search:      displayPath + " " + filepath.Base(path) + " active vscode",
		description: description,
		recommended: true,
		scoreBoost:  250,
	}, true
}

func activePackageCommand(cwd, path, action string) string {
	moduleDir, ok := nearestModule(filepath.Dir(path))
	if !ok {
		return ""
	}
	moduleRelative, err := filepath.Rel(cwd, moduleDir)
	if err != nil {
		return ""
	}
	packageRelative, err := filepath.Rel(moduleDir, filepath.Dir(path))
	if err != nil {
		return ""
	}
	// For a one-file program, show the file the user is actually looking at.
	// Multi-file packages still use a package target so dependencies in sibling
	// source files are included.
	if len(goFiles(filepath.Dir(path))) == 1 {
		return ""
	}
	prefix := "go "
	if moduleRelative != "." {
		prefix = "go -C " + quotePowerShellPath(relativePowerShellPath(moduleRelative)) + " "
	}
	target := "."
	if packageRelative != "." {
		target = relativePowerShellPath(packageRelative)
	}
	return prefix + action + " " + quotePowerShellPath(target)
}

func nearestModule(dir string) (string, bool) {
	for {
		if _, ok := regularFile(filepath.Join(dir, "go.mod")); ok {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func relativePowerShellPath(relative string) string {
	relative = filepath.Clean(relative)
	if relative == "." || strings.HasPrefix(relative, `..`+string(filepath.Separator)) {
		return relative
	}
	return `.\` + relative
}

func isRunnableGoFile(path string) bool {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil || file.Name == nil || file.Name.Name != "main" {
		return false
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv == nil && function.Name != nil && function.Name.Name == "main" {
			return true
		}
	}
	return false
}
