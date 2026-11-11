# Testing

Vitest setup. Run via `pnpm test`.

- **Vitest**, `node` environment
- Test files: `src/**/*.{test,spec}.{js,ts}`
- `expect.requireAssertions: true`: every test must contain at least one assertion
- Unit-test stores (`*.svelte.ts` factories) with injected fakes
- No component tests currently: `.svelte` files are not under test
