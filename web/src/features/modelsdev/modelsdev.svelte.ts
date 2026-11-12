import type { ProviderKind } from '#src/features/providers/presets.js';

export const MODELS_DEV_KEY = Symbol('modelsDev');

export type ModelsDevStatus = 'idle' | 'loading' | 'ready' | 'error';

export interface ModelsDevModel {
	id: string;
	name: string;
	type?: string;
}

export interface ModelsDevPreset {
	id: string;
	name: string;
	api: string;
	kind: ProviderKind;
	models: ModelsDevModel[];
}

interface RawProvider {
	id: string;
	name: string;
	api?: string;
	npm?: string;
	models?: Record<string, ModelsDevModel>;
}

interface Cache {
	at: number;
	etag: string | null;
	presets: ModelsDevPreset[];
}

type StorageLike = Pick<Storage, 'getItem' | 'setItem'>;

const URL = 'https://models.dev/api.json';
const STORAGE_KEY = 'modelsdev';
const TTL_MS = 24 * 60 * 60 * 1000;
const RETRY_MS = 5 * 60 * 1000;
const ANTHROPIC_NPM = '@ai-sdk/anthropic';
const SUPPORTED_NPM = [
	ANTHROPIC_NPM,
	'@ai-sdk/openai-compatible',
	'@ai-sdk/openai',
	'@openrouter/ai-sdk-provider'
];
const NON_CHAT_TYPES = new Set([
	'transcription',
	'embedding',
	'image',
	'speech',
	'video',
	'realtime',
	'reranking'
]);

export const trim = (raw: Record<string, RawProvider>): ModelsDevPreset[] =>
	Object.values(raw)
		.flatMap((p) =>
			p.api && p.npm && SUPPORTED_NPM.includes(p.npm)
				? [
						{
							id: p.id,
							name: p.name,
							api: p.api,
							kind:
								p.npm === ANTHROPIC_NPM ? ('anthropic' as const) : ('openai_compatible' as const),
							models: Object.values(p.models ?? {}).map(({ id, name, type }) => ({
								id,
								name,
								type
							}))
						}
					]
				: []
		)
		.sort((a, b) => a.name.localeCompare(b.name));

const normalizeUrl = (url: string) => url.trim().toLowerCase().replace(/\/+$/, '');

export function modelSuggestions(
	presets: ModelsDevPreset[],
	baseUrl: string,
	kind: 'chat' | 'transcription'
): string[] {
	const url = normalizeUrl(baseUrl);
	const models = presets.find((p) => normalizeUrl(p.api) === url)?.models ?? [];
	return models
		.filter((m) =>
			kind === 'transcription' ? m.type === 'transcription' : !NON_CHAT_TYPES.has(m.type ?? '')
		)
		.map((m) => m.id);
}

export function createModelsDevStore(
	fetchFn: typeof fetch = (input, init) => fetch(input, init),
	storage: StorageLike = globalThis.localStorage,
	now: () => number = Date.now
) {
	let status = $state<ModelsDevStatus>('idle');
	let presets = $state<ModelsDevPreset[]>([]);
	let memory: Cache | null = null;
	let failedAt = -Infinity;
	let inflight: Promise<void> | null = null;

	const readCache = (): Promise<Cache | null> =>
		Promise.resolve()
			.then(() => JSON.parse(storage.getItem(STORAGE_KEY) ?? 'null') as Cache | null)
			.then((cache) => (Array.isArray(cache?.presets) ? cache : null))
			.catch(() => null);

	const writeCache = (cache: Cache): Promise<void> =>
		Promise.resolve()
			.then(() => storage.setItem(STORAGE_KEY, JSON.stringify(cache)))
			.catch(() => undefined);

	async function use(cache: Cache): Promise<void> {
		memory = cache;
		presets = cache.presets;
		status = 'ready';
		await writeCache(cache);
	}

	async function refresh(): Promise<void> {
		const cached = memory ?? (await readCache());
		if (cached && now() - cached.at < TTL_MS) {
			memory = cached;
			presets = cached.presets;
			status = 'ready';
			return;
		}
		if (now() - failedAt < RETRY_MS) {
			status = 'error';
			return;
		}
		const res = await fetchFn(URL, {
			headers: cached?.etag ? { 'If-None-Match': cached.etag } : {}
		}).catch(() => null);
		if (cached && res?.status === 304) return use({ ...cached, at: now() });
		const raw = res?.ok
			? ((await res.json().catch(() => null)) as Record<string, RawProvider> | null)
			: null;
		if (!res || !raw) {
			failedAt = now();
			presets = cached?.presets ?? [];
			status = 'error';
			return;
		}
		await use({ at: now(), etag: res.headers.get('etag'), presets: trim(raw) });
	}

	function load(): Promise<void> {
		inflight ??= (async () => {
			status = 'loading';
			await refresh();
		})().finally(() => {
			inflight = null;
		});
		return inflight;
	}

	return {
		get status() {
			return status;
		},
		get presets() {
			return presets;
		},
		load
	};
}
