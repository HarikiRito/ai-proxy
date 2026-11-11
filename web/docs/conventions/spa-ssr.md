# SPA only

The app is an SPA, never SSR: the Go api embeds `build/` (`web/embed.go`) and serves it.

- `vite.config.ts` uses `adapter-static` with `fallback: '200.html'`; the api serves `200.html` for unknown page routes (absolute `/_app/` asset paths make deep links work)
- `src/routes/+layout.ts` keeps `ssr = false` and `prerender = true`
- Do not switch to `adapter-node` or enable SSR: it breaks the embed
