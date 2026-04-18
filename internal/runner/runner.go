// Пакет runner применяет мутанты в изолированных временных копиях модуля,
// запускает go test параллельно и определяет статус каждого мутанта.
//
// Схема работы для каждого мутанта:
//  1. Синтаксическая проверка через go/parser (без I/O).
//  2. Копирование всего Go-модуля во временную директорию.
//  3. Запись мутированного файла в копию.
//  4. Запуск go test в изолированной копии.
//  5. Удаление временной директории.
//
// Шаги 2–5 выполняются параллельно в пуле Workers горутин.
// Оригинальные файлы никогда не изменяются.
package runner

import (
	"bytes"
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"gitlab.mai.ru/gomutationai/internal/mutator"
)

// Result хранит итог выполнения тестов против одного мутанта.
type Result struct {
	Mutant   mutator.Mutant
	Duration time.Duration
	// Output — усечённый вывод тестового прогона (для диагностики).
	Output string
}

// Runner управляет параллельным изолированным прогоном тестов.
type Runner struct {
	// PackageDir — директория анализируемого Go-пакета (абсолютный путь).
	PackageDir string
	// ModuleRoot — директория, содержащая go.mod (абсолютный путь).
	ModuleRoot string
	// Timeout — максимальное время одного тестового прогона.
	Timeout time.Duration
	// Workers — число параллельных тестовых прогонов.
	Workers int
	// Verbose включает подробный вывод статуса для каждого мутанта.
	Verbose bool
}

// New создаёт Runner. Автоматически определяет корень модуля по packageDir.
func New(packageDir string, timeout time.Duration, workers int, verbose bool) (*Runner, error) {
	absDir, err := filepath.Abs(packageDir)
	if err != nil {
		return nil, fmt.Errorf("определение абсолютного пути: %w", err)
	}

	modRoot, err := findModuleRoot(absDir)
	if err != nil {
		return nil, fmt.Errorf("поиск go.mod: %w", err)
	}

	if timeout == 0 {
		timeout = 30 * time.Second
	}
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	return &Runner{
		PackageDir: absDir,
		ModuleRoot: modRoot,
		Timeout:    timeout,
		Workers:    workers,
		Verbose:    verbose,
	}, nil
}

// Run запускает все мутанты параллельно и возвращает результаты.
// Порядок результатов соответствует порядку входных мутантов.
func (r *Runner) Run(ctx context.Context, mutants []mutator.Mutant) ([]Result, error) {
	results := make([]Result, len(mutants))

	// Семафор для ограничения числа параллельных горутин
	sem := make(chan struct{}, r.Workers)

	var wg sync.WaitGroup
	var mu sync.Mutex // защищает firstErr и Verbose-вывод
	var firstErr error

	for i := range mutants {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Захватываем слот в семафоре
			sem <- struct{}{}
			defer func() { <-sem }()

			if ctx.Err() != nil {
				return
			}

			res, err := r.runOneIsolated(ctx, &mutants[idx])

			mu.Lock()
			defer mu.Unlock()

			if err != nil && firstErr == nil {
				firstErr = err
			}
			results[idx] = res

			if r.Verbose {
				fmt.Printf("[runner] %-25s %-30s → %s  (%s)\n",
					filepath.Base(mutants[idx].File),
					mutants[idx].OperatorName,
					mutants[idx].Status,
					res.Duration.Round(time.Millisecond),
				)
			}
		}(i)
	}

	wg.Wait()
	return results, firstErr
}

// runOneIsolated выполняет полный цикл для одного мутанта в изолированной копии модуля.
func (r *Runner) runOneIsolated(ctx context.Context, m *mutator.Mutant) (Result, error) {
	start := time.Now()

	// Шаг 1: синтаксическая проверка — без создания файлов, быстро.
	if status, errMsg := verifySyntax(m); status != "" {
		m.Status = status
		return Result{Mutant: *m, Duration: time.Since(start), Output: errMsg}, nil
	}

	// Шаг 2: создаём временную директорию и копируем туда весь модуль.
	tmpDir, err := os.MkdirTemp("", "gomutationai-*")
	if err != nil {
		return Result{}, fmt.Errorf("создание tmpdir: %w", err)
	}
	defer os.RemoveAll(tmpDir) // гарантируем очистку

	if err := copyDir(r.ModuleRoot, tmpDir); err != nil {
		return Result{}, fmt.Errorf("копирование модуля: %w", err)
	}

	// Шаг 3: записываем мутированный файл в копию модуля.
	absFile, err := filepath.Abs(m.File)
	if err != nil {
		return Result{}, fmt.Errorf("абсолютный путь файла: %w", err)
	}
	relFile, err := filepath.Rel(r.ModuleRoot, absFile)
	if err != nil {
		return Result{}, fmt.Errorf("относительный путь файла: %w", err)
	}
	tmpFile := filepath.Join(tmpDir, relFile)

	if err := os.WriteFile(tmpFile, m.MutatedSrc, 0644); err != nil {
		return Result{}, fmt.Errorf("запись мутанта: %w", err)
	}

	// Шаг 4: вычисляем путь пакета относительно tmpDir и запускаем тесты.
	relPkg, err := filepath.Rel(r.ModuleRoot, r.PackageDir)
	if err != nil {
		return Result{}, fmt.Errorf("относительный путь пакета: %w", err)
	}
	pkgArg := "./" + relPkg

	output, killed, timedOut := r.execTest(ctx, tmpDir, pkgArg)

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

// verifySyntax проверяет синтаксическую корректность мутированного исходника
// через go/parser — до создания каких-либо временных файлов.
func verifySyntax(m *mutator.Mutant) (mutator.Status, string) {
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, m.File, m.MutatedSrc, 0)
	if err != nil {
		return mutator.StatusCompileError, err.Error()
	}
	return "", ""
}

// execTest запускает `go test` в указанной директории с таймаутом.
// Возвращает (вывод, killed, timedOut).
func (r *Runner) execTest(ctx context.Context, dir, pkg string) (string, bool, bool) {
	tCtx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	cmd := exec.CommandContext(tCtx, "go", "test", "-count=1", "-timeout",
		fmt.Sprintf("%.0fs", r.Timeout.Seconds()), pkg)
	cmd.Dir = dir

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()

	if tCtx.Err() == context.DeadlineExceeded {
		return out.String(), false, true
	}

	if err != nil {
		output := out.String()
		// Ошибка сборки отличается от провала тестов
		if strings.Contains(output, "build failed") ||
			strings.Contains(output, "[build failed]") ||
			strings.Contains(output, "syntax error") ||
			strings.Contains(output, "cannot ") {
			return output, false, false
		}
		// Тесты упали — мутант обнаружен
		return output, true, false
	}

	// Тесты прошли — мутант выжил
	return out.String(), false, false
}

// findModuleRoot находит директорию, содержащую go.mod, через `go env GOMOD`.
func findModuleRoot(dir string) (string, error) {
	cmd := exec.Command("go", "env", "GOMOD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return dir, nil // на случай если go.mod отсутствует
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		// Модуль не найден — используем саму директорию
		return dir, nil
	}
	return filepath.Dir(gomod), nil
}

// copyDir рекурсивно копирует дерево директорий src в dst.
// Пропускает директории .git и прочие служебные директории.
func copyDir(src, dst string) error {
	// Директории, которые не нужно копировать
	skipDirs := map[string]bool{
		".git":         true,
		".idea":        true,
		".vscode":      true,
		"node_modules": true,
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Пропускаем служебные директории
		if d.IsDir() && skipDirs[d.Name()] {
			return fs.SkipDir
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		return copyFile(path, target)
	})
}

// copyFile копирует один файл из src в dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
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
