<script lang="ts">
	import { onMount } from 'svelte';
	import { cn } from '#src/utils.js';
	import { createHealthStore } from './health.svelte';

	const health = createHealthStore();

	onMount(() => {
		void health.check();
	});

	const dot = {
		checking: 'bg-muted-foreground',
		healthy: 'bg-green-500',
		unhealthy: 'bg-red-500'
	} as const;
</script>

<div class="flex flex-col gap-4">
	<h1 class="text-4xl font-bold tracking-tight">ai-proxy</h1>
	<p class="max-w-2xl text-lg text-balance text-muted-foreground">
		Self-hosted OpenAI-compatible AI proxy: one key, many providers, smart routing.
	</p>
	<div class="flex items-center gap-2 text-sm" data-testid="health-indicator">
		<span class={cn('size-2.5 rounded-full', dot[health.status])}></span>
		<span>Backend: {health.status}</span>
	</div>
</div>
