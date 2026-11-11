<script lang="ts">
	import * as Dialog from '#src/components/ui/dialog/index.js';
	import { Button } from '#src/components/ui/button/index.js';
	import { Input } from '#src/components/ui/input/index.js';
	import { Label } from '#src/components/ui/label/index.js';
	import type { createProvidersStore, Provider } from './providers.svelte';

	interface Props {
		store: ReturnType<typeof createProvidersStore>;
		provider: Provider | null;
		onclose: () => void;
	}

	const { store, provider, onclose }: Props = $props();

	let name = $state('');
	let secret = $state('');
	let maxInFlight = $state(4);
	let weight = $state(1);
	let priority = $state(0);

	function submit(event: SubmitEvent) {
		event.preventDefault();
		if (!provider) return;
		void store.addKey(provider.id, { name, secret, maxInFlight, weight, priority }).then((ok) => {
			secret = '';
			if (!ok) return;
			name = '';
			onclose();
		});
	}
</script>

<Dialog.Root
	open={provider !== null}
	onOpenChange={(open) => {
		if (!open) {
			secret = '';
			onclose();
		}
	}}
>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>Add key to {provider?.name}</Dialog.Title>
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
				<Input id="key-secret" type="password" autocomplete="off" bind:value={secret} />
			</div>
			<div class="flex flex-col gap-2">
				<Label for="key-in-flight">Max in flight</Label>
				<Input id="key-in-flight" type="number" min="1" bind:value={maxInFlight} />
			</div>
			{#if provider?.rotation === 'weighted_round_robin'}
				<div class="flex flex-col gap-2">
					<Label for="key-weight">Weight</Label>
					<Input id="key-weight" type="number" min="1" bind:value={weight} />
				</div>
			{/if}
			{#if provider?.rotation === 'priority_overflow'}
				<div class="flex flex-col gap-2">
					<Label for="key-priority">Priority</Label>
					<Input id="key-priority" type="number" bind:value={priority} />
				</div>
			{/if}
			{#if store.error}
				<p class="text-sm text-destructive">{store.error}</p>
			{/if}
			<Dialog.Footer>
				<Button type="submit">Add key</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
