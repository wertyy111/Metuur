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
	renderer.Draw([]rune("go bu"), 5, items, 0, suggest.ModeSpec, true, 120, 30, 12, 2)
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
	if strings.Contains(rendered, "\r\n") {
		t.Fatalf("overlay must not create rows or scroll the terminal: %q", rendered)
	}
}

func TestRendererWaveUsesOneRowAndPreservesInputCursor(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output, config.Default())
	renderer.DrawWave(40, 3)

	rendered := output.String()
	if strings.ContainsAny(rendered, "\r\n") {
		t.Fatalf("wave must stay on one terminal row: %q", rendered)
	}
	for _, want := range []string{
		ansiSaveCursor,
		"\x1b[1;1H",
		ansiEraseLine,
		ansiRestoreCursor,
		ansiShowCursor,
		"38;2;162;119;255",
		"38;2;97;255;202",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("wave output is missing %q: %q", want, rendered)
		}
	}
	glyphCount := 0
	for _, glyph := range "▁▂▃▄▅▆▇█" {
		glyphCount += strings.Count(rendered, string(glyph))
	}
	if glyphCount != 39 {
		t.Fatalf("wave glyph count = %d, want 39: %q", glyphCount, rendered)
	}
}

func TestRendererShowsCounterOnlyForScrollableResults(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output, config.Default())
	items := make([]suggest.Suggestion, 8)
	for index := range items {
		items[index] = suggest.Suggestion{Insert: "go item " + string(rune('a'+index)), Description: "item"}
	}
	renderer.Draw([]rune("go"), 2, items, 6, suggest.ModeSpec, true, 100, 30, 4, 2)
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
		30,
		10,
		2,
	)
	plain := stripANSI(output.String())
	if !strings.Contains(plain, "t -w .") {
		t.Fatalf("inline ghost completion is missing: %q", plain)
	}
	if strings.Contains(plain, "╭") {
		t.Fatalf("boxed menu must stay hidden: %q", plain)
	}
}

func TestRendererKeepsInputAndCursorVisible(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output, config.Default())
	renderer.Draw(
		[]rune("go"),
		2,
		[]suggest.Suggestion{{Insert: "go run .", Kind: "run"}},
		0,
		suggest.ModeSpec,
		false,
		100,
		30,
		12,
		2,
	)
	rendered := output.String()
	if !strings.Contains(rendered, "\x1b[2D") ||
		!strings.Contains(rendered, fg(irisPalette.Text)+"go") {
		t.Fatalf("editable input was not repainted visibly: %q", rendered)
	}
	if !strings.Contains(rendered, ansiShowCursor) {
		t.Fatalf("renderer did not restore the visible cursor: %q", rendered)
	}
}

func TestRendererShrinksWithoutScrollingInShortTerminal(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output, config.Default())
	items := make([]suggest.Suggestion, 8)
	for index := range items {
		items[index] = suggest.Suggestion{Insert: "go item " + string(rune('a'+index)), Description: "item"}
	}
	renderer.Draw([]rune("go"), 2, items, 0, suggest.ModeSpec, true, 80, 8, 5, 3)
	rendered := output.String()
	plain := stripANSI(rendered)
	if strings.Contains(rendered, "\r\n") {
		t.Fatalf("short overlay scrolled the terminal: %q", rendered)
	}
	if !strings.Contains(plain, "go item a") || strings.Contains(plain, "go item d") {
		t.Fatalf("short overlay did not fit available rows: %q", plain)
	}
}

func TestRendererUsesInlineFallbackOnLastTerminalRow(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output, config.Default())
	renderer.Draw(
		[]rune("go"), 2,
		[]suggest.Suggestion{{Insert: "go run .", Kind: "run"}},
		0, suggest.ModeSpec, true,
		80, 8, 5, 7,
	)
	rendered := output.String()
	plain := stripANSI(rendered)
	if !strings.Contains(plain, " run .") || !strings.Contains(plain, "<Tab> Accept") {
		t.Fatalf("last-row fallback is not actionable: %q", plain)
	}
	if strings.Contains(rendered, ansiEraseLine) || strings.Contains(rendered, "\r\n") {
		t.Fatalf("last-row fallback modified terminal rows: %q", rendered)
	}
}

func TestRendererNeverOverwritesWrappedInputTail(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output, config.Default())
	line := strings.Repeat("x", 50)
	renderer.Draw(
		[]rune(line), 20,
		[]suggest.Suggestion{{Insert: line + " suffix", Kind: "run"}},
		0, suggest.ModeSpec, true,
		30, 10, 5, 3,
	)
	rendered := output.String()
	if strings.Contains(rendered, ansiEraseLine) || strings.Contains(rendered, "<Tab>") ||
		strings.Contains(stripANSI(rendered), "suffix") {
		t.Fatalf("wrapped input tail was touched by the overlay: %q", rendered)
	}
	if !strings.Contains(rendered, ansiShowCursor) {
		t.Fatalf("wrapped input cursor was not restored: %q", rendered)
	}
}

func TestRendererNeverOverwritesMultilinePaste(t *testing.T) {
	var output bytes.Buffer
	renderer := New(&output, config.Default())
	line := "go test\nnext"
	renderer.Draw(
		[]rune(line), len([]rune(line)),
		[]suggest.Suggestion{{Insert: "go test ./...", Kind: "test"}},
		0, suggest.ModeSpec, true,
		80, 20, 4, 3,
	)
	rendered := output.String()
	if strings.Contains(rendered, ansiEraseLine) || strings.Contains(rendered, "<Tab>") ||
		strings.Contains(stripANSI(rendered), "go test ./...") {
		t.Fatalf("multiline paste was touched by the overlay: %q", rendered)
	}
	if !strings.Contains(rendered, ansiShowCursor) {
		t.Fatalf("multiline paste cursor was not restored: %q", rendered)
	}
}

func TestResponsiveWidthNeverExceedsTerminal(t *testing.T) {
	for _, test := range []struct{ terminal, want int }{{120, 76}, {60, 60}, {30, 30}} {
		if got := responsiveWidth(test.terminal, 76); got != test.want {
			t.Fatalf("responsiveWidth(%d) = %d, want %d", test.terminal, got, test.want)
		}
	}
}
