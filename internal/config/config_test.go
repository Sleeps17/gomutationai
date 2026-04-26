package config

import (
	"os"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.LLMBaseURL != "https://api.openai.com/v1" {
		t.Errorf("LLMBaseURL = %q, want default", cfg.LLMBaseURL)
	}
	if cfg.LLMModel != "gpt-4o-mini" {
		t.Errorf("LLMModel = %q, want gpt-4o-mini", cfg.LLMModel)
	}
	if !cfg.StructuredOutput {
		t.Error("StructuredOutput должен быть true по умолчанию")
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", cfg.Timeout)
	}
	if cfg.Workers != 0 {
		t.Errorf("Workers = %d, want 0", cfg.Workers)
	}
	if cfg.MaxMutants != 0 {
		t.Errorf("MaxMutants = %d, want 0", cfg.MaxMutants)
	}
	if cfg.Verbose {
		t.Error("Verbose должен быть false по умолчанию")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	cfg, err := Load("nonexistent_config.yaml")
	if err != nil {
		t.Fatalf("Load при отсутствии файла не должна возвращать ошибку, got: %v", err)
	}
	if cfg.LLMModel != "gpt-4o-mini" {
		t.Errorf("ожидались дефолтные значения, LLMModel = %q", cfg.LLMModel)
	}
}

func TestLoad_ValidYAML(t *testing.T) {
	f, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	_, err = f.WriteString(`
llm_base_url: "http://localhost:11434/v1"
llm_model: "llama3"
llm_api_key: "test-key"
structured_output: false
workers: 4
max_mutants: 10
verbose: true
timeout: 60s
output: "report.json"
`)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg, err := Load(f.Name())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLMBaseURL != "http://localhost:11434/v1" {
		t.Errorf("LLMBaseURL = %q", cfg.LLMBaseURL)
	}
	if cfg.LLMModel != "llama3" {
		t.Errorf("LLMModel = %q", cfg.LLMModel)
	}
	if cfg.LLMAPIKey != "test-key" {
		t.Errorf("LLMAPIKey = %q", cfg.LLMAPIKey)
	}
	if cfg.StructuredOutput {
		t.Error("StructuredOutput должен быть false")
	}
	if cfg.Workers != 4 {
		t.Errorf("Workers = %d", cfg.Workers)
	}
	if cfg.MaxMutants != 10 {
		t.Errorf("MaxMutants = %d", cfg.MaxMutants)
	}
	if !cfg.Verbose {
		t.Error("Verbose должен быть true")
	}
	if cfg.Timeout != 60*time.Second {
		t.Errorf("Timeout = %v", cfg.Timeout)
	}
	if cfg.OutputFile != "report.json" {
		t.Errorf("OutputFile = %q", cfg.OutputFile)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	f, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	f.WriteString("{ невалидный yaml :")
	f.Close()

	_, err = Load(f.Name())
	if err == nil {
		t.Error("ожидалась ошибка для невалидного YAML")
	}
}

func TestLoad_PartialYAML(t *testing.T) {
	f, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	// Только часть полей — остальные должны взяться из Default
	f.WriteString("llm_model: \"gpt-4o\"\n")
	f.Close()

	cfg, err := Load(f.Name())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLMModel != "gpt-4o" {
		t.Errorf("LLMModel = %q, want gpt-4o", cfg.LLMModel)
	}
	// Дефолтные значения должны сохраниться
	if cfg.LLMBaseURL != "https://api.openai.com/v1" {
		t.Errorf("LLMBaseURL должен остаться дефолтным, got %q", cfg.LLMBaseURL)
	}
}
