// Пакет cmd реализует CLI-интерфейс инструмента gomutator с помощью cobra.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gitlab.mai.ru/gomutationai/internal/config"
)

var (
	cfgFile string
	cfg     *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "gomutator",
	Short: "Инструмент мутационного тестирования Go с поддержкой AI",
	Long: `gomutator — гибридный инструмент мутационного тестирования для языка Go.
Сочетает классические AST-операторы мутации с генерацией мутантов через
OpenAI-совместимый LLM API для получения семантически осмысленных дефектов.

Поддерживаемые LLM-провайдеры:
  - OpenAI (https://api.openai.com/v1)
  - Ollama (http://localhost:11434/v1)
  - LM Studio, vLLM и другие OpenAI-совместимые серверы`,
}

// Execute запускает корневую команду cobra.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", ".gomutator.yaml", "путь к файлу конфигурации")
}

// initConfig загружает конфигурацию из файла при старте любой команды.
func initConfig() {
	var err error
	cfg, err = config.Load(cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка загрузки конфигурации: %v\n", err)
		os.Exit(1)
	}
}

