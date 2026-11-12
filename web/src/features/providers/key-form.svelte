<script lang="ts">
	import * as Dialog from '#src/components/ui/dialog/index.js';
	import { Button } from '#src/components/ui/button/index.js';
	import { Input } from '#src/components/ui/input/index.js';
	import { Label } from '#src/components/ui/label/index.js';
	import { Switch } from '#src/components/ui/switch/index.js';
	import type { createProvidersStore, Provider, ProviderKey } from './providers.svelte';

	interface Props {
		store: ReturnType<typeof createProvidersStore>;
		target: { provider: Provider; key: ProviderKey | null } | null;
		onclose: () => void;
	}

	const { store, target, onclose }: Props = $props();

	let name = $state('');
	let secret = $state('');
	let maxInFlight = $state(4);
	let weight = $state(1);
	let enabled = $state(true);

	const editing = $derived(target?.key ?? null);

	$effect(() => {
		name = editing?.name ?? '';
		maxInFlight = editing?.maxInFlight ?? 4;
		weight = editing?.weight ?? 1;
		enabled = editing?.enabled ?? true;
		secret = '';
	});

	function submit(event: SubmitEvent) {
		event.preventDefault();
		if (!target) return;
		const input = { name, secret, maxInFlight, weight };
		const done = editing
			? store.updateKey(editing.id, { ...input, enabled })
			: store.addKey(target.provider.id, input);
		void done.then((ok) => {
			secret = '';
			if (ok) onclose();
		});
	}
</script>

<Dialog.Root
	open={target !== null}
	onOpenChange={(open) => {
		if (!open) {
			secret = '';
			onclose();
		}
	}}
>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>
				{editing ? `Edit key ${editing.name}` : `Add key to ${target?.provider.name}`}
			</Dialog.Title>
			<Dialog.Description>
				The secret is encrypted on save and cannot be shown again.
			</Dialog.Description>
		</Dialog.Header>
		<form class="flex flex-col gap-4" onsubmit={submit}>
			<div class="flex flex-col gap-2">
				<Label for="key-name">Name</Label>
				<Input id="key-name" bind:value={name} />
			</div>
			<div class="flex flex-col gap-2">
				<Label for="key-secret">Secret</Label>
				<Input
					id="key-secret"
					type="password"
					autocomplete="off"
					placeholder={editing ? 'Leave blank to keep the current secret' : ''}
					bind:value={secret}
				/>
			</div>
			<div class="flex flex-col gap-2">
				<Label for="key-in-flight">Max in flight</Label>
				<Input id="key-in-flight" type="number" min="1" bind:value={maxInFlight} />
			</div>
			<div class="flex flex-col gap-2">
				<Label for="key-weight">Weight</Label>
				<Input id="key-weight" type="number" min="1" bind:value={weight} />
			</div>
			{#if editing}
				<div class="flex items-center gap-2">
					<Switch bind:checked={enabled} />
					<Label>Enabled</Label>
				</div>
			{/if}
			{#if store.error}
				<p class="text-sm text-destructive">{store.error}</p>
			{/if}
			<Dialog.Footer>
				<Button type="submit">{editing ? 'Save' : 'Add key'}</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
