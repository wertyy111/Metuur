package localai

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func InspectEnvironment(cwd, activeFile string, recent []string, lastCommand string, lastExitCode int) Environment {
	return EnrichEnvironment(Environment{
		CWD:            cwd,
		ActiveFile:     activeFile,
		RecentCommands: recent,
		LastCommand:    lastCommand,
		LastExitCode:   lastExitCode,
	})
}

func EnrichEnvironment(env Environment) Environment {
	cwd := env.CWD
	absolute, err := filepath.Abs(cwd)
	if err != nil {
		absolute = cwd
	}
	env.CWD = absolute
	env.ActiveFile = relativeIfInside(absolute, env.ActiveFile)
	env.Module = readModule(filepath.Join(absolute, "go.mod"))
	if len(env.RecentCommands) > 5 {
		env.RecentCommands = env.RecentCommands[len(env.RecentCommands)-5:]
	}
	env.RecentCommands = append([]string(nil), env.RecentCommands...)
	env.GoFiles = findGoFiles(absolute, 60)
	env.GitStatus = gitSummary(absolute)
	return env
}

func readModule(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1]
		}
	}
	return ""
}

func findGoFiles(root string, limit int) []string {
	var result []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && shouldSkipDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			relative, relErr := filepath.Rel(root, path)
			if relErr == nil && relative != "." && strings.Count(relative, string(filepath.Separator)) >= 4 {
				return filepath.SkipDir
			}
			return nil
		}
		if len(result) >= limit {
			return filepath.SkipAll
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".go") {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr == nil {
			result = append(result, filepath.ToSlash(relative))
		}
		return nil
	})
	sort.Strings(result)
	return result
}

func shouldSkipDirectory(name string) bool {
	name = strings.ToLower(name)
	return name == ".git" || name == ".idea" || name == ".vscode" || name == "vendor" ||
		name == "node_modules" || name == "bin" || name == "dist" || strings.HasPrefix(name, ".")
}

func relativeIfInside(root, path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, `..`+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(relative)
}

func gitSummary(cwd string) string {
	dir, err := filepath.Abs(cwd)
	if err != nil {
		return ""
	}
	for {
		metadata := filepath.Join(dir, ".git")
		if info, statErr := os.Stat(metadata); statErr == nil {
			if !info.IsDir() {
				pointer, readErr := os.ReadFile(metadata)
				if readErr != nil {
					return ""
				}
				line := strings.TrimSpace(string(pointer))
				if !strings.HasPrefix(line, "gitdir:") {
					return ""
				}
				metadata = strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
				if !filepath.IsAbs(metadata) {
					metadata = filepath.Join(dir, metadata)
				}
			}
			head, readErr := os.ReadFile(filepath.Join(metadata, "HEAD"))
			if readErr != nil {
				return ""
			}
			value := strings.TrimSpace(string(head))
			if strings.HasPrefix(value, "ref:") {
				return "branch " + strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(value, "ref:")), "refs/heads/")
			}
			if len(value) > 12 {
				value = value[:12]
			}
			return "detached " + value
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
