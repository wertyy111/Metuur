package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wertyy111/metuur/internal/config"
	"github.com/wertyy111/metuur/internal/suggest"
)

func TestRendererReservesRowsBelowPrompt(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output, config.Default())
	items := []suggest.Suggestion{
		{Label: "go", Insert: "go ", Description: "Go"},
		{Label: "gofmt", Insert: "gofmt ", Description: "format"},
	}

	renderer.Redraw([]rune("g"), 1, items, 0, suggest.ModeSpec, true)
	rendered := output.String()
	// Two items plus the top and bottom borders require four reserved rows.
	if !strings.Contains(rendered, "\r\n\r\n\r\n\r\n\x1b[4A") {
		t.Fatalf("menu rows were not reserved before drawing: %q", rendered)
	}

	output.Reset()
	renderer.Redraw([]rune("go"), 2, items, 0, suggest.ModeSpec, true)
	if strings.Contains(output.String(), "\r\n") {
		t.Fatalf("redraw reserved rows again and would scroll the terminal: %q", output.String())
	}
}

func TestRendererUsesScrollableSuggestionWindow(t *testing.T) {
	var output bytes.Buffer
	cfg := config.Default()
	cfg.MaxSuggestions = 3
	renderer := New(&output, cfg)
	items := []suggest.Suggestion{
		{Label: "build", Insert: "go build "},
		{Label: "clean", Insert: "go clean "},
		{Label: "run", Insert: "go run "},
		{Label: "test", Insert: "go test "},
		{Label: "vet", Insert: "go vet "},
	}

	renderer.Redraw([]rune("go"), 2, items, 3, suggest.ModeSpec, true)
	rendered := output.String()
	if !strings.Contains(rendered, "4/5") || !strings.Contains(rendered, "test") ||
		!strings.Contains(rendered, "vet") || strings.Contains(rendered, "build") {
		t.Fatalf("suggestion window did not follow selection: %q", rendered)
	}
	if strings.Contains(rendered, "Metuur 0.2.1") || !strings.Contains(rendered, "╭─ 4/5") {
		t.Fatalf("top border should contain only the counter: %q", rendered)
	}
}
