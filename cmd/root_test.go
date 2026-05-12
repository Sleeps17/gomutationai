package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitConfig_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	yaml := `
llm_model: "test-model"
llm_base_url: "http://example.local/v1"
workers: 3
timeout: 15s
verbose: true
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	oldFile := cfgFile
	oldCfg := cfg
	cfgFile = path
	defer func() {
		cfgFile = oldFile
		cfg = oldCfg
	}()

	initConfig()

	if cfg == nil {
		t.Fatal("cfg = nil после initConfig")
	}
	if cfg.LLMModel != "test-model" {
		t.Errorf("LLMModel = %q, want test-model", cfg.LLMModel)
	}
	if cfg.Workers != 3 {
		t.Errorf("Workers = %d, want 3", cfg.Workers)
	}
	if !cfg.Verbose {
		t.Error("Verbose должен быть true")
	}
}

func TestInitConfig_MissingFile_UsesDefaults(t *testing.T) {
	oldFile := cfgFile
	oldCfg := cfg
	// Несуществующий путь — config.Load возвращает дефолты без ошибки
	cfgFile = filepath.Join(t.TempDir(), "no-such-config.yaml")
	defer func() {
		cfgFile = oldFile
		cfg = oldCfg
	}()

	initConfig()

	if cfg == nil {
		t.Fatal("cfg = nil; ожидались дефолты")
	}
}
