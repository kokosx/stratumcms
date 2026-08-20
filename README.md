# StratumCMS

StratumCMS is a self-hosted CMS distributed as one Go binary. It uses `net/http`, server-rendered templates and Datastar, a local Turso/libSQL database, filesystem media, and a disposable filesystem page cache. It needs no external database, Redis, Node runtime, or hosted service.

The MVP provides first-run setup, session-based admin login, administrator/editor/author roles, Pages and Posts, a nested block editor with draft preview and revisions, publishing, themes and Site Styles, image media, menus, redirects, disk-backed page caching, users, health/readiness checks, diagnostics, maintenance, and portable backup/restore.

## Build and run

Requirements: Go version from `go.mod`; `sqlc` is only needed when changing SQL queries.

```sh
go build ./cmd/stratum
STRATUM_DATA_DIR=/srv/stratum STRATUM_ADDR=127.0.0.1:8080 ./stratum serve
```

Open the server URL. A fresh installation redirects to `/setup`, where the first administrator is created.

Configuration is intentionally environment-only:

- `STRATUM_ADDR` — listen address, default `:8080`.
- `STRATUM_DATA_DIR` — writable data directory, default `./data`.
- `STRATUM_SECURE_COOKIES` — boolean; set `true` behind production HTTPS.

Invalid explicit values fail at startup. The data directory contains persistent `stratum.db`, `media/`, and future user-installed `themes/` or `blocks/`. `cache/` and `tmp/` are disposable. `backups/` contains locally generated archives and is excluded from backups.

## Operations

```sh
./stratum doctor
./stratum maintenance
./stratum backup
./stratum backup --output /safe/location/site.tar.gz
STRATUM_DATA_DIR=/empty/restore-target ./stratum restore site.tar.gz
```

Backups contain a consistent database snapshot, persistent user files, and a SHA-256 manifest. Restore validates paths, format, contents, and checksums, refuses a non-empty target, clears disposable state, and invalidates restored sessions. Keep archives private: they contain account and site data.

`GET /health` is cheap liveness. `GET /ready` checks the database, schema, active theme, Site Styles, and required directories.

## Production deployment

Bind Stratum to localhost or a private interface and terminate TLS at a reverse proxy such as Caddy or nginx. Use a persistent writable data directory, set `STRATUM_SECURE_COOKIES=true`, deny web access to the data directory, run `doctor` after deployments, and schedule regular tested backups plus `maintenance`.

Custom CSS is trusted administrator-authored site content. It is served only as `text/css` and is not applied to the admin UI.

## Development

```sh
sqlc generate             # when sqlc is installed and queries change
go mod tidy
gofmt -w $(rg --files -g '*.go')
go test -count=1 ./...
go test -race ./...
go vet ./...
go build ./cmd/stratum
```

The executable is in `cmd/stratum`; HTTP orchestration is in `internal/app`; content/editor/document/block logic is separated from HTTP; presentation composes rendering, themes, styles, and menus; database setup and generated query code live under `internal/storage`; migrations are immutable, ordered files under `internal/migrations/sql`; and the page cache remains rebuildable rather than authoritative.
