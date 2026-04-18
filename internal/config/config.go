// Пакет config содержит конфигурацию инструмента мутационного тестирования.
package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config хранит все настройки инструмента.
type Config struct {
	// Таймаут на один запуск тестов
	Timeout time.Duration `yaml:"timeout"`

	// LLMBaseURL — базовый URL OpenAI-совместимого API.
	// Примеры: https://api.openai.com/v1, http://localhost:11434/v1 (Ollama)
	LLMBaseURL string `yaml:"llm_base_url"`

	// LLMAPIKey — токен доступа к LLM-сервису.
	// Может быть пустым для локальных моделей без авторизации.
	LLMAPIKey string `yaml:"llm_api_key"`

	// LLMModel — идентификатор модели, например "gpt-4o-mini" или "llama3".
	LLMModel string `yaml:"llm_model"`

	// StructuredOutput включает режим Structured Output (JSON Schema).
	// Работает только с моделями, которые его поддерживают.
	StructuredOutput bool `yaml:"structured_output"`

	// OutputFile — путь для сохранения JSON-отчёта.
	OutputFile string `yaml:"output"`

	// Workers — число параллельных тестовых прогонов.
	// 0 означает автоматический выбор по числу логических ядер CPU.
	Workers int `yaml:"workers"`

	// MaxMutants ограничивает число мутантов (0 = без ограничений).
	MaxMutants int `yaml:"max_mutants"`

	// Verbose включает подробный вывод статуса для каждого мутанта.
	Verbose bool `yaml:"verbose"`
}

// Default возвращает конфигурацию с безопасными значениями по умолчанию.
func Default() *Config {
	return &Config{
		Timeout:          30 * time.Second,
		LLMBaseURL:       "https://api.openai.com/v1",
		LLMModel:         "gpt-4o-mini",
		StructuredOutput: true,
	}
}

// Load читает конфигурацию из YAML-файла.
// Если файл не существует, возвращается конфигурация по умолчанию без ошибки.
func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
