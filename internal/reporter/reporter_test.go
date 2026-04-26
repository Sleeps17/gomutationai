package reporter

import (
	"encoding/json"
	"os"
	"testing"

	"gitlab.mai.ru/gomutationai/internal/mutator"
	"gitlab.mai.ru/gomutationai/internal/runner"
)

// makeResult создаёт тестовый runner.Result с заданным статусом.
func makeResult(id, operator string, status mutator.Status) runner.Result {
	return runner.Result{
		Mutant: mutator.Mutant{
			ID:           id,
			OperatorName: operator,
			Status:       status,
			File:         "pkg/math.go",
			Line:         10,
		},
	}
}

func TestBuild_EmptyResults(t *testing.T) {
	rep := Build(nil)
	if rep.TotalMutants != 0 {
		t.Errorf("TotalMutants = %d, want 0", rep.TotalMutants)
	}
	if rep.MutationScore != 0 {
		t.Errorf("MutationScore = %f, want 0", rep.MutationScore)
	}
	if rep.CompilabilityRate != 0 {
		t.Errorf("CompilabilityRate = %f, want 0", rep.CompilabilityRate)
	}
	if rep.DiversityIndex != 0 {
		t.Errorf("DiversityIndex = %f, want 0", rep.DiversityIndex)
	}
}

func TestBuild_AllKilled(t *testing.T) {
	results := []runner.Result{
		makeResult("m1", "OpA", mutator.StatusKilled),
		makeResult("m2", "OpB", mutator.StatusKilled),
		makeResult("m3", "OpC", mutator.StatusKilled),
	}
	rep := Build(results)

	if rep.TotalMutants != 3 {
		t.Errorf("TotalMutants = %d, want 3", rep.TotalMutants)
	}
	if rep.Killed != 3 {
		t.Errorf("Killed = %d, want 3", rep.Killed)
	}
	if rep.MutationScore != 1.0 {
		t.Errorf("MutationScore = %f, want 1.0", rep.MutationScore)
	}
	if rep.CompilabilityRate != 1.0 {
		t.Errorf("CompilabilityRate = %f, want 1.0", rep.CompilabilityRate)
	}
	if rep.DiversityIndex != 1.0 {
		t.Errorf("DiversityIndex = %f, want 1.0", rep.DiversityIndex)
	}
}

func TestBuild_AllCompileErrors(t *testing.T) {
	results := []runner.Result{
		makeResult("m1", "Op", mutator.StatusCompileError),
		makeResult("m2", "Op", mutator.StatusCompileError),
	}
	rep := Build(results)

	if rep.CompileErrors != 2 {
		t.Errorf("CompileErrors = %d, want 2", rep.CompileErrors)
	}
	if rep.MutationScore != 0 {
		t.Errorf("MutationScore должен быть 0 когда нет валидных мутантов")
	}
	if rep.CompilabilityRate != 0 {
		t.Errorf("CompilabilityRate = %f, want 0", rep.CompilabilityRate)
	}
}

func TestBuild_MixedStatuses(t *testing.T) {
	results := []runner.Result{
		makeResult("m1", "OpA", mutator.StatusKilled),
		makeResult("m2", "OpB", mutator.StatusKilled),
		makeResult("m3", "OpC", mutator.StatusSurvived),
		makeResult("m4", "OpD", mutator.StatusCompileError),
		makeResult("m5", "OpE", mutator.StatusTimeout),
	}
	rep := Build(results)

	if rep.TotalMutants != 5 {
		t.Errorf("TotalMutants = %d, want 5", rep.TotalMutants)
	}
	if rep.Killed != 2 {
		t.Errorf("Killed = %d, want 2", rep.Killed)
	}
	if rep.Survived != 1 {
		t.Errorf("Survived = %d, want 1", rep.Survived)
	}
	if rep.CompileErrors != 1 {
		t.Errorf("CompileErrors = %d, want 1", rep.CompileErrors)
	}
	if rep.Timeouts != 1 {
		t.Errorf("Timeouts = %d, want 1", rep.Timeouts)
	}
	// MutationScore = 2 / (5-1) = 0.5
	if rep.MutationScore != 0.5 {
		t.Errorf("MutationScore = %f, want 0.5", rep.MutationScore)
	}
	// CompilabilityRate = (5-1)/5 = 0.8
	if rep.CompilabilityRate != 0.8 {
		t.Errorf("CompilabilityRate = %f, want 0.8", rep.CompilabilityRate)
	}
}

func TestBuild_DiversityIndex(t *testing.T) {
	// 3 мутанта, 2 уникальных оператора → DiversityIndex = 2/3
	results := []runner.Result{
		makeResult("m1", "OpA", mutator.StatusKilled),
		makeResult("m2", "OpA", mutator.StatusKilled),
		makeResult("m3", "OpB", mutator.StatusSurvived),
	}
	rep := Build(results)
	expected := 2.0 / 3.0
	if rep.DiversityIndex != expected {
		t.Errorf("DiversityIndex = %f, want %f", rep.DiversityIndex, expected)
	}
}

func TestByOperator(t *testing.T) {
	results := []runner.Result{
		makeResult("m1", "OpA", mutator.StatusKilled),
		makeResult("m2", "OpA", mutator.StatusSurvived),
		makeResult("m3", "OpB", mutator.StatusKilled),
	}
	rep := Build(results)
	byOp := rep.ByOperator()

	opA, ok := byOp["OpA"]
	if !ok {
		t.Fatal("OpA должен быть в ByOperator")
	}
	if opA.Total != 2 {
		t.Errorf("OpA.Total = %d, want 2", opA.Total)
	}
	if opA.Killed != 1 {
		t.Errorf("OpA.Killed = %d, want 1", opA.Killed)
	}
	if opA.Score != 0.5 {
		t.Errorf("OpA.Score = %f, want 0.5", opA.Score)
	}

	opB, ok := byOp["OpB"]
	if !ok {
		t.Fatal("OpB должен быть в ByOperator")
	}
	if opB.Score != 1.0 {
		t.Errorf("OpB.Score = %f, want 1.0", opB.Score)
	}
}

func TestSaveJSON(t *testing.T) {
	results := []runner.Result{
		makeResult("m1", "OpA", mutator.StatusKilled),
	}
	rep := Build(results)

	f, err := os.CreateTemp("", "report-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.Close()

	if err := rep.SaveJSON(f.Name()); err != nil {
		t.Fatalf("SaveJSON: %v", err)
	}

	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	var decoded Report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("JSON невалидный: %v", err)
	}
	if decoded.TotalMutants != 1 {
		t.Errorf("TotalMutants в JSON = %d, want 1", decoded.TotalMutants)
	}
	if decoded.Killed != 1 {
		t.Errorf("Killed в JSON = %d, want 1", decoded.Killed)
	}
}

func TestSaveJSON_InvalidPath(t *testing.T) {
	rep := Build(nil)
	err := rep.SaveJSON("/nonexistent/dir/report.json")
	if err == nil {
		t.Error("ожидалась ошибка для несуществующего пути")
	}
}

func TestPrintConsole_NoPanic(t *testing.T) {
	results := []runner.Result{
		makeResult("m1", "OpA", mutator.StatusKilled),
		makeResult("m2", "OpB", mutator.StatusSurvived),
		makeResult("m3", "OpC", mutator.StatusCompileError),
	}
	rep := Build(results)
	// Не должно паниковать
	rep.PrintConsole()
}

func TestPrintOperatorBreakdown_NoPanic(t *testing.T) {
	results := []runner.Result{
		makeResult("m1", "OpA", mutator.StatusKilled),
		makeResult("m2", "OpA", mutator.StatusSurvived),
	}
	rep := Build(results)
	rep.PrintOperatorBreakdown()
}

// ── filterByStatus ──────────────────────────────────────────────────────────

func TestFilterByStatus(t *testing.T) {
	results := []runner.Result{
		makeResult("m1", "Op", mutator.StatusKilled),
		makeResult("m2", "Op", mutator.StatusSurvived),
		makeResult("m3", "Op", mutator.StatusSurvived),
	}
	survived := filterByStatus(results, mutator.StatusSurvived)
	if len(survived) != 2 {
		t.Errorf("filterByStatus вернул %d, want 2", len(survived))
	}
	killed := filterByStatus(results, mutator.StatusKilled)
	if len(killed) != 1 {
		t.Errorf("filterByStatus вернул %d, want 1", len(killed))
	}
	none := filterByStatus(results, mutator.StatusTimeout)
	if len(none) != 0 {
		t.Errorf("filterByStatus вернул %d, want 0", len(none))
	}
}

// ── shortPath ───────────────────────────────────────────────────────────────

func TestShortPath(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"pkg/math.go", "pkg/math.go"},
		{"a/b/c/math.go", "c/math.go"},
		{"math.go", "math.go"},
	}
	for _, c := range cases {
		got := shortPath(c.input)
		if got != c.want {
			t.Errorf("shortPath(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ── truncate ─────────────────────────────────────────────────────────────────

func TestTruncate(t *testing.T) {
	cases := []struct {
		input string
		n     int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello…"},
		{"", 5, ""},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc…"},
		{"  a + b  ", 20, "a + b"},        // trimspace
		{"a\n+\nb", 20, "a + b"},          // newlines replaced
	}
	for _, c := range cases {
		got := truncate(c.input, c.n)
		if got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.input, c.n, got, c.want)
		}
	}
}
