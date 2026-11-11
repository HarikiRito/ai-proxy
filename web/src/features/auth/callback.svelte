<script lang="ts">
	import { onMount } from 'svelte';
	import { createAuthStore } from './auth.svelte';

	const auth = createAuthStore();

	onMount(() => {
		void auth.init();
	});
</script>

<p class="text-sm text-muted-foreground">
	{auth.status === 'error' ? 'Sign-in failed. Try again.' : 'Signing you in...'}
</p>
