# Architecture

Modular monolith, one binary, split-ready. `cmd/api/main.go` is the composition root: it builds platform pieces, modules and the bus, then wires them.

## Layout

- `internal/platform/` auth (WorkOS JWT verify + owner-check middleware), config, db (ent client on SQLite), httpx (chi + SPA fallback), logging (slog). No domain imports. `crypto` = AES-256-GCM `Cipher` keyed by `MASTER_KEY`.
- `internal/events/` in-process sync bus: `Publish(ctx, evt)`, `Subscribe(name, handler)`. Handlers run in the publisher's ctx; first error aborts and returns to the publisher.
- `internal/modules/<module>/` domain modules; each `service.go` is the public facade (types + `Service`, built by `New(Deps)`); `store.go` is the unexported ent adapter. Modules: `health` (sample), `providers` (upstream providers + AES-256-GCM sealed API keys; tables `providers_*`, facade never returns secrets), `proxykeys` (client proxy keys, sha256-hashed, shown once at creation; table `proxykeys_keys`), `virtualmodels` (client-facing model names, kind chat/transcription, priority tiers of provider entries with weights, strategy, cache-affinity; tables `virtualmodels_*`; reads providers through a consumer-defined `ProviderDirectory` adapted in `cmd/api`, entry status computed on read; writes replace the whole tier tree in one transaction), `routing` (no tables; engine that turns a virtual model into leased provider-key targets: tiers, strategies, in-memory per-key leases and 429 cooldowns, failover before the first byte, queue wait; depends on facades through consumer-defined interfaces; `Watch` frees leaked leases), `settings` (singleton runtime limits, defaults when no row, `Get` cached until `Update`; table `settings_settings`).
- `internal/gateway/` OpenAI-compatible `/v1` HTTP (proxy-key auth middleware, `GET /models`, `POST /chat/completions`, `POST /audio/transcriptions`, native Anthropic `POST /messages` with Anthropic-format errors); calls facades and the routing engine only, never ent.
- `internal/graph/` gqlgen, follow-schema layout, `schema/<module>.gql`, flat resolvers that call facades only. Add a dataloader package when cross-module reads appear.
- `internal/ent/` one schema package, one `*ent.Client`, schema files and tables prefixed per module. Atlas versioned migrations in `backend/migrations`, authored by `cmd/migrate`, applied with the atlas CLI. Only `cmd/api` imports `web` (the embedded SPA). Generated code is committed.

## Rules (enforced by depguard where possible)

- Modules (all under `internal/modules/`) call each other only through a facade or a consumer-defined interface. Never import another module's store or its `internal/ent/<entity>` package.
- Each module owns its tables. Edges and FKs exist only inside a module; cross-module references are a plain indexed ID column with no ent edge.
- "When X happens, Y reacts" is an event. "I need X's data" is a facade batch call (`ByIDs`), loaded through a dataloader in the graph layer.
- Platform never imports a domain module. Resolvers never import ent.
- New module: create `internal/modules/<module>/`, then add its entity package to the other modules' depguard deny lists in `.golangci.yml`.

## Split path

1. Module to `cmd/<svc>`: new composition root with only that module, its tables and its migrations.
2. Facade to client: implement the same `Service` interface as a gRPC/federation client; callers and dataloaders do not change.
3. Bus to outbox + asynq: replace the `events.Bus` implementation, writing events in the publisher's transaction and delivering asynchronously. Handlers must already be idempotent.
