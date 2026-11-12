import { describe, expect, it, vi } from 'vitest';
import {
	addEntry,
	addTier,
	createVirtualModelsStore,
	type DraftTier,
	draftFromModel,
	moveTier,
	type ProviderOption,
	providerOptionsFor,
	removeEntry,
	removeTier,
	toInput,
	updateEntry,
	type VirtualModel
} from './virtualmodels.svelte';

const tier = (...models: string[]): DraftTier => ({
	entries: models.map((model) => ({ providerId: 'p1', model, weight: 1 }))
});
const names = (tiers: DraftTier[]) => tiers.map((t) => t.entries.map((e) => e.model).join(','));

const providers: ProviderOption[] = [
	{
		id: 'p1',
		name: 'openai',
		kind: 'openai_compatible',
		baseUrl: 'https://api.example.com',
		enabled: true
	},
	{
		id: 'p2',
		name: 'claude',
		kind: 'anthropic',
		baseUrl: 'https://api.example.com',
		enabled: true
	},
	{
		id: 'p3',
		name: 'sub',
		kind: 'claude_subscription',
		baseUrl: 'https://api.example.com',
		enabled: true
	},
	{
		id: 'p4',
		name: 'sleepy',
		kind: 'openai_compatible',
		baseUrl: 'https://api.example.com',
		enabled: false
	}
];

const model = (over: Partial<VirtualModel> = {}): VirtualModel => ({
	id: 'm1',
	name: 'smart',
	kind: 'chat',
	strategy: 'weighted_round_robin',
	cacheAffinity: false,
	tiers: [
		{
			priority: 0,
			entries: [
				{
					id: 'e1',
					providerId: 'p1',
					providerName: 'openai',
					model: 'gpt-5',
					weight: 3,
					status: 'ok'
				}
			]
		}
	],
	createdAt: '',
	updatedAt: '',
	...over
});

describe('tier helpers', () => {
	it('addTier appends an empty tier with one blank entry', () => {
		const out = addTier([tier('a')]);
		expect(out).toHaveLength(2);
		expect(out[1].entries).toEqual([{ providerId: '', model: '', weight: 1 }]);
	});

	it('removeTier drops the tier and keeps the order of the rest', () => {
		expect(names(removeTier([tier('a'), tier('b'), tier('c')], 1))).toEqual(['a', 'c']);
	});

	it('moveTier swaps with the neighbour and ignores moves off the ends', () => {
		const tiers = [tier('a'), tier('b'), tier('c')];
		expect(names(moveTier(tiers, 2, 'up'))).toEqual(['a', 'c', 'b']);
		expect(names(moveTier(tiers, 0, 'down'))).toEqual(['b', 'a', 'c']);
		expect(names(moveTier(tiers, 0, 'up'))).toEqual(['a', 'b', 'c']);
		expect(names(moveTier(tiers, 2, 'down'))).toEqual(['a', 'b', 'c']);
	});

	it('entry helpers only touch the addressed tier and never mutate the input', () => {
		const tiers = [tier('a'), tier('b')];
		const added = addEntry(tiers, 1);
		expect(names(added)).toEqual(['a', 'b,']);
		const edited = updateEntry(added, 1, 1, { model: 'x', weight: 4 });
		expect(edited[1].entries[1]).toEqual({ providerId: '', model: 'x', weight: 4 });
		expect(names(removeEntry(edited, 1, 0))).toEqual(['a', 'x']);
		expect(names(tiers)).toEqual(['a', 'b']);
	});
});

describe('providerOptionsFor', () => {
	it('chat offers every enabled provider, never a disabled one', () => {
		expect(providerOptionsFor('chat', providers).map((p) => p.id)).toEqual(['p1', 'p2', 'p3']);
	});

	it('transcription offers only enabled openai_compatible providers', () => {
		expect(providerOptionsFor('transcription', providers).map((p) => p.id)).toEqual(['p1']);
	});
});

describe('draft mapping', () => {
	it('draftFromModel then toInput round-trips, and update has no name', () => {
		const draft = draftFromModel(model());
		expect(toInput(draft, false)).toEqual({
			name: 'smart',
			kind: 'chat',
			strategy: 'weighted_round_robin',
			cacheAffinity: false,
			tiers: [{ entries: [{ providerId: 'p1', model: 'gpt-5', weight: 3 }] }]
		});
		expect(toInput(draft, true)).not.toHaveProperty('name');
	});
});

function setup(over?: (op: string) => Response | undefined) {
	const calls: { query: string; variables: Record<string, unknown> }[] = [];
	const authFetch = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
		const { query, variables } = JSON.parse(init?.body as string);
		calls.push({ query, variables });
		const op = /\b(createVirtualModel|updateVirtualModel|deleteVirtualModel|virtualModels)\b/.exec(
			query
		)?.[1];
		return Promise.resolve(
			over?.(op ?? '') ??
				Response.json({
					data:
						op === 'virtualModels' ? { virtualModels: [model()], providers } : { [op ?? '']: {} }
				})
		);
	});
	return { store: createVirtualModelsStore(authFetch), calls };
}

describe('store', () => {
	it('load fetches virtual models and providers', async () => {
		const { store } = setup();
		await store.load();
		expect(store.models.map((m) => m.name)).toEqual(['smart']);
		expect(store.providers).toHaveLength(4);
		expect(store.error).toBe('');
	});

	it('submit creates with the full draft, reloads and resets', async () => {
		const { store, calls } = setup();
		store.updateDraft({ name: 'fresh', strategy: 'priority_overflow', cacheAffinity: false });
		store.updateEntry(0, 0, { providerId: 'p1', model: 'gpt-5', weight: 2 });
		expect(await store.submit()).toBe(true);
		expect(calls[0].variables).toEqual({
			input: {
				name: 'fresh',
				kind: 'chat',
				strategy: 'priority_overflow',
				cacheAffinity: false,
				tiers: [{ entries: [{ providerId: 'p1', model: 'gpt-5', weight: 2 }] }]
			}
		});
		expect(calls[0].query).toContain('createVirtualModel');
		expect(calls[1].query).toContain('virtualModels');
		expect(store.draft.name).toBe('');
		expect(store.editingId).toBeNull();
	});

	it('submit while editing updates by id and never sends the name', async () => {
		const { store, calls } = setup();
		store.startEdit(model());
		store.updateDraft({ name: 'renamed' });
		expect(store.editingId).toBe('m1');
		expect(await store.submit()).toBe(true);
		expect(calls[0].query).toContain('updateVirtualModel');
		expect(calls[0].variables.id).toBe('m1');
		expect(calls[0].variables.input).not.toHaveProperty('name');
	});

	it('switching to transcription clears providers that cannot transcribe', async () => {
		const { store } = setup();
		await store.load();
		store.updateDraft({ kind: 'chat' });
		store.addEntry(0);
		store.updateEntry(0, 0, { providerId: 'p1' });
		store.updateEntry(0, 1, { providerId: 'p2' });
		store.updateDraft({ kind: 'transcription' });
		expect(store.draft.tiers[0].entries.map((e) => e.providerId)).toEqual(['p1', '']);
		expect(store.providerOptions.map((p) => p.id)).toEqual(['p1']);
	});

	it('providerLabel flags a selected provider that is disabled or gone, keeping its name', async () => {
		const { store } = setup();
		await store.load();
		store.startEdit(
			model({
				tiers: [
					{
						priority: 0,
						entries: [
							{
								id: 'e1',
								providerId: 'p4',
								providerName: 'sleepy',
								model: 'a',
								weight: 1,
								status: 'provider_disabled'
							},
							{
								id: 'e2',
								providerId: 'gone',
								providerName: '',
								model: 'b',
								weight: 1,
								status: 'provider_missing'
							}
						]
					}
				]
			})
		);
		expect(store.providerLabel('p1')).toBe('openai');
		expect(store.providerLabel('p4')).toBe('sleepy (unavailable)');
		expect(store.providerLabel('gone')).toBe('Unknown provider (unavailable)');
		expect(store.providerLabel('')).toBe('Provider');
	});

	it('keeps the error of a failed reload after a successful save', async () => {
		const { store } = setup((op) =>
			op === 'virtualModels' ? new Response('', { status: 500 }) : undefined
		);
		store.updateDraft({ name: 'fresh' });
		store.updateEntry(0, 0, { providerId: 'p1', model: 'gpt-5' });
		expect(await store.submit()).toBe(true);
		expect(store.error).toBe('Request failed');
	});

	it('startCreate and startEdit clear a previous error', async () => {
		const { store } = setup(() => new Response('', { status: 401 }));
		await store.load();
		expect(store.error).toBe('Signed out');
		store.startCreate();
		expect(store.error).toBe('');
		await store.load();
		store.startEdit(model());
		expect(store.error).toBe('');
	});

	it('remove deletes by id and reloads', async () => {
		const { store, calls } = setup();
		expect(await store.remove('m1')).toBe(true);
		expect(calls[0].variables).toEqual({ id: 'm1' });
		expect(calls[1].query).toContain('virtualModels');
	});

	it('maps 401, 403 and the first GraphQL error without rejecting', async () => {
		for (const [status, message] of [
			[401, 'Signed out'],
			[403, 'Access denied']
		] as const) {
			const { store } = setup(() => new Response('', { status }));
			await store.load();
			expect(store.error).toBe(message);
		}
		const gql = setup((op) =>
			op === 'createVirtualModel'
				? Response.json({ errors: [{ message: 'name already exists' }, { message: 'other' }] })
				: undefined
		);
		expect(await gql.store.submit()).toBe(false);
		expect(gql.store.error).toBe('name already exists');
		const down = createVirtualModelsStore(() => Promise.reject(new Error('offline')));
		await expect(down.load()).resolves.toBeUndefined();
		expect(down.error).toBe('Request failed');
	});
});
