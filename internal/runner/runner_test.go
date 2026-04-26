package runner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gitlab.mai.ru/gomutationai/internal/mutator"
)

// moduleRoot возвращает корень Go-модуля (директория с go.mod).
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	// Идём вверх: runner/ → internal/ → gomutationai/
	root := filepath.Join(filepath.Dir(file), "..", "..")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func testdataDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(moduleRoot(t), "testdata", "example")
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// ── New ─────────────────────────────────────────────────────────────────────

func TestNew_ValidDir(t *testing.T) {
	dir := testdataDir(t)
	r, err := New(dir, 30*time.Second, 2, false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r.PackageDir == "" {
		t.Error("PackageDir не должен быть пустым")
	}
	if r.ModuleRoot == "" {
		t.Error("ModuleRoot не должен быть пустым")
	}
	if r.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", r.Timeout)
	}
	if r.Workers != 2 {
		t.Errorf("Workers = %d, want 2", r.Workers)
	}
}

func TestNew_DefaultTimeout(t *testing.T) {
	dir := testdataDir(t)
	r, err := New(dir, 0, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Timeout != 30*time.Second {
		t.Errorf("Timeout по умолчанию = %v, want 30s", r.Timeout)
	}
}

func TestNew_DefaultWorkers(t *testing.T) {
	dir := testdataDir(t)
	r, err := New(dir, 10*time.Second, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Workers <= 0 {
		t.Errorf("Workers должен быть > 0, got %d", r.Workers)
	}
}

func TestNew_InvalidDir(t *testing.T) {
	// filepath.Abs не падает даже для несуществующего пути,
	// но findModuleRoot должен вернуть сам путь без ошибки
	r, err := New("/nonexistent/path/xyz", 10*time.Second, 1, false)
	// Ошибка возможна только если Abs упал, что маловероятно
	if err != nil {
		t.Logf("New вернул ошибку (допустимо): %v", err)
		return
	}
	if r == nil {
		t.Error("runner не должен быть nil")
	}
}

// ── verifySyntax ─────────────────────────────────────────────────────────────

func TestVerifySyntax_ValidGo(t *testing.T) {
	m := &mutator.Mutant{
		File: "math.go",
		MutatedSrc: []byte(`package example

func Add(a, b int) int {
	return a + b
}
`),
	}
	status, msg := verifySyntax(m)
	if status != "" {
		t.Errorf("verifySyntax для корректного кода вернул status=%q, msg=%q", status, msg)
	}
}

func TestVerifySyntax_InvalidGo(t *testing.T) {
	m := &mutator.Mutant{
		File:       "math.go",
		MutatedSrc: []byte(`package example\nfunc broken( {`),
	}
	status, msg := verifySyntax(m)
	if status != mutator.StatusCompileError {
		t.Errorf("verifySyntax для некорректного кода вернул status=%q, want compile_error", status)
	}
	if msg == "" {
		t.Error("сообщение об ошибке не должно быть пустым")
	}
}

// ── extractFailedTest ────────────────────────────────────────────────────────

func TestExtractFailedTest_Found(t *testing.T) {
	output := `--- FAIL: TestAdd (0.00s)
    math_test.go:7: Add(2,3) = -1, want 5
FAIL
`
	got := extractFailedTest(output)
	if got != "TestAdd" {
		t.Errorf("extractFailedTest = %q, want TestAdd", got)
	}
}

func TestExtractFailedTest_NotFound(t *testing.T) {
	output := "ok  \tgithub.com/example\t0.004s\n"
	got := extractFailedTest(output)
	if got != "" {
		t.Errorf("extractFailedTest = %q, want empty", got)
	}
}

func TestExtractFailedTest_MultipleFailures(t *testing.T) {
	output := `--- FAIL: TestFirst (0.00s)
--- FAIL: TestSecond (0.00s)
`
	got := extractFailedTest(output)
	if got != "TestFirst" {
		t.Errorf("extractFailedTest должен вернуть первый упавший тест, got %q", got)
	}
}

func TestExtractFailedTest_EmptyOutput(t *testing.T) {
	got := extractFailedTest("")
	if got != "" {
		t.Errorf("extractFailedTest для пустого вывода = %q, want empty", got)
	}
}

// ── truncate ─────────────────────────────────────────────────────────────────

func TestTruncate_ShortString(t *testing.T) {
	got := truncate("hello", 10)
	if got != "hello" {
		t.Errorf("truncate = %q, want hello", got)
	}
}

func TestTruncate_LongString(t *testing.T) {
	got := truncate("hello world", 5)
	if got != "hello..." {
		t.Errorf("truncate = %q, want hello...", got)
	}
}

func TestTruncate_ExactLength(t *testing.T) {
	got := truncate("hello", 5)
	if got != "hello" {
		t.Errorf("truncate = %q, want hello", got)
	}
}

func TestTruncate_Empty(t *testing.T) {
	got := truncate("", 10)
	if got != "" {
		t.Errorf("truncate пустой строки = %q, want empty", got)
	}
}

// ── copyFile ─────────────────────────────────────────────────────────────────

func TestCopyFile(t *testing.T) {
	src, err := os.CreateTemp("", "src-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(src.Name())
	content := "hello, world"
	src.WriteString(content)
	src.Close()

	dst, err := os.CreateTemp("", "dst-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	dst.Close()
	defer os.Remove(dst.Name())

	if err := copyFile(src.Name(), dst.Name()); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	data, err := os.ReadFile(dst.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Errorf("содержимое dst = %q, want %q", string(data), content)
	}
}

func TestCopyFile_InvalidSrc(t *testing.T) {
	err := copyFile("nonexistent_src.txt", "/tmp/dst.txt")
	if err == nil {
		t.Error("ожидалась ошибка для несуществующего источника")
	}
}

func TestCopyFile_InvalidDst(t *testing.T) {
	src, err := os.CreateTemp("", "src-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(src.Name())
	src.Close()

	err = copyFile(src.Name(), "/nonexistent/dir/dst.txt")
	if err == nil {
		t.Error("ожидалась ошибка для несуществующего пути назначения")
	}
}

// ── copyDir ──────────────────────────────────────────────────────────────────

func TestCopyDir(t *testing.T) {
	src, err := os.MkdirTemp("", "src-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(src)

	// Создаём структуру: src/a.txt, src/sub/b.txt, src/.git/HEAD
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("file a"), 0644)
	os.MkdirAll(filepath.Join(src, "sub"), 0755)
	os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("file b"), 0644)
	os.MkdirAll(filepath.Join(src, ".git"), 0755)
	os.WriteFile(filepath.Join(src, ".git", "HEAD"), []byte("ref: main"), 0644)

	dst, err := os.MkdirTemp("", "dst-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dst)

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	// Проверяем что файлы скопированы
	data, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	if err != nil || string(data) != "file a" {
		t.Error("a.txt не скопирован корректно")
	}

	data, err = os.ReadFile(filepath.Join(dst, "sub", "b.txt"))
	if err != nil || string(data) != "file b" {
		t.Error("sub/b.txt не скопирован корректно")
	}

	// .git должен быть пропущен
	if _, err := os.Stat(filepath.Join(dst, ".git")); !os.IsNotExist(err) {
		t.Error(".git не должен быть скопирован")
	}
}

func TestCopyDir_SkipServiceDirs(t *testing.T) {
	src, err := os.MkdirTemp("", "src-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(src)

	for _, dir := range []string{".git", ".idea", ".vscode", "node_modules"} {
		os.MkdirAll(filepath.Join(src, dir), 0755)
		os.WriteFile(filepath.Join(src, dir, "file"), []byte("skip me"), 0644)
	}
	os.WriteFile(filepath.Join(src, "main.go"), []byte("package main"), 0644)

	dst, err := os.MkdirTemp("", "dst-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dst)

	if err := copyDir(src, dst); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{".git", ".idea", ".vscode", "node_modules"} {
		if _, err := os.Stat(filepath.Join(dst, dir)); !os.IsNotExist(err) {
			t.Errorf("директория %s не должна быть скопирована", dir)
		}
	}

	if _, err := os.Stat(filepath.Join(dst, "main.go")); err != nil {
		t.Error("main.go должен быть скопирован")
	}
}

// ── findModuleRoot ────────────────────────────────────────────────────────────

func TestFindModuleRoot(t *testing.T) {
	root := moduleRoot(t)
	got, err := findModuleRoot(root)
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}
	// go.mod должен быть в найденной директории
	if _, err := os.Stat(filepath.Join(got, "go.mod")); err != nil {
		t.Errorf("go.mod не найден в %s", got)
	}
}

func TestFindModuleRoot_NestedDir(t *testing.T) {
	// Поиск из внутренней директории должен найти тот же корень
	nested := filepath.Join(moduleRoot(t), "testdata", "example")
	got, err := findModuleRoot(nested)
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(got, "go.mod")); err != nil {
		t.Errorf("go.mod не найден в %s", got)
	}
}

// ── execTest ─────────────────────────────────────────────────────────────────

func TestExecTest_PassingTests(t *testing.T) {
	dir := testdataDir(t)
	modRoot, err := findModuleRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := &Runner{
		PackageDir: dir,
		ModuleRoot: modRoot,
		Timeout:    30 * time.Second,
		Workers:    1,
	}

	// Запускаем тесты в оригинальном (немутированном) пакете — они должны пройти
	relPkg, _ := filepath.Rel(modRoot, dir)
	output, killed, timedOut := r.execTest(context.Background(), modRoot, "./"+relPkg)
	_ = output
	if timedOut {
		t.Error("тесты не должны завершаться по таймауту")
	}
	if killed {
		t.Errorf("оригинальные тесты не должны падать, output: %s", output)
	}
}

func TestExecTest_BuildFailed(t *testing.T) {
	// Создаём временный модуль с ошибкой компиляции
	tmp, err := os.MkdirTemp("", "buildtest-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module test\ngo 1.21\n"), 0644)
	os.WriteFile(filepath.Join(tmp, "broken.go"), []byte("package main\nfunc f( {"), 0644)

	r := &Runner{Timeout: 10 * time.Second}
	output, killed, timedOut := r.execTest(context.Background(), tmp, ".")
	if timedOut {
		t.Error("не должно быть таймаута")
	}
	if killed {
		t.Error("killed должен быть false при ошибке компиляции")
	}
	_ = output
}

// ── Run (интеграционные) ──────────────────────────────────────────────────────

func TestRun_CompileErrorMutant(t *testing.T) {
	dir := testdataDir(t)
	r, err := New(dir, 30*time.Second, 1, false)
	if err != nil {
		t.Fatal(err)
	}

	mutants := []mutator.Mutant{
		{
			ID:         "test_compile_err",
			File:       filepath.Join(dir, "math.go"),
			MutatedSrc: []byte("package example\nfunc broken( {"),
			Status:     mutator.StatusPending,
		},
	}

	results, err := r.Run(context.Background(), mutants)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("ожидался 1 результат, got %d", len(results))
	}
	if results[0].Mutant.Status != mutator.StatusCompileError {
		t.Errorf("status = %s, want compile_error", results[0].Mutant.Status)
	}
}

func TestRun_SurvivedMutant(t *testing.T) {
	dir := testdataDir(t)
	r, err := New(dir, 30*time.Second, 1, false)
	if err != nil {
		t.Fatal(err)
	}

	// Читаем оригинальный файл
	origPath := filepath.Join(dir, "math.go")
	orig, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatal(err)
	}

	// Мутант: меняем комментарий (не влияет на поведение) — должен выжить
	mutated := strings.Replace(string(orig), "// Add returns the sum", "// Add returns sum", 1)

	mutants := []mutator.Mutant{
		{
			ID:         "test_survived",
			File:       origPath,
			MutatedSrc: []byte(mutated),
			Status:     mutator.StatusPending,
		},
	}

	results, err := r.Run(context.Background(), mutants)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("ожидался 1 результат, got %d", len(results))
	}
	if results[0].Mutant.Status != mutator.StatusSurvived {
		t.Errorf("status = %s, want survived", results[0].Mutant.Status)
	}
}

func TestRun_KilledMutant(t *testing.T) {
	dir := testdataDir(t)
	r, err := New(dir, 30*time.Second, 1, false)
	if err != nil {
		t.Fatal(err)
	}

	origPath := filepath.Join(dir, "math.go")
	orig, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatal(err)
	}

	// Мутант: меняем + на - в Add — тесты должны упасть
	mutated := strings.Replace(string(orig), "return a + b", "return a - b", 1)

	mutants := []mutator.Mutant{
		{
			ID:         "test_killed",
			File:       origPath,
			MutatedSrc: []byte(mutated),
			Status:     mutator.StatusPending,
		},
	}

	results, err := r.Run(context.Background(), mutants)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("ожидался 1 результат, got %d", len(results))
	}
	if results[0].Mutant.Status != mutator.StatusKilled {
		t.Errorf("status = %s, want killed", results[0].Mutant.Status)
	}
	if results[0].Mutant.KilledBy == "" {
		t.Error("KilledBy не должен быть пустым для убитого мутанта")
	}
}

func TestRun_EmptyMutants(t *testing.T) {
	dir := testdataDir(t)
	r, err := New(dir, 30*time.Second, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	results, err := r.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run с пустым списком: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("ожидался пустой список результатов, got %d", len(results))
	}
}

func TestRun_Verbose(t *testing.T) {
	dir := testdataDir(t)
	r, err := New(dir, 30*time.Second, 1, true) // verbose=true
	if err != nil {
		t.Fatal(err)
	}

	origPath := filepath.Join(dir, "math.go")
	orig, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(string(orig), "return a + b", "return a - b", 1)

	mutants := []mutator.Mutant{
		{ID: "verbose_test", File: origPath, MutatedSrc: []byte(mutated), Status: mutator.StatusPending},
	}
	// Не должно паниковать при verbose=true
	_, err = r.Run(context.Background(), mutants)
	if err != nil {
		t.Fatalf("Run verbose: %v", err)
	}
}

func TestTruncateOutput(t *testing.T) {
	long := strings.Repeat("x", 600)
	got := truncate(long, 500)
	if len(got) > 503 { // 500 + "..."
		t.Errorf("truncate должен обрезать строку, len=%d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Error("обрезанная строка должна заканчиваться на ...")
	}
}
