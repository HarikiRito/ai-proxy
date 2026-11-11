# State

Svelte 5 runes. See [error-handling.md](error-handling.md) for how failures are reported.

- State uses Svelte 5 runes: `$state` for mutable values, `$derived` for computed values
- A feature with state exposes a `create<Name>Store()` factory in `<name>.svelte.ts`; the feature component creates it once
- The store takes its I/O (e.g. `fetch`) as an argument with a real default, so tests inject a fake
- Expose state through getters; mutate only inside store methods
