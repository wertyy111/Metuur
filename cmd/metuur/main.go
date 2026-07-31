package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/wertyy111/metuur/internal/app"
	"github.com/wertyy111/metuur/internal/config"
	"github.com/wertyy111/metuur/internal/console"
	"github.com/wertyy111/metuur/internal/shell"
)

const version = "0.2.1"

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
