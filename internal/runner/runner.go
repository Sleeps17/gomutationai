// Пакет runner применяет мутанты к исходным файлам, запускает go test
// и определяет, был ли каждый мутант обнаружен (killed) или выжил (survived).
package runner

import (
	"bytes"
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sleeps/gomutator/internal/mutator"
)

// Result хранит итог выполнения тестов против одного мутанта.
type Result struct {
	Mutant   mutator.Mutant
	Duration time.Duration
	// Output — усечённый вывод тестового прогона (для диагностики).
	Output string
}

// Runner управляет прогоном тестов пакета для каждого мутанта.
type Runner struct {
	// PackageDir — корневая директория Go-пакета.
	PackageDir string
	// Timeout — максимальное время одного тестового прогона.
	Timeout time.Duration
	// Verbose включает подробный вывод статуса для каждого мутанта.
	Verbose bool
}

// New создаёт Runner для указанной директории пакета.
func New(packageDir string, timeout time.Duration, verbose bool) *Runner {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Runner{PackageDir: packageDir, Timeout: timeout, Verbose: verbose}
}

// Run применяет каждый мутант, запускает тесты и заполняет поле Status мутанта.
func (r *Runner) Run(ctx context.Context, mutants []mutator.Mutant) ([]Result, error) {
	results := make([]Result, 0, len(mutants))
	for i := range mutants {
		res, err := r.runOne(ctx, &mutants[i])
		if err != nil {
			return results, err
		}
		results = append(results, res)
		if r.Verbose {
			fmt.Printf("[runner] %s %-30s %s → %s\n",
				filepath.Base(mutants[i].File),
				mutants[i].OperatorName,
				mutants[i].ID,
				mutants[i].Status,
			)
		}
	}
	return results, nil
}

// runOne выполняет полный цикл для одного мутанта:
// синтаксическая проверка → запись → тесты → восстановление.
func (r *Runner) runOne(ctx context.Context, m *mutator.Mutant) (Result, error) {
	start := time.Now()

	// Шаг 1: проверяем синтаксическую корректность мутанта через go/parser.
	if status, errMsg := verifySyntax(m); status != "" {
		m.Status = status
		return Result{Mutant: *m, Duration: time.Since(start), Output: errMsg}, nil
	}

	// Шаг 2: читаем оригинал, записываем мутированный файл, прогоняем тесты,
	// всегда восстанавливаем оригинал — даже при ошибке.
	origSrc, err := os.ReadFile(m.File)
	if err != nil {
		return Result{}, fmt.Errorf("чтение %s: %w", m.File, err)
	}

	if err := os.WriteFile(m.File, m.MutatedSrc, 0644); err != nil {
		return Result{}, fmt.Errorf("запись мутанта в %s: %w", m.File, err)
	}

	output, killed, timedOut := r.execTest(ctx, m.File)

	// Восстанавливаем оригинал в любом случае
	if err := os.WriteFile(m.File, origSrc, 0644); err != nil {
		return Result{}, fmt.Errorf("восстановление %s: %w", m.File, err)
	}

	switch {
	case timedOut:
		m.Status = mutator.StatusTimeout
	case killed:
		m.Status = mutator.StatusKilled
		m.KilledBy = extractFailedTest(output)
	default:
		m.Status = mutator.StatusSurvived
	}

	return Result{
		Mutant:   *m,
		Duration: time.Since(start),
		Output:   truncate(output, 500),
	}, nil
}

// verifySyntax проверяет синтаксическую корректность мутированного исходника.
// Возвращает непустой Status и сообщение об ошибке, если код невалиден.
func verifySyntax(m *mutator.Mutant) (mutator.Status, string) {
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, m.File, m.MutatedSrc, 0)
	if err != nil {
		return mutator.StatusCompileError, err.Error()
	}
	return "", ""
}

// execTest запускает `go test` с таймаутом для пакета, содержащего мутированный файл.
// Возвращает (вывод, killed, timedOut).
func (r *Runner) execTest(ctx context.Context, mutatedFile string) (string, bool, bool) {
	// Запускаем тесты только для пакета с изменённым файлом
	pkg := "./" + relDir(r.PackageDir, mutatedFile)

	tCtx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	cmd := exec.CommandContext(tCtx, "go", "test", "-count=1", "-timeout",
		fmt.Sprintf("%.0fs", r.Timeout.Seconds()), pkg)
	cmd.Dir = r.PackageDir

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()

	if tCtx.Err() == context.DeadlineExceeded {
		return out.String(), false, true
	}

	if err != nil {
		output := out.String()
		// Ошибка компиляции отличается от провала тестов
		if strings.Contains(output, "build failed") || strings.Contains(output, "syntax error") {
			return output, false, false
		}
		// Тесты упали — мутант обнаружен
		return output, true, false
	}

	// Тесты прошли — мутант выжил
	return out.String(), false, false
}

// relDir возвращает директорию файла относительно базовой директории пакета.
func relDir(base, file string) string {
	dir := filepath.Dir(file)
	rel, err := filepath.Rel(base, dir)
	if err != nil || rel == "." {
		return "."
	}
	return rel
}

// extractFailedTest извлекает имя первого упавшего теста из вывода `go test`.
func extractFailedTest(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "--- FAIL:") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				return parts[2]
			}
		}
	}
	return ""
}

// truncate обрезает строку до n байт, добавляя многоточие.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
