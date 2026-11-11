# Components

Adding and wrapping shadcn-svelte components. See [styling.md](styling.md) for tokens and `cn()`.

## Adding a component

```bash
pnpm dlx shadcn-svelte@latest add <component-name> --overwrite --yes
```

Generated primitives land in `src/components/ui/<name>/`. They are lint-ignored by glob (`src/components/ui/**`) in both `eslint.config.js` and `biome.json` - do not hand-edit generated files against lint rules meant for app code.

## `App*` wrapper convention

Wrapping a primitive in an `App*` component is **opt-in** - only add one where it earns its keep (shared default props, project-specific styling hooks, etc.). Do not wrap every primitive by default. Currently wrapped:

- `AppSonner.svelte`

```svelte
<!-- src/components/ui/AppBadge.svelte -->
<script lang="ts">
	import { Badge } from 'src/components/ui/badge/index.js';
	import { cn } from '#src/utils.js';
	let { class: className, ...rest } = $props();
</script>

<Badge class={cn(className)} {...rest} />
```

## Icons

Icons come from `@lucide/svelte` - no hand-rolled icon components.

## Installed components

button, sonner, badge, card, dialog, input, label, select, separator, switch, table, textarea. Add more with the command above.
