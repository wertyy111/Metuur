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
	Logo     string `json:"logo"`
	Command  string `json:"command"`
	Muted    string `json:"muted"`
	Selected string `json:"selected"`
}

type AIConfig struct {
	Enabled    bool   `json:"enabled"`
	Provider   string `json:"provider"`
	Endpoint   string `json:"endpoint"`
	Model      string `json:"model"`
	DataDir    string `json:"dataDir,omitempty"`
	APIKeyEnv  string `json:"apiKeyEnv,omitempty"`
	DebounceMS int    `json:"debounceMS"`
	TimeoutMS  int    `json:"timeoutMS"`
}

type Config struct {
	Prompt           string   `json:"prompt"`
	Shell            string   `json:"shell"`
	LocalAIEnabled   bool     `json:"localAIEnabled"`
	MaxSuggestions   int      `json:"maxSuggestions"`
	MaxHistory       int      `json:"maxHistory"`
	ShowDescriptions bool     `json:"showDescriptions"`
	ShowHiddenFiles  bool     `json:"showHiddenFiles"`
	AI               AIConfig `json:"ai"`
	Theme            Theme    `json:"theme"`
}

func Default() Config {
	return Config{
		Prompt:           "λ ",
		Shell:            "auto",
		LocalAIEnabled:   true,
		MaxSuggestions:   5,
		MaxHistory:       5000,
		ShowDescriptions: true,
		ShowHiddenFiles:  false,
		AI: AIConfig{
			Enabled:    true,
			Provider:   "portable",
			Endpoint:   "http://127.0.0.1:11435/v1",
			Model:      "qwen2.5-coder:0.5b",
			DebounceMS: 500,
			TimeoutMS:  5000,
		},
		Theme: Theme{
			Accent:   "141",
			Logo:     "213",
			Command:  "121",
			Muted:    "248",
			Selected: "60",
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

func ModelPath() string {
	return filepath.Join(Dir(), "micro-ai.json")
}

func AIDir() string {
	return filepath.Join(Dir(), "ai")
}

func AIDataDir(cfg Config) string {
	if override := os.Getenv("METUUR_AI_DIR"); override != "" {
		return filepath.Clean(os.ExpandEnv(override))
	}
	if cfg.AI.DataDir != "" {
		return filepath.Clean(os.ExpandEnv(cfg.AI.DataDir))
	}
	return AIDir()
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
	if c.Theme.Logo == "" {
		c.Theme.Logo = defaults.Theme.Logo
	}
	if c.Theme.Command == "" {
		c.Theme.Command = defaults.Theme.Command
	}
	if c.Theme.Muted == "" {
		c.Theme.Muted = defaults.Theme.Muted
	}
	if c.Theme.Selected == "" {
		c.Theme.Selected = defaults.Theme.Selected
	}
	if c.AI.Provider == "" {
		c.AI.Provider = defaults.AI.Provider
	}
	if c.AI.Endpoint == "" {
		c.AI.Endpoint = defaults.AI.Endpoint
	}
	if c.AI.Model == "" {
		c.AI.Model = defaults.AI.Model
	}
	if c.AI.DebounceMS < 100 {
		c.AI.DebounceMS = defaults.AI.DebounceMS
	}
	if c.AI.TimeoutMS < 500 {
		c.AI.TimeoutMS = defaults.AI.TimeoutMS
	}
}
