# backend

Go GraphQL backend: modular monolith, split-ready. chi, gqlgen, ent + Atlas migrations, SQLite (`modernc.org/sqlite`, no cgo).
Rules and the path to microservices: [ARCHITECTURE.md](ARCHITECTURE.md). Run from this directory; see the root README for start-up.

Routes: `/healthz` (JSON, 503 when the db is unreachable), `/graphql` (health, viewer, providers, proxy keys), `/playground` when `DEBUG=true`, everything else serves the embedded SPA (`200.html` fallback).

## Make targets

| target | does |
| --- | --- |
| `web` | build the SPA into `../web/build` (embedded by `../web/embed.go`) |
| `migrate` | apply Atlas migrations to `$(DB_FILE)` (default `data/app.db`), creating it when missing |
| `run` | `migrate`, then api with air hot reload on :8080 |
| `generate` | ent + gqlgen codegen (generated code is committed) |
| `migrate-diff NAME=x` | write a migration from the ent schema (scratch sqlite replay) |
| `lint` / `fmt` | golangci-lint v2 / gofumpt + goimports |
| `test` | `go test -race` (migration test needs the atlas CLI) |
| `check` | lint, fmt diff, tidy diff, regenerate with no diff, test, govulncheck, build |
| `build` | `bin/api` |
