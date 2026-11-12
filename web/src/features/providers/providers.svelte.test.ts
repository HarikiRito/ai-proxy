import { describe, expect, it, vi } from 'vitest';
import type { ModelsDevPreset } from '#src/features/modelsdev/modelsdev.svelte.js';
import { capacityOf, createProvidersStore, type Provider, parseModels } from './providers.svelte';

const SECRET = 'sk-DISTINCTIVE-secret-abcd1234';

const provider = (over: Partial<Provider> = {}): Provider => ({
	id: 'p1',
	name: 'openai',
	kind: 'openai_compatible',
	baseUrl: 'https://api.openai.com/v1',
	enabled: true,
	models: [],
	cacheAffinitySeconds: 600,
	capacity: 0,
	keys: [],
	...over
});

const openai: ModelsDevPreset = {
	id: 'openai',
	name: 'OpenAI',
	api: 'https://api.openai.com/v1',
	kind: 'openai_compatible',
	models: [
		{ id: 'gpt-4o', name: 'GPT-4o' },
		{ id: 'text-embedding', name: 'Emb', type: 'embedding' }
	]
};
const anthropic: ModelsDevPreset = {
	id: 'anthropic',
	name: 'Anthropic',
	api: 'https://api.anthropic.com',
	kind: 'anthropic',
	models: []
};

const key = (over = {}) => ({
	id: 'k1',
	name: 'main',
	enabled: true,
	maxInFlight: 4,
	weight: 1,
	maskedKey: '••••1234',
	...over
});

function setup(providers: Provider[] = [], override?: (op: string) => Response | undefined) {
	const calls: { op: string; variables: Record<string, unknown> }[] = [];
	const authFetch = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
		const { query, variables } = JSON.parse(init?.body as string);
		const op =
			/\b(createProvider|updateProvider|deleteProvider|addProviderKey|updateProviderKey|deleteProviderKey|providers)\b/.exec(
				query
			)?.[1] ?? '';
		calls.push({ op, variables });
		const res =
			override?.(op) ??
			Response.json({ data: op === 'providers' ? { providers } : { [op]: { id: 'x' } } });
		return Promise.resolve(res);
	});
	return { store: createProvidersStore(authFetch), authFetch, calls };
}

describe('capacityOf', () => {
	it('sums max in flight of enabled keys only', () => {
		expect(
			capacityOf([
				key({ maxInFlight: 4 }),
				key({ maxInFlight: 10, enabled: false }),
				key({ maxInFlight: 2 })
			])
		).toBe(6);
	});
});

describe('parseModels', () => {
	it('trims, drops empty lines and dedupes keeping order', () => {
		expect(parseModels(' a \n\n b\r\na\n  \n')).toEqual(['a', 'b']);
		expect(parseModels('')).toEqual([]);
	});
});

describe('load', () => {
	it('posts the providers query to /graphql and exposes the list', async () => {
		const { store, authFetch } = setup([provider()]);
		await store.load();
		expect(authFetch).toHaveBeenCalledWith('/graphql', expect.objectContaining({ method: 'POST' }));
		expect(store.providers.map((p) => p.name)).toEqual(['openai']);
		expect(store.error).toBe('');
	});

	it.each([
		[401, 'Signed out'],
		[403, 'Access denied'],
		[500, 'Request failed']
	])('maps HTTP %i to "%s" without rejecting', async (status, message) => {
		const { store } = setup([], () => new Response(null, { status }));
		await store.load();
		expect(store.error).toBe(message);
	});

	it('surfaces GraphQL error messages', async () => {
		const { store } = setup([], () =>
			Response.json({ errors: [{ message: 'name already exists' }] })
		);
		await store.load();
		expect(store.error).toBe('name already exists');
	});

	it('surfaces network failures as a message', async () => {
		const store = createProvidersStore(() => Promise.reject(new TypeError('down')));
		await store.load();
		expect(store.error).toBe('Request failed');
	});
});

describe('presets and draft', () => {
	it('applyPreset prefills kind, base URL, cache affinity and name, all still editable', () => {
		const { store } = setup();
		store.applyPreset(anthropic);
		expect(store.draft).toMatchObject({
			presetId: 'anthropic',
			name: 'Anthropic',
			kind: 'anthropic',
			baseUrl: 'https://api.anthropic.com',
			cacheAffinitySeconds: 300
		});
		store.updateDraft({ baseUrl: 'https://proxy.example.com' });
		expect(store.draft.baseUrl).toBe('https://proxy.example.com');
	});

	it('applyPreset fills models with the preset chat models, editable afterwards', () => {
		const { store } = setup();
		store.applyPreset(openai);
		expect(store.draft.models).toBe('gpt-4o');
		store.updateDraft({ models: 'gpt-4o\nmine' });
		expect(store.draft.models).toBe('gpt-4o\nmine');
	});

	it('startEdit loads stored models one per line', () => {
		const { store } = setup();
		store.startEdit(provider({ models: ['a', 'b'] }));
		expect(store.draft.models).toBe('a\nb');
	});

	it('keeps a typed name when a preset is applied', () => {
		const { store } = setup();
		store.updateDraft({ name: 'my-groq' });
		store.applyPreset(openai);
		expect(store.draft.name).toBe('my-groq');
	});

	it('custom preset keeps whatever address was typed', () => {
		const { store } = setup();
		store.applyPreset(openai);
		store.applyPreset(undefined);
		expect(store.draft).toMatchObject({ presetId: 'custom', baseUrl: 'https://api.openai.com/v1' });
	});

	it('claude_subscription kind defaults cache affinity to 300', () => {
		const { store } = setup();
		store.updateDraft({ kind: 'claude_subscription' });
		expect(store.draft.cacheAffinitySeconds).toBe(300);
	});
});

describe('submitDraft', () => {
	it('creates the provider from the draft, reloads and resets the draft', async () => {
		const { store, calls } = setup([provider()]);
		store.applyPreset(openai);
		await store.submitDraft();
		expect(calls[0].op).toBe('createProvider');
		expect(calls[0].variables.input).toMatchObject({
			name: 'OpenAI',
			kind: 'openai_compatible',
			baseUrl: 'https://api.openai.com/v1',
			models: ['gpt-4o'],
			cacheAffinitySeconds: 600,
			enabled: true
		});
		expect(calls[1].op).toBe('providers');
		expect(store.providers).toHaveLength(1);
		expect(store.draft.name).toBe('');
	});

	it('updates when editing an existing provider', async () => {
		const { store, calls } = setup();
		store.startEdit(provider({ id: 'p9', models: ['a', 'b'] }));
		store.updateDraft({ enabled: false });
		await store.submitDraft();
		expect(calls[0].op).toBe('updateProvider');
		expect(calls[0].variables).toMatchObject({
			id: 'p9',
			input: { enabled: false, models: ['a', 'b'] }
		});
	});

	it('keeps the draft and shows the error when the server rejects it', async () => {
		const { store } = setup([], (op) =>
			op === 'createProvider'
				? Response.json({ errors: [{ message: 'base URL must be an http(s) URL with a host' }] })
				: undefined
		);
		store.updateDraft({ name: 'x', baseUrl: 'nope' });
		await store.submitDraft();
		expect(store.error).toContain('base URL');
		expect(store.draft.name).toBe('x');
	});
});

describe('provider and key mutations', () => {
	it('deleteProvider and deleteKey send ids and reload', async () => {
		const { store, calls } = setup();
		await store.deleteProvider('p1');
		await store.deleteKey('k1');
		expect(calls.map((c) => c.op)).toEqual([
			'deleteProvider',
			'providers',
			'deleteProviderKey',
			'providers'
		]);
		expect(calls[0].variables).toEqual({ id: 'p1' });
		expect(calls[2].variables).toEqual({ id: 'k1' });
	});

	it('addKey sends the secret once and never retains it in store state', async () => {
		const { store, calls } = setup([provider({ keys: [key()] })]);
		await store.addKey('p1', {
			name: 'main',
			secret: SECRET,
			maxInFlight: 4,
			weight: 1
		});
		expect(calls[0].op).toBe('addProviderKey');
		expect(JSON.stringify(calls[0].variables)).toContain(SECRET);
		const state = JSON.stringify({
			providers: store.providers,
			draft: store.draft,
			error: store.error
		});
		expect(state).not.toContain(SECRET);
	});

	it('updateKey sends only the given fields, a new secret replaces the key', async () => {
		const { store, calls } = setup();
		await store.updateKey('k1', { secret: SECRET, enabled: false });
		expect(calls[0]).toEqual({
			op: 'updateProviderKey',
			variables: { id: 'k1', input: { secret: SECRET, enabled: false } }
		});
		expect(JSON.stringify({ p: store.providers, e: store.error })).not.toContain(SECRET);
	});
});

describe('updateKey', () => {
	it('omits a blank secret so the current one is kept', async () => {
		const { store, calls } = setup();
		await store.updateKey('k1', {
			name: 'n',
			secret: '',
			maxInFlight: 2,
			weight: 3,
			enabled: true
		});
		expect(calls[0].variables).toEqual({
			id: 'k1',
			input: { name: 'n', maxInFlight: 2, weight: 3, enabled: true }
		});
	});

	it('sends a non-empty secret', async () => {
		const { store, calls } = setup();
		await store.updateKey('k1', { secret: SECRET, weight: 2 });
		expect(calls[0].variables).toEqual({ id: 'k1', input: { secret: SECRET, weight: 2 } });
	});
});

describe('error clearing', () => {
	const failing = () => Response.json({ errors: [{ message: 'boom' }] });

	it.each([
		['resetDraft', (s: ReturnType<typeof setup>['store']) => s.resetDraft()],
		['startEdit', (s: ReturnType<typeof setup>['store']) => s.startEdit(provider())],
		['clearError', (s: ReturnType<typeof setup>['store']) => s.clearError()]
	])('%s clears a stale error', async (_name, action) => {
		const { store } = setup([], failing);
		await store.load();
		expect(store.error).toBe('boom');
		action(store);
		expect(store.error).toBe('');
	});
});
