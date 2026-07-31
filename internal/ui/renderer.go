package ui

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/wertyy111/metuur/internal/config"
	"github.com/wertyy111/metuur/internal/suggest"
)

const (
	boxWidth     = 76
	contentWidth = boxWidth - 2
	labelWidth   = 36
)

type Renderer struct {
	out       io.Writer
	cfg       config.Config
	menuLines int
}

func New(out io.Writer, cfg config.Config) *Renderer {
	return &Renderer{out: out, cfg: cfg}
}

func (r *Renderer) Redraw(buffer []rune, cursor int, suggestions []suggest.Suggestion, selected int, mode suggest.Mode, menuVisible bool) {
	r.clearMenu()
	line := string(buffer)
	fmt.Fprintf(
		r.out,
		"\x1b[?25l\r\x1b[2K\x1b[38;5;%sm%s\x1b[0m\x1b[38;5;121m%s\x1b[0m",
		r.cfg.Theme.Accent,
		r.cfg.Prompt,
		line,
	)

	if menuVisible && selected >= 0 && selected < len(suggestions) && cursor == len(buffer) {
		insert := suggestions[selected].Insert
		if strings.HasPrefix(strings.ToLower(insert), strings.ToLower(line)) {
			insertRunes := []rune(insert)
			if len(insertRunes) > len(buffer) {
				fmt.Fprintf(r.out, "\x1b[38;5;%sm%s\x1b[0m", r.cfg.Theme.Muted, string(insertRunes[len(buffer):]))
			}
		}
	}

	column := utf8.RuneCountInString(r.cfg.Prompt) + cursor
	fmt.Fprint(r.out, "\r")
	if column > 0 {
		fmt.Fprintf(r.out, "\x1b[%dC", column)
	}
	if !menuVisible || len(suggestions) == 0 {
		fmt.Fprint(r.out, "\x1b[?25h")
		return
	}

	lines := r.menu(line, suggestions, selected, mode)
	r.reserveMenuSpace(len(lines), column)
	fmt.Fprint(r.out, "\x1b7")
	for i, renderLine := range lines {
		fmt.Fprintf(r.out, "\x1b8\x1b[%dB\r\x1b[2K%s", i+1, renderLine)
	}
	fmt.Fprint(r.out, "\x1b8\x1b[?25h")
}

func (r *Renderer) reserveMenuSpace(lines, column int) {
	additional := lines - r.menuLines
	if additional <= 0 {
		return
	}
	for range additional {
		fmt.Fprint(r.out, "\r\n")
	}
	fmt.Fprintf(r.out, "\x1b[%dA\r", additional)
	if column > 0 {
		fmt.Fprintf(r.out, "\x1b[%dC", column)
	}
	r.menuLines = lines
}

func (r *Renderer) menu(line string, suggestions []suggest.Suggestion, selected int, mode suggest.Mode) []string {
	counter := fmt.Sprintf("%d/%d", selected+1, len(suggestions))
	top := borderLine("╭─ "+counter+" ", "╮")
	start, end := suggestionWindow(len(suggestions), selected, r.cfg.MaxSuggestions)
	lines := make([]string, 0, end-start+2)
	lines = append(lines, r.accent(top))

	for i := start; i < end; i++ {
		item := suggestions[i]
		selector := " "
		if i == selected {
			selector = ">"
		}
		icon := kindIcon(item.Kind)
		descriptionWidth := contentWidth - 5 - labelWidth
		label := fit(item.Label, labelWidth)
		description := ""
		if r.cfg.ShowDescriptions {
			description = fit(item.Description, descriptionWidth)
		} else {
			description = strings.Repeat(" ", descriptionWidth)
		}
		content := " " + selector + " " + icon + " " + label + description
		content = fit(content, contentWidth)

		if i == selected {
			lines = append(lines, r.accent("│")+
				fmt.Sprintf("\x1b[48;5;%sm\x1b[38;5;255m%s\x1b[0m", r.cfg.Theme.Selected, content)+
				r.accent("│"))
			continue
		}

		left := " " + selector + " " + icon + " "
		lines = append(lines,
			r.accent("│")+
				fmt.Sprintf("\x1b[38;5;121m%s%s\x1b[0m", left, label)+
				fmt.Sprintf("\x1b[38;5;%sm%s\x1b[0m", r.cfg.Theme.Muted, description)+
				r.accent("│"),
		)
	}

	modeText := strings.ToUpper(string(mode))
	help := "<Tab> вставить · <Enter> запуск · ↑↓ · <Esc> скрыть · " + modeText
	bottom := borderLine("╰─ "+help+" ", "╯")
	lines = append(lines, r.accent(bottom))
	return lines
}

func suggestionWindow(total, selected, size int) (start, end int) {
	if size < 1 || size > total {
		size = total
	}
	if selected < 0 {
		selected = 0
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

func (r *Renderer) PrepareCommand(line string) {
	r.clearMenu()
	r.menuLines = 0
	fmt.Fprintf(
		r.out,
		"\x1b[?25h\r\x1b[2K\x1b[38;5;%sm%s\x1b[0m\x1b[38;5;121m%s\x1b[0m\r\n",
		r.cfg.Theme.Accent,
		r.cfg.Prompt,
		line,
	)
}

func (r *Renderer) ClearLine() {
	r.clearMenu()
	r.menuLines = 0
	fmt.Fprint(r.out, "\x1b[?25h\r\x1b[2K")
}

func (r *Renderer) ClearScreen() {
	r.menuLines = 0
	fmt.Fprint(r.out, "\x1b[2J\x1b[H")
}

func (r *Renderer) Error(err error) {
	fmt.Fprintf(r.out, "\x1b[38;5;203mmetuur: %v\x1b[0m\r\n", err)
}

func (r *Renderer) clearMenu() {
	if r.menuLines == 0 {
		return
	}
	fmt.Fprint(r.out, "\x1b[?25l\x1b7")
	for i := 1; i <= r.menuLines; i++ {
		fmt.Fprintf(r.out, "\x1b8\x1b[%dB\r\x1b[2K", i)
	}
	fmt.Fprint(r.out, "\x1b8\x1b[?25h")
}

func (r *Renderer) accent(value string) string {
	return fmt.Sprintf("\x1b[38;5;%sm%s\x1b[0m", r.cfg.Theme.Accent, value)
}

func borderLine(start, end string) string {
	remaining := boxWidth - utf8.RuneCountInString(start) - utf8.RuneCountInString(end)
	if remaining < 0 {
		remaining = 0
	}
	return start + strings.Repeat("─", remaining) + end
}

func fit(value string, width int) string {
	runes := []rune(value)
	if len(runes) > width {
		if width <= 1 {
			return string(runes[:width])
		}
		runes = append(runes[:width-1], '…')
	}
	return string(runes) + strings.Repeat(" ", width-len(runes))
}

func kindIcon(kind string) string {
	switch kind {
	case "history":
		return "◷"
	case "file":
		return "▱"
	case "run":
		return "▷"
	case "format":
		return "◇"
	case "build":
		return "⬡"
	case "workspace":
		return "◆"
	case "git":
		return "◆"
	case "intent", "ai":
		return "*"
	default:
		return "›"
	}
}
