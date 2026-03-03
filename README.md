# Diatune Safe (Go)

Открытая платформа для безопасной настройки профиля терапии при диабете 1 типа в режиме `только-предложения`.

Главный принцип: сервис **никогда не меняет настройки автоматически**.
Он только формирует предложения и отправляет их через API/Telegram для ручной проверки.

## Важно

- Это не медицинское изделие и не замена врачу.
- Любые изменения коэффициентов подтверждаются человеком.
- При риске гипогликемий агрессивные изменения автоматически блокируются.

## Что реализовано

- Многоблочный профиль (`ICR`, `ISF`, `basal`) по временным блокам.
- Источники данных: `Nightscout` + синтетический источник.
- Продвинутый анализ:
  - постпрандиальные дельты,
  - эффективность корректировок,
  - дрейф в голодном окне,
  - робастная статистика (`MAD`, `winsorized mean`),
  - оценка вариативности,
  - вероятностная модель решения (Monte Carlo).
- Политика безопасности:
  - лимит суточного шага,
  - блокировки по гипо,
  - блокировки при низкой уверенности,
  - блокировки по вероятностному риску,
  - физиологические границы параметров.
- Полный аудит в SQLite:
  - профили,
  - отчеты,
  - рекомендации,
  - ручные подтверждения.
- HTTP API, Telegram-бот, планировщик.
- Проверка на истории и недельная статистика.
- Локализация под РФ: русский язык, `Europe/Moscow`, отображение mmol/L.
- Рекомендации сразу в терминах профиля AAPS (`IC`, `ISF`, `Basal`) по временным блокам.

## Архитектура

- `cmd/diatune-safe/main.go` - единый CLI (`api`, `bot`, `worker`, `analyze`, `bootstrap`, `backtest`, `weekstats`)
- `internal/config` - конфигурация из env
- `internal/datasource` - источники Nightscout/синтетика
- `internal/engine` - алгоритмы рекомендаций
- `internal/safety` - ограничения безопасности
- `internal/repository` - хранение и аудит в SQLite
- `internal/service` - оркестрация
- `internal/api` - HTTP API
- `internal/telegram` - Telegram-бот
- `internal/scheduler` - фоновые периодические запуски

## Быстрый старт

1. Подготовить `.env`:

```bash
cp .env.example .env
```

2. Установить Go (1.24+) и зависимости:

```bash
go mod tidy
```

3. Создать профиль пациента:

```bash
go run ./cmd/diatune-safe bootstrap --patient-id demo
```

4. Разовый анализ:

```bash
go run ./cmd/diatune-safe analyze --patient-id demo --days 14 --synthetic
```

5. Запуск API:

```bash
go run ./cmd/diatune-safe api --host 0.0.0.0 --port 8080
```

## Telegram-бот

Заполните в `.env`:

- `TELEGRAM_BOT_TOKEN`
- `TELEGRAM_ALLOWED_USER_IDS` (через запятую, опционально)

Запуск:

```bash
go run ./cmd/diatune-safe bot
```

Команды:

- `/analyze [patient_id] [days]` - свежий анализ
- `/latest [patient_id]` - последний отчет
- `/pending [patient_id]` - список рекомендаций для ручной проверки
- `/ack <recommendation_id> [reviewer]` - отметить рекомендацию как проверенную
- `/backtest [patient_id] [days]` - проверка алгоритма на истории
- `/weekstats [patient_id] [days]` - сравнение недели к неделе
- `/version` - версия сервиса

## Планировщик (worker)

Настройки в `.env`:

- `AUTO_ANALYSIS_ENABLED=true`
- `AUTO_ANALYSIS_INTERVAL_MINUTES=360`
- `AUTO_ANALYSIS_PATIENT_IDS=patient-a,patient-b`
- `MONTE_CARLO_SAMPLES=1200`
- `MIN_BENEFIT_PROBABILITY=0.35`
- `MAX_HYPO_RISK_PROBABILITY=0.60`
- `DAILY_RECOMMENDATION_ENABLED=true`
- `DAILY_RECOMMENDATION_TIME=22:00` (формат `HH:MM`)
- `DAILY_RECOMMENDATION_PATIENT_IDS=patient-a,patient-b` (опционально)
- `WEEKLY_STATS_ENABLED=true`
- `WEEKLY_STATS_DAY=mon` (`sun..sat`)
- `WEEKLY_STATS_TIME=21:00` (формат `HH:MM`)
- `WEEKLY_STATS_LOOKBACK_DAYS=7`
- `WEEKLY_STATS_PATIENT_IDS=patient-a,patient-b` (опционально)
- `TIMEZONE=Europe/Moscow`

Запуск:

```bash
go run ./cmd/diatune-safe worker
```

или:

```bash
go run ./cmd/diatune-safe worker --patients patient-a,patient-b
```

## API

Если задан `APP_API_KEY`, передавайте заголовок:

`X-API-Key: <APP_API_KEY>`

Основные эндпоинты:

- `GET /healthz`
- `GET /v1/patients/{patient_id}/profile`
- `PUT /v1/patients/{patient_id}/profile`
- `POST /v1/patients/{patient_id}/analyze?days=14&prefer_real_data=true`
- `GET /v1/patients/{patient_id}/backtest?days=42&prefer_real_data=true`
- `GET /v1/patients/{patient_id}/weekly-stats?days=7&prefer_real_data=true`
- `GET /v1/patients/{patient_id}/reports/latest`
- `GET /v1/patients/{patient_id}/reports?limit=20`
- `GET /v1/patients/{patient_id}/recommendations/pending`
- `POST /v1/recommendations/{recommendation_id}/acknowledge`

## Тесты

```bash
go test ./...
```

## Версионирование

- Текущая версия хранится в `VERSION`.
- CLI-команда: `diatune-safe version`.
- Telegram-команда: `/version`.

## Docker

```bash
docker build -t diatune-safe .
docker run --rm -p 8080:8080 --env-file .env diatune-safe
```

## Публикация в GitHub

Автопубликация (без локального `git`, через `gh api`):

```bash
GH_TOKEN=... ./scripts/publish_private_repo.sh <owner/repo> "Релиз 0.0.4"
```

## Лицензия

MIT (`LICENSE`).

## OSS-процессы

- История изменений: `CHANGELOG.md`
- Правила вклада: `CONTRIBUTING.md`
- Политика безопасности: `SECURITY.md`
- Кодекс поведения: `CODE_OF_CONDUCT.md`
