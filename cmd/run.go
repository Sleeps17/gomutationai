// Пакет cmd реализует команду run — точку входа для запуска мутационного тестирования.
package cmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Sleeps17/gomutationai/internal/analyzer"
	"github.com/Sleeps17/gomutationai/internal/mutator"
	aimutator "github.com/Sleeps17/gomutationai/internal/mutator/ai"
	"github.com/Sleeps17/gomutationai/internal/reporter"
	"github.com/Sleeps17/gomutationai/internal/runner"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run [директория ...]",
	Short: "Запустить мутационное тестирование Go-пакета",
	Long: `Анализирует один или несколько Go-пакетов, генерирует мутантов с помощью LLM,
запускает тесты и выводит отчёт с метриками качества тестового набора.

Инструмент автоматически находит тестовые файлы (*_test.go) в указанных директориях,
определяет, какие production-функции покрыты тестами, и мутирует только их.`,
	Args: cobra.ArbitraryArgs,
	RunE: runMutation,
}

func init() {
	rootCmd.AddCommand(runCmd)

	// Параметры LLM
	runCmd.Flags().String("llm-url", "", "базовый URL OpenAI-совместимого API (например http://localhost:11434/v1)")
	runCmd.Flags().String("llm-key", "", "токен доступа к LLM (или переменная OPENAI_API_KEY)")
	runCmd.Flags().String("model", "", "идентификатор модели, например gpt-4o-mini или llama3")
	runCmd.Flags().Bool("structured-output", true, "использовать Structured Output / JSON Schema (если модель поддерживает)")
	runCmd.Flags().Int("llm-workers", 0, "число параллельных запросов к LLM (0 = 4)")

	// Управление прогоном
	runCmd.Flags().DurationP("timeout", "t", 0, "таймаут на один тестовый прогон, например 30s")
	runCmd.Flags().IntP("workers", "w", 0, "число параллельных тестовых прогонов (0 = NumCPU)")
	runCmd.Flags().Int("max-mutants", 0, "ограничить число мутантов (0 — без ограничений)")
	runCmd.Flags().BoolP("verbose", "v", false, "выводить статус каждого мутанта")
	runCmd.Flags().Bool("dry-run", false, "только сгенерировать мутантов, не запускать тесты")
	runCmd.Flags().Int("callee-depth", 1, "глубина расширения покрытия по графу вызовов (0 = только напрямую тестируемые функции)")

	// Отчёт
	runCmd.Flags().StringP("output", "o", "", "сохранить JSON-отчёт в файл")
}

func runMutation(cmd *cobra.Command, args []string) error {
	applyFlags(cmd)

	// Один или несколько пакетных директорий; по умолчанию текущая
	dirs := args
	if len(dirs) == 0 {
		dirs = []string{"."}
	}

	calleeDepth, _ := cmd.Flags().GetInt("callee-depth")

	fmt.Printf("gomutationai  модель: %s  пакеты: %s\n\n", cfg.LLMModel, strings.Join(dirs, ", "))

	// ── 1. Анализ исходных файлов ──────────────────────────────────────────
	fmt.Println("→ Анализ исходных файлов...")
	analyses, err := analyzer.AnalyzePackages(dirs)
	if err != nil {
		return fmt.Errorf("анализ провалился: %w", err)
	}
	if len(analyses) == 0 {
		return fmt.Errorf("Go-файлы не найдены в %s", strings.Join(dirs, ", "))
	}

	// ── 2. Определение покрытых тестами функций ────────────────────────────
	fmt.Println("→ Поиск функций, покрытых тестами...")
	tested, err := analyzer.ParseTestedFunctions(dirs)
	if err != nil {
		return fmt.Errorf("анализ тестов: %w", err)
	}

	callGraph := analyzer.BuildCallGraph(analyses)
	coveredContext := analyzer.ExpandWithCalleesContext(tested, callGraph, calleeDepth)

	fmt.Printf("  Покрытых тестами функций: %d (с callees глубиной %d: %d)\n",
		len(tested), calleeDepth, len(coveredContext))

	// Проставляем TestBody и фильтруем функции по покрытию
	totalFuncs := 0
	for _, fa := range analyses {
		filtered := fa.Functions[:0]
		for _, fn := range fa.Functions {
			tf, ok := coveredContext[fn.Name]
			if !ok {
				continue
			}
			fn.TestName = tf.TestName
			fn.TestBody = tf.TestBody
			filtered = append(filtered, fn)
			totalFuncs++
		}
		fa.Functions = filtered
	}
	fmt.Printf("  Функций для мутации: %d\n\n", totalFuncs)

	// ── 3. Генерация мутантов через LLM (параллельно по файлам) ──────────
	fmt.Printf("→ Генерация мутантов через LLM (llm-workers: %d)...\n", effectiveLLMWorkers(cfg.LLMWorkers))
	aiMut := aimutator.New(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel, cfg.StructuredOutput, cfg.LLMWorkers)

	type fileResult struct {
		path    string
		mutants []mutator.Mutant
		err     error
	}
	fileResults := make([]fileResult, len(analyses))

	var wg sync.WaitGroup
	var printMu sync.Mutex
	for i, fa := range analyses {
		if len(fa.Functions) == 0 {
			continue
		}
		wg.Add(1)
		go func(idx int, fa *analyzer.FileAnalysis) {
			defer wg.Done()
			ms, err := aiMut.Generate(context.Background(), fa)
			fileResults[idx] = fileResult{path: fa.FilePath, mutants: ms, err: err}

			printMu.Lock()
			if err != nil {
				fmt.Fprintf(os.Stderr, "  [ai] %s: %v\n", fa.FilePath, err)
			} else {
				fmt.Printf("  %s → %d мутантов\n", fa.FilePath, len(ms))
			}
			printMu.Unlock()
		}(i, fa)
	}
	wg.Wait()

	var mutants []mutator.Mutant
	for _, fr := range fileResults {
		if fr.err == nil {
			mutants = append(mutants, fr.mutants...)
		}
	}

	// Ограничение числа мутантов
	if cfg.MaxMutants > 0 && len(mutants) > cfg.MaxMutants {
		mutants = mutants[:cfg.MaxMutants]
	}

	fmt.Printf("\n  Итого мутантов: %d\n", len(mutants))

	// ── Dry-run: только показываем список мутантов ─────────────────────────
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		fmt.Println("\n[dry-run] Запуск тестов пропущен.")
		printMutantList(mutants)
		return nil
	}

	if len(mutants) == 0 {
		fmt.Println("  Нет мутантов для тестирования.")
		return nil
	}

	// ── 4. Прогон тестов ───────────────────────────────────────────────────
	// Запускаем тесты из первой указанной директории (базовая для runner)
	fmt.Printf("\n→ Запуск тестов против мутантов (workers: %d)...\n", effectiveWorkers(cfg.Workers))
	r, err := runner.New(dirs[0], cfg.Timeout, cfg.Workers, cfg.Verbose)
	if err != nil {
		return fmt.Errorf("инициализация runner: %w", err)
	}
	results, err := r.Run(context.Background(), mutants)
	if err != nil {
		return fmt.Errorf("ошибка runner: %w", err)
	}

	// ── 5. Формирование отчёта ─────────────────────────────────────────────
	rep := reporter.Build(results)
	rep.PrintConsole()
	rep.PrintOperatorBreakdown()

	if cfg.OutputFile != "" {
		if err := rep.SaveJSON(cfg.OutputFile); err != nil {
			fmt.Fprintf(os.Stderr, "Предупреждение: не удалось сохранить JSON-отчёт: %v\n", err)
		} else {
			fmt.Printf("  JSON-отчёт сохранён: %s\n\n", cfg.OutputFile)
		}
	}

	if rep.MutationScore < 0.5 {
		fmt.Fprintf(os.Stderr, "\n  ПРЕДУПРЕЖДЕНИЕ: Mutation Score %.0f%% ниже порога 50%%\n\n",
			rep.MutationScore*100)
	}

	return nil
}

// applyFlags применяет значения флагов командной строки поверх конфига из файла.
func applyFlags(cmd *cobra.Command) {
	if v, err := cmd.Flags().GetString("llm-url"); err == nil && v != "" {
		cfg.LLMBaseURL = v
	}
	if v, err := cmd.Flags().GetString("llm-key"); err == nil && v != "" {
		cfg.LLMAPIKey = v
	}
	if v, err := cmd.Flags().GetString("model"); err == nil && v != "" {
		cfg.LLMModel = v
	}
	if v, err := cmd.Flags().GetBool("structured-output"); err == nil {
		cfg.StructuredOutput = v
	}
	if v, err := cmd.Flags().GetString("output"); err == nil && v != "" {
		cfg.OutputFile = v
	}
	if v, err := cmd.Flags().GetDuration("timeout"); err == nil && v > 0 {
		cfg.Timeout = v
	}
	if v, err := cmd.Flags().GetInt("workers"); err == nil && v > 0 {
		cfg.Workers = v
	}
	if v, err := cmd.Flags().GetInt("llm-workers"); err == nil && v > 0 {
		cfg.LLMWorkers = v
	}
	if v, err := cmd.Flags().GetInt("max-mutants"); err == nil && v > 0 {
		cfg.MaxMutants = v
	}
	if v, err := cmd.Flags().GetBool("verbose"); err == nil && v {
		cfg.Verbose = true
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
}

// printMutantList выводит таблицу сгенерированных мутантов (для dry-run).
func printMutantList(mutants []mutator.Mutant) {
	fmt.Printf("\n%-20s %-30s %-5s %s\n", "ID", "Оператор", "Стр.", "Описание")
	fmt.Println(strings.Repeat("─", 90))
	for _, m := range mutants {
		fmt.Printf("%-20s %-30s %-5d %s\n",
			m.ID, m.OperatorName, m.Line,
			truncate(m.Description, 45))
	}
}

// effectiveWorkers возвращает фактическое число воркеров тестов с учётом дефолта.
func effectiveWorkers(w int) int {
	if w <= 0 {
		return runtime.NumCPU()
	}
	return w
}

// effectiveLLMWorkers возвращает фактическое число параллельных LLM-запросов.
func effectiveLLMWorkers(w int) int {
	if w <= 0 {
		return 4
	}
	return w
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
