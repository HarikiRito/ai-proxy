<script lang="ts">
	import { onMount } from 'svelte';
	import { createAuthStore } from './auth.svelte';

	const auth = createAuthStore();

	onMount(() => {
		void auth.init().then(() => auth.signIn());
	});
</script>

<p class="text-sm text-muted-foreground">
	{auth.status === 'error'
		? 'Sign-in is unavailable. Try again later.'
		: 'Redirecting to sign in...'}
</p>
