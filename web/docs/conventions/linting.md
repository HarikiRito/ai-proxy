# Linting

Hybrid Biome / ESLint split. Run via `pnpm lint`.

- **Biome** owns `.ts`, `.js`, `.json`, `.css` - fast, single-binary formatting + linting for everything that isn't Svelte markup
- **ESLint + prettier-plugin-svelte** own `.svelte` files - Biome has no Svelte parser, so ESLint fills that gap and prettier-plugin-svelte handles formatting
- Generated shadcn primitives under `src/components/ui/**` are glob-ignored in both configs; `App*.svelte` wrappers remain linted

## Strict rules in force

- `no-explicit-any` - no `any`; prefer `unknown` / `satisfies`
- `no-floating-promises` - every Promise must be awaited or explicitly handled
- `consistent-type-imports` - `import type` for type-only imports
- `switch-exhaustiveness-check` - switch statements over unions must be exhaustive

## Exception

- `prefer-const` is disabled for `.svelte` files - Svelte's `$bindable()` requires `let` bindings that ESLint's `prefer-const` would otherwise flag as errors

`pnpm lint` (Biome check + ESLint check) is the gate - it must pass before merging.
