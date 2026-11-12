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
	import { accountStatus, tokenInfo } from './accounts';
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
		store.startLive();
		return store.stopLive;
	});

	const MODE_LABELS = {
		API_KEY: 'API key',
		SETUP_TOKEN: 'Setup token',
		LOGIN_DIR: 'Login directory'
	};
	const TOKEN_LEVEL = { ok: 'secondary', warning: 'outline', expired: 'destructive' } as const;

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
		{@const account = provider.kind === 'claude_subscription'}
		{@const noun = account ? 'account' : 'key'}
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
				{#if account && store.liveError}
					<p class="text-sm text-destructive">{store.liveError}</p>
				{/if}
				{#each provider.keys as key (key.id)}
					{@const status = accountStatus(
						key,
						store.live.find((l) => l.keyId === key.id),
						Date.now(),
						provider.enabled
					)}
					{@const token = tokenInfo(key, Date.now())}
					<div class="flex flex-wrap items-center gap-3 text-sm">
						<span class="font-medium">{key.name}</span>
						{#if account}
							<Badge variant="outline">{MODE_LABELS[key.authMode]}</Badge>
						{/if}
						<code class="text-muted-foreground">{key.maskedKey}</code>
						{#if account}
							<Badge variant={status.variant}>{status.label}</Badge>
							{#if token}
								<span class="text-muted-foreground">Token age {token.ageDays} days</span>
								<Badge
									variant={TOKEN_LEVEL[token.level]}
									class={token.level === 'warning' ? 'text-amber-600' : ''}
								>
									{token.level === 'expired' ? 'Expired' : `Expires in ${token.daysLeft} days`}
								</Badge>
							{/if}
						{/if}
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
							onclick={() => remove(`Delete ${noun} ${key.name}?`, () => store.deleteKey(key.id))}
						>
							Delete
						</Button>
					</div>
				{:else}
					<p class="text-sm text-muted-foreground">No {noun}s yet.</p>
				{/each}
			</Card.Content>
			<Card.Footer class="gap-2">
				<Button variant="outline" size="sm" onclick={() => openKeyForm(provider, null)}>
					Add {noun}
				</Button>
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
