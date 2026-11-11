<script lang="ts">
	import '#src/app.css';
	import { ModeWatcher } from 'mode-watcher';
	import AppSonner from '#src/components/ui/sonner/AppSonner.svelte';
	import AppThemeToggle from '#src/components/ui/AppThemeToggle.svelte';

	const { children } = $props();
</script>

<ModeWatcher />

<nav
	class="sticky top-0 z-50 border-b bg-background/95 backdrop-blur supports-backdrop-filter:bg-background/60"
>
	<div class="mx-auto flex max-w-4xl items-center justify-between px-4 py-3">
		<a href="/" class="text-sm font-medium tracking-wide uppercase">ai-proxy</a>

		<div class="flex items-center gap-1">
			<AppThemeToggle />
		</div>
	</div>
</nav>

<main class="mx-auto max-w-4xl px-4 py-10">
	{@render children()}
</main>

<AppSonner />
