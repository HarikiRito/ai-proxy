# Error handling

- No `try/catch` in `src/`; convert failures to values with `.then(...).catch(...)` at the I/O boundary
- Store methods never reject: a failed request becomes a state value (see `features/home/health.svelte.ts`)
- No floating promises: `await` them or mark with `void` (enforced by lint)
