package localai

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInspectEnvironmentCollectsOnlySmallWorkspaceFacts(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "go.mod"), []byte("module example.test/metuur\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cwd, "cmd", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(cwd, "cmd", "app", "main.go")
	if err := os.WriteFile(active, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cwd, "vendor", "hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "vendor", "hidden", "secret.go"), []byte("package hidden"), 0o644); err != nil {
		t.Fatal(err)
	}

	env := InspectEnvironment(cwd, active, []string{"go fmt ./..."}, "go test ./...", 0)
	if env.Module != "example.test/metuur" || env.ActiveFile != "cmd/app/main.go" {
		t.Fatalf("wrong environment: %#v", env)
	}
	if len(env.GoFiles) != 1 || env.GoFiles[0] != "cmd/app/main.go" {
		t.Fatalf("unexpected scanned files: %#v", env.GoFiles)
	}
}

func TestGitSummaryAlwaysReturnsPromptly(t *testing.T) {
	started := time.Now()
	_ = gitSummary(t.TempDir())
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("git summary blocked for %s", elapsed)
	}
}
