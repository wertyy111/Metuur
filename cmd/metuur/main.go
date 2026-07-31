package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/wertyy111/metuur/internal/app"
	"github.com/wertyy111/metuur/internal/config"
	"github.com/wertyy111/metuur/internal/console"
	"github.com/wertyy111/metuur/internal/history"
	"github.com/wertyy111/metuur/internal/localai"
	"github.com/wertyy111/metuur/internal/shell"
	"github.com/wertyy111/metuur/internal/suggest"
)

const version = "0.3.1"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "metuur:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("Metuur supports Windows only")
	}
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version", "-v":
			fmt.Println("Metuur", version)
			return nil
		case "help", "--help", "-h":
			printHelp()
			return nil
		case "config":
			return configCommand(args[1:])
		case "doctor":
			return doctor()
		case "ai":
			return aiCommand(args[1:])
		default:
			return fmt.Errorf("unknown command %q; run metuur help", args[0])
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return app.Run(cfg, version)
}

func aiCommand(args []string) error {
	action := "status"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if action == "setup" {
		return setupAI(cfg, args[1:])
	}
	if action == "suggest" {
		if len(args) < 2 {
			return fmt.Errorf("usage: metuur ai suggest <unfinished command or intent>")
		}
		return suggestAI(cfg, strings.Join(args[1:], " "))
	}
	if action != "status" {
		return fmt.Errorf("unknown ai action %q (use status, setup, or suggest)", action)
	}
	model := localai.Load(config.ModelPath())
	stats := model.Stats()
	fmt.Println("Metuur local AI")
	fmt.Printf("  Provider:    %s\n", cfg.AI.Provider)
	fmt.Printf("  Endpoint:    %s\n", cfg.AI.Endpoint)
	fmt.Printf("  LLM model:   %s\n", cfg.AI.Model)
	if strings.EqualFold(cfg.AI.Provider, "portable") {
		fmt.Printf("  Data:        %s\n", config.AIDataDir(cfg))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	provider, providerErr := localai.CheckProvider(ctx, cfg.AI.Provider, cfg.AI.Endpoint, cfg.AI.Model)
	providerName := cfg.AI.Provider
	if strings.EqualFold(providerName, "portable") {
		providerName = "Portable AI"
	}
	if providerErr != nil {
		fmt.Printf("  %-12s OFFLINE (run `metuur ai setup`)\n", providerName+":")
	} else if !provider.HasModel {
		fmt.Printf("  %-12s ONLINE, model missing (run `metuur ai setup`)\n", providerName+":")
	} else {
		fmt.Printf("  %-12s ONLINE, model ready\n", providerName+":")
	}
	fmt.Printf("  Learned:     %d commands\n", stats.Commands)
	fmt.Printf("  Ranker size: %d bytes\n", stats.Bytes)
	return nil
}

func suggestAI(cfg config.Config, input string) error {
	trace := func(message string) {
		if os.Getenv("METUUR_AI_TRACE") == "1" {
			fmt.Fprintln(os.Stderr, "[ai]", message)
		}
	}
	trace("checking provider")
	if strings.EqualFold(cfg.AI.Provider, "portable") {
		if err := localai.StartPortable(config.AIDataDir(cfg), cfg.AI.Endpoint, cfg.AI.Model); err != nil {
			return err
		}
	}
	readyCtx, readyCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer readyCancel()
	if err := localai.WaitForProvider(readyCtx, cfg.AI.Provider, cfg.AI.Endpoint, cfg.AI.Model); err != nil {
		return fmt.Errorf("local AI is not ready: %w", err)
	}
	trace("provider ready")
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	apiKey := ""
	if cfg.AI.APIKeyEnv != "" {
		apiKey = os.Getenv(cfg.AI.APIKeyEnv)
	}
	client := localai.NewClient(localai.ProviderConfig{
		Endpoint: cfg.AI.Endpoint,
		Model:    cfg.AI.Model,
		APIKey:   apiKey,
		Timeout:  time.Duration(cfg.AI.TimeoutMS) * time.Millisecond,
	})
	requestCtx, requestCancel := context.WithTimeout(context.Background(), time.Duration(cfg.AI.TimeoutMS)*time.Millisecond)
	defer requestCancel()
	store := history.Load(config.HistoryPath(), cfg.MaxHistory)
	trace("building workspace candidates")
	engine, engineErr := suggest.New(store, cfg.ShowHiddenFiles)
	if engineErr != nil {
		return engineErr
	}
	environment := localai.Environment{CWD: cwd}
	if activeFile, ok := engine.ActiveGoFile(cwd); ok {
		environment.ActiveFile = activeFile
	}
	for _, item := range engine.Suggest(input, cwd, suggest.ModeSpec, 16) {
		environment.Candidates = append(environment.Candidates, item.Insert)
	}
	trace(fmt.Sprintf("%d candidates; collecting context", len(environment.Candidates)))
	environment = localai.EnrichEnvironment(environment)
	trace("requesting completion")
	command, err := client.Suggest(requestCtx, input, environment)
	if err != nil {
		return err
	}
	if command == "" {
		return errors.New("the model did not produce a safe Go command")
	}
	fmt.Println(command)
	return nil
}

func setupAI(cfg config.Config, args []string) error {
	if strings.EqualFold(cfg.AI.Provider, "portable") {
		if len(args) > 1 {
			return fmt.Errorf("usage: metuur ai setup [data-directory]")
		}
		if len(args) == 1 {
			absolute, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			cfg.AI.DataDir = absolute
		}
		aiDataDir := config.AIDataDir(cfg)
		fmt.Println("Installing Metuur's small, private completion engine...")
		fmt.Println("  Data:", aiDataDir)
		if err := localai.SetupPortable(context.Background(), aiDataDir, os.Stdout); err != nil {
			return err
		}
		if err := localai.StartPortable(aiDataDir, cfg.AI.Endpoint, cfg.AI.Model); err != nil {
			return err
		}
		fmt.Println("Starting the model (first start can take several seconds)...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := localai.WaitForProvider(ctx, cfg.AI.Provider, cfg.AI.Endpoint, cfg.AI.Model); err != nil {
			return fmt.Errorf("portable AI did not become ready: %w", err)
		}
		if err := config.Save(cfg, true); err != nil {
			return err
		}
		fmt.Println("Local completion is ready. Start typing a Go command in Metuur.")
		return nil
	}
	if !strings.EqualFold(cfg.AI.Provider, "ollama") {
		return fmt.Errorf("automatic setup supports portable and ollama providers")
	}
	ollamaPath, err := findOllama()
	if err != nil {
		fmt.Println("Ollama is not installed; running the official Windows installer...")
		installer := exec.Command(
			"powershell.exe", "-NoLogo", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command",
			"irm https://ollama.com/install.ps1 | iex",
		)
		installer.Stdout = os.Stdout
		installer.Stderr = os.Stderr
		installer.Stdin = os.Stdin
		if installErr := installer.Run(); installErr != nil {
			return fmt.Errorf("install Ollama: %w", installErr)
		}
		ollamaPath, err = findOllama()
		if err != nil {
			return fmt.Errorf("Ollama installation completed but ollama.exe was not found")
		}
	}

	if _, statusErr := localai.CheckOllama(context.Background(), cfg.AI.Endpoint, cfg.AI.Model); statusErr != nil {
		server := exec.Command(ollamaPath, "serve")
		server.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if startErr := server.Start(); startErr == nil {
			_ = server.Process.Release()
			time.Sleep(2 * time.Second)
		}
	}

	fmt.Printf("Downloading the lightweight model %s...\n", cfg.AI.Model)
	pull := exec.Command(ollamaPath, "pull", cfg.AI.Model)
	pull.Stdout = os.Stdout
	pull.Stderr = os.Stderr
	pull.Stdin = os.Stdin
	if err := pull.Run(); err != nil {
		return fmt.Errorf("download model %s: %w", cfg.AI.Model, err)
	}
	if err := config.Save(cfg, true); err != nil {
		return err
	}
	fmt.Println("Local AI is ready. Restart Metuur if it is already open.")
	return nil
}

func findOllama() (string, error) {
	if path, err := exec.LookPath("ollama.exe"); err == nil {
		return path, nil
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		path := filepath.Join(local, "Programs", "Ollama", "ollama.exe")
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("ollama.exe not found")
}

func configCommand(args []string) error {
	action := "show"
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "init":
		if err := config.Save(config.Default(), false); err != nil {
			return err
		}
		fmt.Println("Config:", config.Path())
	case "show":
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		data, _ := json.MarshalIndent(cfg, "", "  ")
		fmt.Println(string(data))
	case "path":
		fmt.Println(config.Path())
	default:
		return fmt.Errorf("unknown config action %q (use init, show, path)", action)
	}
	return nil
}

func doctor() error {
	fmt.Println("Metuur doctor")
	fmt.Printf("  OS:          %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  Go runtime:  %s\n", runtime.Version())
	fmt.Printf("  Config:      %s\n", config.Path())

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("  Config load: FAIL (%v)\n", err)
	} else {
		fmt.Println("  Config load: OK")
	}
	runner, shellErr := shell.New(cfg.Shell)
	if shellErr != nil {
		fmt.Printf("  Shell:       FAIL (%v)\n", shellErr)
	} else {
		fmt.Printf("  Shell:       OK (%s)\n", runner.Name())
	}
	if git, gitErr := exec.LookPath("git.exe"); gitErr == nil {
		fmt.Printf("  Git:         OK (%s)\n", git)
	} else {
		fmt.Println("  Git:         optional, not found")
	}
	if cfg.AI.Enabled {
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		status, aiErr := localai.CheckProvider(ctx, cfg.AI.Provider, cfg.AI.Endpoint, cfg.AI.Model)
		cancel()
		if aiErr != nil {
			fmt.Println("  Local AI:    optional, offline (run `metuur ai setup`)")
		} else if !status.HasModel {
			fmt.Println("  Local AI:    optional, model missing (run `metuur ai setup`)")
		} else {
			fmt.Printf("  Local AI:    OK (%s)\n", cfg.AI.Model)
		}
	}
	terminal, terminalErr := console.Open()
	if terminalErr != nil {
		fmt.Printf("  Terminal:    FAIL (%v)\n", terminalErr)
	} else {
		_ = terminal.Close()
		fmt.Println("  Terminal:    OK (Windows Console API + ANSI)")
	}
	if err != nil || shellErr != nil || terminalErr != nil {
		return fmt.Errorf("one or more required checks failed")
	}
	return nil
}

func printHelp() {
	fmt.Print(`Metuur — intelligent Go command assistant for Windows

Usage:
  metuur                 start the interactive PowerShell shell
  metuur doctor          check Windows, terminal and shell support
  metuur ai status       show local AI and adaptive ranker status
  metuur ai setup [dir]  install the portable lightweight local model
  metuur ai suggest ...  test completion for an unfinished command
  metuur config init     create the default config
  metuur config show     print the active config
  metuur config path     print the config file path
  metuur version         print the version

Keys:
  Tab / Right           accept the selected suggestion
  Up / Down             select an item (or browse history on an empty line)
  Shift+Tab             show or hide the menu
  Ctrl+Space            show or hide the menu
  Ctrl+Y                copy the entire input line
  Ctrl+Shift+C          copy the input line (when VS Code passes the key)
  Ctrl+R                switch between spec and history modes
  Ctrl+A / Ctrl+E       move to start / end
  Ctrl+W / Ctrl+U       delete word / clear line
  Ctrl+L / Ctrl+C       clear screen / cancel command
  Esc                   hide suggestions
`)
}
