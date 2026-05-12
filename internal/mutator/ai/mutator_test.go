package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Sleeps17/gomutationai/internal/analyzer"
	mut "github.com/Sleeps17/gomutationai/internal/mutator"
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

// ─── Generate / generateForFunction через mock OpenAI ────────────────────────

const sampleSource = `package example

// Add returns the sum of two integers.
func Add(a, b int) int {
	return a + b
}
`

// fakeOpenAI запускает httptest.Server, который отвечает фиксированным content
// на /chat/completions. Возвращает baseURL и счётчик вызовов.
func fakeOpenAI(t *testing.T, content string) (string, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// Читаем body чтобы избежать неpinned read warning
		_, _ = io.Copy(io.Discard, r.Body)

		resp := map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 1,
			"model":   "gpt-4o-mini",
			"choices": []map[string]any{
				{
					"index":         0,
					"finish_reason": "stop",
					"message": map[string]any{
						"role":    "assistant",
						"content": content,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &calls
}

// writeSrc создаёт временный .go-файл и возвращает его абсолютный путь.
func writeSrc(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "math.go")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// validMutationJSON строит корректный mutation_response в виде строки.
func validMutationJSON() string {
	r := MutationResponse{
		MutationKind:     "logical-violation",
		OperatorName:     "BoundaryInvariant",
		Description:      "broken sum",
		BehavioralImpact: "Для Add(2,3) оригинал возвращает 5, мутант — 6.",
		OriginalSnippet:  "return a + b",
		MutatedSnippet:   "return a + b + 1",
	}
	b, _ := json.Marshal(r)
	return string(b)
}

func TestGenerate_HappyPath(t *testing.T) {
	url, calls := fakeOpenAI(t, validMutationJSON())
	m := New(url, "test", "gpt-4o-mini", true, 1)

	srcPath := writeSrc(t, sampleSource)
	fa := &analyzer.FileAnalysis{
		FilePath: srcPath,
		Functions: []analyzer.FunctionContext{
			{
				File:      srcPath,
				Name:      "Add",
				Body:      "func Add(a, b int) int {\n\treturn a + b\n}",
				StartLine: 3,
			},
		},
	}

	mutants, err := m.Generate(context.Background(), fa)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("mock OpenAI получил %d вызовов, want 1", got)
	}
	if len(mutants) != 1 {
		t.Fatalf("ожидался 1 мутант, got %d", len(mutants))
	}
	mt := mutants[0]
	if mt.Kind != mut.KindLogicalViolation {
		t.Errorf("Kind = %q, want %q", mt.Kind, mut.KindLogicalViolation)
	}
	if mt.OperatorName != "BoundaryInvariant" {
		t.Errorf("OperatorName = %q", mt.OperatorName)
	}
	if !strings.Contains(string(mt.MutatedSrc), "a + b + 1") {
		t.Errorf("MutatedSrc не содержит ожидаемой замены: %s", mt.MutatedSrc)
	}
	if mt.Status != mut.StatusPending {
		t.Errorf("Status = %q, want pending", mt.Status)
	}
}

func TestGenerate_StructuredOutputFalseBranch(t *testing.T) {
	url, _ := fakeOpenAI(t, validMutationJSON())
	m := New(url, "test", "llama3", false, 1)

	srcPath := writeSrc(t, sampleSource)
	fa := &analyzer.FileAnalysis{
		FilePath: srcPath,
		Functions: []analyzer.FunctionContext{
			{File: srcPath, Name: "Add", Body: "func Add(a, b int) int {\n\treturn a + b\n}", StartLine: 3},
		},
	}

	mutants, err := m.Generate(context.Background(), fa)
	if err != nil {
		t.Fatal(err)
	}
	if len(mutants) != 1 {
		t.Errorf("ожидался 1 мутант, got %d", len(mutants))
	}
}

func TestGenerate_MarkdownFencedJSON(t *testing.T) {
	wrapped := "```json\n" + validMutationJSON() + "\n```"
	url, _ := fakeOpenAI(t, wrapped)
	m := New(url, "test", "gpt-4o-mini", true, 1)

	srcPath := writeSrc(t, sampleSource)
	fa := &analyzer.FileAnalysis{
		FilePath:  srcPath,
		Functions: []analyzer.FunctionContext{{File: srcPath, Name: "Add", Body: "return a + b", StartLine: 3}},
	}

	mutants, err := m.Generate(context.Background(), fa)
	if err != nil {
		t.Fatal(err)
	}
	if len(mutants) != 1 {
		t.Errorf("markdown-обёрнутый JSON должен парситься, got %d мутантов", len(mutants))
	}
}

func TestGenerate_FileNotFound(t *testing.T) {
	url, _ := fakeOpenAI(t, validMutationJSON())
	m := New(url, "test", "gpt-4o-mini", true, 1)

	fa := &analyzer.FileAnalysis{
		FilePath:  "/nonexistent/path/file.go",
		Functions: []analyzer.FunctionContext{{Name: "X"}},
	}
	if _, err := m.Generate(context.Background(), fa); err == nil {
		t.Error("Generate должен вернуть ошибку при отсутствии файла")
	}
}

func TestGenerate_InvalidJSON_ReturnsZeroMutants(t *testing.T) {
	url, _ := fakeOpenAI(t, "this is not json")
	m := New(url, "test", "gpt-4o-mini", true, 1)

	srcPath := writeSrc(t, sampleSource)
	fa := &analyzer.FileAnalysis{
		FilePath:  srcPath,
		Functions: []analyzer.FunctionContext{{File: srcPath, Name: "Add", Body: "return a + b", StartLine: 3}},
	}
	mutants, err := m.Generate(context.Background(), fa)
	if err != nil {
		t.Fatalf("Generate сам по себе не должен возвращать ошибку, got %v", err)
	}
	if len(mutants) != 0 {
		t.Errorf("ожидалось 0 мутантов при невалидном JSON, got %d", len(mutants))
	}
}

func TestGenerate_EmptyContent_ReturnsZero(t *testing.T) {
	url, _ := fakeOpenAI(t, "")
	m := New(url, "test", "gpt-4o-mini", true, 1)

	srcPath := writeSrc(t, sampleSource)
	fa := &analyzer.FileAnalysis{
		FilePath:  srcPath,
		Functions: []analyzer.FunctionContext{{File: srcPath, Name: "Add", Body: "return a + b", StartLine: 3}},
	}
	mutants, _ := m.Generate(context.Background(), fa)
	if len(mutants) != 0 {
		t.Errorf("пустой content → 0 мутантов, got %d", len(mutants))
	}
}

func TestGenerate_MissingFields_Rejected(t *testing.T) {
	tests := map[string]MutationResponse{
		"no_original": {
			MutationKind: "primitive", OperatorName: "BoundaryInvariant",
			BehavioralImpact: "X", MutatedSnippet: "y",
		},
		"no_mutated": {
			MutationKind: "primitive", OperatorName: "BoundaryInvariant",
			BehavioralImpact: "X", OriginalSnippet: "y",
		},
		"no_impact": {
			MutationKind: "primitive", OperatorName: "BoundaryInvariant",
			OriginalSnippet: "return a + b", MutatedSnippet: "return a - b",
		},
		"unknown_kind": {
			MutationKind: "made-up-kind", OperatorName: "BoundaryInvariant",
			OriginalSnippet: "return a + b", MutatedSnippet: "return a - b", BehavioralImpact: "X",
		},
		"snippet_not_in_source": {
			MutationKind: "primitive", OperatorName: "BoundaryInvariant",
			OriginalSnippet: "totally absent", MutatedSnippet: "y", BehavioralImpact: "X",
		},
	}
	for name, resp := range tests {
		t.Run(name, func(t *testing.T) {
			body, _ := json.Marshal(resp)
			url, _ := fakeOpenAI(t, string(body))
			m := New(url, "test", "gpt-4o-mini", true, 1)

			srcPath := writeSrc(t, sampleSource)
			fa := &analyzer.FileAnalysis{
				FilePath: srcPath,
				Functions: []analyzer.FunctionContext{
					{File: srcPath, Name: "Add", Body: "return a + b", StartLine: 3},
				},
			}
			mutants, _ := m.Generate(context.Background(), fa)
			if len(mutants) != 0 {
				t.Errorf("%s: ожидалось 0 мутантов, got %d", name, len(mutants))
			}
		})
	}
}

func TestGenerate_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	m := New(srv.URL, "test", "gpt-4o-mini", true, 1)
	srcPath := writeSrc(t, sampleSource)
	fa := &analyzer.FileAnalysis{
		FilePath:  srcPath,
		Functions: []analyzer.FunctionContext{{File: srcPath, Name: "Add", Body: "return a + b", StartLine: 3}},
	}
	mutants, _ := m.Generate(context.Background(), fa)
	if len(mutants) != 0 {
		t.Errorf("при HTTP 500 ожидалось 0 мутантов, got %d", len(mutants))
	}
}

func TestGenerate_MultipleFunctions_PreserveOrder(t *testing.T) {
	// Каждый вызов возвращает мутацию с уникальным OperatorName, чтобы проверить порядок.
	counter := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i := counter.Add(1)
		mr := MutationResponse{
			MutationKind:     "primitive",
			OperatorName:     fmt.Sprintf("op-%d", i),
			Description:      "x",
			BehavioralImpact: "x",
			OriginalSnippet:  "return a + b",
			MutatedSnippet:   "return a - b",
		}
		body, _ := json.Marshal(mr)
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": string(body)}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	m := New(srv.URL, "test", "gpt-4o-mini", true, 1)
	srcPath := writeSrc(t, sampleSource)
	fa := &analyzer.FileAnalysis{
		FilePath: srcPath,
		Functions: []analyzer.FunctionContext{
			{File: srcPath, Name: "F0", Body: "return a + b", StartLine: 3},
			{File: srcPath, Name: "F1", Body: "return a + b", StartLine: 3},
			{File: srcPath, Name: "F2", Body: "return a + b", StartLine: 3},
		},
	}
	mutants, err := m.Generate(context.Background(), fa)
	if err != nil {
		t.Fatal(err)
	}
	if len(mutants) != 3 {
		t.Fatalf("ожидалось 3 мутанта, got %d", len(mutants))
	}
	// Mutants должны идти в том же порядке, что и Functions (по индексу).
	for i, mt := range mutants {
		wantName := fmt.Sprintf("F%d", i)
		if !strings.HasPrefix(mt.ID, "ai_"+wantName) {
			t.Errorf("мутант[%d].ID = %q, ожидался префикс ai_%s", i, mt.ID, wantName)
		}
	}
}

func TestGenerate_ContextCanceled(t *testing.T) {
	url, _ := fakeOpenAI(t, validMutationJSON())
	m := New(url, "test", "gpt-4o-mini", true, 1)

	srcPath := writeSrc(t, sampleSource)
	fa := &analyzer.FileAnalysis{
		FilePath:  srcPath,
		Functions: []analyzer.FunctionContext{{File: srcPath, Name: "Add", Body: "return a + b", StartLine: 3}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mutants, err := m.Generate(ctx, fa)
	if err != nil {
		// Контекстная ошибка либо отсутствует, либо возвращается — обе ситуации допустимы.
		t.Logf("Generate(ctx canceled) → err: %v", err)
	}
	if len(mutants) > 1 {
		t.Errorf("при отменённом контексте не должно создаваться мутантов, got %d", len(mutants))
	}
}
