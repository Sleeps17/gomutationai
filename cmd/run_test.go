package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Sleeps17/gomutationai/internal/config"
	"github.com/Sleeps17/gomutationai/internal/mutator"
	"github.com/spf13/cobra"
)

// ─── truncate ────────────────────────────────────────────────────────────────

func TestTruncate_Short(t *testing.T) {
	if got := truncate("hi", 10); got != "hi" {
		t.Errorf("truncate(short) = %q, want hi", got)
	}
}

func TestTruncate_Equal(t *testing.T) {
	if got := truncate("hello", 5); got != "hello" {
		t.Errorf("truncate(equal) = %q, want hello", got)
	}
}

func TestTruncate_Long(t *testing.T) {
	got := truncate("hello world", 5)
	if got != "hello…" {
		t.Errorf("truncate(long) = %q, want hello…", got)
	}
}

func TestTruncate_Empty(t *testing.T) {
	if got := truncate("", 10); got != "" {
		t.Errorf("truncate(empty) = %q, want \"\"", got)
	}
}

// ─── effectiveWorkers / effectiveLLMWorkers ──────────────────────────────────

func TestEffectiveWorkers_PositiveStaysSame(t *testing.T) {
	if got := effectiveWorkers(7); got != 7 {
		t.Errorf("effectiveWorkers(7) = %d, want 7", got)
	}
}

func TestEffectiveWorkers_ZeroFallsBackToNumCPU(t *testing.T) {
	if got := effectiveWorkers(0); got != runtime.NumCPU() {
		t.Errorf("effectiveWorkers(0) = %d, want %d (NumCPU)", got, runtime.NumCPU())
	}
}

func TestEffectiveWorkers_NegativeFallsBack(t *testing.T) {
	if got := effectiveWorkers(-3); got != runtime.NumCPU() {
		t.Errorf("effectiveWorkers(-3) должен дать NumCPU, got %d", got)
	}
}

func TestEffectiveLLMWorkers_PositiveStaysSame(t *testing.T) {
	if got := effectiveLLMWorkers(5); got != 5 {
		t.Errorf("effectiveLLMWorkers(5) = %d, want 5", got)
	}
}

func TestEffectiveLLMWorkers_ZeroDefaults(t *testing.T) {
	if got := effectiveLLMWorkers(0); got != 4 {
		t.Errorf("effectiveLLMWorkers(0) = %d, want 4", got)
	}
}

func TestEffectiveLLMWorkers_NegativeDefaults(t *testing.T) {
	if got := effectiveLLMWorkers(-1); got != 4 {
		t.Errorf("effectiveLLMWorkers(-1) = %d, want 4", got)
	}
}

// ─── applyFlags ──────────────────────────────────────────────────────────────

func TestApplyFlags_OverridesAllStringFlags(t *testing.T) {
	cfg = &config.Config{}
	cmd := freshRunCmd()
	cmd.SetArgs([]string{
		"--llm-url", "http://example.com",
		"--llm-key", "secret",
		"--model", "gpt-4o",
		"--output", "out.json",
	})
	if err := cmd.ParseFlags([]string{
		"--llm-url=http://example.com",
		"--llm-key=secret",
		"--model=gpt-4o",
		"--output=out.json",
	}); err != nil {
		t.Fatal(err)
	}
	applyFlags(cmd)

	if cfg.LLMBaseURL != "http://example.com" {
		t.Errorf("LLMBaseURL = %q", cfg.LLMBaseURL)
	}
	if cfg.LLMAPIKey != "secret" {
		t.Errorf("LLMAPIKey = %q", cfg.LLMAPIKey)
	}
	if cfg.LLMModel != "gpt-4o" {
		t.Errorf("LLMModel = %q", cfg.LLMModel)
	}
	if cfg.OutputFile != "out.json" {
		t.Errorf("OutputFile = %q", cfg.OutputFile)
	}
}

func TestApplyFlags_TimeoutAndWorkers(t *testing.T) {
	cfg = &config.Config{}
	cmd := freshRunCmd()
	if err := cmd.ParseFlags([]string{
		"--timeout=12s",
		"--workers=8",
		"--llm-workers=3",
		"--max-mutants=20",
		"--verbose",
	}); err != nil {
		t.Fatal(err)
	}
	applyFlags(cmd)

	if cfg.Timeout != 12*time.Second {
		t.Errorf("Timeout = %v, want 12s", cfg.Timeout)
	}
	if cfg.Workers != 8 {
		t.Errorf("Workers = %d", cfg.Workers)
	}
	if cfg.LLMWorkers != 3 {
		t.Errorf("LLMWorkers = %d", cfg.LLMWorkers)
	}
	if cfg.MaxMutants != 20 {
		t.Errorf("MaxMutants = %d", cfg.MaxMutants)
	}
	if !cfg.Verbose {
		t.Error("Verbose должен быть true")
	}
}

func TestApplyFlags_DefaultTimeoutWhenZero(t *testing.T) {
	cfg = &config.Config{} // Timeout = 0
	cmd := freshRunCmd()
	// Не передаём --timeout — должен сработать дефолт 30s
	if err := cmd.ParseFlags([]string{}); err != nil {
		t.Fatal(err)
	}
	applyFlags(cmd)
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout по умолчанию = %v, want 30s", cfg.Timeout)
	}
}

func TestApplyFlags_StructuredOutput(t *testing.T) {
	cfg = &config.Config{}
	cmd := freshRunCmd()
	if err := cmd.ParseFlags([]string{"--structured-output=false"}); err != nil {
		t.Fatal(err)
	}
	applyFlags(cmd)
	if cfg.StructuredOutput {
		t.Error("StructuredOutput должен быть false")
	}
}

// ─── printMutantList ─────────────────────────────────────────────────────────

func TestPrintMutantList_WritesHeaderAndRows(t *testing.T) {
	mutants := []mutator.Mutant{
		{
			ID:           "m1",
			OperatorName: "BoundaryInvariant",
			Line:         42,
			Description:  "off-by-one в Add",
		},
		{
			ID:           "m2",
			OperatorName: "ErrorContract",
			Line:         99,
			Description:  strings.Repeat("очень длинное описание ", 10),
		},
	}

	out := captureStdout(t, func() { printMutantList(mutants) })

	if !strings.Contains(out, "ID") || !strings.Contains(out, "Оператор") {
		t.Errorf("вывод не содержит заголовок таблицы:\n%s", out)
	}
	if !strings.Contains(out, "m1") || !strings.Contains(out, "m2") {
		t.Errorf("вывод не содержит IDs:\n%s", out)
	}
	if !strings.Contains(out, "BoundaryInvariant") {
		t.Errorf("вывод не содержит operator name:\n%s", out)
	}
}

func TestPrintMutantList_EmptyDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("printMutantList(nil) запаниковал: %v", r)
		}
	}()
	out := captureStdout(t, func() { printMutantList(nil) })
	if !strings.Contains(out, "ID") {
		t.Errorf("даже на пустом списке должен быть заголовок:\n%s", out)
	}
}

// ─── runMutation: end-to-end через mock OpenAI и testdata ─────────────────────

func TestRunMutation_NoGoFiles(t *testing.T) {
	cfg = &config.Config{Timeout: 5 * time.Second}
	cmd := freshRunCmd()
	tmp := t.TempDir()
	cmd.SetArgs([]string{tmp})
	if err := cmd.ParseFlags([]string{}); err != nil {
		t.Fatal(err)
	}
	err := runMutation(cmd, []string{tmp})
	if err == nil {
		t.Error("ожидалась ошибка для пустой директории без Go-файлов")
	}
}

func TestRunMutation_DryRun_FullPath(t *testing.T) {
	srv := mockOpenAIWithKilling()
	defer srv.Close()

	dir := findTestdataExample(t)
	out := filepath.Join(t.TempDir(), "report.json")
	cfg = &config.Config{}
	cmd := freshRunCmd()
	args := []string{
		"--llm-url", srv.URL,
		"--llm-key", "fake",
		"--model", "gpt-4o-mini",
		"--max-mutants", "2",
		"--timeout", "10s",
		"--workers", "1",
		"--llm-workers", "1",
		"--dry-run",
		"--output", out,
		"--structured-output=false",
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if err := runMutation(cmd, []string{dir}); err != nil {
			t.Errorf("runMutation: %v", err)
		}
	})
}

func TestRunMutation_FullCycle(t *testing.T) {
	srv := mockOpenAIWithKilling()
	defer srv.Close()

	dir := findTestdataExample(t)
	out := filepath.Join(t.TempDir(), "report.json")
	cfg = &config.Config{}
	cmd := freshRunCmd()
	args := []string{
		"--llm-url", srv.URL,
		"--llm-key", "fake",
		"--model", "gpt-4o-mini",
		"--max-mutants", "1",
		"--timeout", "30s",
		"--workers", "1",
		"--llm-workers", "1",
		"--output", out,
		"--verbose",
		"--structured-output=false",
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if err := runMutation(cmd, []string{dir}); err != nil {
			t.Errorf("runMutation: %v", err)
		}
	})
	if _, err := os.Stat(out); err != nil {
		t.Errorf("JSON-отчёт не создан: %v", err)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// freshRunCmd создаёт новый cobra.Command с тем же набором флагов, что и runCmd,
// чтобы тесты не делили глобальное состояние флагов между прогонами.
func freshRunCmd() *cobra.Command {
	c := &cobra.Command{Use: "run"}
	c.Flags().String("llm-url", "", "")
	c.Flags().String("llm-key", "", "")
	c.Flags().String("model", "", "")
	c.Flags().Bool("structured-output", true, "")
	c.Flags().Int("llm-workers", 0, "")
	c.Flags().Duration("timeout", 0, "")
	c.Flags().Int("workers", 0, "")
	c.Flags().Int("max-mutants", 0, "")
	c.Flags().Bool("verbose", false, "")
	c.Flags().Bool("dry-run", false, "")
	c.Flags().Int("callee-depth", 1, "")
	c.Flags().String("output", "", "")
	return c
}

// mockOpenAIWithKilling возвращает httptest-сервер, который отвечает мутацией,
// убиваемой тестами в testdata/example (заменяет "return a + b" на "return a - b").
func mockOpenAIWithKilling() *httptest.Server {
	mut := map[string]string{
		"mutation_kind":     "developer-mistake",
		"operator_name":     "BoundaryInvariant",
		"description":       "off-by-one",
		"behavioral_impact": "Для Add(2,3) оригинал=5, мутант=-1.",
		"original_snippet":  "return a + b",
		"mutated_snippet":   "return a - b",
	}
	mutBody, _ := json.Marshal(mut)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": string(mutBody)}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// findTestdataExample возвращает абсолютный путь к testdata/example в корне модуля.
func findTestdataExample(t *testing.T) string {
	t.Helper()
	// cmd/run_test.go → корень модуля = ../testdata/example
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "testdata", "example")
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skipf("testdata/example отсутствует: %v", err)
	}
	return abs
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()
	_ = w.Close()
	<-done
	return buf.String()
}
