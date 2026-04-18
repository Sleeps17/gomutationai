// Пакет analyzer выполняет статический анализ Go-файлов на основе AST.
// Он извлекает функции с контекстом и находит узлы, пригодные для мутации.
package analyzer

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Примечание: пакет go/ast используется только для обхода AST в extractFunctions.

// FunctionContext содержит информацию о функции, передаваемую в LLM.
type FunctionContext struct {
	File      string
	Name      string
	Signature string
	Body      string
	Comment   string
	StartLine int
	EndLine   int
}

// FileAnalysis — результат анализа одного Go-файла.
type FileAnalysis struct {
	FilePath  string
	Fset      *token.FileSet
	ASTFile   *ast.File
	Functions []FunctionContext
}

// AnalyzeFile парсит Go-файл и извлекает цели мутации.
func AnalyzeFile(path string) (*FileAnalysis, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("чтение файла %s: %w", path, err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("разбор %s: %w", path, err)
	}

	fa := &FileAnalysis{
		FilePath: path,
		Fset:     fset,
		ASTFile:  file,
	}

	fa.Functions = extractFunctions(fset, file, src)

	return fa, nil
}

// AnalyzePackage анализирует все не-тестовые Go-файлы в директории.
func AnalyzePackage(dir string) ([]*FileAnalysis, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var results []*FileAnalysis
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		fa, err := AnalyzeFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		results = append(results, fa)
	}
	return results, nil
}

// extractFunctions собирает все функции и методы верхнего уровня вместе
// с их исходным текстом и doc-комментариями.
func extractFunctions(fset *token.FileSet, file *ast.File, src []byte) []FunctionContext {
	var funcs []FunctionContext

	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		fc := FunctionContext{
			File:      fset.File(fd.Pos()).Name(),
			Name:      funcName(fd),
			StartLine: fset.Position(fd.Pos()).Line,
			EndLine:   fset.Position(fd.End()).Line,
		}

		// Документирующий комментарий
		if fd.Doc != nil {
			fc.Comment = fd.Doc.Text()
		}

		// Сигнатура: всё до открывающей фигурной скобки тела
		if fd.Body != nil {
			sigEnd := fd.Body.Lbrace
			fc.Signature = strings.TrimSpace(string(src[fset.File(fd.Pos()).Offset(fd.Pos()):fset.File(sigEnd).Offset(sigEnd)]))
		}

		// Полное тело функции в отформатированном виде
		var buf bytes.Buffer
		if err := format.Node(&buf, fset, fd); err == nil {
			fc.Body = buf.String()
		}

		funcs = append(funcs, fc)
	}
	return funcs
}

// funcName возвращает "(*ReceiverType).MethodName" или просто "FuncName".
func funcName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	recv := fd.Recv.List[0].Type
	var buf bytes.Buffer
	format.Node(&buf, token.NewFileSet(), recv)
	return buf.String() + "." + fd.Name.Name
}

