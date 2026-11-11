# ai-proxy

Self-hosted OpenAI-compatible AI proxy: one key, many providers, smart routing.

status: bootstrapping

## Layout

- `backend/` Go api (chi, gqlgen, ent, SQLite via pure-Go `modernc.org/sqlite`, Atlas migrations)
- `web/` SvelteKit SPA; its build is embedded into the api binary (`web/embed.go`)
- `sidecar/` placeholder

## Start up

Needs Go (see `backend/go.mod`), Node >= 24 + pnpm, and the [atlas CLI](https://atlasgo.io).

Local, single process (api serves the SPA):

```sh
make -C backend web      # build the SPA into web/build
make -C backend run      # applies migrations (creates data/app.db if missing), then runs the api on :8080
```

Frontend dev with hot reload (proxies `/healthz` and `/graphql` to :8080):

```sh
cd web && pnpm install && pnpm dev    # :5173
```

Docker (empty volume works, no manual steps):

```sh
docker compose up -d --build --wait   # data-init, migrate (atlas), then api on :8080
open http://localhost:8080
docker compose down -v
```

The standalone image (`docker build .`) does not migrate itself: run `atlas migrate apply` against the `/data` volume first (compose does this via the `migrate` service).

## Checks

```sh
make -C backend check                 # lint, fmt, generate, test -race, govulncheck, build
cd web && pnpm lint && pnpm type:check && pnpm test && pnpm format:check
```

Use Go 1.27.4 or newer (`GOTOOLCHAIN=go1.27.4`). More: [backend/README.md](backend/README.md), [backend/ARCHITECTURE.md](backend/ARCHITECTURE.md).
