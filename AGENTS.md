# Repository Guidelines

## Project Structure & Module Organization

Stratum is a Go HTTP service. The executable entry point is `cmd/stratum/main.go`; it exposes `serve` and `version` commands. Put application orchestration and HTTP handlers in `internal/app`, environment-backed settings in `internal/config`, and filesystem helpers in `internal/platform`. Database access belongs in `internal/storage/turso`. Versioned SQL migrations live in `internal/migrations/sql` and are registered in `internal/migrations/migrations.go`. Runtime database files and generated site data belong under `data/`; do not treat them as source assets.

## Build, Test, and Development Commands

- `go run ./cmd/stratum serve` starts the server on `:8080`; check `GET /health`.
- `go run ./cmd/stratum version` prints the development version.
- `go build ./cmd/stratum` compiles the CLI.
- `go test ./...` runs all package tests. Add tests with new behavior even though none exist yet.
- `gofmt -w $(rg --files -g '*.go' cmd internal)` formats Go source before review.
- `go vet ./...` performs standard static checks.

Use `STRATUM_ADDR` to override the listen address and `STRATUM_DATA_DIR` to use an isolated data directory during development or tests, for example: `STRATUM_DATA_DIR=/tmp/stratum-dev go run ./cmd/stratum serve`.

## Coding Style & Naming Conventions

Use idiomatic Go and `gofmt` (tabs for indentation). Keep package names short and lowercase; use `snake_case.go` filenames and exported `PascalCase` identifiers only when another package needs them. Wrap errors with operation context (for example, `fmt.Errorf("open database: %w", err)`) and pass `context.Context` through I/O boundaries. Keep HTTP route registration in `internal/app` and avoid importing `internal` packages from outside this module.

## Testing Guidelines

Place unit tests beside the code they cover as `*_test.go`, using Go's `testing` package and table-driven cases where several inputs share one behavior. Prefer temporary data directories (`t.TempDir()`) over the repository `data/` directory. Cover success and error paths for configuration, migrations, and HTTP handlers; run `go test ./...` and `go vet ./...` before opening a PR.

## Commit & Pull Request Guidelines

Git history is not available in this checkout, so use concise imperative subjects such as `Add health endpoint` or `Handle migration failure`. Keep commits focused. PRs should explain the behavior change, list validation commands, link the relevant issue when present, and include request/response examples or screenshots for user-visible HTTP changes. Call out schema migrations and any changes to environment variables explicitly.
