package localai

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	modelVersion = 1
	maxCommands  = 256
	maxContexts  = 32
)

type Prediction struct {
	Command string
	Score   float64
}

type Stats struct {
	Commands int
	Bytes    int64
}

type record struct {
	Command  string         `json:"command"`
	Count    int            `json:"count"`
	Updated  int64          `json:"updated"`
	Contexts map[string]int `json:"contexts,omitempty"`
}

type diskModel struct {
	Version int      `json:"version"`
	Records []record `json:"records"`
}

// Model is a tiny adaptive command ranker. It never uses the network.
type Model struct {
	mu      sync.RWMutex
	path    string
	records map[string]*record
}

func NewMemory() *Model {
	return &Model{records: make(map[string]*record)}
}

func Load(path string) *Model {
	model := NewMemory()
	model.path = path
	data, err := os.ReadFile(path)
	if err != nil {
		return model
	}
	var stored diskModel
	if json.Unmarshal(data, &stored) != nil || stored.Version != modelVersion {
		return model
	}
	for index := range stored.Records {
		item := stored.Records[index]
		if item.Command == "" || !isGoCommand(item.Command) {
			continue
		}
		copy := item
		model.records[strings.ToLower(item.Command)] = &copy
	}
	return model
}

func (m *Model) Learn(command, cwd string) {
	command = strings.TrimSpace(command)
	if command == "" || !isGoCommand(command) {
		return
	}
	key := strings.ToLower(command)
	context := contextKey(cwd)

	m.mu.Lock()
	item := m.records[key]
	if item == nil {
		item = &record{Command: command, Contexts: make(map[string]int)}
		m.records[key] = item
	}
	item.Command = command
	item.Count++
	item.Updated = time.Now().Unix()
	if context != "" {
		item.Contexts[context]++
		trimContexts(item.Contexts)
	}
	m.trimLocked()
	data := m.marshalLocked()
	path := m.path
	m.mu.Unlock()

	if path != "" && len(data) > 0 {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, data, 0o644)
	}
}

func (m *Model) Score(command, cwd string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item := m.records[strings.ToLower(strings.TrimSpace(command))]
	if item == nil {
		return 0
	}
	score := math.Log2(float64(item.Count)+1) * 7
	if count := item.Contexts[contextKey(cwd)]; count > 0 {
		score += math.Log2(float64(count)+1) * 11
	}
	if age := time.Now().Unix() - item.Updated; age >= 0 && age < 7*24*60*60 {
		score += 5 * (1 - float64(age)/float64(7*24*60*60))
	}
	return score
}

func (m *Model) Predict(prefix, cwd string, limit int) []Prediction {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if prefix == "" || limit < 1 {
		return nil
	}
	m.mu.RLock()
	result := make([]Prediction, 0, len(m.records))
	for _, item := range m.records {
		if !strings.HasPrefix(strings.ToLower(item.Command), prefix) {
			continue
		}
		score := math.Log2(float64(item.Count)+1) * 7
		if count := item.Contexts[contextKey(cwd)]; count > 0 {
			score += math.Log2(float64(count)+1) * 11
		}
		result = append(result, Prediction{Command: item.Command, Score: score})
	}
	m.mu.RUnlock()
	sort.SliceStable(result, func(i, j int) bool { return result[i].Score > result[j].Score })
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func (m *Model) Stats() Stats {
	m.mu.RLock()
	count := len(m.records)
	path := m.path
	m.mu.RUnlock()
	stats := Stats{Commands: count}
	if info, err := os.Stat(path); err == nil {
		stats.Bytes = info.Size()
	}
	return stats
}

func (m *Model) trimLocked() {
	if len(m.records) <= maxCommands {
		return
	}
	items := make([]*record, 0, len(m.records))
	for _, item := range m.records {
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Updated < items[j].Updated })
	for _, item := range items[:len(items)-maxCommands] {
		delete(m.records, strings.ToLower(item.Command))
	}
}

func (m *Model) marshalLocked() []byte {
	items := make([]record, 0, len(m.records))
	for _, item := range m.records {
		items = append(items, *item)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Updated > items[j].Updated })
	data, _ := json.Marshal(diskModel{Version: modelVersion, Records: items})
	return data
}

func trimContexts(contexts map[string]int) {
	if len(contexts) <= maxContexts {
		return
	}
	type contextCount struct {
		name  string
		count int
	}
	items := make([]contextCount, 0, len(contexts))
	for name, count := range contexts {
		items = append(items, contextCount{name: name, count: count})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].count < items[j].count })
	for _, item := range items[:len(items)-maxContexts] {
		delete(contexts, item.name)
	}
}

func contextKey(cwd string) string {
	absolute, err := filepath.Abs(cwd)
	if err != nil {
		return ""
	}
	return strings.ToLower(filepath.Clean(absolute))
}

func isGoCommand(command string) bool {
	fields := strings.Fields(strings.ToLower(command))
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "go", "gofmt", "goimports", "gopls", "dlv", "staticcheck",
		"golangci-lint", "gotestsum", "govulncheck", "goreleaser", "air",
		"mockgen", "stringer", "goctl":
		return true
	default:
		return false
	}
}
