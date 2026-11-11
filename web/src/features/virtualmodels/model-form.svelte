<script lang="ts">
	import { Button } from '#src/components/ui/button/index.js';
	import * as Dialog from '#src/components/ui/dialog/index.js';
	import { Input } from '#src/components/ui/input/index.js';
	import { Label } from '#src/components/ui/label/index.js';
	import * as Select from '#src/components/ui/select/index.js';
	import { Switch } from '#src/components/ui/switch/index.js';
	import {
		KIND_LABELS,
		KINDS,
		STRATEGIES,
		STRATEGY_LABELS,
		type createVirtualModelsStore,
		type Kind,
		type Strategy
	} from './virtualmodels.svelte';

	interface Props {
		store: ReturnType<typeof createVirtualModelsStore>;
		open?: boolean;
	}

	let { store, open = $bindable(false) }: Props = $props();

	function submit(event: SubmitEvent) {
		event.preventDefault();
		void store.submit().then((ok) => {
			if (ok) open = false;
		});
	}
</script>

<Dialog.Root bind:open onOpenChange={(o) => !o && store.resetDraft()}>
	<Dialog.Content class="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
		<Dialog.Header>
			<Dialog.Title>{store.editingId ? 'Edit virtual model' : 'Add virtual model'}</Dialog.Title>
			<Dialog.Description>
				Tiers are tried in order; entries inside a tier share load by the chosen strategy.
			</Dialog.Description>
		</Dialog.Header>
		<form class="flex flex-col gap-4" onsubmit={submit}>
			<div class="flex flex-col gap-2">
				<Label for="vm-name">Name</Label>
				<Input
					id="vm-name"
					value={store.draft.name}
					readonly={store.editingId !== null}
					oninput={(e) => store.updateDraft({ name: e.currentTarget.value })}
				/>
			</div>
			<div class="grid grid-cols-2 gap-4">
				<div class="flex flex-col gap-2">
					<Label>Kind</Label>
					<Select.Root
						type="single"
						value={store.draft.kind}
						onValueChange={(v) => store.updateDraft({ kind: v as Kind })}
					>
						<Select.Trigger class="w-full">{KIND_LABELS[store.draft.kind]}</Select.Trigger>
						<Select.Content>
							{#each KINDS as kind (kind)}
								<Select.Item value={kind} label={KIND_LABELS[kind]}>{KIND_LABELS[kind]}</Select.Item
								>
							{/each}
						</Select.Content>
					</Select.Root>
				</div>
				<div class="flex flex-col gap-2">
					<Label>Strategy</Label>
					<Select.Root
						type="single"
						value={store.draft.strategy}
						onValueChange={(v) => store.updateDraft({ strategy: v as Strategy })}
					>
						<Select.Trigger class="w-full">{STRATEGY_LABELS[store.draft.strategy]}</Select.Trigger>
						<Select.Content>
							{#each STRATEGIES as strategy (strategy)}
								<Select.Item value={strategy} label={STRATEGY_LABELS[strategy]}>
									{STRATEGY_LABELS[strategy]}
								</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
				</div>
			</div>
			<div class="flex items-center gap-2">
				<Switch
					checked={store.draft.cacheAffinity}
					onCheckedChange={(cacheAffinity) => store.updateDraft({ cacheAffinity })}
				/>
				<Label>Cache affinity</Label>
			</div>

			{#each store.draft.tiers as tier, ti (ti)}
				<div class="flex flex-col gap-2 rounded-md border p-3">
					<div class="flex items-center gap-2">
						<span class="text-sm font-medium">Tier {ti + 1}</span>
						<span class="ml-auto"></span>
						<Button
							type="button"
							variant="outline"
							size="sm"
							disabled={ti === 0}
							onclick={() => store.moveTier(ti, 'up')}>Up</Button
						>
						<Button
							type="button"
							variant="outline"
							size="sm"
							disabled={ti === store.draft.tiers.length - 1}
							onclick={() => store.moveTier(ti, 'down')}>Down</Button
						>
						<Button
							type="button"
							variant="destructive"
							size="sm"
							disabled={store.draft.tiers.length === 1}
							onclick={() => store.removeTier(ti)}>Remove</Button
						>
					</div>
					{#each tier.entries as entry, ei (ei)}
						<div class="flex items-center gap-2">
							<Select.Root
								type="single"
								value={entry.providerId}
								onValueChange={(v) => store.updateEntry(ti, ei, { providerId: v })}
							>
								<Select.Trigger class="w-44">
									{store.providerLabel(entry.providerId)}
								</Select.Trigger>
								<Select.Content>
									{#each store.providerOptions as provider (provider.id)}
										<Select.Item value={provider.id} label={provider.name}>
											{provider.name}
										</Select.Item>
									{/each}
								</Select.Content>
							</Select.Root>
							<Input
								class="flex-1"
								placeholder="Upstream model"
								aria-label="Upstream model"
								value={entry.model}
								oninput={(e) => store.updateEntry(ti, ei, { model: e.currentTarget.value })}
							/>
							<Input
								class="w-20"
								type="number"
								min="1"
								aria-label="Weight"
								value={entry.weight}
								oninput={(e) =>
									store.updateEntry(ti, ei, { weight: e.currentTarget.valueAsNumber || 0 })}
							/>
							<Button
								type="button"
								variant="outline"
								size="sm"
								disabled={tier.entries.length === 1}
								onclick={() => store.removeEntry(ti, ei)}>Remove</Button
							>
						</div>
					{/each}
					<Button
						type="button"
						variant="outline"
						size="sm"
						class="self-start"
						onclick={() => store.addEntry(ti)}>Add entry</Button
					>
				</div>
			{/each}
			<Button type="button" variant="outline" class="self-start" onclick={store.addTier}>
				Add tier
			</Button>

			{#if store.error}
				<p class="text-sm text-destructive">{store.error}</p>
			{/if}
			<Dialog.Footer>
				<Button type="submit">{store.editingId ? 'Save' : 'Add'}</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
