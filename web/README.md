# web

SvelteKit SPA for ai-proxy (Svelte 5, Tailwind 4, adapter-static). `pnpm build` writes `build/` (with the `200.html` fallback) which the Go api embeds and serves.

pnpm is mandatory (`only-allow`). Pin: `pnpm@12.4.1`, Node `>= 24`.

```sh
pnpm install
pnpm dev        # :5173, proxies /healthz and /graphql to the api on :8080
```

| Script                              | Description                     |
| ----------------------------------- | ------------------------------- |
| `pnpm build`                        | build SPA into `build/`         |
| `pnpm lint`                         | Biome (TS/JS) + ESLint (Svelte) |
| `pnpm format` / `pnpm format:check` | Prettier write / check          |
| `pnpm type:check`                   | svelte-check                    |
| `pnpm test`                         | Vitest                          |

Conventions: [`docs/index.md`](docs/index.md).
