package localai

import (
	"path/filepath"
	"testing"
)

func TestModelLearnsCommandsPerWorkspace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "micro-ai.json")
	model := Load(path)
	cwd := filepath.Join(t.TempDir(), "project")
	model.Learn(`go run .\task.go`, cwd)
	model.Learn(`go run .\task.go`, cwd)
	model.Learn(`go test ./...`, t.TempDir())

	predictions := model.Predict("go r", cwd, 5)
	if len(predictions) == 0 || predictions[0].Command != `go run .\task.go` {
		t.Fatalf("workspace command was not learned: %#v", predictions)
	}
	if model.Score(`go run .\task.go`, cwd) <= model.Score(`go test ./...`, cwd) {
		t.Fatal("workspace-specific command should receive a larger score")
	}

	reloaded := Load(path)
	if reloaded.Stats().Commands != 2 {
		t.Fatalf("saved model was not loaded: %#v", reloaded.Stats())
	}
}

func TestModelIgnoresNonGoCommands(t *testing.T) {
	model := NewMemory()
	model.Learn("Remove-Item important.txt", t.TempDir())
	if model.Stats().Commands != 0 {
		t.Fatal("non-Go command entered the local model")
	}
}
