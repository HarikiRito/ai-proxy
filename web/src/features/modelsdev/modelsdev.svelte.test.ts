import { describe, expect, it, vi } from 'vitest';
import {
	createModelsDevStore,
	type ModelsDevPreset,
	modelSuggestions,
	trim
} from './modelsdev.svelte';

const RAW = {
	openai: { id: 'openai', name: 'OpenAI', npm: '@ai-sdk/openai', models: {} },
	zeta: {
		id: 'zeta',
		name: 'Zeta',
		api: 'https://zeta.example/v1',
		npm: '@ai-sdk/openai-compatible',
		models: { z1: { id: 'z1', name: 'Z One', type: 'chat', extra: 1 } }
	},
	alpha: {
		id: 'alpha',
		name: 'Alpha',
		api: 'https://alpha.example',
		npm: '@ai-sdk/anthropic',
		models: { a1: { id: 'a1', name: 'A One' } }
	},
	router: {
		id: 'router',
		name: 'Router',
		api: 'https://router.example/v1',
		npm: '@openrouter/ai-sdk-provider',
		models: {}
	},
	other: { id: 'other', name: 'Other', api: 'https://o.example', npm: '@ai-sdk/google', models: {} }
};

const memoryStorage = (failWrites = false) => {
	const data = new Map<string, string>();
	return {
		data,
		getItem: (k: string) => data.get(k) ?? null,
		setItem: (k: string, v: string) => {
			if (failWrites) throw new Error('quota');
			data.set(k, v);
		}
	};
};

const okResponse = (etag = '"v1"') =>
	new Response(JSON.stringify(RAW), { status: 200, headers: { etag } });

function setup(opts: { failWrites?: boolean } = {}) {
	let time = 1_000_000;
	const storage = memoryStorage(opts.failWrites);
	const fetchFn = vi.fn<typeof fetch>(() => Promise.resolve(okResponse()));
	const store = createModelsDevStore(fetchFn, storage, () => time);
	return { store, storage, fetchFn, advance: (ms: number) => (time += ms) };
}

const HOUR = 3_600_000;

describe('trim', () => {
	it('keeps providers with api and a supported npm, maps kind, sorts by name', () => {
		expect(trim(RAW).map(({ id, kind, api }) => [id, kind, api])).toEqual([
			['alpha', 'anthropic', 'https://alpha.example'],
			['router', 'openai_compatible', 'https://router.example/v1'],
			['zeta', 'openai_compatible', 'https://zeta.example/v1']
		]);
	});

	it('keeps only id, name and type of each model', () => {
		expect(trim(RAW).find((p) => p.id === 'zeta')?.models).toEqual([
			{ id: 'z1', name: 'Z One', type: 'chat' }
		]);
	});
});

describe('createModelsDevStore', () => {
	it('fetches once and serves later loads from cache within the TTL', async () => {
		const { store, fetchFn, advance } = setup();
		await store.load();
		advance(HOUR);
		await store.load();
		expect(fetchFn).toHaveBeenCalledTimes(1);
		expect(store.status).toBe('ready');
		expect(store.presets.map((p) => p.id)).toEqual(['alpha', 'router', 'zeta']);
	});

	it('reuses the persisted cache in a new store', async () => {
		const { storage, fetchFn } = setup();
		const first = createModelsDevStore(fetchFn, storage, () => 1_000_000);
		await first.load();
		const second = createModelsDevStore(fetchFn, storage, () => 1_000_000 + HOUR);
		await second.load();
		expect(fetchFn).toHaveBeenCalledTimes(1);
		expect(second.presets).toHaveLength(3);
	});

	it('revalidates with If-None-Match after the TTL and keeps the cache on 304', async () => {
		const { store, fetchFn, advance } = setup();
		await store.load();
		advance(25 * HOUR);
		fetchFn.mockResolvedValueOnce(new Response(null, { status: 304 }));
		await store.load();
		expect(fetchFn).toHaveBeenCalledTimes(2);
		expect(new Headers(fetchFn.mock.calls[1][1]?.headers).get('If-None-Match')).toBe('"v1"');
		expect(store.status).toBe('ready');
		expect(store.presets).toHaveLength(3);
		advance(HOUR);
		await store.load();
		expect(fetchFn).toHaveBeenCalledTimes(2);
	});

	it('dedupes concurrent loads', async () => {
		const { store, fetchFn } = setup();
		await Promise.all([store.load(), store.load()]);
		expect(fetchFn).toHaveBeenCalledTimes(1);
	});

	it('records a failed fetch without throwing and does not retry within 5 minutes', async () => {
		const { store, fetchFn, advance } = setup();
		fetchFn.mockRejectedValueOnce(new TypeError('offline'));
		await store.load();
		expect(store.status).toBe('error');
		expect(store.presets).toEqual([]);
		advance(4 * 60_000);
		await store.load();
		expect(fetchFn).toHaveBeenCalledTimes(1);
		advance(2 * 60_000);
		await store.load();
		expect(fetchFn).toHaveBeenCalledTimes(2);
		expect(store.status).toBe('ready');
	});

	it('ignores a corrupt cache and refetches', async () => {
		const { store, storage, fetchFn } = setup();
		storage.data.set('modelsdev', '{}');
		await store.load();
		expect(fetchFn).toHaveBeenCalledTimes(1);
		expect(store.presets).toHaveLength(3);
	});

	it('falls back to the stale cache when revalidation fails', async () => {
		const { store, fetchFn, advance } = setup();
		await store.load();
		advance(25 * HOUR);
		fetchFn.mockRejectedValueOnce(new TypeError('offline'));
		await store.load();
		expect(store.status).toBe('error');
		expect(store.presets).toHaveLength(3);
	});

	it('treats a non-ok response as an error', async () => {
		const { store, fetchFn } = setup();
		fetchFn.mockResolvedValueOnce(new Response('x', { status: 500 }));
		await store.load();
		expect(store.status).toBe('error');
	});

	it('tolerates storage writes failing and still avoids refetching', async () => {
		const { store, fetchFn, advance } = setup({ failWrites: true });
		await store.load();
		advance(HOUR);
		await store.load();
		expect(store.status).toBe('ready');
		expect(fetchFn).toHaveBeenCalledTimes(1);
	});
});

describe('modelSuggestions', () => {
	const preset = (api: string, models: ModelsDevPreset['models']): ModelsDevPreset => ({
		id: 'p',
		name: 'P',
		api,
		kind: 'openai_compatible',
		models
	});
	const presets = [
		preset('https://API.Example.com/v1', [
			{ id: 'chat-a', name: 'A' },
			{ id: 'chat-b', name: 'B', type: 'chat' },
			{ id: 'embed', name: 'E', type: 'embedding' },
			{ id: 'whisper', name: 'W', type: 'transcription' },
			{ id: 'tts', name: 'T', type: 'speech' }
		])
	];

	it('matches base URL ignoring case and trailing slash', () => {
		expect(modelSuggestions(presets, 'https://api.example.com/v1/', 'chat')).toEqual([
			'chat-a',
			'chat-b'
		]);
	});

	it('suggests nothing without a match', () => {
		expect(modelSuggestions(presets, 'https://other.example.com', 'chat')).toEqual([]);
		expect(modelSuggestions(presets, '', 'chat')).toEqual([]);
	});

	it('suggests only transcription models for transcription', () => {
		expect(modelSuggestions(presets, 'https://api.example.com/v1', 'transcription')).toEqual([
			'whisper'
		]);
	});
});
