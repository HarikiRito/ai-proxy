<script lang="ts">
	import { Toaster } from '#src/components/ui/sonner/index.js';
	import type { ComponentProps } from 'svelte';

	const { ...rest }: ComponentProps<typeof Toaster> = $props();
</script>

<Toaster {...rest} />
