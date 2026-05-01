package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func testdataExampleDir(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	abs, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "example"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// ── ParseTestedFunctions ────────────────────────────────────────────────────

func TestParseTestedFunctions_Example(t *testing.T) {
	dir := testdataExampleDir(t)
	tested, err := ParseTestedFunctions([]string{dir})
	if err != nil {
		t.Fatalf("ParseTestedFunctions: %v", err)
	}
	// math_test.go содержит Test* функции, вызывающие Add, Divide, Max и др.
	for _, name := range []string{"Add", "Divide", "Max", "IsPositive", "Factorial"} {
		if _, ok := tested[name]; !ok {
			t.Errorf("функция %s должна быть найдена как тестируемая", name)
		}
	}
}

func TestParseTestedFunctions_TestBodyNotEmpty(t *testing.T) {
	dir := testdataExampleDir(t)
	tested, err := ParseTestedFunctions([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	for name, tf := range tested {
		if tf.TestBody == "" {
			t.Errorf("TestBody для %s не должен быть пустым", name)
		}
		if tf.TestName == "" {
			t.Errorf("TestName для %s не должен быть пустым", name)
		}
		if tf.Name != name {
			t.Errorf("Name = %q, want %q", tf.Name, name)
		}
	}
}

func TestParseTestedFunctions_InvalidDir(t *testing.T) {
	_, err := ParseTestedFunctions([]string{"nonexistent/dir"})
	if err == nil {
		t.Error("ожидалась ошибка для несуществующей директории")
	}
}

func TestParseTestedFunctions_EmptyTestBody(t *testing.T) {
	// TestContains в testdata имеет пустое тело — Contains не должен попасть в список
	dir := testdataExampleDir(t)
	tested, err := ParseTestedFunctions([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tested["Contains"]; ok {
		t.Error("Contains не должна быть найдена — TestContains пустой")
	}
}

func TestParseTestedFunctions_NoDuplicates(t *testing.T) {
	// Одна и та же директория дважды — функции не должны дублироваться
	dir := testdataExampleDir(t)
	tested, err := ParseTestedFunctions([]string{dir, dir})
	if err != nil {
		t.Fatal(err)
	}
	// map не может иметь дубликатов — проверяем что результат не пустой
	if len(tested) == 0 {
		t.Error("ожидались тестируемые функции")
	}
}

func TestParseTestedFunctions_CustomTest(t *testing.T) {
	dir, err := os.MkdirTemp("", "testparser-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	err = os.WriteFile(filepath.Join(dir, "foo_test.go"), []byte(`package foo

import "testing"

func TestProcess(t *testing.T) {
	result := ProcessData(42)
	if result != 84 {
		t.Errorf("got %d", result)
	}
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	tested, err := ParseTestedFunctions([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tested["ProcessData"]; !ok {
		t.Error("ProcessData должна быть найдена как тестируемая функция")
	}
	if tested["ProcessData"].TestName != "TestProcess" {
		t.Errorf("TestName = %q, want TestProcess", tested["ProcessData"].TestName)
	}
}

// ── BuildCallGraph ──────────────────────────────────────────────────────────

func TestBuildCallGraph_Basic(t *testing.T) {
	dir := testdataExampleDir(t)
	analyses, err := AnalyzePackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	graph := BuildCallGraph(analyses)
	// graph может быть пустым для простых функций — просто не должен паниковать
	if graph == nil {
		t.Error("BuildCallGraph вернул nil")
	}
}

func TestBuildCallGraph_WithCalls(t *testing.T) {
	f, err := os.CreateTemp("", "callgraph-*.go")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(`package foo

func Helper(x int) int { return x + 1 }

func Caller(x int) int {
	return Helper(x) + Helper(x)
}
`)
	f.Close()

	fa, err := AnalyzeFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	graph := BuildCallGraph([]*FileAnalysis{fa})
	callees, ok := graph["Caller"]
	if !ok {
		t.Fatal("Caller должен быть в графе")
	}
	found := false
	for _, c := range callees {
		if c == "Helper" {
			found = true
		}
	}
	if !found {
		t.Errorf("Helper должен быть в callees Caller, got %v", callees)
	}
}

func TestBuildCallGraph_SelfRecursion(t *testing.T) {
	f, err := os.CreateTemp("", "recursive-*.go")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(`package foo

func Fib(n int) int {
	if n <= 1 { return n }
	return Fib(n-1) + Fib(n-2)
}
`)
	f.Close()

	fa, err := AnalyzeFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	graph := BuildCallGraph([]*FileAnalysis{fa})
	// Fib не должна быть в своих собственных callees (нет само-петли)
	for _, callee := range graph["Fib"] {
		if callee == "Fib" {
			t.Error("само-рекурсивный вызов не должен добавляться в граф")
		}
	}
}

// ── ExpandWithCallees ───────────────────────────────────────────────────────

func TestExpandWithCallees_Depth0(t *testing.T) {
	tested := map[string]TestedFunction{
		"A": {Name: "A"},
	}
	graph := map[string][]string{
		"A": {"B", "C"},
	}
	result := ExpandWithCallees(tested, graph, 0)
	if !result["A"] {
		t.Error("A должна быть в результате")
	}
	if result["B"] || result["C"] {
		t.Error("при depth=0 callees не должны добавляться")
	}
}

func TestExpandWithCallees_Depth1(t *testing.T) {
	tested := map[string]TestedFunction{
		"A": {Name: "A"},
	}
	graph := map[string][]string{
		"A": {"B", "C"},
		"B": {"D"},
	}
	result := ExpandWithCallees(tested, graph, 1)
	if !result["A"] {
		t.Error("A должна быть в результате")
	}
	if !result["B"] || !result["C"] {
		t.Error("прямые callees B и C должны быть добавлены при depth=1")
	}
	if result["D"] {
		t.Error("D не должна быть добавлена при depth=1 (слишком глубоко)")
	}
}

func TestExpandWithCallees_Depth2(t *testing.T) {
	tested := map[string]TestedFunction{
		"A": {Name: "A"},
	}
	graph := map[string][]string{
		"A": {"B"},
		"B": {"C"},
		"C": {"D"},
	}
	result := ExpandWithCallees(tested, graph, 2)
	if !result["A"] || !result["B"] || !result["C"] {
		t.Error("A, B, C должны быть в результате при depth=2")
	}
	if result["D"] {
		t.Error("D не должна быть добавлена при depth=2")
	}
}

func TestExpandWithCalleesContext_PropagatesSourceTest(t *testing.T) {
	tested := map[string]TestedFunction{
		"A": {Name: "A", TestName: "TestA", TestBody: "body-a"},
	}
	graph := map[string][]string{
		"A": {"B"},
		"B": {"C"},
	}

	result := ExpandWithCalleesContext(tested, graph, 2)

	if result["A"].TestName != "TestA" {
		t.Fatalf("A должна сохранить исходный тест, got %q", result["A"].TestName)
	}
	if result["B"].TestName != "TestA" {
		t.Fatalf("B должна унаследовать TestA, got %q", result["B"].TestName)
	}
	if result["C"].TestName != "TestA" {
		t.Fatalf("C должна унаследовать TestA, got %q", result["C"].TestName)
	}
	if result["C"].TestBody != "body-a" {
		t.Fatalf("C должна унаследовать TestBody исходного теста, got %q", result["C"].TestBody)
	}
}

func TestExpandWithCallees_MutualRecursion(t *testing.T) {
	tested := map[string]TestedFunction{
		"A": {Name: "A"},
	}
	// A→B, B→A — взаимная рекурсия
	graph := map[string][]string{
		"A": {"B"},
		"B": {"A"},
	}
	// Не должно зависнуть
	result := ExpandWithCallees(tested, graph, 10)
	if !result["A"] || !result["B"] {
		t.Error("A и B должны быть в результате")
	}
}

func TestExpandWithCallees_EmptyTested(t *testing.T) {
	result := ExpandWithCallees(map[string]TestedFunction{}, map[string][]string{}, 1)
	if len(result) != 0 {
		t.Errorf("ожидался пустой результат, got %v", result)
	}
}

// ── extractCallName ─────────────────────────────────────────────────────────

func parseCallExpr(t *testing.T, src string) *ast.CallExpr {
	t.Helper()
	// Оборачиваем выражение в функцию для корректного разбора
	full := "package p\nfunc f(){\n_ = " + src + "\n}"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", full, 0)
	if err != nil {
		t.Fatalf("parseCallExpr: %v", err)
	}
	fn := file.Decls[0].(*ast.FuncDecl)
	stmt := fn.Body.List[0].(*ast.AssignStmt)
	return stmt.Rhs[0].(*ast.CallExpr)
}

func TestExtractCallName_SimpleFunc(t *testing.T) {
	call := parseCallExpr(t, "Foo()")
	name := extractCallName(call)
	if name != "Foo" {
		t.Errorf("extractCallName = %q, want Foo", name)
	}
}

func TestExtractCallName_SelectorExpr(t *testing.T) {
	call := parseCallExpr(t, "obj.Bar()")
	name := extractCallName(call)
	if name != "Bar" {
		t.Errorf("extractCallName = %q, want Bar", name)
	}
}

func TestExtractCallName_TestingT(t *testing.T) {
	for _, src := range []string{"t.Error()", "b.Run(\"x\", nil)", "m.Run()", "testing.T{}"} {
		fset := token.NewFileSet()
		full := "package p\nfunc f(){\n" + src + "\n}"
		file, err := parser.ParseFile(fset, "", full, 0)
		if err != nil {
			continue
		}
		fn := file.Decls[0].(*ast.FuncDecl)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := extractCallName(call)
			if name != "" {
				t.Errorf("вызовы через t/b/m/testing должны возвращать пустую строку, got %q для %s", name, src)
			}
			return true
		})
	}
}

func TestExtractCallName_RequireAssert(t *testing.T) {
	for _, src := range []string{"require.NoError(t, nil)", "assert.Equal(t, 1, 1)"} {
		fset := token.NewFileSet()
		full := "package p\nfunc f(){\n" + src + "\n}"
		file, err := parser.ParseFile(fset, "", full, 0)
		if err != nil {
			continue
		}
		fn := file.Decls[0].(*ast.FuncDecl)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := extractCallName(call)
			if name != "" {
				t.Errorf("вызовы testify должны возвращать пустую строку, got %q для %s", name, src)
			}
			return true
		})
	}
}

// ── isInternalName ──────────────────────────────────────────────────────────

func TestIsInternalName_Builtins(t *testing.T) {
	builtins := []string{
		"append", "len", "cap", "make", "new", "copy", "delete",
		"close", "panic", "recover", "print", "println",
		"complex", "real", "imag",
	}
	for _, name := range builtins {
		if !isInternalName(name) {
			t.Errorf("isInternalName(%q) = false, ожидалось true", name)
		}
	}
}

func TestIsInternalName_TestingHelpers(t *testing.T) {
	helpers := []string{
		"Error", "Errorf", "Fatal", "Fatalf",
		"Log", "Logf", "Skip", "Skipf",
		"Run", "Parallel", "Helper", "Cleanup",
		"NoError", "Equal", "NotEqual",
	}
	for _, name := range helpers {
		if !isInternalName(name) {
			t.Errorf("isInternalName(%q) = false, ожидалось true", name)
		}
	}
}

func TestIsInternalName_ProductionFunc(t *testing.T) {
	names := []string{"Add", "Divide", "MyFunc", "ProcessOrder", "Validate"}
	for _, name := range names {
		if isInternalName(name) {
			t.Errorf("isInternalName(%q) = true, ожидалось false для production-функции", name)
		}
	}
}
