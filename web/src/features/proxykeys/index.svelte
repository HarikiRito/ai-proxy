<script lang="ts">
	import { getContext, onMount } from 'svelte';
	import { Badge } from '#src/components/ui/badge/index.js';
	import { Button } from '#src/components/ui/button/index.js';
	import * as Dialog from '#src/components/ui/dialog/index.js';
	import { Input } from '#src/components/ui/input/index.js';
	import { Label } from '#src/components/ui/label/index.js';
	import * as Table from '#src/components/ui/table/index.js';
	import { Textarea } from '#src/components/ui/textarea/index.js';
	import { AUTH_FETCH_KEY } from '#src/features/auth/auth.svelte.js';
	import {
		createProxyKeysStore,
		parseModels,
		type AuthFetch,
		type ProxyKey
	} from './proxykeys.svelte';

	const store = createProxyKeysStore(getContext<AuthFetch>(AUTH_FETCH_KEY));

	let name = $state('');
	let modelsText = $state('');
	let editTarget = $state<ProxyKey | null>(null);
	let editText = $state('');

	onMount(() => {
		void store.load();
	});

	function create(event: SubmitEvent) {
		event.preventDefault();
		void store.create(name, parseModels(modelsText)).then((ok) => {
			if (!ok) return;
			name = '';
			modelsText = '';
		});
	}

	function openEdit(key: ProxyKey) {
		store.clearError();
		editTarget = key;
		editText = key.allowedModels.join('\n');
	}

	function saveEdit(event: SubmitEvent) {
		event.preventDefault();
		if (!editTarget) return;
		void store.setAllowedModels(editTarget.id, parseModels(editText)).then((ok) => {
			if (ok) editTarget = null;
		});
	}

	function revoke(key: ProxyKey) {
		if (confirm(`Revoke key ${key.name}? Clients using it stop working immediately.`)) {
			void store.revoke(key.id);
		}
	}

	const formatTime = (iso: string | null) => (iso ? new Date(iso).toLocaleString() : 'never');
</script>

<div class="flex flex-col gap-6">
	<h1 class="text-2xl font-bold tracking-tight">Proxy keys</h1>

	<form class="flex flex-col gap-4" onsubmit={create}>
		<div class="flex flex-col gap-2">
			<Label for="pk-name">Name</Label>
			<Input id="pk-name" bind:value={name} />
		</div>
		<div class="flex flex-col gap-2">
			<Label for="pk-models">Allowed models (optional)</Label>
			<Textarea
				id="pk-models"
				placeholder="One per line or comma separated; empty allows all models"
				bind:value={modelsText}
			/>
		</div>
		<Button type="submit" class="self-start">Create key</Button>
	</form>

	{#if store.error && !editTarget}
		<p class="text-sm text-destructive" data-testid="proxykeys-error">{store.error}</p>
	{/if}

	<Table.Root>
		<Table.Header>
			<Table.Row>
				<Table.Head>Name</Table.Head>
				<Table.Head>Created</Table.Head>
				<Table.Head>Last used</Table.Head>
				<Table.Head>Status</Table.Head>
				<Table.Head>Allowed models</Table.Head>
				<Table.Head></Table.Head>
			</Table.Row>
		</Table.Header>
		<Table.Body>
			{#each store.keys as key (key.id)}
				<Table.Row>
					<Table.Cell class="font-medium">{key.name}</Table.Cell>
					<Table.Cell>{formatTime(key.createdAt)}</Table.Cell>
					<Table.Cell>{formatTime(key.lastUsedAt)}</Table.Cell>
					<Table.Cell>
						<Badge variant={key.status === 'active' ? 'default' : 'secondary'}>{key.status}</Badge>
					</Table.Cell>
					<Table.Cell>
						{key.allowedModels.length ? key.allowedModels.join(', ') : 'all models'}
					</Table.Cell>
					<Table.Cell class="flex justify-end gap-2">
						<Button variant="outline" size="sm" onclick={() => openEdit(key)}>Edit models</Button>
						{#if key.status === 'active'}
							<Button variant="destructive" size="sm" onclick={() => revoke(key)}>Revoke</Button>
						{/if}
					</Table.Cell>
				</Table.Row>
			{:else}
				{#if !store.loading}
					<Table.Row>
						<Table.Cell colspan={6} class="text-muted-foreground">No proxy keys yet.</Table.Cell>
					</Table.Row>
				{/if}
			{/each}
		</Table.Body>
	</Table.Root>
</div>

<Dialog.Root
	open={store.revealed !== null}
	onOpenChange={(open) => {
		if (!open) store.dismissReveal();
	}}
>
	<Dialog.Content
		interactOutsideBehavior="ignore"
		escapeKeydownBehavior="ignore"
		showCloseButton={false}
	>
		<Dialog.Header>
			<Dialog.Title>Key {store.revealed?.name} created</Dialog.Title>
			<Dialog.Description>Copy this key now. It cannot be shown again.</Dialog.Description>
		</Dialog.Header>
		<code class="rounded bg-muted p-2 text-sm break-all" data-testid="proxykey-secret">
			{store.revealed?.secret}
		</code>
		{#if store.error}
			<p class="text-sm text-destructive">{store.error}</p>
		{/if}
		<Dialog.Footer>
			<Button variant="outline" onclick={() => void store.copy()}>
				{store.copied ? 'Copied' : 'Copy'}
			</Button>
			<Button onclick={store.dismissReveal}>Done</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<Dialog.Root
	open={editTarget !== null}
	onOpenChange={(open) => {
		if (!open) editTarget = null;
	}}
>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>Allowed models for {editTarget?.name}</Dialog.Title>
			<Dialog.Description
				>One per line or comma separated; empty allows all models.</Dialog.Description
			>
		</Dialog.Header>
		<form class="flex flex-col gap-4" onsubmit={saveEdit}>
			<Textarea bind:value={editText} />
			{#if store.error}
				<p class="text-sm text-destructive">{store.error}</p>
			{/if}
			<Dialog.Footer>
				<Button type="submit">Save</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
