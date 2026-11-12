<script lang="ts">
	import { getContext, onMount } from 'svelte';
	import { Badge } from '#src/components/ui/badge/index.js';
	import { Button } from '#src/components/ui/button/index.js';
	import { Label } from '#src/components/ui/label/index.js';
	import * as Select from '#src/components/ui/select/index.js';
	import * as Table from '#src/components/ui/table/index.js';
	import { AUTH_FETCH_KEY } from '#src/features/auth/auth.svelte.js';
	import {
		createUsageStore,
		GROUP_LABELS,
		GROUPS,
		keyState,
		PAGE_SIZE,
		statusVariant,
		tokenText,
		type AuthFetch
	} from './usage.svelte';

	const store = createUsageStore(getContext<AuthFetch>(AUTH_FETCH_KEY));

	onMount(() => {
		void store.load();
		store.startLive();
		return store.stopLive;
	});

	const ALL = 'all';
	const proxyKeyLabel = $derived(
		store.proxyKeys.keys.find((k) => k.id === store.filter.proxyKeyId)?.name ?? 'All'
	);
	const from = $derived(store.page * PAGE_SIZE + 1);
	const to = $derived(Math.min((store.page + 1) * PAGE_SIZE, store.total));
	const affinityVariant = { hit: 'default', break: 'destructive', new: 'secondary' } as const;
	const formatTime = (iso: string) => new Date(iso).toLocaleString();
</script>

<div class="flex flex-col gap-8">
	<h1 class="text-2xl font-bold tracking-tight">Usage</h1>

	<section class="flex flex-col gap-3" data-testid="usage-live">
		<h2 class="text-lg font-semibold">Live status</h2>
		{#if store.errors.live}
			<p class="text-sm text-destructive">{store.errors.live}</p>
		{/if}
		<Table.Root>
			<Table.Header>
				<Table.Row>
					<Table.Head>Provider</Table.Head>
					<Table.Head>Key</Table.Head>
					<Table.Head>State</Table.Head>
					<Table.Head>In flight / max</Table.Head>
					<Table.Head>Free slots</Table.Head>
				</Table.Row>
			</Table.Header>
			<Table.Body>
				{#each store.live as key (key.keyId)}
					{@const state = keyState(key)}
					<Table.Row>
						<Table.Cell class="font-medium">{key.providerName}</Table.Cell>
						<Table.Cell>{key.keyName}</Table.Cell>
						<Table.Cell><Badge variant={state.variant}>{state.label}</Badge></Table.Cell>
						<Table.Cell>{key.inFlight} / {key.maxInFlight}</Table.Cell>
						<Table.Cell>{key.free}</Table.Cell>
					</Table.Row>
				{:else}
					{#if !store.loading.live}
						<Table.Row>
							<Table.Cell colspan={5} class="text-muted-foreground"
								>No provider keys yet.</Table.Cell
							>
						</Table.Row>
					{/if}
				{/each}
			</Table.Body>
		</Table.Root>
	</section>

	<div class="flex flex-wrap items-end gap-4">
		<div class="flex flex-col gap-2">
			<Label>Proxy key</Label>
			<Select.Root
				type="single"
				value={store.filter.proxyKeyId || ALL}
				onValueChange={(v) => void store.setFilter({ proxyKeyId: v === ALL ? '' : v })}
			>
				<Select.Trigger class="w-48">{proxyKeyLabel}</Select.Trigger>
				<Select.Content>
					<Select.Item value={ALL} label="All">All</Select.Item>
					{#each store.proxyKeys.keys as key (key.id)}
						<Select.Item value={key.id} label={key.name}>{key.name}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		</div>
		<div class="flex flex-col gap-2">
			<Label>Virtual model</Label>
			<Select.Root
				type="single"
				value={store.filter.virtualModel || ALL}
				onValueChange={(v) => void store.setFilter({ virtualModel: v === ALL ? '' : v })}
			>
				<Select.Trigger class="w-48">{store.filter.virtualModel || 'All'}</Select.Trigger>
				<Select.Content>
					<Select.Item value={ALL} label="All">All</Select.Item>
					{#each store.virtualModels.models as model (model.id)}
						<Select.Item value={model.name} label={model.name}>{model.name}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		</div>
	</div>

	<section class="flex flex-col gap-3" data-testid="usage-totals">
		<div class="flex items-center gap-4">
			<h2 class="text-lg font-semibold">Totals</h2>
			<div class="flex gap-2">
				{#each GROUPS as group (group)}
					<Button
						variant={store.groupBy === group ? 'default' : 'outline'}
						size="sm"
						onclick={() => void store.setGroupBy(group)}>{GROUP_LABELS[group]}</Button
					>
				{/each}
			</div>
		</div>
		{#if store.errors.totals}
			<p class="text-sm text-destructive">{store.errors.totals}</p>
		{/if}
		<Table.Root>
			<Table.Header>
				<Table.Row>
					<Table.Head>{GROUP_LABELS[store.groupBy]}</Table.Head>
					<Table.Head>Requests</Table.Head>
					<Table.Head>Errors</Table.Head>
					<Table.Head>Input tokens</Table.Head>
					<Table.Head>Output tokens</Table.Head>
					<Table.Head>Cache read</Table.Head>
					<Table.Head>Cache creation</Table.Head>
				</Table.Row>
			</Table.Header>
			<Table.Body>
				{#each store.totals as row (row.label)}
					<Table.Row>
						<Table.Cell class="font-medium">{row.label}</Table.Cell>
						<Table.Cell>{row.requests}</Table.Cell>
						<Table.Cell>{row.errors}</Table.Cell>
						<Table.Cell>{row.inputTokens}</Table.Cell>
						<Table.Cell>{row.outputTokens}</Table.Cell>
						<Table.Cell>{row.cacheReadTokens}</Table.Cell>
						<Table.Cell>{row.cacheCreationTokens}</Table.Cell>
					</Table.Row>
				{:else}
					{#if !store.loading.totals}
						<Table.Row>
							<Table.Cell colspan={7} class="text-muted-foreground">No usage yet.</Table.Cell>
						</Table.Row>
					{/if}
				{/each}
			</Table.Body>
		</Table.Root>
	</section>

	<section class="flex flex-col gap-3" data-testid="usage-records">
		<h2 class="text-lg font-semibold">Records</h2>
		{#if store.errors.records}
			<p class="text-sm text-destructive" data-testid="usage-error">{store.errors.records}</p>
		{/if}
		<Table.Root>
			<Table.Header>
				<Table.Row>
					<Table.Head>Time</Table.Head>
					<Table.Head>Proxy key</Table.Head>
					<Table.Head>Virtual model</Table.Head>
					<Table.Head>Provider / key</Table.Head>
					<Table.Head>Model</Table.Head>
					<Table.Head>Status</Table.Head>
					<Table.Head>Duration</Table.Head>
					<Table.Head>In</Table.Head>
					<Table.Head>Out</Table.Head>
					<Table.Head>Cache read</Table.Head>
					<Table.Head>Cache create</Table.Head>
					<Table.Head>Steps</Table.Head>
					<Table.Head>Affinity</Table.Head>
				</Table.Row>
			</Table.Header>
			<Table.Body>
				{#each store.records as r (r.id)}
					<Table.Row>
						<Table.Cell>{formatTime(r.createdAt)}</Table.Cell>
						<Table.Cell>{r.proxyKeyName}</Table.Cell>
						<Table.Cell>{r.virtualModel}</Table.Cell>
						<Table.Cell
							>{r.providerName ? `${r.providerName} / ${r.keyName ?? '-'}` : '-'}</Table.Cell
						>
						<Table.Cell>{r.model ?? '-'}</Table.Cell>
						<Table.Cell><Badge variant={statusVariant(r.status)}>{r.status}</Badge></Table.Cell>
						<Table.Cell>{r.durationMs} ms</Table.Cell>
						<Table.Cell>{tokenText(r.inputTokens)}</Table.Cell>
						<Table.Cell>{tokenText(r.outputTokens)}</Table.Cell>
						<Table.Cell>{tokenText(r.cacheReadTokens)}</Table.Cell>
						<Table.Cell>{tokenText(r.cacheCreationTokens)}</Table.Cell>
						<Table.Cell class={r.steps > 1 ? 'font-bold text-destructive' : ''}
							>{r.steps}</Table.Cell
						>
						<Table.Cell>
							{#if r.affinity}
								<Badge
									variant={affinityVariant[r.affinity as keyof typeof affinityVariant] ?? 'outline'}
									>{r.affinity}</Badge
								>
							{:else}
								-
							{/if}
						</Table.Cell>
					</Table.Row>
				{:else}
					{#if !store.loading.records}
						<Table.Row>
							<Table.Cell colspan={13} class="text-muted-foreground">No usage records.</Table.Cell>
						</Table.Row>
					{/if}
				{/each}
			</Table.Body>
		</Table.Root>
		<div class="flex items-center justify-end gap-3 text-sm">
			<span class="text-muted-foreground" data-testid="usage-range"
				>{store.total ? `${from}-${to}` : '0'} of {store.total}</span
			>
			<Button
				variant="outline"
				size="sm"
				disabled={!store.hasPrev}
				onclick={() => void store.setPage(store.page - 1)}>Prev</Button
			>
			<Button
				variant="outline"
				size="sm"
				disabled={!store.hasNext}
				onclick={() => void store.setPage(store.page + 1)}>Next</Button
			>
		</div>
	</section>
</div>
