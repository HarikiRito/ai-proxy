<script lang="ts">
	import * as Dialog from '#src/components/ui/dialog/index.js';
	import { Button } from '#src/components/ui/button/index.js';
	import { Input } from '#src/components/ui/input/index.js';
	import { Label } from '#src/components/ui/label/index.js';
	import * as Select from '#src/components/ui/select/index.js';
	import { Switch } from '#src/components/ui/switch/index.js';
	import { SvelteDate } from 'svelte/reactivity';
	import type { AuthMode, createProvidersStore, Provider, ProviderKey } from './providers.svelte';

	interface Props {
		store: ReturnType<typeof createProvidersStore>;
		target: { provider: Provider; key: ProviderKey | null } | null;
		onclose: () => void;
	}

	const { store, target, onclose }: Props = $props();

	const MODES: { value: AuthMode; label: string }[] = [
		{ value: 'SETUP_TOKEN', label: 'Setup token' },
		{ value: 'LOGIN_DIR', label: 'Login directory' }
	];

	let name = $state('');
	let secret = $state('');
	let maxInFlight = $state(4);
	let weight = $state(1);
	let enabled = $state(true);
	let authMode = $state<AuthMode>('SETUP_TOKEN');
	let createdAt = $state('');
	let initialCreatedAt = $state('');

	const editing = $derived(target?.key ?? null);
	const subscription = $derived(target?.provider.kind === 'claude_subscription');
	const noun = $derived(subscription ? 'account' : 'key');
	const setupToken = $derived(authMode === 'SETUP_TOKEN');
	const modeChanged = $derived(subscription && editing !== null && authMode !== editing.authMode);
	const modeLabel = $derived(MODES.find((m) => m.value === authMode)?.label);

	// Every open creates a new target object, so this resets the fields on each open.
	$effect(() => {
		const key = target?.key;
		name = key?.name ?? '';
		maxInFlight = key?.maxInFlight ?? 4;
		weight = key?.weight ?? 1;
		enabled = key?.enabled ?? true;
		secret = '';
		authMode = key?.authMode === 'LOGIN_DIR' ? 'LOGIN_DIR' : 'SETUP_TOKEN';
		initialCreatedAt =
			key?.tokenCreatedAt?.slice(0, 10) ?? new SvelteDate().toISOString().slice(0, 10);
		createdAt = initialCreatedAt;
	});

	function submit(event: SubmitEvent) {
		event.preventDefault();
		if (!target) return;
		const account = subscription
			? {
					authMode,
					...(setupToken &&
						createdAt &&
						createdAt !== initialCreatedAt && { tokenCreatedAt: `${createdAt}T00:00:00Z` })
				}
			: {};
		const input = { name, secret, maxInFlight, weight, ...account };
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
				{editing ? `Edit ${noun} ${editing.name}` : `Add ${noun} to ${target?.provider.name}`}
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
			{#if subscription}
				<div class="flex flex-col gap-2">
					<Label>Mode</Label>
					<Select.Root type="single" bind:value={authMode}>
						<Select.Trigger class="w-full">{modeLabel}</Select.Trigger>
						<Select.Content>
							{#each MODES as mode (mode.value)}
								<Select.Item value={mode.value} label={mode.label}>{mode.label}</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
				</div>
			{/if}
			<div class="flex flex-col gap-2">
				<Label for="key-secret">
					{!subscription ? 'Secret' : setupToken ? 'Setup token' : 'Account directory name'}
				</Label>
				<Input
					id="key-secret"
					type={subscription && !setupToken ? 'text' : 'password'}
					autocomplete="off"
					placeholder={editing && !modeChanged ? 'Leave blank to keep the current one' : ''}
					bind:value={secret}
				/>
				{#if subscription}
					<p class="text-sm text-muted-foreground">
						{setupToken
							? 'From claude setup-token.'
							: 'Prepared once in the sidecar container, see the sidecar README.'}
					</p>
				{/if}
			</div>
			{#if subscription && setupToken}
				<div class="flex flex-col gap-2">
					<Label for="key-created">Token created</Label>
					<Input id="key-created" type="date" bind:value={createdAt} />
				</div>
			{/if}
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
				<Button type="submit" disabled={modeChanged && !secret}>
					{editing ? 'Save' : `Add ${noun}`}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
