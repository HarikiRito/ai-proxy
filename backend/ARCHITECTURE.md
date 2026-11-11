# Architecture

Modular monolith, one binary, split-ready. `cmd/api/main.go` is the composition root: it builds platform pieces, modules and the bus, then wires them.

## Layout

- `internal/platform/` auth (WorkOS JWT verify + owner-check middleware), config, db (ent client on SQLite), httpx (chi + SPA fallback), logging (slog). No domain imports.
- `internal/events/` in-process sync bus: `Publish(ctx, evt)`, `Subscribe(name, handler)`. Handlers run in the publisher's ctx; first error aborts and returns to the publisher.
- `internal/modules/<module>/` domain modules; each `service.go` is the public facade (types + `Service`, built by `New(Deps)`); `store.go` is the unexported ent adapter. Sample: `health`.
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
