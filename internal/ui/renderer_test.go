package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wertyy111/metuur/internal/config"
	"github.com/wertyy111/metuur/internal/suggest"
)

func TestRendererUsesIRISOverlayWithoutRedrawingPrompt(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output, config.Default())
	items := []suggest.Suggestion{
		{Insert: "go build ./...", Description: "compile packages", Kind: "build"},
		{Insert: "go run .\\main.go", Description: "run file", Kind: "run"},
	}
	renderer.Draw([]rune("go bu"), 5, items, 0, suggest.ModeSpec, true, 120, 12)
	rendered := output.String()
	plain := stripANSI(rendered)
	if strings.Contains(plain, "λ ") || strings.Contains(plain, "PS ") {
		t.Fatalf("overlay must not redraw the real PowerShell prompt: %q", plain)
	}
	for _, want := range []string{"╭", "▶", "go build ./...", "<Tab> Accept", "<Ctrl+R> Mode", "╯"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("IRIS element %q is missing: %q", want, plain)
		}
	}
	if !strings.Contains(rendered, "38;2;162;119;255") || !strings.Contains(rendered, "48;2;61;55;94") {
		t.Fatalf("IRIS border/selection palette is missing: %q", rendered)
	}
	if !strings.Contains(rendered, "\r\n\r\n\r\n\r\n\x1b[4A") {
		t.Fatalf("two rows plus borders must reserve four terminal lines: %q", rendered)
	}
}

func TestRendererShowsCounterOnlyForScrollableResults(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output, config.Default())
	items := make([]suggest.Suggestion, 8)
	for index := range items {
		items[index] = suggest.Suggestion{Insert: "go item " + string(rune('a'+index)), Description: "item"}
	}
	renderer.Draw([]rune("go"), 2, items, 6, suggest.ModeSpec, true, 100, 4)
	plain := stripANSI(output.String())
	if !strings.Contains(plain, "7/8") || strings.Contains(plain, "go item a") || !strings.Contains(plain, "go item g") {
		t.Fatalf("scroll counter/window differs from IRIS: %q", plain)
	}
}

func TestRendererShowsGhostTextWhenMenuIsHidden(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output, config.Default())
	renderer.Draw(
		[]rune("gofm"),
		4,
		[]suggest.Suggestion{{Insert: "gofmt -w .", Kind: "format"}},
		0,
		suggest.ModeSpec,
		false,
		100,
		10,
	)
	plain := stripANSI(output.String())
	if !strings.Contains(plain, "t -w .") {
		t.Fatalf("inline ghost completion is missing: %q", plain)
	}
	if strings.Contains(plain, "╭") {
		t.Fatalf("boxed menu must stay hidden: %q", plain)
	}
}

func TestResponsiveWidthNeverExceedsTerminal(t *testing.T) {
	for _, test := range []struct{ terminal, want int }{{120, 76}, {60, 60}, {30, 30}} {
		if got := responsiveWidth(test.terminal, 76); got != test.want {
			t.Fatalf("responsiveWidth(%d) = %d, want %d", test.terminal, got, test.want)
		}
	}
}
