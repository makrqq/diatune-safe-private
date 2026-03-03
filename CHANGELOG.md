# Changelog

All notable changes to this project are documented in this file.

## 0.0.3 - 2026-03-03

- Telegram output reformatted to be human-readable:
  - compact sections (summary, warnings, recommendations, blocked reasons),
  - multiline recommendation cards instead of one long line,
  - top-5 focus for open recommendations in report view.
- Added safe Telegram message chunking to avoid oversized unreadable payloads.
- Added repository-level versioning:
  - `VERSION` file in root,
  - `internal/version` package,
  - `version` CLI command and `/version` bot command.
- API `GET /healthz` now returns service version in `version` field.

## 0.0.2 - 2026-03-03

- Reworked recommendation behavior for manual-review workflow:
  - softer safety/probability gates,
  - still strict `suggest-only` mode with no automatic profile changes.
- Added explicit AAPS-oriented recommendation lines per time block (`IC`, `ISF`, `Basal`).
- Added RU-focused formatting defaults:
  - timezone `Europe/Moscow`,
  - report wording in Russian,
  - mmol/L display in Telegram reports.
- Added historical analytics features:
  - `backtest` report,
  - weekly stats report and worker schedule for weekly summary.
- Added API endpoints:
  - `GET /v1/patients/{patient_id}/backtest`
  - `GET /v1/patients/{patient_id}/weekly-stats`
- Added project hygiene for publishing:
  - excludes temporary/local files from repository upload,
  - cleaned accidental temp artifacts from repo.
