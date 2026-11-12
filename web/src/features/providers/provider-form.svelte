<script lang="ts">
	import * as Dialog from '#src/components/ui/dialog/index.js';
	import * as Select from '#src/components/ui/select/index.js';
	import { Button } from '#src/components/ui/button/index.js';
	import { Input } from '#src/components/ui/input/index.js';
	import { Label } from '#src/components/ui/label/index.js';
	import { Textarea } from '#src/components/ui/textarea/index.js';
	import { Switch } from '#src/components/ui/switch/index.js';
	import type { createModelsDevStore } from '#src/features/modelsdev/modelsdev.svelte.js';
	import type { ProviderKind } from './presets';
	import type { createProvidersStore } from './providers.svelte';

	interface Props {
		store: ReturnType<typeof createProvidersStore>;
		modelsDev: ReturnType<typeof createModelsDevStore>;
		open?: boolean;
	}

	let { store, modelsDev, open = $bindable(false) }: Props = $props();

	const KINDS: { value: ProviderKind; label: string }[] = [
		{ value: 'openai_compatible', label: 'OpenAI compatible' },
		{ value: 'anthropic', label: 'Anthropic' },
		{ value: 'claude_subscription', label: 'Claude subscription' }
	];

	const presetLabel = $derived(
		modelsDev.presets.find((p) => p.id === store.draft.presetId)?.name ?? 'Custom'
	);
	const subscription = $derived(store.draft.kind === 'claude_subscription');
	const kindLabel = $derived(KINDS.find((k) => k.value === store.draft.kind)?.label);

	function submit(event: SubmitEvent) {
		event.preventDefault();
		void store.submitDraft().then((ok) => {
			if (ok) open = false;
		});
	}
</script>

<Dialog.Root bind:open onOpenChange={(o) => !o && store.resetDraft()}>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>{store.editingId ? 'Edit provider' : 'Add provider'}</Dialog.Title>
			<Dialog.Description>Pick a preset or enter any compatible address.</Dialog.Description>
		</Dialog.Header>
		<form class="flex flex-col gap-4" onsubmit={submit}>
			{#if !store.editingId && !subscription}
				<div class="flex flex-col gap-2">
					<Label>Preset</Label>
					<Select.Root
						type="single"
						value={store.draft.presetId}
						onValueChange={(v) => store.applyPreset(modelsDev.presets.find((p) => p.id === v))}
					>
						<Select.Trigger class="w-full">{presetLabel}</Select.Trigger>
						<Select.Content>
							<Select.Item value="custom" label="Custom">Custom</Select.Item>
							{#each modelsDev.presets as preset (preset.id)}
								<Select.Item value={preset.id} label={preset.name}>{preset.name}</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
					{#if modelsDev.status === 'error'}
						<p class="text-sm text-muted-foreground">Could not load presets from models.dev.</p>
					{/if}
				</div>
			{/if}
			<div class="flex flex-col gap-2">
				<Label for="provider-name">Name</Label>
				<Input
					id="provider-name"
					value={store.draft.name}
					oninput={(e) => store.updateDraft({ name: e.currentTarget.value })}
				/>
			</div>
			<div class="flex flex-col gap-2">
				<Label>Kind</Label>
				<Select.Root
					type="single"
					value={store.draft.kind}
					onValueChange={(v) => store.updateDraft({ kind: v as ProviderKind })}
				>
					<Select.Trigger class="w-full">{kindLabel}</Select.Trigger>
					<Select.Content>
						{#each KINDS as kind (kind.value)}
							<Select.Item value={kind.value} label={kind.label}>{kind.label}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>
			<div class="flex flex-col gap-2">
				<Label for="provider-url">{subscription ? 'Sidecar address' : 'Base URL'}</Label>
				<Input
					id="provider-url"
					placeholder={subscription ? 'http://sidecar:3000' : ''}
					value={store.draft.baseUrl}
					oninput={(e) => store.updateDraft({ baseUrl: e.currentTarget.value, presetId: 'custom' })}
				/>
			</div>
			{#if !subscription}
				<div class="flex flex-col gap-2">
					<Label for="provider-models">Models (one per line)</Label>
					<Textarea
						id="provider-models"
						rows={5}
						value={store.draft.models}
						oninput={(e) => store.updateDraft({ models: e.currentTarget.value })}
					/>
				</div>
			{/if}
			<div class="flex flex-col gap-2">
				<Label for="provider-affinity">Cache affinity (seconds)</Label>
				<Input
					id="provider-affinity"
					type="number"
					min="0"
					value={store.draft.cacheAffinitySeconds}
					oninput={(e) =>
						store.updateDraft({ cacheAffinitySeconds: e.currentTarget.valueAsNumber || 0 })}
				/>
			</div>
			<div class="flex items-center gap-2">
				<Switch
					checked={store.draft.enabled}
					onCheckedChange={(enabled) => store.updateDraft({ enabled })}
				/>
				<Label>Enabled</Label>
			</div>
			{#if store.error}
				<p class="text-sm text-destructive">{store.error}</p>
			{/if}
			<Dialog.Footer>
				<Button type="submit">{store.editingId ? 'Save' : 'Add'}</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
