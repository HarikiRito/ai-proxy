# Styling

Tailwind CSS v4 CSS-first setup and shadcn design tokens. See [components.md](components.md) for component-level usage.

- **CSS-first config** - no `tailwind.config.js`; everything lives in `src/app.css` via `@import "tailwindcss"`
- **shadcn neutral theme** - design tokens (`--background`, `--foreground`, `--primary`, `--muted-foreground`, `--popover`, etc.) sourced verbatim from the shadcn-svelte neutral registry palette
- **Token layering** in `src/app.css`:
  - `@import "tailwindcss"` then `@import "tw-animate-css"`
  - `@custom-variant dark` - enables the `.dark` variant
  - `:root { --radius: 0.625rem; ...tokens }` - light theme values
  - `.dark { ...tokens }` - dark theme overrides
  - `@theme inline { --color-* mappings, --radius-sm/md/lg/xl }` - maps CSS vars to Tailwind utilities
  - `@layer base { * { @apply border-border outline-ring/50; } body { @apply bg-background text-foreground; } }`
- **Dark mode** - toggled via `mode-watcher`, which sets/removes the `.dark` class; no manual class toggling in feature code
- **`cn()`** - import from `#src/utils.js` (not the `cn` package directly) for every conditional/merged Tailwind class list:

```ts
import { cn } from '#src/utils.js';

const classes = cn('px-2 py-1', isActive && 'bg-primary', className);
```

`src/utils.ts` is a thin facade re-exporting `cn` from the official `cn` npm package (which replaced the old local clsx + tailwind-merge helper), alongside bits-ui type re-exports. The facade exists because shadcn-svelte's generated components import `cn` from it - app code should import from the same place for consistency.
