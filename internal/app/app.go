package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/wertyy111/metuur/internal/config"
	winconsole "github.com/wertyy111/metuur/internal/console"
	"github.com/wertyy111/metuur/internal/history"
	"github.com/wertyy111/metuur/internal/localai"
	"github.com/wertyy111/metuur/internal/shell"
	"github.com/wertyy111/metuur/internal/suggest"
	"github.com/wertyy111/metuur/internal/ui"
)

const (
	overlayDelay        = 20 * time.Millisecond
	escapeSequenceDelay = 35 * time.Millisecond
	waveFrameDelay      = 90 * time.Millisecond
)

func Run(cfg config.Config, version string) error {
	_ = version
	terminal, err := winconsole.Open()
	if err != nil {
		return err
	}
	defer terminal.Close()

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	width, height, _, _ := terminal.Size()
	session, err := shell.StartInteractive(cfg.Shell, cwd, width, height)
	if err != nil {
		return err
	}
	defer session.Close()

	store := history.Load(config.HistoryPath(), cfg.MaxHistory)
	engine, err := suggest.New(store, cfg.ShowHiddenFiles)
	if err != nil {
		return err
	}
	if cfg.LocalAIEnabled {
		engine.SetLocalAI(localai.Load(config.ModelPath()))
	}

	var aiCompleter *localai.Completer
	if cfg.AI.Enabled {
		aiDataDir := config.AIDataDir(cfg)
		if strings.EqualFold(cfg.AI.Provider, "portable") && localai.PortableReady(aiDataDir) {
			go func() { _ = localai.StartPortable(aiDataDir, cfg.AI.Endpoint, cfg.AI.Model) }()
		}
		apiKey := ""
		if cfg.AI.APIKeyEnv != "" {
			apiKey = os.Getenv(cfg.AI.APIKeyEnv)
		}
		aiCompleter = localai.NewCompleter(localai.ProviderConfig{
			Endpoint: cfg.AI.Endpoint,
			Model:    cfg.AI.Model,
			APIKey:   apiKey,
			Timeout:  time.Duration(cfg.AI.TimeoutMS) * time.Millisecond,
		}, time.Duration(cfg.AI.DebounceMS)*time.Millisecond)
		defer aiCompleter.Close()
	}

	screen := &lockedWriter{writer: os.Stdout}
	childOutputEvents := make(chan struct{}, 1)
	childScreen := &notifyingWriter{writer: screen, events: childOutputEvents}
	renderer := ui.New(screen, cfg)
	defer renderer.ReleaseHeader()
	type promptEvent struct {
		state        shell.PromptState
		acknowledged chan struct{}
	}
	promptEvents := make(chan promptEvent)
	lifecycleDone := make(chan struct{})
	defer close(lifecycleDone)
	streamDone := make(chan error, 1)
	go func() {
		streamDone <- session.Stream(childScreen, func(state shell.PromptState) {
			event := promptEvent{state: state, acknowledged: make(chan struct{})}
			select {
			case promptEvents <- event:
			case <-lifecycleDone:
				return
			}
			// Do not expose the visible prompt until the app has switched from
			// pass-through execution to editable-line tracking. Otherwise a fast
			// keypress can be echoed by PowerShell but missed by Metuur's overlay.
			select {
			case <-event.acknowledged:
			case <-lifecycleDone:
			}
		})
	}()
	waitDone := make(chan error, 1)
	waitContext, cancelWait := context.WithCancel(context.Background())
	defer cancelWait()
	go func() { waitDone <- session.Wait(waitContext) }()

	type inputResult struct {
		data              []byte
		err               error
		flushPendingInput uint64
	}
	inputEvents := make(chan inputResult, 32)
	go func() {
		buffer := make([]byte, 256)
		for {
			n, readErr := terminal.Read(buffer)
			if n > 0 {
				chunk := append([]byte(nil), buffer[:n]...)
				inputEvents <- inputResult{data: chunk}
			}
			if readErr != nil {
				inputEvents <- inputResult{err: readErr}
				return
			}
		}
	}()

	var (
		buffer             []rune
		cursor             int
		selected           int
		mode               = suggest.ModeSpec
		suggestions        []suggest.Suggestion
		historyItems       = store.Commands()
		aiSuggestion       *suggest.Suggestion
		aiQuery            string
		lastAIQuery        string
		userNavigated      bool
		suggestionsEnabled = true
		hiddenUntilInput   bool
		ready              bool
		executing          = true
		waveActive         bool
		waveFrame          int
		lastCommand        string
		commandCWD         = cwd
		lastExitCode       int
		decoder            inputDecoder
		escapeGeneration   uint64
		lastWidth          = width
		lastHeight         = height
	)

	recompute := func() {
		query := string(buffer)
		if query == "" && mode == suggest.ModeSpec {
			suggestions = nil
		} else {
			suggestions = engine.Suggest(query, cwd, mode, max(100, cfg.MaxSuggestions*20))
		}
		if aiSuggestion != nil && aiQuery == query && mode == suggest.ModeSpec {
			suggestions = mergeAISuggestion(suggestions, *aiSuggestion)
		}
		if len(suggestions) == 0 {
			selected = -1
		} else if selected < 0 || selected >= len(suggestions) {
			selected = 0
		}
	}

	resetSelection := func() {
		selected = 0
		userNavigated = false
	}

	redraw := func() {
		if !ready || executing {
			return
		}
		terminalWidth, terminalHeight, cursorColumn, cursorRow := terminal.Size()
		menuVisible := suggestionsEnabled && !hiddenUntilInput && len(suggestions) > 0
		renderer.Draw(
			buffer, cursor, suggestions, selected, mode, menuVisible,
			terminalWidth, terminalHeight, cursorColumn, cursorRow,
		)
	}

	var renderTimer *time.Timer
	var renderTimerC <-chan time.Time
	scheduleRender := func() {
		if renderTimer == nil {
			renderTimer = time.NewTimer(overlayDelay)
		} else {
			if !renderTimer.Stop() {
				select {
				case <-renderTimer.C:
				default:
				}
			}
			renderTimer.Reset(overlayDelay)
		}
		renderTimerC = renderTimer.C
	}
	stopRender := func() {
		if renderTimer != nil && !renderTimer.Stop() {
			select {
			case <-renderTimer.C:
			default:
			}
		}
		renderTimerC = nil
	}

	scheduleAI := func() {
		if aiCompleter == nil {
			return
		}
		query := string(buffer)
		if mode != suggest.ModeSpec || executing || len([]rune(query)) < 3 {
			aiCompleter.Cancel()
			lastAIQuery = ""
			aiSuggestion = nil
			aiQuery = ""
			return
		}
		if query == lastAIQuery {
			return
		}
		lastAIQuery = query
		aiSuggestion = nil
		aiQuery = ""
		activeFile, _ := engine.ActiveGoFile(cwd)
		environment := localai.Environment{
			CWD:            cwd,
			ActiveFile:     activeFile,
			RecentCommands: append([]string(nil), historyItems...),
			LastCommand:    lastCommand,
			LastExitCode:   lastExitCode,
		}
		for index, item := range suggestions {
			if index >= 16 {
				break
			}
			environment.Candidates = append(environment.Candidates, item.Insert)
		}
		if len(environment.Candidates) > 0 {
			_ = aiCompleter.Request(query, environment)
		}
	}

	replaceChildLine := func(value string, appendSpace bool) error {
		value = strings.TrimSpace(value)
		if appendSpace && value != "" && !strings.HasSuffix(value, "/") && !strings.HasSuffix(value, `\`) {
			value += " "
		}
		renderer.ClearOverlay()
		if err := replacePowerShellLine(session, buffer, value); err != nil {
			return err
		}
		buffer = []rune(value)
		cursor = len(buffer)
		resetSelection()
		recompute()
		scheduleAI()
		scheduleRender()
		return nil
	}

	resizeTicker := time.NewTicker(250 * time.Millisecond)
	defer resizeTicker.Stop()
	waveTicker := time.NewTicker(waveFrameDelay)
	defer waveTicker.Stop()
	var aiResults <-chan localai.Completion
	if aiCompleter != nil {
		aiResults = aiCompleter.Results()
	}

	for {
		select {
		case event := <-promptEvents:
			state := event.state
			renderer.Reset()
			if executing && strings.TrimSpace(lastCommand) != "" {
				store.Add(lastCommand)
				historyItems = append(historyItems, lastCommand)
				if len(historyItems) > cfg.MaxHistory {
					historyItems = historyItems[len(historyItems)-cfg.MaxHistory:]
				}
				if state.ExitCode == 0 {
					engine.Learn(lastCommand, commandCWD)
				}
			}
			if state.CWD != "" {
				cwd = state.CWD
				_ = os.Chdir(cwd)
			}
			lastExitCode = state.ExitCode
			ready = true
			executing = false
			buffer = nil
			cursor = 0
			mode = suggest.ModeSpec
			hiddenUntilInput = false
			resetSelection()
			recompute()
			terminalWidth, terminalHeight, cursorColumn, cursorRow := terminal.Size()
			lastWidth, lastHeight = terminalWidth, terminalHeight
			renderer.ReserveHeader(terminalHeight, cursorColumn, cursorRow)
			waveActive = true
			waveFrame = 0
			renderer.DrawWave(terminalWidth, waveFrame)
			close(event.acknowledged)

		case completion, ok := <-aiResults:
			if !ok {
				// A closed async channel must be disabled. Receiving its zero
				// value forever used to starve keyboard events and freeze Metuur.
				aiResults = nil
				continue
			}
			if completion.Err == nil && completion.Command != "" && completion.Query == string(buffer) &&
				mode == suggest.ModeSpec && !userNavigated && !executing {
				aiQuery = completion.Query
				aiSuggestion = &suggest.Suggestion{
					Label:       completion.Command,
					Insert:      completion.Command,
					Description: "ai suggestion",
					Kind:        "ai",
					Score:       550,
				}
				selected = 0
				recompute()
				scheduleRender()
			}

		case <-childOutputEvents:
			// PSReadLine echoes input asynchronously. On a cold ConPTY runner its
			// cursor update can arrive after the input-side render timer. Debounce
			// again from visible child output so Draw observes the settled cursor
			// and gets another chance to publish the full overlay.
			if ready && !executing && len(buffer) > 0 {
				scheduleRender()
			}

		case result := <-inputEvents:
			if result.err != nil {
				renderer.Reset()
				return result.err
			}
			if executing || !ready {
				if _, err := session.Write(result.data); err != nil {
					return err
				}
				continue
			}

			var strokes []inputStroke
			if result.flushPendingInput != 0 {
				if result.flushPendingInput != escapeGeneration {
					continue
				}
				strokes = decoder.FlushPending()
			} else {
				escapeGeneration++ // invalidate an older ambiguity timer
				strokes = decoder.Feed(result.data)
				if decoder.HasPending() {
					escapeGeneration++
					generation := escapeGeneration
					time.AfterFunc(escapeSequenceDelay, func() {
						select {
						case inputEvents <- inputResult{flushPendingInput: generation}:
						default:
						}
					})
				}
			}
			for _, stroke := range strokes {
				if executing {
					if _, err := session.Write(stroke.raw); err != nil {
						return err
					}
					continue
				}
				renderer.ClearOverlay()
				stopRender()

				switch stroke.kind {
				case strokeRune:
					hiddenUntilInput = false
					buffer = insertRune(buffer, cursor, stroke.runeValue)
					cursor++
					resetSelection()
					_, err = session.Write(stroke.raw)

				case strokeBackspace:
					hiddenUntilInput = false
					if cursor > 0 {
						buffer = append(buffer[:cursor-1], buffer[cursor:]...)
						cursor--
						resetSelection()
						_, err = session.Write(stroke.raw)
					}

				case strokeDelete:
					if cursor < len(buffer) {
						buffer = append(buffer[:cursor], buffer[cursor+1:]...)
						resetSelection()
					}
					_, err = session.Write(stroke.raw)

				case strokeLeft:
					if cursor > 0 {
						cursor--
					}
					_, err = session.Write(stroke.raw)

				case strokeRight:
					ghost := currentGhost(string(buffer), cursor == len(buffer), suggestions, selected)
					if suggestionsEnabled && !hiddenUntilInput && ghost != "" {
						_, err = session.Write([]byte(ghost))
						buffer = append(buffer, []rune(ghost)...)
						cursor = len(buffer)
						resetSelection()
					} else {
						if cursor < len(buffer) {
							cursor++
						}
						_, err = session.Write(stroke.raw)
					}

				case strokeHome:
					cursor = 0
					_, err = session.Write(stroke.raw)

				case strokeEnd:
					cursor = len(buffer)
					_, err = session.Write(stroke.raw)

				case strokeUp, strokeDown:
					if len(suggestions) == 0 && len(buffer) == 0 {
						mode = suggest.ModeHistory
						recompute()
					}
					if suggestionsEnabled && len(suggestions) > 0 {
						hiddenUntilInput = false
						userNavigated = true
						if stroke.kind == strokeUp {
							selected--
							if selected < 0 {
								selected = len(suggestions) - 1
							}
						} else {
							selected = (selected + 1) % len(suggestions)
						}
					} else {
						_, err = session.Write(stroke.raw)
					}

				case strokeTab:
					if suggestionsEnabled && !hiddenUntilInput && selected >= 0 && selected < len(suggestions) {
						err = replaceChildLine(suggestions[selected].Insert, mode == suggest.ModeSpec)
					} else {
						_, err = session.Write(stroke.raw)
					}

				case strokeShiftTab:
					suggestionsEnabled = !suggestionsEnabled
					hiddenUntilInput = false

				case strokeEscape:
					hiddenUntilInput = true

				case strokeEnter:
					renderer.Reset()
					stopRender()
					// The wave owns only the row reserved above the initial prompt.
					// Stop updating it before child output is allowed to scroll.
					waveActive = false
					if userNavigated && suggestionsEnabled && !hiddenUntilInput && selected >= 0 && selected < len(suggestions) {
						value := strings.TrimSpace(suggestions[selected].Insert)
						if mode == suggest.ModeSpec && value != "" && !strings.HasSuffix(value, "/") && !strings.HasSuffix(value, `\`) {
							value += " "
						}
						err = replacePowerShellLine(session, buffer, value)
						if err != nil {
							return err
						}
						buffer = []rune(value)
						cursor = len(buffer)
					}
					lastCommand = strings.TrimSpace(string(buffer))
					commandCWD = cwd
					executing = true
					buffer = nil
					cursor = 0
					suggestions = nil
					if aiCompleter != nil {
						aiCompleter.Cancel()
					}
					_, err = session.Write([]byte{'\r'})

				case strokeCtrl:
					err = handleControl(stroke.control, session, &buffer, &cursor, &mode, &hiddenUntilInput, resetSelection, recompute)
					if stroke.control == 0x12 { // Ctrl+R is Metuur mode, not PSReadLine history search.
						userNavigated = false
					}

				case strokeToggleMenu:
					suggestionsEnabled = !suggestionsEnabled
					hiddenUntilInput = false

				case strokePasteStart, strokePasteEnd:
					_, err = session.Write(stroke.raw)

				case strokeUnknown:
					hiddenUntilInput = true
					buffer = nil
					cursor = 0
					resetSelection()
					_, err = session.Write(stroke.raw)
				}
				if err != nil {
					return err
				}
				if !executing {
					recompute()
					scheduleAI()
					scheduleRender()
				}
			}

		case <-renderTimerC:
			renderTimerC = nil
			redraw()

		case <-waveTicker.C:
			if waveActive && ready && !executing {
				waveFrame++
				renderer.DrawWave(lastWidth, waveFrame)
			}

		case <-resizeTicker.C:
			newWidth, newHeight, cursorColumn, cursorRow := terminal.Size()
			if newWidth != lastWidth || newHeight != lastHeight {
				renderer.ClearOverlay()
				if err := session.Resize(newWidth, newHeight); err != nil {
					return err
				}
				renderer.ReserveHeader(newHeight, cursorColumn, cursorRow)
				lastWidth, lastHeight = newWidth, newHeight
				scheduleRender()
			}

		case streamErr := <-streamDone:
			renderer.Reset()
			if streamErr != nil && !errors.Is(streamErr, io.EOF) {
				return streamErr
			}
			return nil

		case waitErr := <-waitDone:
			renderer.Reset()
			if waitErr != nil && !errors.Is(waitErr, context.Canceled) {
				return waitErr
			}
			return nil
		}
	}
}

func handleControl(
	control byte,
	session *shell.Interactive,
	buffer *[]rune,
	cursor *int,
	mode *suggest.Mode,
	hiddenUntilInput *bool,
	resetSelection func(),
	recompute func(),
) error {
	switch control {
	case 0x00: // Ctrl+Space
		return nil
	case 0x01: // Ctrl+A
		*cursor = 0
		_, err := session.Write([]byte("\x1b[H")) // Home; portable across PSReadLine versions.
		return err
	case 0x05: // Ctrl+E
		*cursor = len(*buffer)
		_, err := session.Write([]byte("\x1b[F")) // End; portable across PSReadLine versions.
		return err
	case 0x03: // Ctrl+C
		*buffer = nil
		*cursor = 0
		*hiddenUntilInput = false
		resetSelection()
	case 0x12: // Ctrl+R
		if *mode == suggest.ModeSpec {
			*mode = suggest.ModeHistory
		} else {
			*mode = suggest.ModeSpec
		}
		*hiddenUntilInput = false
		resetSelection()
		recompute()
		return nil
	case 0x15: // Ctrl+U
		original := append([]rune(nil), (*buffer)...)
		*buffer = nil
		*cursor = 0
		resetSelection()
		return replacePowerShellLine(session, original, "")
	case 0x17: // Ctrl+W
		if *cursor > 0 {
			oldCursor := *cursor
			start := *cursor
			for start > 0 && unicode.IsSpace((*buffer)[start-1]) {
				start--
			}
			for start > 0 && !unicode.IsSpace((*buffer)[start-1]) {
				start--
			}
			*buffer = append((*buffer)[:start], (*buffer)[*cursor:]...)
			*cursor = start
			resetSelection()

			// Sending Ctrl+W through ConPTY is not portable: Windows PowerShell
			// may insert the CP437 glyph instead of invoking PSReadLine. Delete the
			// exact mirrored rune range with ordinary Backspace key events.
			backspaces := make([]byte, oldCursor-start)
			for index := range backspaces {
				backspaces[index] = 0x7f
			}
			_, err := session.Write(backspaces)
			return err
		}
		return nil
	case 0x19: // Ctrl+Y, Metuur extension: copy the complete current line.
		return copyInputLine(string(*buffer))
	}
	_, err := session.Write([]byte{control})
	return err
}

// replacePowerShellLine uses navigation/editing keys that both Windows
// PowerShell 5.1 and pwsh 7 bind by default. Ctrl+U itself is not portable:
// older PSReadLine versions echo it as ^U instead of clearing the line.
func replacePowerShellLine(session *shell.Interactive, current []rune, replacement string) error {
	if _, err := session.Write([]byte("\x1b[F")); err != nil { // End
		return err
	}
	if len(current) > 0 {
		backspaces := make([]byte, len(current))
		for index := range backspaces {
			backspaces[index] = 0x7f
		}
		if _, err := session.Write(backspaces); err != nil {
			return err
		}
	}
	if replacement != "" {
		_, err := session.Write([]byte(replacement))
		return err
	}
	return nil
}

type lockedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *lockedWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(data)
}

type notifyingWriter struct {
	writer io.Writer
	events chan<- struct{}
}

func (w *notifyingWriter) Write(data []byte) (int, error) {
	written, err := w.writer.Write(data)
	if written > 0 {
		select {
		case w.events <- struct{}{}:
		default:
		}
	}
	return written, err
}

func mergeAISuggestion(items []suggest.Suggestion, ai suggest.Suggestion) []suggest.Suggestion {
	result := make([]suggest.Suggestion, 0, len(items)+1)
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Insert), strings.TrimSpace(ai.Insert)) {
			if item.Score > ai.Score {
				ai.Score = item.Score
			}
			continue
		}
		result = append(result, item)
	}
	position := len(result)
	for index, item := range result {
		if ai.Score > item.Score {
			position = index
			break
		}
	}
	result = append(result, suggest.Suggestion{})
	copy(result[position+1:], result[position:])
	result[position] = ai
	return result
}

func copyInputLine(line string) error {
	if line == "" {
		return nil
	}
	command := exec.Command(
		"powershell.exe",
		"-NoProfile",
		"-Command",
		"[Console]::InputEncoding=[Text.UTF8Encoding]::new($false); Set-Clipboard -Value ([Console]::In.ReadToEnd())",
	)
	command.Stdin = strings.NewReader(line)
	if err := command.Run(); err != nil {
		return fmt.Errorf("не удалось скопировать строку: %w", err)
	}
	return nil
}

func insertRune(buffer []rune, position int, value rune) []rune {
	buffer = append(buffer, 0)
	copy(buffer[position+1:], buffer[position:])
	buffer[position] = value
	return buffer
}

func currentGhost(buffer string, cursorAtEnd bool, items []suggest.Suggestion, selected int) string {
	if buffer == "" || !cursorAtEnd || len(items) == 0 {
		return ""
	}
	if selected < 0 || selected >= len(items) {
		selected = 0
	}
	command := items[selected].Insert
	if strings.HasPrefix(strings.ToLower(command), strings.ToLower(buffer)) && len(command) >= len(buffer) {
		return command[len(buffer):]
	}
	return ""
}

type strokeKind uint8

const (
	strokeUnknown strokeKind = iota
	strokeRune
	strokeEnter
	strokeTab
	strokeShiftTab
	strokeEscape
	strokeBackspace
	strokeDelete
	strokeLeft
	strokeRight
	strokeUp
	strokeDown
	strokeHome
	strokeEnd
	strokeCtrl
	strokeToggleMenu
	strokePasteStart
	strokePasteEnd
)

type inputStroke struct {
	kind      strokeKind
	raw       []byte
	runeValue rune
	control   byte
}

type inputDecoder struct {
	pending []byte
	inPaste bool
}

func (d *inputDecoder) HasPending() bool {
	return len(d.pending) > 0
}

func (d *inputDecoder) FlushPending() []inputStroke {
	if len(d.pending) == 0 {
		return nil
	}
	raw := append([]byte(nil), d.pending...)
	d.pending = nil
	if len(raw) == 1 && raw[0] == 0x1b {
		return []inputStroke{{kind: strokeEscape, raw: raw}}
	}
	return []inputStroke{{kind: strokeUnknown, raw: raw}}
}

func (d *inputDecoder) Feed(chunk []byte) []inputStroke {
	data := append(d.pending, chunk...)
	d.pending = nil
	var strokes []inputStroke
	for index := 0; index < len(data); {
		remaining := data[index:]
		if bytesPrefix(remaining, "\x1b[200~") {
			strokes = append(strokes, inputStroke{kind: strokePasteStart, raw: append([]byte(nil), remaining[:6]...)})
			d.inPaste = true
			index += 6
			continue
		}
		if bytesPrefix(remaining, "\x1b[201~") {
			strokes = append(strokes, inputStroke{kind: strokePasteEnd, raw: append([]byte(nil), remaining[:6]...)})
			d.inPaste = false
			index += 6
			continue
		}
		if remaining[0] == 0x1b {
			kind, size, complete := decodeEscape(remaining)
			if !complete {
				d.pending = append(d.pending, remaining...)
				break
			}
			strokes = append(strokes, inputStroke{kind: kind, raw: append([]byte(nil), remaining[:size]...)})
			index += size
			continue
		}
		b := remaining[0]
		switch b {
		case '\r', '\n':
			kind := strokeEnter
			if d.inPaste {
				kind = strokeRune
			}
			strokes = append(strokes, inputStroke{kind: kind, raw: []byte{b}, runeValue: rune(b)})
			index++
		case '\t':
			strokes = append(strokes, inputStroke{kind: strokeTab, raw: []byte{b}})
			index++
		case 0x7f, 0x08:
			strokes = append(strokes, inputStroke{kind: strokeBackspace, raw: []byte{b}})
			index++
		case 0x00:
			strokes = append(strokes, inputStroke{kind: strokeToggleMenu, raw: []byte{b}, control: b})
			index++
		default:
			if b < 0x20 {
				strokes = append(strokes, inputStroke{kind: strokeCtrl, raw: []byte{b}, control: b})
				index++
				continue
			}
			if !utf8.FullRune(remaining) {
				d.pending = append(d.pending, remaining...)
				return strokes
			}
			r, size := utf8.DecodeRune(remaining)
			strokes = append(strokes, inputStroke{kind: strokeRune, raw: append([]byte(nil), remaining[:size]...), runeValue: r})
			index += size
		}
	}
	return strokes
}

func decodeEscape(data []byte) (strokeKind, int, bool) {
	if len(data) == 1 {
		// ESC is also the prefix of every VT navigation sequence. Keep it
		// pending briefly so a split "ESC [ A" is not mistaken for Escape.
		return strokeUnknown, 0, false
	}
	sequences := []struct {
		value string
		kind  strokeKind
	}{
		{"\x1b[Z", strokeShiftTab},
		{"\x1b[A", strokeUp},
		{"\x1b[B", strokeDown},
		{"\x1b[C", strokeRight},
		{"\x1b[D", strokeLeft},
		{"\x1b[H", strokeHome},
		{"\x1b[F", strokeEnd},
		{"\x1bOH", strokeHome},
		{"\x1bOF", strokeEnd},
		{"\x1b[3~", strokeDelete},
	}
	for _, sequence := range sequences {
		if len(data) < len(sequence.value) && strings.HasPrefix(sequence.value, string(data)) {
			return strokeUnknown, 0, false
		}
		if bytesPrefix(data, sequence.value) {
			return sequence.kind, len(sequence.value), true
		}
	}
	if data[1] == '[' || data[1] == 'O' {
		for index := 2; index < len(data); index++ {
			if (data[index] >= 'A' && data[index] <= 'Z') ||
				(data[index] >= 'a' && data[index] <= 'z') || data[index] == '~' {
				return strokeUnknown, index + 1, true
			}
		}
		return strokeUnknown, 0, false
	}
	// Alt+key: forward the complete UTF-8 rune with the escape prefix.
	if !utf8.FullRune(data[1:]) {
		return strokeUnknown, 0, false
	}
	_, size := utf8.DecodeRune(data[1:])
	return strokeUnknown, size + 1, true
}

func bytesPrefix(data []byte, prefix string) bool {
	return len(data) >= len(prefix) && string(data[:len(prefix)]) == prefix
}
