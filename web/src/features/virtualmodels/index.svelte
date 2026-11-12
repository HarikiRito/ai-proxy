<script lang="ts">
	import { getContext, onMount } from 'svelte';
	import { Badge } from '#src/components/ui/badge/index.js';
	import { Button } from '#src/components/ui/button/index.js';
	import * as Card from '#src/components/ui/card/index.js';
	import { AUTH_FETCH_KEY } from '#src/features/auth/auth.svelte.js';
	import ModelForm from './model-form.svelte';
	import {
		createVirtualModelsStore,
		KIND_LABELS,
		STATUS_REASONS,
		STRATEGY_LABELS,
		type AuthFetch,
		type VirtualModel
	} from './virtualmodels.svelte';

	const store = createVirtualModelsStore(getContext<AuthFetch>(AUTH_FETCH_KEY));

	let formOpen = $state(false);

	onMount(() => {
		void store.load();
	});

	function openCreate() {
		store.startCreate();
		formOpen = true;
	}

	function openEdit(model: VirtualModel) {
		store.startEdit(model);
		formOpen = true;
	}

	function remove(model: VirtualModel) {
		if (confirm(`Delete virtual model ${model.name}?`)) void store.remove(model.id);
	}
</script>

<div class="flex flex-col gap-6">
	<div class="flex items-center justify-between">
		<h1 class="text-2xl font-bold tracking-tight">Virtual models</h1>
		<Button onclick={openCreate}>Add virtual model</Button>
	</div>

	{#if store.error && !formOpen}
		<p class="text-sm text-destructive" data-testid="virtualmodels-error">{store.error}</p>
	{/if}

	{#each store.models as model (model.id)}
		<Card.Root>
			<Card.Header>
				<Card.Title class="flex items-center gap-2">
					{model.name}
					<Badge variant="secondary">{KIND_LABELS[model.kind]}</Badge>
				</Card.Title>
				<Card.Description>
					{STRATEGY_LABELS[model.strategy]} · cache affinity {model.cacheAffinity ? 'on' : 'off'}
				</Card.Description>
			</Card.Header>
			<Card.Content class="flex flex-col gap-3">
				{#each model.tiers as tier (tier.priority)}
					<div class="flex flex-col gap-1">
						<span class="text-xs font-medium text-muted-foreground uppercase">
							Tier {tier.priority + 1}
						</span>
						{#each tier.entries as entry (entry.id)}
							<div class="flex items-center gap-3 text-sm">
								<span class="font-medium">{entry.providerName || 'Unknown provider'}</span>
								<code class="text-muted-foreground">{entry.model}</code>
								<span>weight {entry.weight}</span>
								{#if entry.status !== 'ok'}
									<Badge variant="destructive" title={STATUS_REASONS[entry.status]}>
										{STATUS_REASONS[entry.status]}
									</Badge>
								{/if}
							</div>
						{/each}
					</div>
				{/each}
			</Card.Content>
			<Card.Footer class="gap-2">
				<Button variant="outline" size="sm" onclick={() => openEdit(model)}>Edit</Button>
				<Button variant="destructive" size="sm" onclick={() => remove(model)}>Delete</Button>
			</Card.Footer>
		</Card.Root>
	{:else}
		{#if !store.loading}
			<p class="text-sm text-muted-foreground">No virtual models yet.</p>
		{/if}
	{/each}
</div>

<ModelForm {store} bind:open={formOpen} />
