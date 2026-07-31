package localai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientCompletesCommandWithWorkspaceContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected endpoint: %s", request.URL.Path)
		}
		var body chatRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "tiny-coder" || !strings.Contains(body.Messages[1].Content, "cmd/server/main.go") {
			t.Fatalf("workspace context is missing: %#v", body)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "```powershell\ngo run .\\cmd\\server\n```"}}},
		})
	}))
	defer server.Close()

	client := NewClient(ProviderConfig{Endpoint: server.URL + "/v1", Model: "tiny-coder", Timeout: time.Second})
	command, err := client.Suggest(context.Background(), "go r", Environment{CWD: `D:\work`, GoFiles: []string{"cmd/server/main.go"}, Candidates: []string{`go run .\cmd\server`}})
	if err != nil {
		t.Fatal(err)
	}
	if command != `go run .\cmd\server` {
		t.Fatalf("unexpected completion: %q", command)
	}
}

func TestClientConvertsIntentAndRejectsUnsafeOutput(t *testing.T) {
	if got := normalizeCompletion("запусти сервер", `go run .\cmd\server`, nil); got != `go run .\cmd\server` {
		t.Fatalf("natural language intent was not converted: %q", got)
	}
	for _, unsafe := range []string{`go test ./...; Remove-Item -Recurse .`, `go test ./... | Out-File log.txt`, `powershell.exe -Command calc`} {
		if got := normalizeCompletion("проверь проект", unsafe, nil); got != "" {
			t.Fatalf("unsafe output accepted: %q", got)
		}
	}
}

func TestClientAcceptsOnlyWorkspaceCandidates(t *testing.T) {
	if got := normalizeCompletion("go tes", "go test -invented", []string{"go test ./...", "go test ."}); got != "" {
		t.Fatalf("invented command was accepted: %q", got)
	}
	if got := normalizeCompletion("go tes", "go test ./...", []string{"go test ./...", "go test ."}); got != "go test ./..." {
		t.Fatalf("valid candidate was rejected: %q", got)
	}
}

func TestCompleterDebouncesAndCancelsOldRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "go test ./..."}}},
		})
	}))
	defer server.Close()

	completer := NewCompleter(ProviderConfig{Endpoint: server.URL, Model: "tiny", Timeout: time.Second}, 30*time.Millisecond)
	defer completer.Close()
	environment := Environment{Candidates: []string{"go test ./..."}}
	_ = completer.Request("go t", environment)
	_ = completer.Request("go te", environment)
	_ = completer.Request("go tes", environment)
	select {
	case result := <-completer.Results():
		if result.Query != "go tes" || result.Command != "go test ./..." {
			t.Fatalf("wrong debounced result: %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for completion")
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one provider call, got %d", calls.Load())
	}
}

func TestCheckOllamaFindsConfiguredModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/tags" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"models": []map[string]string{{"name": "qwen2.5-coder:0.5b"}}})
	}))
	defer server.Close()
	status, err := CheckOllama(context.Background(), server.URL+"/v1", "qwen2.5-coder:0.5b")
	if err != nil || !status.Online || !status.HasModel {
		t.Fatalf("unexpected status: %#v, %v", status, err)
	}
}

func TestCheckOpenAIFindsPortableModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"data": []map[string]string{{"id": "qwen2.5-coder:0.5b"}},
		})
	}))
	defer server.Close()
	status, err := CheckProvider(context.Background(), "portable", server.URL+"/v1", "qwen2.5-coder:0.5b")
	if err != nil || !status.Online || !status.HasModel {
		t.Fatalf("unexpected status: %#v, %v", status, err)
	}
}
