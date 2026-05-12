// Пакет reporter вычисляет метрики мутационного тестирования и форматирует результаты.
package reporter

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/Sleeps17/gomutationai/internal/mutator"
	"github.com/Sleeps17/gomutationai/internal/runner"
)

// Report содержит все метрики и результаты по каждому мутанту.
type Report struct {
	TotalMutants  int `json:"total_mutants"`
	Killed        int `json:"killed"`
	Survived      int `json:"survived"`
	CompileErrors int `json:"compile_errors"`
	Timeouts      int `json:"timeouts"`
	// MutationScore — доля убитых мутантов среди скомпилировавшихся:
	// killed / (total - compile_errors)
	MutationScore float64 `json:"mutation_score"`
	// CompilabilityRate — доля мутантов, прошедших компиляцию:
	// (total - compile_errors) / total
	CompilabilityRate float64 `json:"compilability_rate"`
	// DiversityIndex — отношение числа уникальных операторов к общему числу мутантов.
	DiversityIndex float64         `json:"diversity_index"`
	Results        []runner.Result `json:"results"`
}

// Build агрегирует результаты прогона в Report.
func Build(results []runner.Result) *Report {
	r := &Report{Results: results}
	operatorSet := make(map[string]struct{})

	for _, res := range results {
		r.TotalMutants++
		operatorSet[res.Mutant.OperatorName] = struct{}{}
		switch res.Mutant.Status {
		case mutator.StatusKilled:
			r.Killed++
		case mutator.StatusSurvived:
			r.Survived++
		case mutator.StatusCompileError:
			r.CompileErrors++
		case mutator.StatusTimeout:
			r.Timeouts++
		}
	}

	valid := r.TotalMutants - r.CompileErrors
	if valid > 0 {
		r.MutationScore = float64(r.Killed) / float64(valid)
	}
	if r.TotalMutants > 0 {
		r.CompilabilityRate = float64(r.TotalMutants-r.CompileErrors) / float64(r.TotalMutants)
		r.DiversityIndex = float64(len(operatorSet)) / float64(r.TotalMutants)
	}

	return r
}

// PrintConsole выводит человекочитаемый отчёт в stdout.
func (r *Report) PrintConsole() {
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("           ОТЧЁТ МУТАЦИОННОГО ТЕСТИРОВАНИЯ              ")
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Printf("  Всего мутантов        : %d\n", r.TotalMutants)
	fmt.Printf("  Убито (killed)        : %d\n", r.Killed)
	fmt.Printf("  Выжило (survived)     : %d\n", r.Survived)
	fmt.Printf("  Ошибки компиляции     : %d\n", r.CompileErrors)
	fmt.Printf("  Таймаут               : %d\n", r.Timeouts)
	fmt.Println("───────────────────────────────────────────────────────")
	fmt.Printf("  Mutation Score        : %.2f%%\n", r.MutationScore*100)
	fmt.Printf("  Compilability Rate    : %.2f%%\n", r.CompilabilityRate*100)
	fmt.Printf("  Diversity Index       : %.2f\n", r.DiversityIndex)
	fmt.Println("═══════════════════════════════════════════════════════")

	// Список выживших мутантов — конкретные рекомендации по улучшению тестов
	survived := filterByStatus(r.Results, mutator.StatusSurvived)
	if len(survived) > 0 {
		fmt.Printf("\n  ВЫЖИВШИЕ МУТАНТЫ (%d) — рекомендуется улучшить тесты:\n\n", len(survived))
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  ID\tФайл:Строка\tОператор\tОригинал → Мутация")
		fmt.Fprintln(w, "  ──\t───────────\t────────\t──────────────────")
		for _, res := range survived {
			m := res.Mutant
			fmt.Fprintf(w, "  %s\t%s:%d\t%s\t%s → %s\n",
				m.ID,
				shortPath(m.File), m.Line,
				m.OperatorName,
				truncate(m.Original, 20),
				truncate(m.Mutated, 20),
			)
		}
		w.Flush()
	}

	fmt.Println()
}

// PrintOperatorBreakdown выводит таблицу Mutation Score по каждому оператору.
func (r *Report) PrintOperatorBreakdown() {
	byOp := r.ByOperator()
	type row struct {
		Name  string
		Stats OperatorStats
	}
	rows := make([]row, 0, len(byOp))
	for k, v := range byOp {
		rows = append(rows, row{k, v})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })

	fmt.Println("  МЕТРИКИ ПО ОПЕРАТОРАМ:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  Оператор\tВсего\tУбито\tScore")
	fmt.Fprintln(w, "  ────────\t─────\t─────\t─────")
	for _, row := range rows {
		fmt.Fprintf(w, "  %s\t%d\t%d\t%.0f%%\n",
			row.Name, row.Stats.Total, row.Stats.Killed, row.Stats.Score*100)
	}
	w.Flush()
	fmt.Println()
}

// SaveJSON сохраняет полный отчёт в JSON-файл по указанному пути.
func (r *Report) SaveJSON(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// ByOperator возвращает статистику по каждому оператору мутации.
func (r *Report) ByOperator() map[string]OperatorStats {
	stats := make(map[string]OperatorStats)
	for _, res := range r.Results {
		op := res.Mutant.OperatorName
		s := stats[op]
		s.Total++
		if res.Mutant.Status == mutator.StatusKilled {
			s.Killed++
		}
		stats[op] = s
	}
	for op, s := range stats {
		if s.Total > 0 {
			s.Score = float64(s.Killed) / float64(s.Total)
		}
		stats[op] = s
	}
	return stats
}

// OperatorStats хранит агрегированную статистику для одного оператора мутации.
type OperatorStats struct {
	Total  int
	Killed int
	Score  float64
}

func filterByStatus(results []runner.Result, status mutator.Status) []runner.Result {
	var out []runner.Result
	for _, r := range results {
		if r.Mutant.Status == status {
			out = append(out, r)
		}
	}
	return out
}

func shortPath(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) <= 2 {
		return p
	}
	return strings.Join(parts[len(parts)-2:], "/")
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
