package ui

// This renderer is a Windows adaptation of IRIS' inline overlay model:
// https://github.com/versenilvis/IRIS/blob/d669e97423a7ca9326d17b1289d06ba90942bd77/integration/overlay.go

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"github.com/wertyy111/metuur/internal/config"
	"github.com/wertyy111/metuur/internal/suggest"
)

const (
	defaultBoxWidth  = 76
	maxVisibleItems  = 6
	descriptionWidth = 24

	ansiSaveCursor    = "\x1b7"
	ansiRestoreCursor = "\x1b8"
	ansiEraseLine     = "\x1b[2K"
	ansiAutoWrapOff   = "\x1b[?7l"
	ansiAutoWrapOn    = "\x1b[?7h"
	ansiReset         = "\x1b[0m"
	ansiShowCursor    = "\x1b[?25h"
)

var irisPalette = struct {
	Border, Accent, Muted, Text, TextSelected string
	Match, Description, DescriptionSelected   string
	SelectedBackground, Ghost, Footer         string
}{
	Border:              "#cba6f7",
	Accent:              "#89b4fa",
	Muted:               "#6c7086",
	Text:                "#a6e3a1",
	TextSelected:        "#f5e0dc",
	Match:               "#a6e3a1",
	Description:         "#6c7086",
	DescriptionSelected: "#6c7086",
	SelectedBackground:  "#313244",
	Ghost:               "#6c7086",
	Footer:              "#89b4fa",
}

var wavePalette = [...]string{
	"#cba6f7",
	"#b4befe",
	"#89b4fa",
	"#74c7ec",
	"#89dceb",
	"#94e2d5",
	"#a6e3a1",
	"#94e2d5",
}

var waveGlyphs = [...]rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█', '▇', '▆', '▅', '▄', '▃', '▂'}

type Renderer struct {
	out        io.Writer
	cfg        config.Config
	menuLines  int
	ghostWidth int
}

func New(out io.Writer, cfg config.Config) *Renderer {
	return &Renderer{out: out, cfg: cfg}
}

// ReserveHeader makes row 1 a fixed status row. The terminal scroll region
// starts at row 2, and the editable cursor is restored below that boundary.
// This prevents Backspace, wrapping, and normal child output from reaching the
// animated header.
func (r *Renderer) ReserveHeader(terminalHeight, cursorColumn, cursorRow int) {
	if terminalHeight < 2 {
		return
	}
	row := min(max(cursorRow+1, 2), terminalHeight)
	column := max(cursorColumn+1, 1)
	sequence := fmt.Sprintf("%s\x1b[?6l\x1b[2;%dr\x1b[%d;%dH%s%s",
		ansiAutoWrapOff, terminalHeight, row, column, ansiAutoWrapOn, ansiShowCursor)
	_, _ = io.WriteString(r.out, sequence)
}

// ReleaseHeader restores the terminal's normal full-height scroll region.
func (r *Renderer) ReleaseHeader() {
	_, _ = io.WriteString(r.out, "\x1b[?6l\x1b[r"+ansiAutoWrapOn+ansiShowCursor)
}

// DrawWave paints an animated header into the first terminal row without
// emitting a newline or changing the editable PowerShell cursor. One column is
// deliberately left unused so terminals with auto-wrap enabled cannot scroll.
func (r *Renderer) DrawWave(terminalWidth, frame int) {
	width := min(max(terminalWidth-1, 0), 36)
	if width == 0 {
		return
	}
	startColumn := max((terminalWidth-width)/2, 0)

	var line strings.Builder
	for column := 0; column < width; column++ {
		phase := column + frame
		line.WriteString(fg(wavePalette[phase%len(wavePalette)]))
		line.WriteRune(waveGlyphs[phase%len(waveGlyphs)])
	}

	var output strings.Builder
	output.WriteString(ansiAutoWrapOff)
	output.WriteString(ansiSaveCursor)
	output.WriteString("\x1b[1;1H")
	output.WriteString(ansiEraseLine)
	output.WriteString(cursorForward(startColumn))
	output.WriteString(line.String())
	output.WriteString(ansiReset)
	output.WriteString(ansiRestoreCursor)
	output.WriteString(ansiAutoWrapOn)
	output.WriteString(ansiShowCursor)
	_, _ = io.WriteString(r.out, output.String())
}

// ClearOverlay removes only Metuur's transient pixels. PowerShell continues to
// own and render the actual input line.
func (r *Renderer) ClearOverlay() {
	sequence := r.clearSequence()
	if sequence != "" {
		_, _ = io.WriteString(r.out, sequence)
	}
}

// Reset clears the overlay before command execution. Reserved rows are no
// longer reusable because the child command is free to scroll the terminal.
func (r *Renderer) Reset() {
	r.ClearOverlay()
	r.menuLines = 0
	r.ghostWidth = 0
}

// Draw renders ghost text and the boxed menu from the current PowerShell cursor
// without redrawing or taking ownership of the command line.
func (r *Renderer) Draw(
	buffer []rune,
	cursor int,
	suggestions []suggest.Suggestion,
	selected int,
	mode suggest.Mode,
	menuVisible bool,
	terminalWidth int,
	terminalHeight int,
	cursorColumn int,
	cursorRow int,
) {
	var output strings.Builder
	output.WriteString(r.clearSequence())

	line := string(buffer)
	inputFitsSingleRow := line == ""
	// Some VS Code/PSReadLine combinations leave the editable text painted with
	// an invisible terminal colour after an overlay update. Repaint only the
	// known input span (never the PowerShell prompt), then restore the real
	// cursor. This is visual only; PowerShell still owns the actual line.
	if cursor >= 0 && cursor <= len(buffer) && line != "" &&
		!strings.ContainsAny(line, "\r\n") {
		prefixWidth := displayWidth(string(buffer[:cursor]))
		lineWidth := displayWidth(line)
		lineStart := cursorColumn - prefixWidth
		inputFitsSingleRow = lineStart >= 0 && lineStart+lineWidth < terminalWidth
		if inputFitsSingleRow {
			output.WriteString(ansiSaveCursor)
			output.WriteString(cursorBackward(prefixWidth))
			output.WriteString(fg(irisPalette.Text))
			output.WriteString(line)
			output.WriteString(ansiReset)
			output.WriteString(ansiRestoreCursor)
		}
	}
	// Rows below a wrapped PSReadLine buffer contain the rest of the editable
	// command, not free overlay space. Leave that buffer entirely to PowerShell.
	if !inputFitsSingleRow {
		output.WriteString(ansiShowCursor)
		_, _ = io.WriteString(r.out, output.String())
		return
	}

	freeRows := terminalHeight - cursorRow - 1
	compactFallback := menuVisible && len(suggestions) > 0 && freeRows < 3
	ghost := ""
	if r.cfg.UI.GhostText {
		ghost = ghostText(line, cursor == len(buffer), suggestions, selected)
	}
	if compactFallback && cursor == len(buffer) {
		if selected < 0 || selected >= len(suggestions) {
			selected = 0
		}
		if ghost == "" {
			ghost = "  " + suggestions[selected].Insert
		}
		ghost += "  <Tab> Accept"
	}
	if ghost != "" {
		available := terminalWidth - cursorColumn
		ghost = truncate(ghost, max(available, 0))
	}
	if ghost != "" {
		output.WriteString(ansiSaveCursor)
		output.WriteString(fg(irisPalette.Ghost))
		output.WriteString(ghost)
		output.WriteString(ansiReset)
		output.WriteString(ansiRestoreCursor)
		r.ghostWidth = displayWidth(ghost)
	}

	if !menuVisible || len(suggestions) == 0 || compactFallback {
		output.WriteString(ansiShowCursor)
		_, _ = io.WriteString(r.out, output.String())
		return
	}
	if selected < 0 || selected >= len(suggestions) {
		selected = 0
	}

	// Never create rows with CRLF: that makes VS Code scroll the terminal and
	// pushes the editable PowerShell line out of view. Render only into rows
	// that already exist below the cursor and shrink the result window to fit.
	visibleItems := min(maxVisibleItems, freeRows-2)
	start, end := suggestionWindow(len(suggestions), selected, visibleItems)
	boxWidth := responsiveWidth(terminalWidth, r.cfg.UI.MaxWidth)
	targetColumn := cursorColumn
	if targetColumn+boxWidth > terminalWidth {
		targetColumn = max(terminalWidth-boxWidth, 0)
	}
	lines := r.menu(line, suggestions, selected, mode, start, end, boxWidth)

	output.WriteString(ansiAutoWrapOff)
	output.WriteString(ansiSaveCursor)
	for index, lineText := range lines {
		output.WriteString(ansiRestoreCursor)
		output.WriteString(cursorDown(index + 1))
		output.WriteByte('\r')
		output.WriteString(ansiEraseLine)
		output.WriteString(cursorForward(targetColumn))
		output.WriteString(lineText)
	}
	output.WriteString(ansiRestoreCursor)
	output.WriteString(ansiAutoWrapOn)
	output.WriteString(ansiShowCursor)
	r.menuLines = len(lines)
	_, _ = io.WriteString(r.out, output.String())
}

func (r *Renderer) clearSequence() string {
	if r.menuLines == 0 && r.ghostWidth == 0 {
		return ""
	}
	var output strings.Builder
	output.WriteString(ansiAutoWrapOff)
	output.WriteString(ansiSaveCursor)
	if r.ghostWidth > 0 {
		output.WriteString(strings.Repeat(" ", r.ghostWidth+4))
	}
	for line := 1; line <= r.menuLines; line++ {
		output.WriteString(ansiRestoreCursor)
		output.WriteString(cursorDown(line))
		output.WriteByte('\r')
		output.WriteString(ansiEraseLine)
	}
	output.WriteString(ansiRestoreCursor)
	output.WriteString(ansiAutoWrapOn)
	r.menuLines = 0
	r.ghostWidth = 0
	return output.String()
}

func (r *Renderer) menu(
	query string,
	items []suggest.Suggestion,
	selected int,
	mode suggest.Mode,
	start int,
	end int,
	boxWidth int,
) []string {
	inner := boxWidth - 2
	lines := make([]string, 0, end-start+2)
	isClassic := strings.EqualFold(r.cfg.UI.Style, "classic") || strings.EqualFold(r.cfg.UI.Style, "minimal")

	header := ""
	if len(items) > maxVisibleItems {
		header = fg(irisPalette.Border) + fmt.Sprintf(" %d/%d ", selected+1, len(items)) + ansiReset
	}
	headerPadding := 3
	if isClassic {
		headerPadding = -1
	}
	lines = append(lines, titledEdge("╭", "╮", inner, header, headerPadding))

	iconWidth := 2
	if isClassic || !r.cfg.UI.NerdFonts {
		iconWidth = 0
	}
	iconGap := 0
	if iconWidth > 0 {
		iconGap = 1
	}
	markerWidth := 1
	sidePadding := 2
	gap := 2
	titleWidth := inner - sidePadding - markerWidth - 1 - iconWidth - iconGap - gap - descriptionWidth
	if titleWidth < 8 {
		titleWidth = max(inner-markerWidth-iconWidth-6, 4)
	}
	descWidth := inner - sidePadding - markerWidth - 1 - iconWidth - iconGap - titleWidth - gap
	descWidth = max(descWidth, 0)

	for index := start; index < end; index++ {
		item := items[index]
		isSelected := index == selected
		background := ""
		if isSelected {
			background = bg(irisPalette.SelectedBackground)
		}
		marker := " "
		markerColor := irisPalette.Muted
		if isSelected {
			marker = "▶"
			markerColor = irisPalette.Accent
		}
		icon := fixedWidth(kindIcon(item.Kind), iconWidth)
		command := item.Insert
		if command == "" {
			command = item.Label
		}
		title := matchedTitle(command, query, isSelected, titleWidth)
		description := item.Description
		if item.Kind == "ai" {
			description = "ai suggestion"
		}
		description = fixedWidth(description, descWidth)
		descColor := irisPalette.Description
		if isSelected {
			descColor = irisPalette.DescriptionSelected
		}

		var row strings.Builder
		row.WriteString(fg(irisPalette.Border))
		row.WriteString("│")
		row.WriteString(ansiReset)
		row.WriteString(background)
		row.WriteString(" ")
		row.WriteString(fg(markerColor))
		if isSelected {
			row.WriteString("\x1b[1m")
		}
		row.WriteString(marker)
		row.WriteString(ansiReset)
		row.WriteString(background)
		row.WriteString(" ")
		if iconWidth > 0 {
			row.WriteString(fg(map[bool]string{true: irisPalette.Accent, false: irisPalette.Muted}[isSelected]))
			row.WriteString(icon)
			row.WriteString(ansiReset)
			row.WriteString(background)
			row.WriteString(" ")
		}
		row.WriteString(title)
		row.WriteString(background)
		row.WriteString(strings.Repeat(" ", gap))
		row.WriteString(fg(descColor))
		row.WriteString(description)
		row.WriteString(ansiReset)
		row.WriteString(background)
		row.WriteString(" ")
		row.WriteString(ansiReset)
		row.WriteString(fg(irisPalette.Border))
		row.WriteString("│")
		row.WriteString(ansiReset)
		lines = append(lines, row.String())
	}

	footer := ""
	if !isClassic {
		footer = fg(irisPalette.Footer) + " <Tab> Accept • <Ctrl+R> Mode " + ansiReset
		if mode == suggest.ModeHistory {
			footer = fg(irisPalette.Footer) + " <Tab> Accept • <Ctrl+R> Spec " + ansiReset
		}
	}
	lines = append(lines, titledEdge("╰", "╯", inner, footer, max(inner-displayWidthANSI(footer)-2, 0)))
	return lines
}

func (r *Renderer) Error(err error) {
	r.Reset()
	fmt.Fprintf(r.out, "\x1b[38;2;255;97;136mmetuur: %v\x1b[0m\r\n", err)
}

func ghostText(buffer string, cursorAtEnd bool, items []suggest.Suggestion, selected int) string {
	if buffer == "" || !cursorAtEnd || len(items) == 0 {
		return ""
	}
	if selected < 0 || selected >= len(items) {
		selected = 0
	}
	command := items[selected].Insert
	if command == "" {
		command = items[selected].Label
	}
	if strings.HasPrefix(strings.ToLower(command), strings.ToLower(buffer)) && len(command) >= len(buffer) {
		return command[len(buffer):]
	}
	return ""
}

func matchedTitle(title, typed string, selected bool, width int) string {
	display := fixedWidth(title, width)
	textColor := irisPalette.Text
	if selected {
		textColor = irisPalette.TextSelected
	}
	background := ""
	if selected {
		background = bg(irisPalette.SelectedBackground)
	}
	if typed == "" || !strings.HasPrefix(strings.ToLower(display), strings.ToLower(typed)) {
		return background + fg(textColor) + display + ansiReset
	}
	highlightWidth := min(displayWidth(typed), displayWidth(display))
	highlighted, rest := splitWidth(display, highlightWidth)
	return background + fg(irisPalette.Match) + "\x1b[1m" + highlighted + ansiReset +
		background + fg(textColor) + rest + ansiReset
}

func titledEdge(left, right string, inner int, content string, leftPadding int) string {
	contentWidth := displayWidthANSI(content)
	if content == "" {
		return fg(irisPalette.Border) + left + strings.Repeat("─", inner) + right + ansiReset
	}
	leftDashes := max(leftPadding, 0)
	if leftPadding < 0 {
		leftDashes = max((inner-contentWidth)/2, 0)
	}
	rightDashes := inner - leftDashes - contentWidth
	if rightDashes < 0 {
		leftDashes = max(leftDashes+rightDashes, 0)
		rightDashes = 0
	}
	return fg(irisPalette.Border) + left + strings.Repeat("─", leftDashes) + ansiReset +
		content + fg(irisPalette.Border) + strings.Repeat("─", rightDashes) + right + ansiReset
}

func suggestionWindow(total, selected, size int) (start, end int) {
	if size < 1 || size > total {
		size = total
	}
	start = selected - size/2
	if start < 0 {
		start = 0
	}
	end = start + size
	if end > total {
		end = total
		start = max(0, end-size)
	}
	return start, end
}

func responsiveWidth(terminalWidth, configured int) int {
	if configured <= 0 {
		configured = defaultBoxWidth
	}
	if terminalWidth <= 0 {
		return configured
	}
	width := min(configured, terminalWidth)
	if terminalWidth >= 40 {
		width = max(width, 40)
	}
	return max(width, 1)
}

func fixedWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = truncate(value, width)
	return value + strings.Repeat(" ", max(width-displayWidth(value), 0))
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	prefix, _ := splitWidth(value, width-1)
	return prefix + "…"
}

func splitWidth(value string, width int) (string, string) {
	if width <= 0 {
		return "", value
	}
	used := 0
	index := 0
	for index < len(value) {
		r, size := utf8.DecodeRuneInString(value[index:])
		runeWidth := runewidth.RuneWidth(r)
		if used+runeWidth > width {
			break
		}
		used += runeWidth
		index += size
	}
	return value[:index], value[index:]
}

func displayWidth(value string) int {
	return runewidth.StringWidth(value)
}

func displayWidthANSI(value string) int {
	plain := stripANSI(value)
	return displayWidth(plain)
}

func stripANSI(value string) string {
	var output strings.Builder
	for index := 0; index < len(value); {
		if value[index] == '\x1b' && index+1 < len(value) && value[index+1] == '[' {
			index += 2
			for index < len(value) {
				b := value[index]
				index++
				if b >= 0x40 && b <= 0x7e {
					break
				}
			}
			continue
		}
		output.WriteByte(value[index])
		index++
	}
	return output.String()
}

func kindIcon(kind string) string {
	switch kind {
	case "history":
		return ""
	case "ai", "intent":
		return "󰫢"
	default:
		return ""
	}
}

func fg(hex string) string {
	r, g, b := parseHex(hex)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}

func bg(hex string) string {
	r, g, b := parseHex(hex)
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
}

func parseHex(value string) (int, int, int) {
	value = strings.TrimPrefix(value, "#")
	if len(value) != 6 {
		return 255, 255, 255
	}
	var r, g, b int
	_, _ = fmt.Sscanf(value, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

func cursorUp(lines int) string {
	if lines <= 0 {
		return ""
	}
	return fmt.Sprintf("\x1b[%dA", lines)
}

func cursorDown(lines int) string {
	if lines <= 0 {
		return ""
	}
	return fmt.Sprintf("\x1b[%dB", lines)
}

func cursorForward(columns int) string {
	if columns <= 0 {
		return ""
	}
	return fmt.Sprintf("\x1b[%dC", columns)
}

func cursorBackward(columns int) string {
	if columns <= 0 {
		return ""
	}
	return fmt.Sprintf("\x1b[%dD", columns)
}
