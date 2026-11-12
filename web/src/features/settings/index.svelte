<script lang="ts">
	import { getContext, onMount } from 'svelte';
	import { Button } from '#src/components/ui/button/index.js';
	import { Input } from '#src/components/ui/input/index.js';
	import { Label } from '#src/components/ui/label/index.js';
	import { AUTH_FETCH_KEY } from '#src/features/auth/auth.svelte.js';
	import { createSettingsStore, type AuthFetch, type Settings } from './settings.svelte';

	const store = createSettingsStore(getContext<AuthFetch>(AUTH_FETCH_KEY));

	const fields: { key: keyof Settings; label: string; unit: string }[] = [
		{ key: 'maxBodyBytes', label: 'Max request body', unit: 'bytes' },
		{ key: 'requestTimeoutSeconds', label: 'Request timeout', unit: 'seconds' },
		{ key: 'queueWaitSeconds', label: 'Queue wait', unit: 'seconds' },
		{ key: 'defaultCooldownSeconds', label: 'Default cooldown', unit: 'seconds' }
	];

	let form = $state<Settings | null>(null);
	let saved = $state(false);

	onMount(() => {
		void store.load().then(() => {
			form = store.settings && { ...store.settings };
		});
	});

	function save(event: SubmitEvent) {
		event.preventDefault();
		if (!form) return;
		saved = false;
		void store.save({ ...form }).then((ok) => {
			saved = ok;
		});
	}
</script>

<div class="flex flex-col gap-6">
	<h1 class="text-2xl font-bold tracking-tight">Settings</h1>

	{#if form}
		<form class="flex max-w-md flex-col gap-4" onsubmit={save}>
			{#each fields as field (field.key)}
				<div class="flex flex-col gap-2">
					<Label for="settings-{field.key}">{field.label} ({field.unit})</Label>
					<Input id="settings-{field.key}" type="number" min="1" bind:value={form[field.key]} />
				</div>
			{/each}
			<Button type="submit" class="self-start">Save</Button>
		</form>
	{/if}

	{#if store.error}
		<p class="text-sm text-destructive" data-testid="settings-error">{store.error}</p>
	{:else if saved}
		<p class="text-sm text-muted-foreground">Saved</p>
	{/if}
</div>
