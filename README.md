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

Frontend dev with hot reload (proxies `/healthz`, `/config.json` and `/graphql` to :8080):

```sh
cd web && pnpm install && pnpm dev    # :5173
```

Docker (empty volume works, no manual steps):

```sh
docker compose up -d --build --wait   # data-init, migrate (atlas), then api on :8080
docker compose exec api wget -qO- http://localhost:8080/healthz
docker compose down -v
```

The standalone image (`docker build .`) does not migrate itself: run `atlas migrate apply` against the `/data` volume first (compose does this via the `migrate` service).

## Admin login (WorkOS AuthKit, owner only)

`/graphql` requires a WorkOS access token whose user is the owner. `/healthz`, `/config.json` and the SPA stay public; `/v1/*` is the OpenAI-compatible API behind proxy keys (`POST /v1/chat/completions` and `GET /v1/models` are live, including SSE streaming). The api refuses to start without any variable in the table below except `WORKOS_DEV_MODE`.

| Env | Meaning |
| --- | --- |
| `WORKOS_CLIENT_ID` | WorkOS client id (`client_...`); also selects the JWKS (`https://api.workos.com/sso/jwks/<id>`) |
| `WORKOS_API_KEY` | WorkOS secret key (`sk_...`), used server side to look up the token's user; never sent to the browser |
| `WORKOS_ISSUER` | exact `iss` of the access tokens (copy from a decoded token or the dashboard; typically `https://api.workos.com/user_management/<client_id>`, or your custom auth domain) |
| `MASTER_KEY` | base64 of 32 random bytes (`openssl rand -base64 32`); encrypts provider API keys at rest. Losing or changing it makes saved keys unreadable |
| `OWNER_EMAIL` | the only email allowed in; must be verified in WorkOS (case-insensitive match) |
| `WORKOS_DEV_MODE` | `true` keeps the refresh token in localStorage. Needed on local `http` and on staging: the WorkOS-hosted auth domain is cross-site, so cookie mode breaks on reload. Use `false` only once a same-site custom auth domain exists; default `false` |

Local (`http://localhost:5173`, set in `backend/.env`) and staging (`https://ai-proxy.harikidev.win`, set in Dokploy env), both with `WORKOS_DEV_MODE=true`, use the same vars; only the WorkOS dashboard URIs differ.

WorkOS dashboard (per origin; add the `http://localhost:5173` ones for local dev too):

- Redirect URI: `<origin>/auth/callback`
- Initiate login URL: `<origin>/auth/login`
- Sign-out redirect: `<origin>/`
- CORS (allowed web origin): `<origin>`
- Sign-up disabled; invite the owner account only.

## Deployment

- URL: https://ai-proxy.harikidev.win (`/healthz` for status)
- Dokploy compose app `ai-proxy`, built from `docker-compose.yml` on `main`
- Every push to `main` auto-deploys via Dokploy
- Dokploy env must define `WORKOS_CLIENT_ID`, `WORKOS_API_KEY`, `WORKOS_ISSUER`, `OWNER_EMAIL`, `MASTER_KEY`, `WORKOS_DEV_MODE=true` (see Admin login; back up `MASTER_KEY`)

## Checks

```sh
make -C backend check                 # lint, fmt, generate, test -race, govulncheck, build
cd web && pnpm lint && pnpm type:check && pnpm test && pnpm format:check
```

Use Go 1.27.4 or newer (`GOTOOLCHAIN=go1.27.4`). More: [backend/README.md](backend/README.md), [backend/ARCHITECTURE.md](backend/ARCHITECTURE.md).
