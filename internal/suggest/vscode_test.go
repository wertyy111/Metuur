package suggest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestActiveFileBridgeRejectsStaleState(t *testing.T) {
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	stateDirectory := filepath.Join(localAppData, "Metuur")
	if err := os.MkdirAll(stateDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(stateDirectory, "vscode-active-file.json")

	writeState := func(updatedAt time.Time) {
		t.Helper()
		data, err := json.Marshal(vscodeActiveFile{
			Path:      `D:\workspace\main.go`,
			Workspace: `D:\workspace`,
			UpdatedAt: updatedAt,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(statePath, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	writeState(time.Now().Add(-time.Minute))
	if _, ok := activeFileBridgeState(); ok {
		t.Fatal("stale VS Code bridge state must not override current editor detection")
	}

	writeState(time.Now())
	state, ok := activeFileBridgeState()
	if !ok || state.Path != `D:\workspace\main.go` {
		t.Fatalf("fresh VS Code bridge state was rejected: %#v, ok=%v", state, ok)
	}
}
