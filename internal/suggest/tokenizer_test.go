package suggest

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wertyy111/metuur/internal/history"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("METUUR_DISABLE_VSCODE_ACTIVE", "1")
	os.Exit(m.Run())
}

func TestParsePowerShellLine(t *testing.T) {
	tests := []struct {
		line    string
		tokens  int
		current string
		start   int
		trail   bool
	}{
		{"git ch", 2, "ch", 4, false},
		{"git checkout ", 2, "", 13, true},
		{`code "my file`, 2, "my file", 5, false},
		{`go run .\cmd\`, 3, `.\cmd\`, 7, false},
	}
	for _, test := range tests {
		got := parse(test.line)
		if len(got.Tokens) != test.tokens || got.Current != test.current ||
			got.ReplaceStart != test.start || got.Trailing != test.trail {
			t.Fatalf("parse(%q) = %#v", test.line, got)
		}
	}
}

func TestMatchScore(t *testing.T) {
	if matchScore("checkout", "ch") <= matchScore("switch", "ch") {
		t.Fatal("prefix match should outrank fuzzy match")
	}
	if !mathIsNegativeInfinity(matchScore("status", "xyz")) {
		t.Fatal("unmatched candidate should be rejected")
	}
}

func mathIsNegativeInfinity(value float64) bool {
	return value < -1e100
}

func TestRootSuggestionsContainOnlyGoTools(t *testing.T) {
	store := history.Load(filepath.Join(t.TempDir(), "history.txt"), 100000)
	engine, err := New(store, false)
	if err != nil {
		t.Fatal(err)
	}
	items := engine.Suggest("d", t.TempDir(), ModeSpec, 20)
	foundDelve := false
	for _, item := range items {
		if strings.EqualFold(item.Label, "dlv") {
			foundDelve = true
		}
		if strings.EqualFold(item.Label, "docker") || strings.EqualFold(item.Label, "dotnet") {
			t.Fatalf("non-Go tool leaked into suggestions: %#v", item)
		}
	}
	if !foundDelve {
		t.Fatalf("dlv not found in Go-only suggestions: %#v", items)
	}
}

func TestGoCatalogIsNotTruncatedToViewport(t *testing.T) {
	store := history.Load(filepath.Join(t.TempDir(), "history.txt"), 100000)
	engine, err := New(store, false)
	if err != nil {
		t.Fatal(err)
	}
	items := engine.Suggest("go", t.TempDir(), ModeSpec, 100)
	if len(items) < 19 {
		t.Fatalf("expected the full go command catalog, got %d: %#v", len(items), items)
	}
	for _, want := range []string{"run", "test", "vet", "work"} {
		found := false
		for _, item := range items {
			if item.Label == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("go catalog is missing %q: %#v", want, items)
		}
	}
}

func TestCompleteWorkflowIsSuggestedAutomatically(t *testing.T) {
	store := history.Load(filepath.Join(t.TempDir(), "history.txt"), 100000)
	engine, err := New(store, false)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	writeTestFile(t, filepath.Join(cwd, "main.go"), "package main\nfunc main() {}\n")

	items := engine.Suggest("gofmt", cwd, ModeSpec, 10)
	if len(items) == 0 || items[0].Insert != `gofmt -w .\main.go` {
		t.Fatalf("recommended gofmt workflow is not first: %#v", items)
	}

	items = engine.Suggest("go", cwd, ModeSpec, 10)
	if len(items) == 0 || items[0].Insert != `go run .\main.go` {
		t.Fatalf("ready workspace commands were not opened automatically: %#v", items)
	}

	items = engine.Suggest("go fmt bug", cwd, ModeSpec, 10)
	if len(items) != 0 && items[0].Kind == "format" {
		t.Fatalf("formatter target was invented in an empty folder: %#v", items)
	}
}

func TestBareGoStartsWithReadyWorkspaceCommands(t *testing.T) {
	t.Setenv("METUUR_ACTIVE_FILE", "")
	store := history.Load(filepath.Join(t.TempDir(), "history.txt"), 100000)
	engine, err := New(store, false)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	writeTestFile(t, filepath.Join(cwd, "main.go"), "package main\nfunc main() {}\n")
	moduleDir := filepath.Join(cwd, "tool")
	writeTestFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com/tool\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(moduleDir, "demo_test.go"), "package tool\n")

	items := engine.Suggest("go", cwd, ModeSpec, 100)
	want := []string{
		`go run .\main.go`,
		`go build .\main.go`,
		`gofmt -w .\main.go`,
		`go -C .\tool test ./...`,
		`go vet .\main.go`,
	}
	if len(items) < len(want) {
		t.Fatalf("expected at least %d ready commands, got %#v", len(want), items)
	}
	for index, command := range want {
		if items[index].Insert != command {
			t.Fatalf("ready command %d = %q, want %q; all: %#v", index, items[index].Insert, command, items)
		}
	}
}

func TestActiveVSCodeFileIsSuggestedFirst(t *testing.T) {
	t.Setenv("METUUR_DISABLE_VSCODE_ACTIVE", "")
	store := history.Load(filepath.Join(t.TempDir(), "history.txt"), 100000)
	engine, err := New(store, false)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	first := filepath.Join(cwd, "first.go")
	active := filepath.Join(cwd, "opened.go")
	writeTestFile(t, first, "package main\nfunc main() {}\n")
	writeTestFile(t, active, "package main\nfunc main() {}\n")
	t.Setenv("METUUR_ACTIVE_FILE", active)

	cases := []struct {
		line string
		want string
	}{
		{"go", `go run .\opened.go`},
		{"go run", `go run .\opened.go`},
		{"go build", `go build .\opened.go`},
		{"gofmt", `gofmt -w .\opened.go`},
	}
	for _, test := range cases {
		items := engine.Suggest(test.line, cwd, ModeSpec, 20)
		if len(items) == 0 || items[0].Insert != test.want {
			t.Fatalf("%q did not prioritize active file %q: %#v", test.line, test.want, items)
		}
	}
}

func TestActiveVSCodeFileIsReadFromWorkspaceState(t *testing.T) {
	t.Setenv("METUUR_DISABLE_VSCODE_ACTIVE", "")
	store := history.Load(filepath.Join(t.TempDir(), "history.txt"), 100000)
	engine, err := New(store, false)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	active := filepath.Join(cwd, "opened.go")
	writeTestFile(t, filepath.Join(cwd, "first.go"), "package main\nfunc main() {}\n")
	writeTestFile(t, active, "package main\nfunc main() {}\n")

	appData := t.TempDir()
	t.Setenv("APPDATA", appData)
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("METUUR_ACTIVE_FILE", "")
	storage := filepath.Join(appData, "Code", "User", "workspaceStorage", "test-workspace")
	workspaceURI := (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(cwd)}).String()
	activeURI := (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(active)}).String()
	writeTestFile(t, filepath.Join(storage, "workspace.json"), fmt.Sprintf(`{"folder":%q}`, workspaceURI))
	writeTestFile(t, filepath.Join(storage, "state.vscdb"),
		"sqlite-data\x00history.entries[{\"editor\":{\"resource\":\""+activeURI+"\"}}]\x00")

	items := engine.Suggest("go", cwd, ModeSpec, 20)
	if len(items) == 0 || items[0].Insert != `go run .\opened.go` {
		t.Fatalf("VS Code workspace state was not detected: %#v", items)
	}
}

func TestActiveVSCodeFileUsesItsModuleAndPackage(t *testing.T) {
	t.Setenv("METUUR_DISABLE_VSCODE_ACTIVE", "")
	store := history.Load(filepath.Join(t.TempDir(), "history.txt"), 100000)
	engine, err := New(store, false)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	module := filepath.Join(cwd, "tool")
	active := filepath.Join(module, "cmd", "demo", "main.go")
	writeTestFile(t, filepath.Join(module, "go.mod"), "module example.com/tool\n\ngo 1.22\n")
	writeTestFile(t, active, "package main\nfunc main() {}\n")
	t.Setenv("METUUR_ACTIVE_FILE", active)

	items := engine.Suggest("go", cwd, ModeSpec, 20)
	want := `go -C .\tool run .\cmd\demo`
	if len(items) == 0 || items[0].Insert != want {
		t.Fatalf("active module package was not suggested as %q: %#v", want, items)
	}
}

func TestGoRunSuggestsRunnableFilesFromWorkspace(t *testing.T) {
	store := history.Load(filepath.Join(t.TempDir(), "history.txt"), 100000)
	engine, err := New(store, false)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	writeTestFile(t, filepath.Join(cwd, "main.go"), "package main\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(cwd, "task.go"), "package main\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(cwd, "helper.go"), "package main\nfunc helper() {}\n")
	writeTestFile(t, filepath.Join(cwd, "main_test.go"), "package main\nfunc main() {}\n")

	items := engine.Suggest("go r", cwd, ModeSpec, 10)
	if len(items) < 2 {
		t.Fatalf("expected runnable workspace files, got %#v", items)
	}
	if items[0].Insert != `go run .\main.go` || items[1].Insert != `go run .\task.go` {
		t.Fatalf("runnable files were not ranked first: %#v", items)
	}
	for _, item := range items {
		if strings.Contains(item.Insert, "helper.go") || strings.Contains(item.Insert, "_test.go") {
			t.Fatalf("non-runnable file was suggested: %#v", item)
		}
	}
}

func TestGoRunSuggestsNestedModuleWithGoC(t *testing.T) {
	store := history.Load(filepath.Join(t.TempDir(), "history.txt"), 100000)
	engine, err := New(store, false)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	moduleDir := filepath.Join(cwd, "tool")
	writeTestFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com/tool\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(moduleDir, "cmd", "demo", "main.go"), "package main\nfunc main() {}\n")

	items := engine.Suggest("go run ", cwd, ModeSpec, 10)
	want := `go -C .\tool run .\cmd\demo`
	for _, item := range items {
		if item.Insert == want {
			return
		}
	}
	t.Fatalf("nested module command %q was not suggested: %#v", want, items)
}

func TestGoFmtCorrectsGenericPatternToWorkspaceFiles(t *testing.T) {
	store := history.Load(filepath.Join(t.TempDir(), "history.txt"), 100000)
	engine, err := New(store, false)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	writeTestFile(t, filepath.Join(cwd, "main.go"), "package main\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(cwd, "helper.go"), "package main\nfunc helper() {}\n")
	writeTestFile(t, filepath.Join(cwd, "notes.txt"), "not Go\n")

	items := engine.Suggest("go fmt ./...", cwd, ModeSpec, 10)
	if len(items) < 2 {
		t.Fatalf("expected workspace format targets, got %#v", items)
	}
	if items[0].Insert != `gofmt -w .\helper.go` || items[1].Insert != `gofmt -w .\main.go` {
		t.Fatalf("workspace files were not ranked first: %#v", items)
	}
	for _, item := range items {
		if strings.Contains(item.Insert, "notes.txt") {
			t.Fatalf("non-Go file was suggested: %#v", item)
		}
		if item.Insert == "go fmt ./... " || item.Insert == "go fmt . " {
			t.Fatalf("module-only formatter was suggested without go.mod: %#v", item)
		}
	}
}

func TestGoFmtUsesModuleAwareCommand(t *testing.T) {
	store := history.Load(filepath.Join(t.TempDir(), "history.txt"), 100000)
	engine, err := New(store, false)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	writeTestFile(t, filepath.Join(cwd, "go.mod"), "module example.com/demo\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(cwd, "main.go"), "package main\nfunc main() {}\n")

	items := engine.Suggest("go fm", cwd, ModeSpec, 10)
	if len(items) == 0 || items[0].Insert != "go fmt ./..." {
		t.Fatalf("module-aware formatter was not first: %#v", items)
	}
}

func TestGoBuildUsesRunnableWorkspaceFilesWithoutModule(t *testing.T) {
	store := history.Load(filepath.Join(t.TempDir(), "history.txt"), 100000)
	engine, err := New(store, false)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	writeTestFile(t, filepath.Join(cwd, "main.go"), "package main\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(cwd, "task.go"), "package main\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(cwd, "helper.go"), "package main\nfunc helper() {}\n")

	items := engine.Suggest("go build .", cwd, ModeSpec, 10)
	if len(items) < 2 {
		t.Fatalf("expected buildable workspace files, got %#v", items)
	}
	if items[0].Insert != `go build .\main.go` || items[1].Insert != `go build .\task.go` {
		t.Fatalf("workspace build commands were not ranked first: %#v", items)
	}
	for _, item := range items {
		if item.Insert == "go build . " || item.Insert == "go build ./... " ||
			strings.Contains(item.Insert, "helper.go") {
			t.Fatalf("invalid build target was suggested: %#v", item)
		}
	}

	items = engine.Suggest("go build no-such-target", cwd, ModeSpec, 20)
	for _, item := range items {
		if item.Kind == "build" {
			t.Fatalf("unrelated fuzzy build target was suggested: %#v", item)
		}
	}
}

func TestGoBuildUsesNestedModuleWithGoC(t *testing.T) {
	store := history.Load(filepath.Join(t.TempDir(), "history.txt"), 100000)
	engine, err := New(store, false)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	moduleDir := filepath.Join(cwd, "tool")
	writeTestFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com/tool\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(moduleDir, "cmd", "demo", "main.go"), "package main\nfunc main() {}\n")

	items := engine.Suggest("go bu", cwd, ModeSpec, 10)
	want := `go -C .\tool build .\cmd\demo`
	for _, item := range items {
		if item.Insert == want {
			return
		}
	}
	t.Fatalf("nested module build command %q was not suggested: %#v", want, items)
}

func TestAllWorkspaceGoCommandsUseRealFilesAndModules(t *testing.T) {
	store := history.Load(filepath.Join(t.TempDir(), "history.txt"), 100000)
	engine, err := New(store, false)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	writeTestFile(t, filepath.Join(cwd, "main.go"), "package main\nfunc main() {}\n")
	moduleDir := filepath.Join(cwd, "tool")
	writeTestFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com/tool\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(moduleDir, "cmd", "demo", "main.go"), "package main\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(moduleDir, "internal", "thing", "thing.go"), "package thing\n")
	writeTestFile(t, filepath.Join(moduleDir, "internal", "thing", "thing_test.go"), "package thing\nfunc TestThing(t *testing.T) {}\n")

	cases := []struct {
		line string
		want string
	}{
		{"go test ./...", `go -C .\tool test ./...`},
		{"go vet ./...", `go vet .\main.go`},
		{"go generate ./...", `go -C .\tool generate ./...`},
		{"go fix ./...", `go fix .\main.go`},
		{"go list ./...", `go list .\main.go`},
		{"go install .", `go install .\main.go`},
		{"go doc .", `go -C .\tool doc .\cmd\demo`},
	}
	for _, test := range cases {
		items := engine.Suggest(test.line, cwd, ModeSpec, 20)
		if !hasSuggestion(items, test.want) {
			t.Fatalf("%q did not suggest %q: %#v", test.line, test.want, items)
		}
	}
}

func TestGoModAndGoWorkUseDiscoveredModules(t *testing.T) {
	store := history.Load(filepath.Join(t.TempDir(), "history.txt"), 100000)
	engine, err := New(store, false)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	moduleDir := filepath.Join(cwd, "tool")
	writeTestFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com/tool\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(moduleDir, "main.go"), "package main\nfunc main() {}\n")

	items := engine.Suggest("go mod tidy", cwd, ModeSpec, 10)
	if len(items) == 0 || items[0].Insert != `go -C .\tool mod tidy` {
		t.Fatalf("go mod did not target the discovered module: %#v", items)
	}
	items = engine.Suggest("go work", cwd, ModeSpec, 10)
	if len(items) == 0 || items[0].Insert != `go work init .\tool` {
		t.Fatalf("go work did not use the discovered module: %#v", items)
	}
}

func TestWorkspaceAwareGoToolsUseFilesAndModules(t *testing.T) {
	store := history.Load(filepath.Join(t.TempDir(), "history.txt"), 100000)
	engine, err := New(store, false)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	writeTestFile(t, filepath.Join(cwd, "main.go"), "package main\nfunc main() {}\n")
	moduleDir := filepath.Join(cwd, "tool")
	writeTestFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com/tool\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(moduleDir, "main.go"), "package main\nfunc main() {}\n")

	cases := []struct {
		line string
		want string
	}{
		{"go clean", `go -C .\tool clean`},
		{"go get example.com/lib@latest", `go -C .\tool get example.com/lib@latest`},
		{"goimports", `goimports -w .\main.go`},
		{"gopls check", `gopls check .\main.go`},
		{"mockgen", `mockgen -source .\main.go`},
		{"dlv debug", `dlv debug .\main.go`},
		{"staticcheck", `Push-Location .\tool; try { staticcheck ./... } finally { Pop-Location }`},
		{"gotestsum", `Push-Location .\tool; try { gotestsum -- ./... } finally { Pop-Location }`},
		{"govulncheck", `Push-Location .\tool; try { govulncheck ./... } finally { Pop-Location }`},
	}
	for _, test := range cases {
		items := engine.Suggest(test.line, cwd, ModeSpec, 20)
		if !hasSuggestion(items, test.want) {
			t.Fatalf("%q did not suggest %q: %#v", test.line, test.want, items)
		}
	}
}

func TestRecipeDatabaseExpandsPartialCommandChains(t *testing.T) {
	store := history.Load(filepath.Join(t.TempDir(), "history.txt"), 100000)
	engine, err := New(store, false)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	writeTestFile(t, filepath.Join(cwd, "main.go"), "package main\nfunc main() {}\n")
	moduleDir := filepath.Join(cwd, "tool")
	writeTestFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com/tool\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(moduleDir, "cmd", "demo", "main.go"), "package main\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(moduleDir, "demo_test.go"), "package tool\nfunc TestDemo(t *testing.T) {}\n")

	cases := []struct {
		line string
		want string
	}{
		{"go b", `go build .\main.go`},
		{"go t", `go -C .\tool test ./...`},
		{"go mo t", `go -C .\tool mod tidy`},
		{"go w i", `go work init .\tool`},
		{"dlv d", `dlv debug .\main.go`},
		{"gopls ch", `gopls check .\main.go`},
		{"stat", `Push-Location .\tool; try { staticcheck ./... } finally { Pop-Location }`},
	}
	for _, test := range cases {
		items := engine.Suggest(test.line, cwd, ModeSpec, 20)
		if !hasSuggestion(items, test.want) {
			t.Fatalf("partial %q did not expand to %q: %#v", test.line, test.want, items)
		}
	}
}

func hasSuggestion(items []Suggestion, want string) bool {
	for _, item := range items {
		if item.Insert == want {
			return true
		}
	}
	return false
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
