# Contributing

Thanks for considering a contribution.

## Scope

- This project is `suggest-only`: no automatic therapy changes.
- Safety-first behavior is mandatory for all algorithm changes.
- Any risky behavior must be behind explicit configuration and documented.

## Development Setup

1. Install Go `1.24+`.
2. Copy env template:
   - `cp .env.example .env`
3. Run tests:
   - `go test ./...`
4. Optional local run:
   - `go run ./cmd/diatune-safe api`
   - `go run ./cmd/diatune-safe bot`
   - `go run ./cmd/diatune-safe worker`

## Pull Requests

- Keep PRs focused and small.
- Include test coverage for behavior changes.
- Update `README.md` and `CHANGELOG.md` when user-facing behavior changes.
- Do not commit secrets, tokens, real patient identifiers, or local history files.
