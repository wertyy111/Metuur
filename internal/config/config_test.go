package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOldConfigReceivesAIAndIRISUIDefaults(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte(`{"prompt":"λ ","theme":{"accent":"135"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AI.Enabled || cfg.AI.Provider != "portable" || cfg.AI.Model != "qwen2.5-coder:0.5b" ||
		cfg.UI.Style != "modern" || cfg.UI.MaxWidth != 76 || !cfg.UI.GhostText || !cfg.UI.NerdFonts {
		t.Fatalf("defaults were not migrated: %#v", cfg)
	}
}

func TestAICanBeExplicitlyDisabled(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte(`{"ai":{"enabled":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.Enabled {
		t.Fatal("explicit ai.enabled=false was ignored")
	}
}

func TestAIDataDirPrefersEnvironmentOverride(t *testing.T) {
	cfg := Default()
	cfg.AI.DataDir = filepath.Join(t.TempDir(), "configured")
	override := filepath.Join(t.TempDir(), "override")
	t.Setenv("METUUR_AI_DIR", override)
	if got := AIDataDir(cfg); got != override {
		t.Fatalf("unexpected AI directory: %q", got)
	}
}
