package suggest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wertyy111/metuur/internal/history"
)

func TestIntentUnderstandsNaturalLanguageAndWorkspace(t *testing.T) {
	t.Setenv("METUUR_DISABLE_VSCODE_ACTIVE", "1")
	cwd := t.TempDir()
	mainFile := filepath.Join(cwd, "main.go")
	if err := os.WriteFile(mainFile, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	engine, err := New(history.Load(filepath.Join(t.TempDir(), "history.txt"), 100), false)
	if err != nil {
		t.Fatal(err)
	}

	for _, phrase := range []string{"запусти открытый файл", "pfgencnb afqk", "запуст программу"} {
		items := engine.Suggest(phrase, cwd, ModeSpec, 5)
		if len(items) == 0 || items[0].Kind != "intent" || items[0].Insert != `go run .\main.go` {
			t.Fatalf("intent %q was not understood: %#v", phrase, items)
		}
	}
}

func TestIntentMapsProjectActions(t *testing.T) {
	t.Setenv("METUUR_DISABLE_VSCODE_ACTIVE", "1")
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "go.mod"), []byte("module example.test/app\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	engine, err := New(history.Load(filepath.Join(t.TempDir(), "history.txt"), 100), false)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"собери проект":         "go build .",
		"отформатируй весь код": "go fmt ./...",
		"проверь тесты":         "go test ./...",
		"обнови зависимости":    "go mod tidy",
		"очисти кэш":            "go clean -cache -testcache",
		"проверь на уязвимости": "govulncheck ./...",
	}
	for phrase, want := range cases {
		items := engine.Suggest(phrase, cwd, ModeSpec, 5)
		if len(items) == 0 || items[0].Insert != want {
			t.Errorf("%q: want %q, got %#v", phrase, want, items)
		}
	}
}
