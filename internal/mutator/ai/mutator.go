// Пакет ai реализует генератор мутантов на основе LLM через OpenAI-совместимый API.
// Поддерживает любой сервис: OpenAI, Azure OpenAI, Ollama, LM Studio и другие.
//
// Принцип работы:
//  1. Для каждой функции формируется Chain-of-Thought промпт.
//  2. Запрос отправляется в LLM (опционально — со Structured Output).
//  3. Ответ парсится в MutationResponse и преобразуется в мутант.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/Sleeps17/gomutationai/internal/analyzer"
	mut "github.com/Sleeps17/gomutationai/internal/mutator"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"
)

// MutationResponse — структура ответа, которую LLM возвращает в формате JSON.
type MutationResponse struct {
	MutationKind     string `json:"mutation_kind"`
	OperatorName     string `json:"operator_name"`
	Description      string `json:"description"`
	BehavioralImpact string `json:"behavioral_impact"`
	OriginalSnippet  string `json:"original_snippet"`
	MutatedSnippet   string `json:"mutated_snippet"`
}

// validMutationKinds — допустимые значения поля mutation_kind в ответе LLM.
var validMutationKinds = map[string]mut.Kind{
	string(mut.KindTestGap):          mut.KindTestGap,
	string(mut.KindLogicalViolation): mut.KindLogicalViolation,
	string(mut.KindDeveloperMistake): mut.KindDeveloperMistake,
	string(mut.KindPrimitive):        mut.KindPrimitive,
}

// Mutator генерирует мутанты с помощью OpenAI-совместимого LLM API.
type Mutator struct {
	client           openai.Client
	model            string
	structuredOutput bool
	// sem ограничивает число одновременных запросов к LLM.
	sem chan struct{}
}

// New создаёт новый AI-мутатор.
//
//   - baseURL — базовый URL API, например "https://api.openai.com/v1"
//     или "http://localhost:11434/v1" для Ollama.
//   - apiKey — токен доступа; допустима пустая строка для локальных моделей.
//   - model — идентификатор модели, например "gpt-4o-mini" или "llama3".
//   - structuredOutput — включить ли режим Structured Output (JSON Schema).
//   - workers — максимальное число одновременных запросов к LLM (0 = 4).
func New(baseURL, apiKey, model string, structuredOutput bool, workers int) *Mutator {
	opts := []option.RequestOption{}

	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	// Если ключ не передан, SDK автоматически читает OPENAI_API_KEY из окружения.
	// Для локальных моделей без авторизации передаём заглушку.
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	} else if os.Getenv("OPENAI_API_KEY") == "" {
		// Некоторые OpenAI-совместимые серверы требуют непустой ключ
		opts = append(opts, option.WithAPIKey("no-key"))
	}

	if model == "" {
		model = "gpt-4o-mini"
	}
	if workers <= 0 {
		workers = 4
	}

	return &Mutator{
		client:           openai.NewClient(opts...),
		model:            model,
		structuredOutput: structuredOutput,
		sem:              make(chan struct{}, workers),
	}
}

// Generate запрашивает у LLM по одной мутации на каждую функцию в файле.
// Запросы выполняются параллельно с ограничением через семафор Mutator.sem.
func (m *Mutator) Generate(ctx context.Context, fa *analyzer.FileAnalysis) ([]mut.Mutant, error) {
	src, err := os.ReadFile(fa.FilePath)
	if err != nil {
		return nil, err
	}

	type entry struct {
		mutant *mut.Mutant
		err    error
	}

	// Предвыделяем слайс чтобы сохранить порядок функций
	entries := make([]entry, len(fa.Functions))

	var wg sync.WaitGroup
	for i, fn := range fa.Functions {
		wg.Add(1)
		go func(idx int, fn analyzer.FunctionContext) {
			defer wg.Done()

			// Захватываем слот в семафоре — ограничение на параллельные LLM-запросы
			m.sem <- struct{}{}
			defer func() { <-m.sem }()

			if ctx.Err() != nil {
				return
			}
			m2, err := m.generateForFunction(ctx, fn, src, idx)
			entries[idx] = entry{mutant: m2, err: err}
		}(i, fn)
	}
	wg.Wait()

	var all []mut.Mutant
	for _, e := range entries {
		if e.err != nil {
			continue
		}
		if e.mutant != nil {
			all = append(all, *e.mutant)
		}
	}
	return all, nil
}

func (m *Mutator) generateForFunction(
	ctx context.Context,
	fn analyzer.FunctionContext,
	fileSrc []byte,
	idx int,
) (*mut.Mutant, error) {
	prompt := BuildPrompt(fn.Body, fn.File, fn.StartLine, m.structuredOutput, fn.TestBody)

	params := openai.ChatCompletionNewParams{
		Model: m.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(SystemPrompt),
			openai.UserMessage(prompt),
		},
		MaxTokens: param.NewOpt[int64](1024),
	}

	// Подключаем Structured Output, если он включён в конфигурации.
	// Это гарантирует, что модель вернёт строго соответствующий JSON Schema ответ.
	if m.structuredOutput {
		params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        "mutation_response",
					Description: param.NewOpt("Описание мутации для Go-кода"),
					Schema:      MutationJSONSchema,
					Strict:      param.NewOpt(true),
				},
			},
		}
	}

	completion, err := m.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("LLM API: %w", err)
	}

	if len(completion.Choices) == 0 {
		return nil, fmt.Errorf("LLM вернул пустой список вариантов")
	}

	rawJSON := strings.TrimSpace(completion.Choices[0].Message.Content)
	if rawJSON == "" {
		return nil, fmt.Errorf("LLM вернул пустой ответ")
	}

	// На случай если модель всё равно обернула JSON в markdown-блок
	rawJSON = stripMarkdownFences(rawJSON)

	var resp MutationResponse
	if err := json.Unmarshal([]byte(rawJSON), &resp); err != nil {
		return nil, fmt.Errorf("разбор JSON-ответа: %w\nсырой ответ: %s", err, rawJSON)
	}

	if resp.OriginalSnippet == "" || resp.MutatedSnippet == "" {
		return nil, fmt.Errorf("LLM вернул пустые поля original_snippet или mutated_snippet")
	}

	// Отклоняем мутант, если LLM не смогла обосновать изменение поведения —
	// это признак семантически эквивалентной мутации.
	if resp.BehavioralImpact == "" {
		return nil, fmt.Errorf("LLM не указала behavioral_impact — мутация предположительно эквивалентна")
	}

	kind, ok := validMutationKinds[strings.ToLower(strings.TrimSpace(resp.MutationKind))]
	if !ok {
		return nil, fmt.Errorf("LLM вернул неизвестное mutation_kind: %q", resp.MutationKind)
	}

	// Проверяем, что original_snippet действительно присутствует в исходном файле
	if !strings.Contains(string(fileSrc), resp.OriginalSnippet) {
		return nil, fmt.Errorf("original_snippet не найден в файле: %q", resp.OriginalSnippet)
	}

	mutatedSrc := []byte(strings.Replace(string(fileSrc), resp.OriginalSnippet, resp.MutatedSnippet, 1))

	// Определяем номер строки по первому вхождению original_snippet
	lineNum := fn.StartLine
	for i, line := range strings.Split(string(fileSrc), "\n") {
		if strings.Contains(line, resp.OriginalSnippet) {
			lineNum = i + 1
			break
		}
	}

	return &mut.Mutant{
		ID:               fmt.Sprintf("ai_%s_%d", fn.Name, idx),
		File:             fn.File,
		Line:             lineNum,
		OperatorName:     resp.OperatorName,
		Kind:             kind,
		Description:      resp.Description,
		BehavioralImpact: resp.BehavioralImpact,
		TargetTest:       fn.TestName,
		Original:         resp.OriginalSnippet,
		Mutated:          resp.MutatedSnippet,
		MutatedSrc:       mutatedSrc,
		Status:           mut.StatusPending,
	}, nil
}

// stripMarkdownFences удаляет обёртку ```json ... ``` если модель её добавила.
func stripMarkdownFences(s string) string {
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
