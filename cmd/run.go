// Пакет cmd реализует команду run — точку входа для запуска мутационного тестирования.
package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	aimutator "github.com/sleeps/gomutator/internal/mutator/ai"
	"github.com/sleeps/gomutator/internal/mutator"
	"github.com/sleeps/gomutator/internal/analyzer"
	"github.com/sleeps/gomutator/internal/reporter"
	"github.com/sleeps/gomutator/internal/runner"
)

var runCmd = &cobra.Command{
	Use:   "run [директория-пакета]",
	Short: "Запустить мутационное тестирование Go-пакета",
	Long: `Анализирует Go-пакет, генерирует мутантов с помощью LLM,
запускает тесты и выводит отчёт с метриками качества тестового набора.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMutation,
}

func init() {
	rootCmd.AddCommand(runCmd)

	// Параметры LLM
	runCmd.Flags().String("llm-url", "", "базовый URL OpenAI-совместимого API (например http://localhost:11434/v1)")
	runCmd.Flags().String("llm-key", "", "токен доступа к LLM (или переменная OPENAI_API_KEY)")
	runCmd.Flags().String("model", "", "идентификатор модели, например gpt-4o-mini или llama3")
	runCmd.Flags().Bool("structured-output", true, "использовать Structured Output / JSON Schema (если модель поддерживает)")

	// Управление прогоном
	runCmd.Flags().DurationP("timeout", "t", 0, "таймаут на один тестовый прогон, например 30s")
	runCmd.Flags().Int("max-mutants", 0, "ограничить число мутантов (0 — без ограничений)")
	runCmd.Flags().BoolP("verbose", "v", false, "выводить статус каждого мутанта")
	runCmd.Flags().Bool("dry-run", false, "только сгенерировать мутантов, не запускать тесты")

	// Отчёт
	runCmd.Flags().StringP("output", "o", "", "сохранить JSON-отчёт в файл")
}

func runMutation(cmd *cobra.Command, args []string) error {
	applyFlags(cmd)

	packageDir := "."
	if len(args) > 0 {
		packageDir = args[0]
	}

	fmt.Printf("gomutator  модель: %s  пакет: %s\n\n", cfg.LLMModel, packageDir)

	// ── 1. Анализ исходных файлов ──────────────────────────────────────────
	fmt.Println("→ Анализ исходных файлов...")
	analyses, err := analyzer.AnalyzePackage(packageDir)
	if err != nil {
		return fmt.Errorf("анализ провалился: %w", err)
	}
	if len(analyses) == 0 {
		return fmt.Errorf("Go-файлы не найдены в %s", packageDir)
	}

	// ── 2. Генерация мутантов через LLM ───────────────────────────────────
	fmt.Println("→ Генерация мутантов через LLM...")
	aiMut := aimutator.New(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel, cfg.StructuredOutput)

	var mutants []mutator.Mutant
	for _, fa := range analyses {
		ms, err := aiMut.Generate(context.Background(), fa)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [ai] %s: %v\n", fa.FilePath, err)
			continue
		}
		mutants = append(mutants, ms...)
		fmt.Printf("  %s → %d мутантов\n", fa.FilePath, len(ms))
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

	// ── 3. Прогон тестов ───────────────────────────────────────────────────
	fmt.Println("\n→ Запуск тестов против мутантов...")
	r := runner.New(packageDir, cfg.Timeout, cfg.Verbose)
	results, err := r.Run(context.Background(), mutants)
	if err != nil {
		return fmt.Errorf("ошибка runner: %w", err)
	}

	// ── 4. Формирование отчёта ─────────────────────────────────────────────
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
