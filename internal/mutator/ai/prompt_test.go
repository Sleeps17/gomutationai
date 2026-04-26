package ai

import (
	"strings"
	"testing"
)

func TestBuildPrompt_ContainsFuncBody(t *testing.T) {
	body := "func Add(a, b int) int { return a + b }"
	prompt := BuildPrompt(body, "math.go", 10, true, "")
	if !strings.Contains(prompt, body) {
		t.Error("промпт должен содержать тело функции")
	}
}

func TestBuildPrompt_ContainsFileName(t *testing.T) {
	prompt := BuildPrompt("func F(){}", "myfile.go", 5, true, "")
	if !strings.Contains(prompt, "myfile.go") {
		t.Error("промпт должен содержать имя файла")
	}
}

func TestBuildPrompt_ContainsLineHint(t *testing.T) {
	prompt := BuildPrompt("func F(){}", "f.go", 42, true, "")
	if !strings.Contains(prompt, "42") {
		t.Error("промпт должен содержать номер строки")
	}
}

func TestBuildPrompt_StructuredOutputTrue_NoFormatInstructions(t *testing.T) {
	prompt := BuildPrompt("func F(){}", "f.go", 1, true, "")
	// При structured output инструкции по формату не нужны в промпте
	if strings.Contains(prompt, "Верни ТОЛЬКО валидный JSON") {
		t.Error("при structured_output=true не должно быть JSON-инструкций в промпте")
	}
}

func TestBuildPrompt_StructuredOutputFalse_HasFormatInstructions(t *testing.T) {
	prompt := BuildPrompt("func F(){}", "f.go", 1, false, "")
	if !strings.Contains(prompt, "Верни ТОЛЬКО валидный JSON") {
		t.Error("при structured_output=false должны быть JSON-инструкции в промпте")
	}
	if !strings.Contains(prompt, "operator_name") {
		t.Error("промпт должен содержать поля JSON схемы")
	}
	if !strings.Contains(prompt, "behavioral_impact") {
		t.Error("промпт должен содержать поле behavioral_impact")
	}
}

func TestBuildPrompt_WithTestBody(t *testing.T) {
	testBody := "func TestAdd(t *testing.T) { ... }"
	prompt := BuildPrompt("func Add(){}", "f.go", 1, true, testBody)
	if !strings.Contains(prompt, testBody) {
		t.Error("промпт должен содержать тело теста")
	}
	if !strings.Contains(prompt, "Тест, покрывающий функцию") {
		t.Error("промпт должен содержать заголовок секции теста")
	}
}

func TestBuildPrompt_WithoutTestBody_NoTestSection(t *testing.T) {
	prompt := BuildPrompt("func F(){}", "f.go", 1, true, "")
	if strings.Contains(prompt, "Тест, покрывающий функцию") {
		t.Error("без testBody не должно быть секции теста")
	}
}

func TestBuildPrompt_ContainsAntiEquivalenceRule(t *testing.T) {
	prompt := BuildPrompt("func F(){}", "f.go", 1, true, "")
	if !strings.Contains(prompt, "ЗАПРЕЩЕНО") {
		t.Error("промпт должен содержать запрет на эквивалентные мутации")
	}
}

func TestBuildPrompt_NonEmpty(t *testing.T) {
	prompt := BuildPrompt("func F(){}", "f.go", 1, true, "")
	if strings.TrimSpace(prompt) == "" {
		t.Error("BuildPrompt не должен возвращать пустую строку")
	}
}

// ── stripMarkdownFences ─────────────────────────────────────────────────────

func TestStripMarkdownFences_WithJsonBlock(t *testing.T) {
	input := "```json\n{\"key\": \"value\"}\n```"
	got := stripMarkdownFences(input)
	want := "{\"key\": \"value\"}"
	if got != want {
		t.Errorf("stripMarkdownFences = %q, want %q", got, want)
	}
}

func TestStripMarkdownFences_WithPlainBlock(t *testing.T) {
	input := "```\n{\"key\": \"value\"}\n```"
	got := stripMarkdownFences(input)
	want := "{\"key\": \"value\"}"
	if got != want {
		t.Errorf("stripMarkdownFences = %q, want %q", got, want)
	}
}

func TestStripMarkdownFences_NoFences(t *testing.T) {
	input := `{"key": "value"}`
	got := stripMarkdownFences(input)
	if got != input {
		t.Errorf("stripMarkdownFences без фенсов изменил строку: %q", got)
	}
}

func TestStripMarkdownFences_Whitespace(t *testing.T) {
	input := "```json\n  {\"a\": 1}  \n```"
	got := stripMarkdownFences(input)
	if !strings.Contains(got, `{"a": 1}`) {
		t.Errorf("stripMarkdownFences = %q, должен содержать JSON", got)
	}
}

// ── MutationJSONSchema ──────────────────────────────────────────────────────

func TestMutationJSONSchema_RequiredFields(t *testing.T) {
	required, ok := MutationJSONSchema["required"].([]string)
	if !ok {
		t.Fatal("поле required должно быть []string")
	}
	for _, field := range []string{"operator_name", "description", "behavioral_impact", "original_snippet", "mutated_snippet"} {
		found := false
		for _, r := range required {
			if r == field {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("поле %q должно быть в required", field)
		}
	}
}

func TestMutationJSONSchema_NoAdditionalProperties(t *testing.T) {
	if v, ok := MutationJSONSchema["additionalProperties"].(bool); !ok || v {
		t.Error("additionalProperties должен быть false")
	}
}
