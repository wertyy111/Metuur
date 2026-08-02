package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type UIConfig struct {
	Style     string `json:"style"`
	MaxWidth  int    `json:"maxWidth"`
	GhostText bool   `json:"ghostText"`
	NerdFonts bool   `json:"nerdFonts"`
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
	Shell           string   `json:"shell"`
	LocalAIEnabled  bool     `json:"localAIEnabled"`
	MaxSuggestions  int      `json:"maxSuggestions"`
	MaxHistory      int      `json:"maxHistory"`
	ShowHiddenFiles bool     `json:"showHiddenFiles"`
	UI              UIConfig `json:"ui"`
	AI              AIConfig `json:"ai"`
}

func Default() Config {
	return Config{
		Shell:           "auto",
		LocalAIEnabled:  true,
		MaxSuggestions:  100,
		MaxHistory:      5000,
		ShowHiddenFiles: false,
		UI: UIConfig{
			Style:     "modern",
			MaxWidth:  76,
			GhostText: true,
			NerdFonts: true,
		},
		AI: AIConfig{
			Enabled:    true,
			Provider:   "portable",
			Endpoint:   "http://127.0.0.1:11435/v1",
			Model:      "qwen2.5-coder:0.5b",
			DebounceMS: 500,
			TimeoutMS:  5000,
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
	if c.Shell == "" {
		c.Shell = defaults.Shell
	}
	if c.MaxSuggestions < 1 {
		c.MaxSuggestions = defaults.MaxSuggestions
	}
	if c.MaxSuggestions > 1000 {
		c.MaxSuggestions = 1000
	}
	if c.MaxHistory < 100 {
		c.MaxHistory = defaults.MaxHistory
	}
	if c.UI.Style == "" {
		c.UI.Style = defaults.UI.Style
	}
	if c.UI.MaxWidth < 40 {
		c.UI.MaxWidth = defaults.UI.MaxWidth
	}
	if c.UI.MaxWidth > 240 {
		c.UI.MaxWidth = 240
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
