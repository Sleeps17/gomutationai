# gomutationai

[![CI](https://github.com/Sleeps17/gomutationai/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/Sleeps17/gomutationai/actions/workflows/ci.yml)
[![Release](https://github.com/Sleeps17/gomutationai/actions/workflows/release.yml/badge.svg)](https://github.com/Sleeps17/gomutationai/actions/workflows/release.yml)
[![codecov](https://codecov.io/gh/Sleeps17/gomutationai/graph/badge.svg?token=RZ81R280CI)](https://codecov.io/gh/Sleeps17/gomutationai)
[![Go Report Card](https://goreportcard.com/badge/github.com/Sleeps17/gomutationai)](https://goreportcard.com/report/github.com/Sleeps17/gomutationai)
[![Go Reference](https://pkg.go.dev/badge/github.com/Sleeps17/gomutationai.svg)](https://pkg.go.dev/github.com/Sleeps17/gomutationai)
[![Go version](https://img.shields.io/github/go-mod/go-version/Sleeps17/gomutationai)](https://go.dev/dl/)
[![Release version](https://img.shields.io/github/v/release/Sleeps17/gomutationai?display_name=tag&sort=semver)](https://github.com/Sleeps17/gomutationai/releases)

Инструмент мутационного тестирования для языка Go на основе больших языковых моделей (LLM).

В отличие от классических мутаторов (go-mutesting, Gremlins), которые заменяют токены по заранее
заданным правилам, **gomutationai** генерирует мутанты с помощью LLM — модель анализирует логику
функции и предлагает семантически осмысленное изменение, имитирующее реальную ошибку программиста.

## Принцип работы

```
Исходный код Go
      │
      ▼
  AST-анализ                  ← go/parser, go/ast
  Извлечение функций с контекстом
      │
      ▼
  Запрос к LLM                ← OpenAI-совместимый API
  Chain-of-Thought промпт
  [Structured Output / JSON]
      │
      ▼
  Верификация мутанта         ← go/parser (синтаксис)
      │
      ▼
  Запуск go test              ← на каждый мутант
      │
      ▼
  Отчёт: Mutation Score,
         Compilability Rate,
         Diversity Index
```

Поддерживаемые LLM-провайдеры (любой OpenAI-совместимый сервер):

| Провайдер   | Base URL                          | Примечание                          |
|-------------|-----------------------------------|-------------------------------------|
| OpenAI      | `https://api.openai.com/v1`       | Рекомендуется `gpt-4o` или `gpt-4o-mini` |
| Ollama      | `http://localhost:11434/v1`       | Локально, без API-ключа             |
| LM Studio   | `http://localhost:1234/v1`        | Локально, без API-ключа             |
| vLLM        | `http://localhost:8000/v1`        | Self-hosted                         |
| Azure OpenAI| `https://<resource>.openai.azure.com/openai/deployments/<deployment>` | Нужен ключ |

## Требования

- Go 1.21+
- Доступ к OpenAI-совместимому LLM API
- Тестируемый пакет должен иметь тесты (`go test` должен работать)

## Установка

```bash
go install github.com/Sleeps17/gomutationai@latest
```

Или запуск напрямую без сборки:

```bash
go run . run [флаги] [директория ...]
```

## Быстрый старт

### OpenAI

```bash
export OPENAI_API_KEY=sk-...

gomutationai run \
  --model gpt-4o-mini \
  --output report.json \
  ./path/to/your/package
```

### Несколько пакетов

```bash
gomutationai run \
  --model gpt-4o-mini \
  ./pkg/orders ./pkg/users ./pkg/auth
```

### Ollama (локально, без ключа)

```bash
# Запустить модель в Ollama
ollama run llama3

gomutationai run \
  --llm-url http://localhost:11434/v1 \
  --model llama3 \
  --structured-output=false \
  ./path/to/your/package
```

### LM Studio

```bash
gomutationai run \
  --llm-url http://localhost:1234/v1 \
  --model local-model \
  --structured-output=false \
  ./path/to/your/package
```

### Dry-run (только генерация, без запуска тестов)

```bash
gomutationai run --dry-run ./mypackage
```

## Принцип выбора функций для мутации

Инструмент не мутирует весь код подряд — он анализирует тестовые файлы и мутирует только те
функции, которые реально покрыты тестами:

```
*_test.go файлы
      │
      ▼
  Парсинг Test*-функций        ← go/ast
  Извлечение вызовов production-функций
      │
      ▼
  Граф вызовов (call graph)    ← AST production-кода
  BFS-расширение до глубины --callee-depth
      │
      ▼
  Финальное множество функций для мутации
```

**Пример:** если `TestOrder` вызывает `PlaceOrder`, а `PlaceOrder` внутри вызывает `ValidateCart`
и `ChargePayment`, то при `--callee-depth=1` мутируются все три функции — даже если на
`ValidateCart` и `ChargePayment` нет собственных тестов.

Тело теста передаётся в LLM вместе с кодом функции. Модель генерирует мутацию, которую
**именно этот тест с наибольшей вероятностью не поймает** — это делает мутации максимально
полезными для оценки качества тестового набора.

## Параметры запуска

### Команда `run`

```
gomutationai run [директория ...] [флаги]
```

Можно передать одну или несколько директорий. Если директория не указана, используется текущая (`.`).

#### Параметры LLM

| Флаг                  | Переменная окружения | По умолчанию                    | Описание |
|-----------------------|----------------------|---------------------------------|----------|
| `--llm-url`           | —                    | `https://api.openai.com/v1`     | Базовый URL OpenAI-совместимого API |
| `--llm-key`           | `OPENAI_API_KEY`     | —                               | Токен доступа к LLM. Для локальных моделей можно не указывать |
| `--model`             | —                    | `gpt-4o-mini`                   | Идентификатор модели |
| `--structured-output` | —                    | `true`                          | Использовать [Structured Output](https://platform.openai.com/docs/guides/structured-outputs) (JSON Schema). Отключить для моделей без поддержки |

#### Выбор функций для мутации

| Флаг              | По умолчанию | Описание |
|-------------------|--------------|----------|
| `--callee-depth`  | `1`          | Глубина расширения покрытия по графу вызовов. `0` — только напрямую тестируемые функции, `1` — плюс их прямые подфункции и т.д. |

#### Управление прогоном

| Флаг            | Сокр. | По умолчанию  | Описание |
|-----------------|-------|---------------|----------|
| `--timeout`     | `-t`  | `30s`         | Таймаут одного тестового прогона. Формат: `10s`, `2m` |
| `--workers`     | `-w`  | `NumCPU`      | Число параллельных тестовых прогонов. Каждый запускается в изолированной копии модуля |
| `--max-mutants` | —     | `0` (без огр.)| Максимальное число мутантов. Удобно для быстрой проверки |
| `--verbose`     | `-v`  | `false`       | Выводить статус каждого мутанта в процессе прогона |
| `--dry-run`     | —     | `false`       | Только сгенерировать и показать мутантов, не запускать тесты |

#### Отчёт

| Флаг       | Сокр. | По умолчанию | Описание |
|------------|-------|--------------|----------|
| `--output` | `-o`  | —            | Сохранить JSON-отчёт в файл |

#### Глобальные параметры

| Флаг       | По умолчанию       | Описание |
|------------|--------------------|----------|
| `--config` | `.gomutationai.yaml`  | Путь к файлу конфигурации |

## Файл конфигурации

Вместо флагов можно использовать файл `.gomutationai.yaml` в директории запуска.
Флаги командной строки имеют приоритет над настройками файла.

```yaml
# .gomutationai.yaml

# URL OpenAI-совместимого API
llm_base_url: "https://api.openai.com/v1"

# Токен доступа (лучше передавать через OPENAI_API_KEY)
llm_api_key: ""

# Идентификатор модели
llm_model: "gpt-4o-mini"

# Structured Output: true для OpenAI gpt-4o и выше,
# false для локальных моделей (Ollama, LM Studio)
structured_output: true

# Таймаут одного тестового прогона
timeout: 30s

# Число параллельных тестовых прогонов (0 = NumCPU)
workers: 0

# Ограничение числа мутантов (0 — без ограничений)
max_mutants: 0

# Путь для JSON-отчёта
output: ""

# Подробный вывод
verbose: false
```

## Метрики

После завершения прогона инструмент выводит три метрики:

### Mutation Score
```
killed / (total - compile_errors)
```
Основная метрика. Показывает, какую долю мутантов обнаружили тесты.
- `1.0` (100%) — тесты обнаружили все мутанты, набор тестов качественный
- `< 0.5` (50%) — инструмент выведет предупреждение, тесты нуждаются в улучшении

### Compilability Rate
```
(total - compile_errors) / total
```
Доля мутантов, которые успешно прошли синтаксический анализ.
Низкое значение означает, что модель часто генерирует некорректный Go-код.
Для OpenAI gpt-4o обычно близко к `1.0`.

### Diversity Index
```
unique_operator_names / total
```
Вариативность сгенерированных мутаций. Значение `1.0` означает, что каждый
мутант использует уникальный тип изменения.

## Статусы мутантов

| Статус          | Описание |
|-----------------|----------|
| `killed`        | Тесты упали — мутант обнаружен. Хороший результат |
| `survived`      | Тесты прошли — мутант не обнаружен. Тесты нуждаются в улучшении |
| `compile_error` | Мутант не прошёл синтаксический анализ |
| `timeout`       | Тестовый прогон превысил таймаут (вероятно, мутация вызвала бесконечный цикл) |

## Пример вывода

```
gomutationai  модель: gpt-4o-mini  пакеты: ./mypackage

→ Анализ исходных файлов...
→ Поиск функций, покрытых тестами...
  Покрытых тестами функций: 5 (с callees глубиной 1: 6)
  Функций для мутации: 6

→ Генерация мутантов через LLM...
  mypackage/math.go → 6 мутантов

  Итого мутантов: 6

→ Запуск тестов против мутантов...

═══════════════════════════════════════════════════════
           ОТЧЁТ МУТАЦИОННОГО ТЕСТИРОВАНИЯ
═══════════════════════════════════════════════════════
  Всего мутантов        : 6
  Убито (killed)        : 4
  Выжило (survived)     : 2
  Ошибки компиляции     : 0
  Таймаут               : 0
───────────────────────────────────────────────────────
  Mutation Score        : 66.67%
  Compilability Rate    : 100.00%
  Diversity Index       : 0.83
═══════════════════════════════════════════════════════

  ВЫЖИВШИЕ МУТАНТЫ (2) — рекомендуется улучшить тесты:

  ID              Файл:Строка         Оператор           Оригинал → Мутация
  ──              ───────────         ────────           ──────────────────
  ai_Divide_1     mypackage/math.go:12  OffByOneError    b == 0 → b <= 0
  ai_Contains_4   mypackage/math.go:43  WrongReturn      return true → return false
```

## Structured Output

Флаг `--structured-output` управляет форматом ответа LLM:

**`true` (по умолчанию)** — используется [Structured Outputs API](https://platform.openai.com/docs/guides/structured-outputs).
Модель обязана вернуть JSON, строго соответствующий схеме. Поддерживается OpenAI
(`gpt-4o`, `gpt-4o-mini` и новее).

**`false`** — инструкции по формату включаются в текст промпта. Совместимо с любой
моделью, но возможны отклонения от ожидаемой структуры ответа.

Правило выбора: если модель поддерживает `response_format: json_schema` — включайте.
Для Ollama, LM Studio и большинства локальных моделей используйте `--structured-output=false`.

## Структура проекта

```
gomutationai/
├── main.go                        # Точка входа
├── cmd/
│   ├── root.go                    # Корневая команда cobra
│   └── run.go                     # Команда run
└── internal/
    ├── analyzer/
    │   ├── analyzer.go            # AST-анализ, извлечение функций
    │   └── testparser.go          # Парсинг тестов, граф вызовов, BFS-расширение
    ├── config/
    │   └── config.go              # Конфигурация (YAML + defaults)
    ├── mutator/
    │   ├── mutant.go              # Тип Mutant и статусы
    │   └── ai/
    │       ├── mutator.go         # LLM-генератор мутантов
    │       └── prompt.go          # Промпты и JSON Schema
    ├── runner/
    │   └── runner.go              # Запуск go test, восстановление файлов
    └── reporter/
       └── reporter.go            # Метрики и форматирование отчёта
```

## Параллельный изолированный запуск

Каждый мутант тестируется в **отдельной временной копии всего Go-модуля**:

```
модуль/          →  /tmp/gomutationai-abc123/   ← копия 1 (мутант #1)
  go.mod              go.mod
  go.sum              go.sum
  pkg/                pkg/
    math.go  ←─────── math.go  (мутирован)

                /tmp/gomutationai-def456/   ← копия 2 (мутант #2)
                /tmp/gomutationai-ghi789/   ← копия 3 (мутант #3)
                ...
```

Это даёт две гарантии:
- **Изоляция**: мутанты не мешают друг другу, оригинальные файлы никогда не изменяются
- **Параллелизм**: `--workers` копий запускают `go test` одновременно

По умолчанию число воркеров равно `runtime.NumCPU()`. На машине с 8 ядрами 8 мутантов
тестируются параллельно — ускорение в `~N` раз по сравнению с последовательным прогоном.

> **Дисковое пространство**: каждый воркер держит копию модуля во время прогона, после
> чего удаляет её. Пиковое потребление: `workers × размер_модуля`.

## Известные ограничения

- Анализируется только один уровень директорий (`AnalyzePackage` не рекурсивна)
- Мутации генерируются последовательно: один запрос к LLM на функцию
- Если `original_snippet` из ответа LLM не найден дословно в файле — мутант пропускается
- Детекция ошибок компиляции при запуске `go test` частично эвристическая; синтаксические ошибки
  отлавливаются через `go/parser` до запуска тестов
