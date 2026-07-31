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
	if !strings.Contains(rendered, "METUUR") || !strings.Contains(rendered, "4/5") {
		t.Fatalf("top border should contain the Metuur logo and counter: %q", rendered)
	}
}

func TestRendererUsesConfiguredPalette(t *testing.T) {
	var output bytes.Buffer
	cfg := config.Default()
	renderer := New(&output, cfg)
	renderer.Redraw([]rune("go"), 2, []suggest.Suggestion{{Label: "go run .", Insert: "go run .", Kind: "ai"}}, 0, suggest.ModeSpec, true)
	rendered := output.String()
	for _, color := range []string{cfg.Theme.Accent, cfg.Theme.Logo, cfg.Theme.Command, cfg.Theme.Selected} {
		if !strings.Contains(rendered, "38;5;"+color+"m") && !strings.Contains(rendered, "48;5;"+color+"m") {
			t.Fatalf("configured color %s is absent from rendered UI: %q", color, rendered)
		}
	}
}
