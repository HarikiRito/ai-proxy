<script lang="ts">
	import * as Dialog from '#src/components/ui/dialog/index.js';
	import * as Select from '#src/components/ui/select/index.js';
	import { Button } from '#src/components/ui/button/index.js';
	import { Input } from '#src/components/ui/input/index.js';
	import { Label } from '#src/components/ui/label/index.js';
	import { Switch } from '#src/components/ui/switch/index.js';
	import { PRESETS, type ProviderKind } from './presets';
	import { ROTATIONS, type createProvidersStore, type Rotation } from './providers.svelte';

	interface Props {
		store: ReturnType<typeof createProvidersStore>;
		open?: boolean;
	}

	let { store, open = $bindable(false) }: Props = $props();

	const KINDS: { value: ProviderKind; label: string }[] = [
		{ value: 'openai_compatible', label: 'OpenAI compatible' },
		{ value: 'anthropic', label: 'Anthropic' },
		{ value: 'claude_subscription', label: 'Claude subscription' }
	];
	const ROTATION_LABELS: Record<Rotation, string> = {
		round_robin: 'Round robin',
		weighted_round_robin: 'Weighted round robin',
		priority_overflow: 'Priority overflow',
		least_in_flight: 'Least in flight'
	};

	const presetLabel = $derived(
		PRESETS.find((p) => p.id === store.draft.presetId)?.label ?? 'Custom'
	);
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
			{#if !store.editingId}
				<div class="flex flex-col gap-2">
					<Label>Preset</Label>
					<Select.Root
						type="single"
						value={store.draft.presetId}
						onValueChange={(v) => store.applyPreset(v)}
					>
						<Select.Trigger class="w-full">{presetLabel}</Select.Trigger>
						<Select.Content>
							<Select.Item value="custom" label="Custom">Custom</Select.Item>
							{#each PRESETS as preset (preset.id)}
								<Select.Item value={preset.id} label={preset.label}>{preset.label}</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
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
				<Label for="provider-url">Base URL</Label>
				<Input
					id="provider-url"
					value={store.draft.baseUrl}
					oninput={(e) => store.updateDraft({ baseUrl: e.currentTarget.value, presetId: 'custom' })}
				/>
			</div>
			<div class="flex flex-col gap-2">
				<Label>Rotation</Label>
				<Select.Root
					type="single"
					value={store.draft.rotation}
					onValueChange={(v) => store.updateDraft({ rotation: v as Rotation })}
				>
					<Select.Trigger class="w-full">{ROTATION_LABELS[store.draft.rotation]}</Select.Trigger>
					<Select.Content>
						{#each ROTATIONS as rotation (rotation)}
							<Select.Item value={rotation} label={ROTATION_LABELS[rotation]}>
								{ROTATION_LABELS[rotation]}
							</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>
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
