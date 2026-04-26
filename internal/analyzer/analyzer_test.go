package analyzer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// testdataDir возвращает путь к testdata/example относительно этого файла.
func testdataDir(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	abs, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "example"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestAnalyzeFile_Valid(t *testing.T) {
	path := filepath.Join(testdataDir(t), "math.go")
	fa, err := AnalyzeFile(path)
	if err != nil {
		t.Fatalf("AnalyzeFile: %v", err)
	}
	if fa.FilePath != path {
		t.Errorf("FilePath = %q, want %q", fa.FilePath, path)
	}
	if fa.Fset == nil {
		t.Error("Fset не должен быть nil")
	}
	if fa.ASTFile == nil {
		t.Error("ASTFile не должен быть nil")
	}

	names := make(map[string]bool)
	for _, fn := range fa.Functions {
		names[fn.Name] = true
	}
	for _, want := range []string{"Add", "Divide", "Max", "IsPositive", "Factorial", "Contains"} {
		if !names[want] {
			t.Errorf("функция %s не найдена в результатах", want)
		}
	}
}

func TestAnalyzeFile_FunctionFields(t *testing.T) {
	path := filepath.Join(testdataDir(t), "math.go")
	fa, err := AnalyzeFile(path)
	if err != nil {
		t.Fatalf("AnalyzeFile: %v", err)
	}
	for _, fn := range fa.Functions {
		if fn.Name == "" {
			t.Error("имя функции не должно быть пустым")
		}
		if fn.File == "" {
			t.Error("поле File не должно быть пустым")
		}
		if fn.Body == "" {
			t.Errorf("тело функции %s не должно быть пустым", fn.Name)
		}
		if fn.StartLine == 0 {
			t.Errorf("StartLine функции %s не должна быть 0", fn.Name)
		}
		if fn.EndLine < fn.StartLine {
			t.Errorf("EndLine(%d) < StartLine(%d) для %s", fn.EndLine, fn.StartLine, fn.Name)
		}
	}
}

func TestAnalyzeFile_NotFound(t *testing.T) {
	_, err := AnalyzeFile("nonexistent_file.go")
	if err == nil {
		t.Error("ожидалась ошибка для несуществующего файла")
	}
}

func TestAnalyzeFile_InvalidSyntax(t *testing.T) {
	f, err := os.CreateTemp("", "invalid-*.go")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("package main\nfunc broken( {")
	f.Close()

	_, err = AnalyzeFile(f.Name())
	if err == nil {
		t.Error("ожидалась ошибка для файла с синтаксической ошибкой")
	}
}

func TestAnalyzeFile_WithComment(t *testing.T) {
	f, err := os.CreateTemp("", "commented-*.go")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(`package example

// MyFunc does something useful.
func MyFunc(x int) int {
	return x * 2
}
`)
	f.Close()

	fa, err := AnalyzeFile(f.Name())
	if err != nil {
		t.Fatalf("AnalyzeFile: %v", err)
	}
	if len(fa.Functions) != 1 {
		t.Fatalf("ожидалась 1 функция, got %d", len(fa.Functions))
	}
	if !strings.Contains(fa.Functions[0].Comment, "MyFunc does something") {
		t.Errorf("Comment = %q, должен содержать doc-комментарий", fa.Functions[0].Comment)
	}
}

func TestAnalyzeFile_Method(t *testing.T) {
	f, err := os.CreateTemp("", "method-*.go")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(`package example

type Foo struct{}

func (f *Foo) Bar() int {
	return 42
}
`)
	f.Close()

	fa, err := AnalyzeFile(f.Name())
	if err != nil {
		t.Fatalf("AnalyzeFile: %v", err)
	}
	if len(fa.Functions) != 1 {
		t.Fatalf("ожидалась 1 функция, got %d", len(fa.Functions))
	}
	// Методы должны иметь имя вида "*Foo.Bar"
	if !strings.Contains(fa.Functions[0].Name, "Bar") {
		t.Errorf("имя метода = %q, ожидалось содержащее Bar", fa.Functions[0].Name)
	}
}

func TestAnalyzePackage(t *testing.T) {
	dir := testdataDir(t)
	analyses, err := AnalyzePackage(dir)
	if err != nil {
		t.Fatalf("AnalyzePackage: %v", err)
	}
	if len(analyses) == 0 {
		t.Error("ожидался хотя бы один файл")
	}
	for _, fa := range analyses {
		if strings.HasSuffix(fa.FilePath, "_test.go") {
			t.Errorf("тестовый файл не должен попасть в анализ: %s", fa.FilePath)
		}
	}
}

func TestAnalyzePackage_NotFound(t *testing.T) {
	_, err := AnalyzePackage("nonexistent/directory")
	if err == nil {
		t.Error("ожидалась ошибка для несуществующей директории")
	}
}

func TestAnalyzePackages_MultipleDir(t *testing.T) {
	dir := testdataDir(t)
	analyses, err := AnalyzePackages([]string{dir, dir})
	if err != nil {
		t.Fatalf("AnalyzePackages: %v", err)
	}
	// Два одинаковых dir — результаты дублируются
	single, _ := AnalyzePackage(dir)
	if len(analyses) != len(single)*2 {
		t.Errorf("AnalyzePackages([dir, dir]) вернул %d, ожидалось %d", len(analyses), len(single)*2)
	}
}

func TestAnalyzePackages_InvalidDir(t *testing.T) {
	_, err := AnalyzePackages([]string{"nonexistent/dir"})
	if err == nil {
		t.Error("ожидалась ошибка для несуществующей директории")
	}
}

func TestAnalyzePackage_EmptyDir(t *testing.T) {
	dir, err := os.MkdirTemp("", "empty-pkg-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	analyses, err := AnalyzePackage(dir)
	if err != nil {
		t.Fatalf("AnalyzePackage: %v", err)
	}
	if len(analyses) != 0 {
		t.Errorf("ожидался пустой результат, got %d файлов", len(analyses))
	}
}
