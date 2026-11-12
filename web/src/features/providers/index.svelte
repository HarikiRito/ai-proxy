<script lang="ts">
	import { getContext, onMount } from 'svelte';
	import { Badge } from '#src/components/ui/badge/index.js';
	import { Button } from '#src/components/ui/button/index.js';
	import * as Card from '#src/components/ui/card/index.js';
	import { AUTH_FETCH_KEY } from '#src/features/auth/auth.svelte.js';
	import {
		MODELS_DEV_KEY,
		type createModelsDevStore
	} from '#src/features/modelsdev/modelsdev.svelte.js';
	import KeyForm from './key-form.svelte';
	import ProviderForm from './provider-form.svelte';
	import {
		createProvidersStore,
		type AuthFetch,
		type Provider,
		type ProviderKey
	} from './providers.svelte';

	const store = createProvidersStore(getContext<AuthFetch>(AUTH_FETCH_KEY));

	const modelsDev = getContext<ReturnType<typeof createModelsDevStore>>(MODELS_DEV_KEY);

	let formOpen = $state(false);
	let keyTarget = $state<{ provider: Provider; key: ProviderKey | null } | null>(null);

	onMount(() => {
		void store.load();
		void modelsDev.load();
	});

	function openCreate() {
		store.resetDraft();
		formOpen = true;
	}

	function openEdit(provider: Provider) {
		store.startEdit(provider);
		formOpen = true;
	}

	function openKeyForm(provider: Provider, key: ProviderKey | null) {
		store.clearError();
		keyTarget = { provider, key };
	}

	function remove(message: string, run: () => Promise<boolean>) {
		if (confirm(message)) void run();
	}
</script>

<div class="flex flex-col gap-6">
	<div class="flex items-center justify-between">
		<h1 class="text-2xl font-bold tracking-tight">Providers</h1>
		<Button onclick={openCreate}>Add provider</Button>
	</div>

	{#if store.error && !formOpen && !keyTarget}
		<p class="text-sm text-destructive" data-testid="providers-error">{store.error}</p>
	{/if}

	{#each store.providers as provider (provider.id)}
		<Card.Root>
			<Card.Header>
				<Card.Title class="flex items-center gap-2">
					{provider.name}
					<Badge variant={provider.enabled ? 'default' : 'secondary'}>
						{provider.enabled ? 'on' : 'off'}
					</Badge>
				</Card.Title>
				<Card.Description>
					{provider.kind} · {provider.baseUrl} · affinity {provider.cacheAffinitySeconds}s ·
					capacity {provider.capacity}
					· {provider.models.length} models
				</Card.Description>
			</Card.Header>
			<Card.Content class="flex flex-col gap-2">
				{#if provider.kind === 'claude_subscription'}
					<p class="text-sm text-muted-foreground">Accounts managed in a later release.</p>
				{:else}
					{#each provider.keys as key (key.id)}
						<div class="flex items-center gap-3 text-sm">
							<span class="font-medium">{key.name}</span>
							<code class="text-muted-foreground">{key.maskedKey}</code>
							<span>max {key.maxInFlight}</span>
							<span>weight {key.weight}</span>
							<Badge variant={key.enabled ? 'default' : 'secondary'}>
								{key.enabled ? 'on' : 'off'}
							</Badge>
							<span class="ml-auto"></span>
							<Button variant="outline" size="sm" onclick={() => openKeyForm(provider, key)}>
								Edit
							</Button>
							<Button
								variant="outline"
								size="sm"
								onclick={() => void store.updateKey(key.id, { enabled: !key.enabled })}
							>
								{key.enabled ? 'Disable' : 'Enable'}
							</Button>
							<Button
								variant="destructive"
								size="sm"
								onclick={() => remove(`Delete key ${key.name}?`, () => store.deleteKey(key.id))}
							>
								Delete
							</Button>
						</div>
					{:else}
						<p class="text-sm text-muted-foreground">No keys yet.</p>
					{/each}
				{/if}
			</Card.Content>
			<Card.Footer class="gap-2">
				{#if provider.kind !== 'claude_subscription'}
					<Button variant="outline" size="sm" onclick={() => openKeyForm(provider, null)}
						>Add key</Button
					>
				{/if}
				<Button variant="outline" size="sm" onclick={() => openEdit(provider)}>Edit</Button>
				<Button
					variant="destructive"
					size="sm"
					onclick={() =>
						remove(`Delete provider ${provider.name} and its keys?`, () =>
							store.deleteProvider(provider.id)
						)}
				>
					Delete
				</Button>
			</Card.Footer>
		</Card.Root>
	{:else}
		{#if !store.loading}
			<p class="text-sm text-muted-foreground">No providers yet.</p>
		{/if}
	{/each}
</div>

<ProviderForm {store} {modelsDev} bind:open={formOpen} />
<KeyForm {store} target={keyTarget} onclose={() => (keyTarget = null)} />
