// Пакет runner применяет мутанты в пуле изолированных копий модуля,
// запускает go test параллельно и определяет статус каждого мутанта.
//
// Схема работы:
//  1. При старте создаётся ровно Workers копий модуля в tmpdir — по одной на воркер.
//  2. Каждый мутант:
//     a. Синтаксическая проверка через go/parser (без I/O).
//     b. Воркер берёт свободную копию из пула, вносит мутацию, запускает go test.
//     c. После теста восстанавливает файл из оригинала и возвращает копию в пул.
//  3. По завершении все копии удаляются.
//
// Это сокращает дисковые операции с O(мутанты × модуль) до O(воркеры × модуль).
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
	if len(mutants) == 0 {
		return nil, nil
	}

	// Создаём пул рабочих директорий — по одной копии модуля на воркер.
	pool, cleanup, err := r.setupWorkerPool()
	if err != nil {
		return nil, fmt.Errorf("создание пула директорий: %w", err)
	}
	defer cleanup()

	results := make([]Result, len(mutants))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for i := range mutants {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			if ctx.Err() != nil {
				return
			}

			res, err := r.runOne(ctx, &mutants[idx], pool)

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

// setupWorkerPool создаёт Workers копий модуля и возвращает канал с путями к ним.
// Второе возвращаемое значение — функция очистки, удаляющая все копии.
func (r *Runner) setupWorkerPool() (chan string, func(), error) {
	dirs := make([]string, 0, r.Workers)

	pool := make(chan string, r.Workers)

	for i := 0; i < r.Workers; i++ {
		tmpDir, err := os.MkdirTemp("", "gomutationai-*")
		if err != nil {
			// Убираем уже созданные директории перед выходом с ошибкой
			for _, d := range dirs {
				os.RemoveAll(d)
			}
			return nil, nil, fmt.Errorf("создание рабочей директории: %w", err)
		}

		if err := copyDir(r.ModuleRoot, tmpDir); err != nil {
			os.RemoveAll(tmpDir)
			for _, d := range dirs {
				os.RemoveAll(d)
			}
			return nil, nil, fmt.Errorf("копирование модуля в рабочую директорию: %w", err)
		}

		dirs = append(dirs, tmpDir)
		pool <- tmpDir
	}

	cleanup := func() {
		for _, d := range dirs {
			os.RemoveAll(d)
		}
	}

	return pool, cleanup, nil
}

// runOne выполняет полный цикл для одного мутанта, используя директорию из пула.
func (r *Runner) runOne(ctx context.Context, m *mutator.Mutant, pool chan string) (Result, error) {
	start := time.Now()

	// Быстрая синтаксическая проверка до захвата рабочей директории.
	if status, errMsg := verifySyntax(m); status != "" {
		m.Status = status
		return Result{Mutant: *m, Duration: time.Since(start), Output: errMsg}, nil
	}

	// Захватываем свободную рабочую директорию из пула.
	workerDir := <-pool
	defer func() { pool <- workerDir }() // возвращаем в пул после завершения

	absFile, err := filepath.Abs(m.File)
	if err != nil {
		return Result{}, fmt.Errorf("абсолютный путь файла: %w", err)
	}
	relFile, err := filepath.Rel(r.ModuleRoot, absFile)
	if err != nil {
		return Result{}, fmt.Errorf("относительный путь файла: %w", err)
	}
	workerFile := filepath.Join(workerDir, relFile)

	// Записываем мутированный файл в рабочую копию.
	if err := os.WriteFile(workerFile, m.MutatedSrc, 0644); err != nil {
		return Result{}, fmt.Errorf("запись мутанта: %w", err)
	}

	// После теста восстанавливаем оригинальный файл из ModuleRoot.
	// ModuleRoot никогда не изменяется — это источник истины.
	origFile := filepath.Join(r.ModuleRoot, relFile)
	defer func() {
		if orig, err := os.ReadFile(origFile); err == nil {
			os.WriteFile(workerFile, orig, 0644) //nolint:errcheck
		}
	}()

	relPkg, err := filepath.Rel(r.ModuleRoot, r.PackageDir)
	if err != nil {
		return Result{}, fmt.Errorf("относительный путь пакета: %w", err)
	}

	output, killed, timedOut := r.execTest(ctx, workerDir, "./"+relPkg)

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
		return dir, nil
	}
	return filepath.Dir(gomod), nil
}

// copyDir рекурсивно копирует дерево директорий src в dst.
// Пропускает служебные директории (.git, .idea, .vscode, node_modules).
func copyDir(src, dst string) error {
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
