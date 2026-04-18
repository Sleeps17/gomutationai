// Пакет ai содержит промпты и схему ответа для LLM-генератора мутантов.
package ai

import "fmt"

// SystemPrompt — системная инструкция для LLM-сессии.
// Определяет роль модели и формат ответа.
const SystemPrompt = `Ты — движок мутационного тестирования. Твоя задача — вносить
реалистичные, семантически осмысленные ошибки в исходный код на Go,
чтобы оценить качество тестового набора.
Ты всегда отвечаешь строго в соответствии с заданной JSON-схемой и больше ничего не пишешь.`

// MutationJSONSchema — JSON Schema для Structured Output.
// Описывает структуру ответа, которую LLM должна вернуть.
// Используется с моделями, поддерживающими режим Structured Output.
var MutationJSONSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"operator_name": map[string]any{
			"type":        "string",
			"description": "Короткое название оператора мутации, например OffByOneError",
		},
		"description": map[string]any{
			"type":        "string",
			"description": "Одно предложение, объясняющее внесённый дефект",
		},
		"original_snippet": map[string]any{
			"type":        "string",
			"description": "Точная подстрока из исходного кода, которую нужно заменить",
		},
		"mutated_snippet": map[string]any{
			"type":        "string",
			"description": "Текст замены — синтаксически корректный Go-код",
		},
	},
	"required":             []string{"operator_name", "description", "original_snippet", "mutated_snippet"},
	"additionalProperties": false,
}

// BuildPrompt формирует Chain-of-Thought промпт для LLM.
// Модель должна:
//  1. Проанализировать логику функции.
//  2. Выбрать одну семантически значимую точку мутации.
//  3. Вернуть структурированный ответ в формате JSON.
//
// Параметр useStructuredOutput указывает, ожидается ли ответ через
// Structured Output API (тогда JSON-инструкции в промпте не нужны).
func BuildPrompt(funcBody, fileName string, lineHint int, useStructuredOutput bool) string {
	formatInstructions := ""
	if !useStructuredOutput {
		// При обычном текстовом режиме явно задаём формат ответа в промпте
		formatInstructions = `
## Формат ответа
Верни ТОЛЬКО валидный JSON без markdown-разметки:
{
  "operator_name": "<название оператора>",
  "description": "<одно предложение об ошибке>",
  "original_snippet": "<точная подстрока для замены>",
  "mutated_snippet": "<текст замены>"
}
`
	}

	return fmt.Sprintf(`Ты — эксперт по тестированию программного обеспечения на Go.

Сгенерируй ОДНУ реалистичную мутацию для функции ниже.
Мутация должна имитировать реальную ошибку разработчика — не просто случайную замену токена.

## Правила
- Мутируй ТОЛЬКО ОДНО место в функции.
- Не добавляй импорты и не меняй сигнатуру функции.
- "original_snippet" — это точная подстрока, присутствующая в коде.
- "mutated_snippet" — валидный Go-код, замещающий original_snippet.
%s
## Функция для мутации
// Файл: %s  (область — строка %d)
%s
`, formatInstructions, fileName, lineHint, funcBody)
}
