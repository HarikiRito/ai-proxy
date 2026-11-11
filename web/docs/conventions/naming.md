# Naming

File and folder casing rules. See [structure.md](structure.md) for where each type lives.

- **PascalCase** - component files (`AppButton.svelte`, `AppThemeToggle.svelte`)
- **camelCase** - non-component files (`health.svelte.ts`)
- **kebab-case** - route folders (`dropdown-menu/`)
- **Exception: `index.svelte`** - a feature's root implementation component is always named `index.svelte`, lower-case, not PascalCase. This stays consistent with the barrel-file `index.ts` convention already used for shadcn primitives (e.g. `src/components/ui/*/index.ts`), and lets a route delegate with a plain `import Feature from 'src/features/<name>/index.svelte'`.
