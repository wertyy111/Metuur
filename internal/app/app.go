package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"

	"github.com/wertyy111/metuur/internal/config"
	winconsole "github.com/wertyy111/metuur/internal/console"
	"github.com/wertyy111/metuur/internal/history"
	"github.com/wertyy111/metuur/internal/localai"
	"github.com/wertyy111/metuur/internal/shell"
	"github.com/wertyy111/metuur/internal/suggest"
	"github.com/wertyy111/metuur/internal/ui"
)

func Run(cfg config.Config, version string) error {
	_ = version
	terminal, err := winconsole.Open()
	if err != nil {
		return err
	}
	defer terminal.Close()

	runner, err := shell.New(cfg.Shell)
	if err != nil {
		return err
	}
	defer runner.Close()
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
			// Loading the model is intentionally detached from the UI. Built-in
			// suggestions remain instant while the local completion engine starts.
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
	renderer := ui.New(os.Stdout, cfg)

	var (
		buffer        []rune
		cursor        int
		selected      int
		mode          = suggest.ModeSpec
		menuVisible   = true
		suggestions   []suggest.Suggestion
		historyItems  = store.Commands()
		historyIndex  = len(historyItems)
		savedBuffer   []rune
		aiSuggestion  *suggest.Suggestion
		aiQuery       string
		lastAIQuery   string
		userNavigated bool
		lastCommand   string
		lastExitCode  int
	)

	recompute := func() {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			cwd = "."
		}
		if len(buffer) == 0 && mode == suggest.ModeSpec {
			suggestions = nil
		} else {
			suggestions = engine.Suggest(string(buffer), cwd, mode, max(100, cfg.MaxSuggestions*20))
		}
		if aiSuggestion != nil && aiQuery == string(buffer) && mode == suggest.ModeSpec {
			suggestions = mergeAISuggestion(suggestions, *aiSuggestion)
		}
		if len(suggestions) == 0 {
			selected = -1
		} else if selected < 0 || selected >= len(suggestions) {
			selected = 0
		}
		renderer.Redraw(buffer, cursor, suggestions, selected, mode, menuVisible)
	}
	resetSelection := func() {
		selected = 0
		userNavigated = false
		historyIndex = len(historyItems)
		savedBuffer = nil
	}
	accept := func() {
		if !menuVisible || selected < 0 || selected >= len(suggestions) {
			return
		}
		buffer = []rune(suggestions[selected].Insert)
		cursor = len(buffer)
		resetSelection()
	}
	setHistory := func(index int) {
		if index < 0 || index > len(historyItems) {
			return
		}
		if historyIndex == len(historyItems) && index < historyIndex {
			savedBuffer = append([]rune(nil), buffer...)
		}
		historyIndex = index
		userNavigated = true
		if index == len(historyItems) {
			buffer = append([]rune(nil), savedBuffer...)
		} else {
			buffer = []rune(historyItems[index])
		}
		cursor = len(buffer)
		selected = 0
		menuVisible = true
	}
	scheduleAI := func() {
		if aiCompleter == nil {
			return
		}
		query := string(buffer)
		if mode != suggest.ModeSpec {
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
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			cwd = "."
		}
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
		if len(environment.Candidates) == 0 {
			return
		}
		_ = aiCompleter.Request(query, environment)
	}

	recompute()
	for {
		if aiCompleter != nil {
			select {
			case completion := <-aiCompleter.Results():
				if completion.Err == nil && completion.Command != "" && completion.Query == string(buffer) &&
					mode == suggest.ModeSpec && !userNavigated {
					aiQuery = completion.Query
					aiSuggestion = &suggest.Suggestion{
						Label:       completion.Command,
						Insert:      completion.Command,
						Description: "локальный " + completion.Model + " · AI-подсказка",
						Kind:        "ai",
						Score:       550,
					}
					selected = 0
					recompute()
				}
			default:
			}
		}

		key, ready, readErr := terminal.ReadKeyTimeout(60 * time.Millisecond)
		if readErr != nil {
			renderer.ClearLine()
			return readErr
		}
		if !ready {
			continue
		}

		if key.Ctrl && key.Kind == winconsole.KeyRune {
			switch unicode.ToLower(key.Rune) {
			case 'a':
				cursor = 0
			case 'e':
				cursor = len(buffer)
			case 'r':
				if mode == suggest.ModeSpec {
					mode = suggest.ModeHistory
				} else {
					mode = suggest.ModeSpec
				}
				menuVisible = true
				resetSelection()
			case ' ':
				menuVisible = !menuVisible
			case 'u':
				buffer = nil
				cursor = 0
				resetSelection()
			case 'w':
				if cursor > 0 {
					start := cursor
					for start > 0 && unicode.IsSpace(buffer[start-1]) {
						start--
					}
					for start > 0 && !unicode.IsSpace(buffer[start-1]) {
						start--
					}
					buffer = append(buffer[:start], buffer[cursor:]...)
					cursor = start
					resetSelection()
				}
			case 'l':
				renderer.ClearScreen()
			case 'c':
				if key.Shift {
					if copyErr := copyInputLine(string(buffer)); copyErr != nil {
						renderer.Error(copyErr)
					}
				} else {
					renderer.PrepareCommand(string(buffer) + "^C")
					buffer = nil
					cursor = 0
					mode = suggest.ModeSpec
					menuVisible = true
					resetSelection()
				}
			case 'd':
				if len(buffer) == 0 {
					renderer.ClearLine()
					fmt.Fprint(os.Stdout, "\r\n")
					return nil
				}
			case 'y':
				if copyErr := copyInputLine(string(buffer)); copyErr != nil {
					renderer.Error(copyErr)
				}
			}
			recompute()
			scheduleAI()
			continue
		}

		switch key.Kind {
		case winconsole.KeyRune:
			if !key.Alt && key.Rune >= 32 {
				buffer = insertRune(buffer, cursor, key.Rune)
				cursor++
				resetSelection()
			}
		case winconsole.KeyBackspace:
			if cursor > 0 {
				buffer = append(buffer[:cursor-1], buffer[cursor:]...)
				cursor--
				resetSelection()
			}
		case winconsole.KeyDelete:
			if cursor < len(buffer) {
				buffer = append(buffer[:cursor], buffer[cursor+1:]...)
				resetSelection()
			}
		case winconsole.KeyLeft:
			if cursor > 0 {
				cursor--
			}
		case winconsole.KeyRight:
			if cursor == len(buffer) && menuVisible && len(suggestions) > 0 {
				accept()
			} else if cursor < len(buffer) {
				cursor++
			}
		case winconsole.KeyHome:
			cursor = 0
		case winconsole.KeyEnd:
			cursor = len(buffer)
		case winconsole.KeyUp:
			if len(buffer) == 0 && mode == suggest.ModeSpec && historyIndex > 0 {
				setHistory(historyIndex - 1)
			} else if menuVisible && len(suggestions) > 0 {
				userNavigated = true
				selected--
				if selected < 0 {
					selected = len(suggestions) - 1
				}
			}
		case winconsole.KeyDown:
			if historyIndex < len(historyItems) {
				setHistory(historyIndex + 1)
			} else if menuVisible && len(suggestions) > 0 {
				userNavigated = true
				selected = (selected + 1) % len(suggestions)
			}
		case winconsole.KeyTab:
			if key.Shift {
				menuVisible = !menuVisible
			} else if !menuVisible {
				menuVisible = true
			} else {
				accept()
			}
		case winconsole.KeyEscape:
			menuVisible = false
		case winconsole.KeyEnter:
			if menuVisible && selected >= 0 && selected < len(suggestions) &&
				(suggestions[selected].Kind == "run" ||
					suggestions[selected].Kind == "format" ||
					suggestions[selected].Kind == "build" ||
					suggestions[selected].Kind == "workspace" ||
					suggestions[selected].Kind == "intent" ||
					suggestions[selected].Kind == "ai") &&
				!strings.HasSuffix(suggestions[selected].Insert, " ") &&
				!strings.EqualFold(strings.TrimSpace(string(buffer)), strings.TrimSpace(suggestions[selected].Insert)) {
				accept()
			}
			line := strings.TrimSpace(string(buffer))
			renderer.PrepareCommand(string(buffer))
			if line == "" {
				buffer = nil
				cursor = 0
				menuVisible = true
				resetSelection()
				recompute()
				continue
			}

			commandCwd, cwdErr := os.Getwd()
			if cwdErr != nil {
				commandCwd = "."
			}
			if suspendErr := terminal.Suspend(); suspendErr != nil {
				return suspendErr
			}
			if aiCompleter != nil {
				aiCompleter.Cancel()
			}
			exit, runErr := runner.Run(line)
			resumeErr := terminal.Resume()
			if resumeErr != nil {
				return resumeErr
			}
			if runErr != nil && !shell.IsCommandFailure(runErr) {
				renderer.Error(runErr)
			}
			if runErr == nil {
				engine.Learn(line, commandCwd)
			}
			lastCommand = line
			lastExitCode = shell.ExitCode(runErr)
			store.Add(line)
			historyItems = append(historyItems, line)
			if len(historyItems) > cfg.MaxHistory {
				historyItems = historyItems[len(historyItems)-cfg.MaxHistory:]
			}
			if exit {
				renderer.ClearLine()
				fmt.Fprint(os.Stdout, "\r\n")
				return nil
			}
			buffer = nil
			cursor = 0
			mode = suggest.ModeSpec
			menuVisible = true
			resetSelection()
		}
		recompute()
		scheduleAI()
	}
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
