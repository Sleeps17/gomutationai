// Пакет mutator определяет тип Mutant — центральную структуру данных инструмента.
package mutator

// Status описывает результат прогона тестов против мутанта.
type Status string

const (
	// StatusPending — мутант ещё не был проверен.
	StatusPending Status = "pending"
	// StatusKilled — тесты упали, мутант обнаружен (хорошо).
	StatusKilled Status = "killed"
	// StatusSurvived — тесты прошли, мутант не обнаружен (тесты слабые).
	StatusSurvived Status = "survived"
	// StatusCompileError — мутант не скомпилировался.
	StatusCompileError Status = "compile_error"
	// StatusTimeout — прогон тестов превысил таймаут.
	StatusTimeout Status = "timeout"
)

// Mutant описывает одно изменение в исходном коде, сгенерированное LLM.
type Mutant struct {
	// Уникальный идентификатор мутанта в рамках одного прогона.
	ID string
	// Путь к файлу, в котором выполнена мутация.
	File string
	// Номер строки мутации.
	Line int
	// Номер колонки мутации.
	Col int
	// Название оператора мутации, предложенного LLM (например "OffByOneError").
	OperatorName string
	// Человекочитаемое описание внесённого дефекта.
	Description string
	// BehavioralImpact — конкретный пример входных данных, на которых мутант меняет поведение.
	// Генерируется LLM как proof-of-non-equivalence.
	BehavioralImpact string
	// TargetTest — имя единственной тестовой функции, которую нужно запускать для этого мутанта.
	TargetTest string
	// Исходный фрагмент кода до мутации.
	Original string
	// Изменённый фрагмент кода после мутации.
	Mutated string
	// MutatedSrc — полное содержимое файла с применённой мутацией.
	// Заполняется runner'ом перед запуском тестов; в отчёт не включается.
	MutatedSrc []byte
	// Результат прогона тестов.
	Status Status
	// KilledBy — имя теста, убившего мутант (заполняется только при Status == Killed).
	KilledBy string
}
