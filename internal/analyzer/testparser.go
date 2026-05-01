// Файл testparser.go содержит логику анализа тестовых файлов Go.
// Он определяет, какие production-функции покрыты тестами, и строит
// граф вызовов для расширения покрытия на подфункции.
package analyzer

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// TestedFunction описывает production-функцию, покрытую тестом.
type TestedFunction struct {
	// Name — имя production-функции.
	Name string
	// TestName — имя Test*-функции, которая её вызывает.
	TestName string
	// TestBody — исходный код теста (передаётся в LLM для контекста).
	TestBody string
}

// ParseTestedFunctions сканирует тестовые файлы в указанных директориях
// и возвращает карту production-функций, покрытых тестами.
// Ключ — имя production-функции, значение — описание теста.
func ParseTestedFunctions(dirs []string) (map[string]TestedFunction, error) {
	result := make(map[string]TestedFunction)

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			funcs, err := parseTestFile(filepath.Join(dir, e.Name()))
			if err != nil {
				return nil, err
			}
			for k, v := range funcs {
				// Если функция уже найдена другим тестом — не перезаписываем
				if _, exists := result[k]; !exists {
					result[k] = v
				}
			}
		}
	}
	return result, nil
}

// parseTestFile разбирает один тестовый файл и возвращает карту
// production-функций, напрямую вызываемых из Test*-функций.
func parseTestFile(path string) (map[string]TestedFunction, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, err
	}

	result := make(map[string]TestedFunction)

	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		// Обрабатываем только Test*-функции с непустым телом
		if !strings.HasPrefix(fd.Name.Name, "Test") {
			continue
		}

		// Исходный код теста для передачи в LLM
		var buf bytes.Buffer
		if err := format.Node(&buf, fset, fd); err == nil {
			testBody := buf.String()

			// Собираем все вызовы production-функций внутри теста
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := extractCallName(call)
				if name == "" || isInternalName(name) {
					return true
				}
				if _, exists := result[name]; !exists {
					result[name] = TestedFunction{
						Name:     name,
						TestName: fd.Name.Name,
						TestBody: testBody,
					}
				}
				return true
			})
		}
	}
	return result, nil
}

// BuildCallGraph строит граф прямых вызовов для всех функций в пакетах.
// Возвращает: map[funcName] → []calleeName
func BuildCallGraph(analyses []*FileAnalysis) map[string][]string {
	graph := make(map[string][]string)

	for _, fa := range analyses {
		for _, decl := range fa.ASTFile.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			caller := funcName(fd)
			seen := make(map[string]bool)

			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				callee := extractCallName(call)
				if callee == "" || callee == caller || seen[callee] {
					return true
				}
				seen[callee] = true
				graph[caller] = append(graph[caller], callee)
				return true
			})
		}
	}
	return graph
}

// ExpandWithCallees расширяет множество тестируемых функций, добавляя их
// вызываемые подфункции (callees) до указанной глубины.
//
// depth=0 — только напрямую покрытые тестами функции.
// depth=1 — добавить прямые подфункции (рекомендуемое значение по умолчанию).
func ExpandWithCallees(
	tested map[string]TestedFunction,
	graph map[string][]string,
	depth int,
) map[string]bool {
	result := make(map[string]bool, len(tested))
	for name := range tested {
		result[name] = true
	}

	if depth <= 0 {
		return result
	}

	// BFS по графу вызовов
	type entry struct {
		name  string
		depth int
	}
	queue := make([]entry, 0, len(result))
	for name := range result {
		queue = append(queue, entry{name, 0})
	}
	visitedAt := make(map[string]int)
	for name := range result {
		visitedAt[name] = 0
	}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if curr.depth >= depth {
			continue
		}

		for _, callee := range graph[curr.name] {
			if _, seen := visitedAt[callee]; !seen {
				visitedAt[callee] = curr.depth + 1
				result[callee] = true
				queue = append(queue, entry{callee, curr.depth + 1})
			}
		}
	}

	return result
}

// ExpandWithCalleesContext возвращает карту функций для мутации вместе
// с тестом-источником, который следует запускать против мутанта.
// Для напрямую покрытых функций берётся их собственный тест, для callees —
// тест ближайшей родительской функции, от которой они были достигнуты в BFS.
func ExpandWithCalleesContext(
	tested map[string]TestedFunction,
	graph map[string][]string,
	depth int,
) map[string]TestedFunction {
	result := make(map[string]TestedFunction, len(tested))
	for name, tf := range tested {
		result[name] = tf
	}

	if depth <= 0 {
		return result
	}

	type entry struct {
		name   string
		depth  int
		source TestedFunction
	}

	names := make([]string, 0, len(tested))
	for name := range tested {
		names = append(names, name)
	}
	sort.Strings(names)

	queue := make([]entry, 0, len(names))
	visitedAt := make(map[string]int, len(tested))
	for _, name := range names {
		tf := tested[name]
		queue = append(queue, entry{name: name, depth: 0, source: tf})
		visitedAt[name] = 0
	}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if curr.depth >= depth {
			continue
		}

		for _, callee := range graph[curr.name] {
			if _, seen := visitedAt[callee]; seen {
				continue
			}
			visitedAt[callee] = curr.depth + 1
			result[callee] = curr.source
			queue = append(queue, entry{
				name:   callee,
				depth:  curr.depth + 1,
				source: curr.source,
			})
		}
	}

	return result
}

// extractCallName извлекает имя вызываемой функции из CallExpr.
// Для методов возвращает имя метода, исключая вызовы через t/b/testing.
func extractCallName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		// Пропускаем вызовы t.Error(), b.Run(), testing.T и т.п.
		if ident, ok := fn.X.(*ast.Ident); ok {
			switch ident.Name {
			case "t", "b", "m", "testing", "require", "assert":
				return ""
			}
		}
		return fn.Sel.Name
	}
	return ""
}

// isInternalName возвращает true для встроенных функций Go и функций testing,
// которые не являются production-кодом и не должны мутироваться.
func isInternalName(name string) bool {
	builtins := map[string]bool{
		"append": true, "len": true, "cap": true, "make": true, "new": true,
		"copy": true, "delete": true, "close": true, "panic": true, "recover": true,
		"print": true, "println": true, "complex": true, "real": true, "imag": true,
		"Error": true, "Errorf": true, "Fatal": true, "Fatalf": true,
		"Log": true, "Logf": true, "Skip": true, "Skipf": true,
		"Run": true, "Parallel": true, "Helper": true, "Cleanup": true,
		"NoError": true, "Equal": true, "NotEqual": true, // testify
	}
	return builtins[name]
}
