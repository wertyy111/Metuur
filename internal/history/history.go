package history

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type Match struct {
	Command   string
	Frequency int
	Recency   int
}

type Store struct {
	mu       sync.RWMutex
	path     string
	limit    int
	commands []string
	stats    map[string]*Match
}

func Load(ownPath string, limit int) *Store {
	s := &Store{
		path:  ownPath,
		limit: limit,
		stats: make(map[string]*Match),
	}
	for _, path := range sourcePaths(ownPath) {
		s.loadFile(path)
	}
	if len(s.commands) > s.limit {
		s.commands = s.commands[len(s.commands)-s.limit:]
		s.reindex()
	}
	return s
}

func (s *Store) Add(command string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}

	s.mu.Lock()
	s.commands = append(s.commands, command)
	if len(s.commands) > s.limit {
		s.commands = s.commands[len(s.commands)-s.limit:]
		s.reindexLocked()
	} else {
		item := s.stats[command]
		if item == nil {
			item = &Match{Command: command}
			s.stats[command] = item
		}
		item.Frequency++
		item.Recency = len(s.commands)
	}
	s.mu.Unlock()

	_ = os.MkdirAll(filepath.Dir(s.path), 0o755)
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		_, _ = file.WriteString(strings.ReplaceAll(command, "\n", " ") + "\n")
		_ = file.Close()
	}
}

func (s *Store) Search(query string, limit int) []Match {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query = strings.ToLower(strings.TrimSpace(query))
	matches := make([]Match, 0, len(s.stats))
	for _, item := range s.stats {
		if query == "" || fuzzyContains(strings.ToLower(item.Command), query) {
			matches = append(matches, *item)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Recency != matches[j].Recency {
			return matches[i].Recency > matches[j].Recency
		}
		return matches[i].Frequency > matches[j].Frequency
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

func (s *Store) Commands() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]string, len(s.commands))
	copy(result, s.commands)
	return result
}

func (s *Store) loadFile(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		s.commands = append(s.commands, line)
		item := s.stats[line]
		if item == nil {
			item = &Match{Command: line}
			s.stats[line] = item
		}
		item.Frequency++
		item.Recency = len(s.commands)
	}
}

func (s *Store) reindex() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reindexLocked()
}

func (s *Store) reindexLocked() {
	s.stats = make(map[string]*Match)
	for i, command := range s.commands {
		item := s.stats[command]
		if item == nil {
			item = &Match{Command: command}
			s.stats[command] = item
		}
		item.Frequency++
		item.Recency = i + 1
	}
}

func sourcePaths(ownPath string) []string {
	appData := os.Getenv("APPDATA")
	return uniquePaths([]string{
		filepath.Join(appData, "Microsoft", "Windows", "PowerShell", "PSReadLine", "ConsoleHost_history.txt"),
		filepath.Join(appData, "Microsoft", "PowerShell", "PSReadLine", "ConsoleHost_history.txt"),
		filepath.Join(appData, "Microsoft", "PowerShell", "PSReadLine", "Visual Studio Code Host_history.txt"),
		ownPath,
	})
}

func uniquePaths(paths []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" || seen[strings.ToLower(path)] {
			continue
		}
		seen[strings.ToLower(path)] = true
		result = append(result, path)
	}
	return result
}

func fuzzyContains(text, query string) bool {
	if strings.Contains(text, query) {
		return true
	}
	queryRunes := []rune(query)
	i := 0
	for _, r := range text {
		if i < len(queryRunes) && r == queryRunes[i] {
			i++
		}
	}
	return i == len(queryRunes)
}
