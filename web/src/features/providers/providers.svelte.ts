import { type ModelsDevPreset, presetModelIds } from '#src/features/modelsdev/modelsdev.svelte.js';
import {
	ANTHROPIC_CACHE_AFFINITY_SECONDS,
	DEFAULT_CACHE_AFFINITY_SECONDS,
	type ProviderKind
} from './presets';

export interface ProviderKey {
	id: string;
	name: string;
	enabled: boolean;
	maxInFlight: number;
	weight: number;
	maskedKey: string;
}

export interface Provider {
	id: string;
	name: string;
	kind: ProviderKind;
	baseUrl: string;
	enabled: boolean;
	models: string[];
	cacheAffinitySeconds: number;
	capacity: number;
	keys: ProviderKey[];
}

export interface Draft {
	presetId: string;
	name: string;
	kind: ProviderKind;
	baseUrl: string;
	enabled: boolean;
	// One model id per line, as typed.
	models: string;
	cacheAffinitySeconds: number;
}

export interface NewKey {
	name: string;
	secret: string;
	maxInFlight: number;
	weight: number;
}

export type KeyPatch = Partial<Omit<NewKey, 'name'>> & { name?: string; enabled?: boolean };

export type AuthFetch = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

interface GraphQLBody {
	data?: Record<string, unknown>;
	errors?: { message: string }[];
}

const CUSTOM = 'custom';
const REQUEST_FAILED = 'Request failed';

const LIST_QUERY = `{ providers { id name kind baseUrl enabled models cacheAffinitySeconds capacity
  keys { id name enabled maxInFlight weight maskedKey } } }`;

const MUTATIONS = {
	createProvider: 'mutation($input: ProviderInput!) { createProvider(input: $input) { id } }',
	updateProvider:
		'mutation($id: ID!, $input: ProviderInput!) { updateProvider(id: $id, input: $input) { id } }',
	deleteProvider: 'mutation($id: ID!) { deleteProvider(id: $id) }',
	addProviderKey:
		'mutation($providerId: ID!, $input: ProviderKeyInput!) { addProviderKey(providerId: $providerId, input: $input) { id } }',
	updateProviderKey:
		'mutation($id: ID!, $input: ProviderKeyInput!) { updateProviderKey(id: $id, input: $input) { id } }',
	deleteProviderKey: 'mutation($id: ID!) { deleteProviderKey(id: $id) }'
} as const;

const HTTP_ERRORS: Record<number, string> = { 401: 'Signed out', 403: 'Access denied' };

export const parseModels = (text: string): string[] =>
	text
		.split('\n')
		.map((line) => line.trim())
		.filter((line, i, all) => line && all.indexOf(line) === i);

export const capacityOf = (keys: Pick<ProviderKey, 'enabled' | 'maxInFlight'>[]): number =>
	keys.filter((k) => k.enabled).reduce((sum, k) => sum + k.maxInFlight, 0);

const cacheAffinityFor = (kind: ProviderKind) =>
	kind === 'openai_compatible' ? DEFAULT_CACHE_AFFINITY_SECONDS : ANTHROPIC_CACHE_AFFINITY_SECONDS;

const emptyDraft = (): Draft => ({
	presetId: CUSTOM,
	name: '',
	kind: 'openai_compatible',
	baseUrl: '',
	enabled: true,
	models: '',
	cacheAffinitySeconds: DEFAULT_CACHE_AFFINITY_SECONDS
});

export function createProvidersStore(authFetch: AuthFetch = (input, init) => fetch(input, init)) {
	let providers = $state<Provider[]>([]);
	let error = $state('');
	let loading = $state(false);
	let draft = $state<Draft>(emptyDraft());
	let editingId = $state<string | null>(null);

	// Returns the GraphQL data, or null after recording why it failed.
	async function request(
		query: string,
		variables?: Record<string, unknown>
	): Promise<Record<string, unknown> | null> {
		const res = await authFetch('/graphql', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ query, variables })
		}).catch(() => null);
		if (!res?.ok) {
			error = (res && HTTP_ERRORS[res.status]) ?? REQUEST_FAILED;
			return null;
		}
		const body = (await res.json().catch(() => null)) as GraphQLBody | null;
		if (body?.errors?.length) {
			error = body.errors[0].message;
			return null;
		}
		return body?.data ?? null;
	}

	async function load(): Promise<void> {
		loading = true;
		const data = await request(LIST_QUERY);
		if (data) {
			providers = data.providers as Provider[];
			error = '';
		}
		loading = false;
	}

	async function mutate(
		name: keyof typeof MUTATIONS,
		variables: Record<string, unknown>
	): Promise<boolean> {
		error = '';
		const ok = (await request(MUTATIONS[name], variables)) !== null;
		if (ok) await load();
		return ok;
	}

	function applyPreset(preset?: ModelsDevPreset): void {
		if (!preset) {
			draft.presetId = CUSTOM;
			return;
		}
		draft = {
			...draft,
			presetId: preset.id,
			name: draft.name || preset.name,
			kind: preset.kind,
			baseUrl: preset.api,
			models: presetModelIds(preset).join('\n'),
			cacheAffinitySeconds: cacheAffinityFor(preset.kind)
		};
	}

	function updateDraft(patch: Partial<Draft>): void {
		const { kind } = patch;
		draft = {
			...draft,
			...(kind && kind !== draft.kind && { cacheAffinitySeconds: cacheAffinityFor(kind) }),
			...patch
		};
	}

	function startEdit(p: Provider): void {
		error = '';
		editingId = p.id;
		draft = {
			presetId: CUSTOM,
			name: p.name,
			kind: p.kind,
			baseUrl: p.baseUrl,
			enabled: p.enabled,
			models: p.models.join('\n'),
			cacheAffinitySeconds: p.cacheAffinitySeconds
		};
	}

	function resetDraft(): void {
		error = '';
		editingId = null;
		draft = emptyDraft();
	}

	async function submitDraft(): Promise<boolean> {
		const input = {
			name: draft.name,
			kind: draft.kind,
			baseUrl: draft.baseUrl,
			enabled: draft.enabled,
			models: parseModels(draft.models),
			cacheAffinitySeconds: draft.cacheAffinitySeconds
		};
		const ok = editingId
			? await mutate('updateProvider', { id: editingId, input })
			: await mutate('createProvider', { input });
		if (ok) resetDraft();
		return ok;
	}

	return {
		get providers() {
			return providers;
		},
		get error() {
			return error;
		},
		get loading() {
			return loading;
		},
		get draft() {
			return draft;
		},
		get editingId() {
			return editingId;
		},
		load,
		applyPreset,
		updateDraft,
		startEdit,
		resetDraft,
		clearError: () => {
			error = '';
		},
		submitDraft,
		deleteProvider: (id: string) => mutate('deleteProvider', { id }),
		addKey: (providerId: string, input: NewKey) => mutate('addProviderKey', { providerId, input }),
		// A blank secret means keep the current one.
		updateKey: (id: string, { secret, ...rest }: KeyPatch) =>
			mutate('updateProviderKey', { id, input: secret ? { ...rest, secret } : rest }),
		deleteKey: (id: string) => mutate('deleteProviderKey', { id })
	};
}
