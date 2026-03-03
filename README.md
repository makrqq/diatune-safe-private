# Diatune Safe (Go)

Open-source платформа для безопасной настройки профиля терапии при диабете 1 типа в режиме `suggest-only`.

Ключевой принцип: сервис **никогда не меняет настройки автоматически**.
Он только формирует рекомендации и отправляет их через API/Telegram для ручного подтверждения.

## Важно

- Это не медицинское изделие и не замена врачу.
- Любые изменения коэффициентов подтверждаются человеком.
- Агрессивные рекомендации автоматически блокируются при риске гипогликемий.
- Проект не является медицинским изделием.

## Что реализовано

- Многоблочный профиль (`ICR`, `ISF`, `basal`) по временным блокам.
- Источники данных: `Nightscout` + synthetic fallback.
- Продвинутый движок анализа:
  - постпрандиальные дельты,
  - эффективность корректировок,
  - дрейф в голодном окне,
  - робастная статистика (`MAD`, winsorized mean),
  - оценка вариативности и согласованности сигналов,
  - вероятностная модель решения (Monte Carlo): вероятность пользы, риск гипо, авто-масштабирование шага.
- Safety policy:
  - лимит суточного шага,
  - блокировки по гипо,
  - блокировки по low-confidence,
  - блокировки по вероятностному риску,
  - физиологические границы параметров.
- Полный audit trail в SQLite:
  - профили,
  - анализы,
  - рекомендации,
  - ручные подтверждения.
- REST API, Telegram-бот, scheduler worker.
- Backtest-валидация и weekly quality monitoring.
- Локализация под РФ: форматы дат, mmol/L в отчетах, timezone `Europe/Moscow`.
- Рекомендации сразу в терминах профиля AAPS (`IC`, `ISF`, `Basal`) по временным блокам.

## Архитектура

- `cmd/diatune-safe/main.go` — единый CLI (`api`, `bot`, `worker`, `analyze`, `bootstrap`, `backtest`, `weekstats`)
- `internal/config` — env-конфигурация
- `internal/datasource` — Nightscout/synthetic источники
- `internal/engine` — алгоритмы рекомендаций
- `internal/safety` — guardrails
- `internal/repository` — SQLite persistence/audit
- `internal/service` — orchestration
- `internal/api` — HTTP API
- `internal/telegram` — Telegram bot
- `internal/scheduler` — фоновые периодические запуски

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

## Telegram

Заполните в `.env`:

- `TELEGRAM_BOT_TOKEN`
- `TELEGRAM_ALLOWED_USER_IDS` (через запятую, опционально)

Запуск:

```bash
go run ./cmd/diatune-safe bot
```

Команды:

- `/analyze [patient_id] [days]`
- `/backtest [patient_id] [days]`
- `/weekstats [patient_id] [days]`
- `/latest [patient_id]`
- `/pending [patient_id]`
- `/ack <recommendation_id> [reviewer]`

## Worker

В `.env`:

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
- `TIMEZONE=Europe/Moscow` (чтобы `DAILY_RECOMMENDATION_TIME` работал в нужной зоне)

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

Основные endpoint-ы:

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

## Docker

```bash
docker build -t diatune-safe .
docker run --rm -p 8080:8080 --env-file .env diatune-safe
```

## Публикация в приватный GitHub (с бинарником)

В проекте уже предусмотрен бинарник:

- `release/diatune-safe-linux-amd64`
- `release/diatune-safe-linux-amd64.sha256`
- `release/diatune-safe-linux-amd64.gz`
- `release/diatune-safe-linux-amd64.gz.sha256`

Автопубликация (без локального `git`, через `gh api`):

```bash
GH_TOKEN=... ./scripts/publish_private_repo.sh <owner/repo> "Initial private import with binary"
```

Пример:

```bash
GH_TOKEN=... ./scripts/publish_private_repo.sh myuser/diatune-safe-private
```

## Лицензирование

MIT (`LICENSE`).

## Процессы OSS

- История изменений: `CHANGELOG.md`
- Как контрибьютить: `CONTRIBUTING.md`
- Политика безопасности: `SECURITY.md`
- Кодекс поведения: `CODE_OF_CONDUCT.md`
