export const KINDS = ['chat', 'transcription'] as const;
export type Kind = (typeof KINDS)[number];

export const STRATEGIES = [
	'round_robin',
	'weighted_round_robin',
	'priority_overflow',
	'least_in_flight'
] as const;
export type Strategy = (typeof STRATEGIES)[number];

export const STRATEGY_LABELS: Record<Strategy, string> = {
	round_robin: 'Round robin',
	weighted_round_robin: 'Weighted round robin',
	priority_overflow: 'Priority overflow',
	least_in_flight: 'Least in flight'
};

export const KIND_LABELS: Record<Kind, string> = { chat: 'Chat', transcription: 'Transcription' };

export type EntryStatus = 'ok' | 'provider_missing' | 'provider_disabled' | 'provider_incompatible';

export const STATUS_REASONS: Record<EntryStatus, string> = {
	ok: '',
	provider_missing: 'Provider was deleted',
	provider_disabled: 'Provider is disabled',
	provider_incompatible: 'Provider cannot serve this kind (needs OpenAI compatible)'
};

export interface ModelEntry {
	id: string;
	providerId: string;
	providerName: string;
	model: string;
	weight: number;
	status: EntryStatus;
}

export interface VirtualModel {
	id: string;
	name: string;
	kind: Kind;
	strategy: Strategy;
	cacheAffinity: boolean;
	tiers: { priority: number; entries: ModelEntry[] }[];
	createdAt: string;
	updatedAt: string;
}

export interface ProviderOption {
	id: string;
	name: string;
	kind: string;
	enabled: boolean;
}

export interface DraftEntry {
	providerId: string;
	model: string;
	weight: number;
}
export interface DraftTier {
	entries: DraftEntry[];
}
export interface Draft {
	name: string;
	kind: Kind;
	strategy: Strategy;
	cacheAffinity: boolean;
	tiers: DraftTier[];
}

export type AuthFetch = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

interface GraphQLBody {
	data?: Record<string, unknown>;
	errors?: { message: string }[];
}

const REQUEST_FAILED = 'Request failed';
const HTTP_ERRORS: Record<number, string> = { 401: 'Signed out', 403: 'Access denied' };
const TRANSCRIPTION_PROVIDER_KIND = 'openai_compatible';

const LIST_QUERY = `{ virtualModels { id name kind strategy cacheAffinity createdAt updatedAt
  tiers { priority entries { id providerId providerName model weight status } } }
  providers { id name kind enabled } }`;

const MUTATIONS = {
	createVirtualModel:
		'mutation($input: CreateVirtualModelInput!) { createVirtualModel(input: $input) { id } }',
	updateVirtualModel:
		'mutation($id: ID!, $input: UpdateVirtualModelInput!) { updateVirtualModel(id: $id, input: $input) { id } }',
	deleteVirtualModel: 'mutation($id: ID!) { deleteVirtualModel(id: $id) }'
} as const;

const blankEntry = (): DraftEntry => ({ providerId: '', model: '', weight: 1 });
const blankTier = (): DraftTier => ({ entries: [blankEntry()] });

const emptyDraft = (): Draft => ({
	name: '',
	kind: 'chat',
	strategy: 'round_robin',
	cacheAffinity: true,
	tiers: [blankTier()]
});

const mapTier = (tiers: DraftTier[], index: number, fn: (t: DraftTier) => DraftTier) =>
	tiers.map((t, i) => (i === index ? fn(t) : t));

export const addTier = (tiers: DraftTier[]): DraftTier[] => [...tiers, blankTier()];

export const removeTier = (tiers: DraftTier[], index: number): DraftTier[] =>
	tiers.filter((_, i) => i !== index);

export function moveTier(tiers: DraftTier[], index: number, direction: 'up' | 'down'): DraftTier[] {
	const target = direction === 'up' ? index - 1 : index + 1;
	if (target < 0 || target >= tiers.length) return tiers;
	const out = [...tiers];
	[out[index], out[target]] = [out[target], out[index]];
	return out;
}

export const addEntry = (tiers: DraftTier[], tierIndex: number): DraftTier[] =>
	mapTier(tiers, tierIndex, (t) => ({ entries: [...t.entries, blankEntry()] }));

export const removeEntry = (tiers: DraftTier[], tierIndex: number, entryIndex: number) =>
	mapTier(tiers, tierIndex, (t) => ({ entries: t.entries.filter((_, i) => i !== entryIndex) }));

export const updateEntry = (
	tiers: DraftTier[],
	tierIndex: number,
	entryIndex: number,
	patch: Partial<DraftEntry>
) =>
	mapTier(tiers, tierIndex, (t) => ({
		entries: t.entries.map((e, i) => (i === entryIndex ? { ...e, ...patch } : e))
	}));

// Disabled providers are never offered (the backend rejects them); transcription also needs OpenAI-compatible.
export const providerOptionsFor = (kind: Kind, providers: ProviderOption[]): ProviderOption[] =>
	providers.filter(
		(p) => p.enabled && (kind !== 'transcription' || p.kind === TRANSCRIPTION_PROVIDER_KIND)
	);

export const draftFromModel = (m: VirtualModel): Draft => ({
	name: m.name,
	kind: m.kind,
	strategy: m.strategy,
	cacheAffinity: m.cacheAffinity,
	tiers: m.tiers.map((t) => ({
		entries: t.entries.map((e) => ({ providerId: e.providerId, model: e.model, weight: e.weight }))
	}))
});

// Names are immutable, so the update input never carries one.
export function toInput(draft: Draft, editing: boolean) {
	const { name, ...settings } = draft;
	return editing ? settings : { name, ...settings };
}

export function createVirtualModelsStore(
	authFetch: AuthFetch = (input, init) => fetch(input, init)
) {
	let models = $state<VirtualModel[]>([]);
	let providers = $state<ProviderOption[]>([]);
	let error = $state('');
	let loading = $state(false);
	let draft = $state<Draft>(emptyDraft());
	let editingId = $state<string | null>(null);
	let editingNames = $state<Record<string, string>>({});

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
			models = data.virtualModels as VirtualModel[];
			providers = data.providers as ProviderOption[];
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

	// Keeps the error: after a save it may be the failure of the reload.
	function resetDraft(): void {
		editingId = null;
		draft = emptyDraft();
		editingNames = {};
	}

	function startCreate(): void {
		error = '';
		resetDraft();
	}

	function startEdit(m: VirtualModel): void {
		error = '';
		editingId = m.id;
		draft = draftFromModel(m);
		editingNames = Object.fromEntries(
			m.tiers.flatMap((t) => t.entries.map((e) => [e.providerId, e.providerName]))
		);
	}

	// A selected provider that is no longer offered keeps its name, flagged.
	function providerLabel(id: string): string {
		if (!id) return 'Provider';
		const found = providers.find((p) => p.id === id);
		const name = found?.name ?? (editingNames[id] || 'Unknown provider');
		return providerOptionsFor(draft.kind, providers).some((p) => p.id === id)
			? name
			: `${name} (unavailable)`;
	}

	function updateDraft(patch: Partial<Draft>): void {
		draft = { ...draft, ...patch };
		if (patch.kind) clearIncompatibleProviders();
	}

	function clearIncompatibleProviders(): void {
		const allowed = providerOptionsFor(draft.kind, providers).map((p) => p.id);
		draft.tiers = draft.tiers.map((t) => ({
			entries: t.entries.map((e) => (allowed.includes(e.providerId) ? e : { ...e, providerId: '' }))
		}));
	}

	async function submit(): Promise<boolean> {
		const input = toInput(draft, editingId !== null);
		const ok = editingId
			? await mutate('updateVirtualModel', { id: editingId, input })
			: await mutate('createVirtualModel', { input });
		if (ok) resetDraft();
		return ok;
	}

	return {
		get models() {
			return models;
		},
		get providers() {
			return providers;
		},
		get providerOptions() {
			return providerOptionsFor(draft.kind, providers);
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
		resetDraft,
		startCreate,
		startEdit,
		providerLabel,
		updateDraft,
		submit,
		clearError: () => {
			error = '';
		},
		addTier: () => {
			draft.tiers = addTier(draft.tiers);
		},
		removeTier: (index: number) => {
			draft.tiers = removeTier(draft.tiers, index);
		},
		moveTier: (index: number, direction: 'up' | 'down') => {
			draft.tiers = moveTier(draft.tiers, index, direction);
		},
		addEntry: (tierIndex: number) => {
			draft.tiers = addEntry(draft.tiers, tierIndex);
		},
		removeEntry: (tierIndex: number, entryIndex: number) => {
			draft.tiers = removeEntry(draft.tiers, tierIndex, entryIndex);
		},
		updateEntry: (tierIndex: number, entryIndex: number, patch: Partial<DraftEntry>) => {
			draft.tiers = updateEntry(draft.tiers, tierIndex, entryIndex, patch);
		},
		remove: (id: string) => mutate('deleteVirtualModel', { id })
	};
}
