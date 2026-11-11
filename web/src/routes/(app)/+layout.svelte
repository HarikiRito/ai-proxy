<script lang="ts">
	import AuthGate from '#src/features/auth/index.svelte';

	const { children } = $props();
</script>

<AuthGate>
	{@render children()}
</AuthGate>
