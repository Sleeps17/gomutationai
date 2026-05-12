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

func TestBuildPrompt_WithTestBody_DoesNotEncourageTestFitting(t *testing.T) {
	testBody := "func TestAdd(t *testing.T) { ... }"
	prompt := BuildPrompt("func Add(){}", "f.go", 1, true, testBody)

	if strings.Contains(prompt, "Выбирай мутацию, которую этот тест НЕ обнаружит") {
		t.Error("промпт не должен напрямую требовать подгонять мутацию под тест")
	}
	if !strings.Contains(prompt, "Не подгоняй мутацию") {
		t.Error("промпт должен запрещать подгонку мутации под конкретные assert-ы")
	}
	if !strings.Contains(prompt, "пробел проверки") {
		t.Error("промпт должен требовать указать пробел проверки, если он виден из теста")
	}
	if !strings.Contains(prompt, "не выдумывай") {
		t.Error("промпт должен запрещать выдумывать пробелы теста")
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

func TestBuildPrompt_ContainsRealisticLogicalMutationGuidance(t *testing.T) {
	prompt := BuildPrompt("func F(){}", "f.go", 1, true, "")
	if !strings.Contains(prompt, "реальную логическую ошибку") {
		t.Error("промпт должен ориентировать модель на логические ошибки")
	}
	if !strings.Contains(prompt, "неверное граничное условие") {
		t.Error("промпт должен перечислять правдоподобные типы логических ошибок")
	}
	if !strings.Contains(prompt, "делает код хуже") {
		t.Error("промпт должен запрещать мутации-исправления")
	}
	if !strings.Contains(prompt, "выглядит как исправление") {
		t.Error("промпт должен явно запрещать исправляющие мутации")
	}
	if !strings.Contains(prompt, "Для входа/вызова X оригинал делает Y, мутант делает Z") {
		t.Error("промпт должен требовать конкретный behavioral_impact")
	}
}

func TestBuildPrompt_ContainsOperatorCategories(t *testing.T) {
	prompt := BuildPrompt("func F(){}", "f.go", 1, true, "")
	for _, name := range []string{"BoundaryCondition", "BooleanLogic", "WrongVariable", "ResourceLifecycle"} {
		if !strings.Contains(prompt, name) {
			t.Errorf("промпт должен содержать категорию оператора %q", name)
		}
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

func TestMutationJSONSchema_OperatorNameEnum(t *testing.T) {
	props, ok := MutationJSONSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties должен быть map[string]any")
	}
	operator, ok := props["operator_name"].(map[string]any)
	if !ok {
		t.Fatal("operator_name должен быть map[string]any")
	}
	enum, ok := operator["enum"].([]string)
	if !ok {
		t.Fatal("operator_name должен содержать enum []string")
	}
	if len(enum) != len(MutationOperatorNames) {
		t.Fatalf("operator_name enum length = %d, want %d", len(enum), len(MutationOperatorNames))
	}
	for _, want := range MutationOperatorNames {
		found := false
		for _, got := range enum {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("operator_name enum должен содержать %q", want)
		}
	}
}
