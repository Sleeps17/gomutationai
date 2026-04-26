package ai

import (
	"os"
	"testing"
)

func TestNew_BasicCreation(t *testing.T) {
	m := New("http://localhost:11434/v1", "test-key", "llama3", false, 2)
	if m == nil {
		t.Fatal("New вернул nil")
	}
	if m.model != "llama3" {
		t.Errorf("model = %q, want llama3", m.model)
	}
	if m.structuredOutput {
		t.Error("structuredOutput должен быть false")
	}
	if cap(m.sem) != 2 {
		t.Errorf("cap(sem) = %d, want 2", cap(m.sem))
	}
}

func TestNew_DefaultModel(t *testing.T) {
	m := New("", "", "", true, 0)
	if m.model != "gpt-4o-mini" {
		t.Errorf("model по умолчанию = %q, want gpt-4o-mini", m.model)
	}
	if !m.structuredOutput {
		t.Error("structuredOutput должен быть true")
	}
}

func TestNew_DefaultWorkers(t *testing.T) {
	m := New("", "", "model", false, 0)
	if cap(m.sem) != 4 {
		t.Errorf("workers по умолчанию = %d, want 4", cap(m.sem))
	}
}

func TestNew_EmptyAPIKey_NoEnv(t *testing.T) {
	old := os.Getenv("OPENAI_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	defer os.Setenv("OPENAI_API_KEY", old)

	m := New("http://localhost:11434/v1", "", "llama3", false, 1)
	if m == nil {
		t.Fatal("New вернул nil")
	}
}

func TestNew_WithEnvAPIKey(t *testing.T) {
	old := os.Getenv("OPENAI_API_KEY")
	os.Setenv("OPENAI_API_KEY", "env-key")
	defer os.Setenv("OPENAI_API_KEY", old)

	m := New("", "", "gpt-4o", true, 1)
	if m == nil {
		t.Fatal("New вернул nil")
	}
	if m.model != "gpt-4o" {
		t.Errorf("model = %q, want gpt-4o", m.model)
	}
}

func TestNew_EmptyBaseURL(t *testing.T) {
	m := New("", "key", "model", true, 1)
	if m == nil {
		t.Error("New вернул nil при пустом baseURL")
	}
}
