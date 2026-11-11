<script lang="ts">
	import { onMount, setContext, type Snippet } from 'svelte';
	import { Button } from '#src/components/ui/button/index.js';
	import { AUTH_FETCH_KEY, createAuthStore } from './auth.svelte';

	const { children }: { children: Snippet } = $props();

	const auth = createAuthStore();
	setContext(AUTH_FETCH_KEY, auth.authFetch);

	onMount(() => {
		void auth.init();
	});
</script>

{#if auth.status === 'loading'}
	<p class="text-sm text-muted-foreground">Loading...</p>
{:else if auth.status === 'signedIn'}
	<div class="flex flex-col gap-8">
		<div class="flex items-center gap-3 text-sm">
			<nav class="flex gap-4">
				<a href="/" class="hover:underline">Home</a>
				<a href="/providers" class="hover:underline">Providers</a>
				<a href="/proxy-keys" class="hover:underline">Proxy keys</a>
				<a href="/virtual-models" class="hover:underline">Virtual models</a>
			</nav>
			<span class="ml-auto"></span>
			<span data-testid="viewer-email" class="text-muted-foreground">{auth.email}</span>
			<Button variant="outline" size="sm" onclick={() => auth.signOut()}>Sign out</Button>
		</div>
		{@render children()}
	</div>
{:else if auth.status === 'refused'}
	<div class="flex flex-col items-start gap-4">
		<h1 class="text-2xl font-bold tracking-tight">Access denied</h1>
		<p class="max-w-xl text-muted-foreground">
			This account is not the owner of this ai-proxy instance. Sign out and use the owner account.
		</p>
		<Button variant="outline" onclick={() => auth.signOut()}>Sign out</Button>
	</div>
{:else if auth.status === 'error'}
	<div class="flex flex-col items-start gap-4">
		<h1 class="text-2xl font-bold tracking-tight">Something went wrong</h1>
		<p class="max-w-xl text-muted-foreground">
			Could not verify your session. Reload the page or try again later.
		</p>
	</div>
{:else}
	<div class="flex flex-col items-start gap-4">
		<h1 class="text-2xl font-bold tracking-tight">ai-proxy admin</h1>
		<p class="max-w-xl text-muted-foreground">
			This is a private admin console. Sign in with the owner account to continue.
		</p>
		<Button onclick={() => void auth.signIn()}>Sign in</Button>
	</div>
{/if}
