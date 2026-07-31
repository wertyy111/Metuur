package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Theme struct {
	Accent   string `json:"accent"`
	Muted    string `json:"muted"`
	Selected string `json:"selected"`
}

type Config struct {
	Prompt           string `json:"prompt"`
	Shell            string `json:"shell"`
	MaxSuggestions   int    `json:"maxSuggestions"`
	MaxHistory       int    `json:"maxHistory"`
	ShowDescriptions bool   `json:"showDescriptions"`
	ShowHiddenFiles  bool   `json:"showHiddenFiles"`
	Theme            Theme  `json:"theme"`
}

func Default() Config {
	return Config{
		Prompt:           "λ ",
		Shell:            "auto",
		MaxSuggestions:   5,
		MaxHistory:       5000,
		ShowDescriptions: true,
		ShowHiddenFiles:  false,
		Theme: Theme{
			Accent:   "135",
			Muted:    "244",
			Selected: "237",
		},
	}
}

func Dir() string {
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		return filepath.Join(local, "Metuur")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ".metuur"
	}
	return filepath.Join(dir, "Metuur")
}

func legacyDir() string {
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		return filepath.Join(local, "WIRIS")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ".wiris"
	}
	return filepath.Join(dir, "WIRIS")
}

func Path() string {
	return filepath.Join(Dir(), "config.json")
}

func HistoryPath() string {
	return filepath.Join(Dir(), "history.txt")
}

// MigrateLegacy copies existing WIRIS settings and history on the first Metuur run.
func MigrateLegacy() error {
	for _, name := range []string{"config.json", "history.txt"} {
		destination := filepath.Join(Dir(), name)
		if _, err := os.Stat(destination); err == nil {
			continue
		}
		source := filepath.Join(legacyDir(), name)
		data, err := os.ReadFile(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read legacy %s: %w", name, err)
		}
		if err := os.MkdirAll(Dir(), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(destination, data, 0o644); err != nil {
			return fmt.Errorf("migrate %s: %w", name, err)
		}
	}
	return nil
}

func Load() (Config, error) {
	cfg := Default()
	if err := MigrateLegacy(); err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", Path(), err)
	}
	cfg.normalize()
	return cfg, nil
}

func Save(cfg Config, overwrite bool) error {
	path := Path()
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
	}
	cfg.normalize()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func (c *Config) normalize() {
	defaults := Default()
	if c.Prompt == "" {
		c.Prompt = defaults.Prompt
	}
	if c.Shell == "" {
		c.Shell = defaults.Shell
	}
	if c.MaxSuggestions < 1 {
		c.MaxSuggestions = defaults.MaxSuggestions
	}
	if c.MaxSuggestions > 20 {
		c.MaxSuggestions = 20
	}
	if c.MaxHistory < 100 {
		c.MaxHistory = defaults.MaxHistory
	}
	if c.Theme.Accent == "" {
		c.Theme.Accent = defaults.Theme.Accent
	}
	if c.Theme.Muted == "" {
		c.Theme.Muted = defaults.Theme.Muted
	}
	if c.Theme.Selected == "" {
		c.Theme.Selected = defaults.Theme.Selected
	}
}
