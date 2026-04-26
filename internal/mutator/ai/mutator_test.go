package ai

import (
	"os"
	"testing"
)

func TestNew_BasicCreation(t *testing.T) {
	m := New("http://localhost:11434/v1", "test-key", "llama3", false)
	if m == nil {
		t.Fatal("New вернул nil")
	}
	if m.model != "llama3" {
		t.Errorf("model = %q, want llama3", m.model)
	}
	if m.structuredOutput {
		t.Error("structuredOutput должен быть false")
	}
}

func TestNew_DefaultModel(t *testing.T) {
	m := New("", "", "", true)
	if m.model != "gpt-4o-mini" {
		t.Errorf("model по умолчанию = %q, want gpt-4o-mini", m.model)
	}
	if !m.structuredOutput {
		t.Error("structuredOutput должен быть true")
	}
}

func TestNew_EmptyAPIKey_NoEnv(t *testing.T) {
	// Убедимся что переменная окружения не задана
	old := os.Getenv("OPENAI_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	defer os.Setenv("OPENAI_API_KEY", old)

	m := New("http://localhost:11434/v1", "", "llama3", false)
	if m == nil {
		t.Fatal("New вернул nil")
	}
	// Клиент должен быть создан с заглушечным ключом
}

func TestNew_WithEnvAPIKey(t *testing.T) {
	old := os.Getenv("OPENAI_API_KEY")
	os.Setenv("OPENAI_API_KEY", "env-key")
	defer os.Setenv("OPENAI_API_KEY", old)

	m := New("", "", "gpt-4o", true)
	if m == nil {
		t.Fatal("New вернул nil")
	}
	if m.model != "gpt-4o" {
		t.Errorf("model = %q, want gpt-4o", m.model)
	}
}

func TestNew_EmptyBaseURL(t *testing.T) {
	// Пустой baseURL — не должно паниковать
	m := New("", "key", "model", true)
	if m == nil {
		t.Error("New вернул nil при пустом baseURL")
	}
}
