import { SvelteDate } from 'svelte/reactivity';
import { createProxyKeysStore } from '../proxykeys/proxykeys.svelte';
import { createVirtualModelsStore } from '../virtualmodels/virtualmodels.svelte';

export interface UsageRecord {
	id: string;
	createdAt: string;
	proxyKeyName: string;
	virtualModel: string;
	providerName: string | null;
	keyName: string | null;
	model: string | null;
	status: number;
	durationMs: number;
	inputTokens: number | null;
	outputTokens: number | null;
	cacheReadTokens: number | null;
	cacheCreationTokens: number | null;
	steps: number;
	affinity: string | null;
}

export interface UsageTotal {
	label: string;
	requests: number;
	inputTokens: number;
	outputTokens: number;
	cacheReadTokens: number;
	cacheCreationTokens: number;
	errors: number;
}

export interface LiveKey {
	providerId: string;
	providerName: string;
	keyId: string;
	keyName: string;
	enabled: boolean;
	maxInFlight: number;
	inFlight: number;
	free: number;
	cooldownUntil: string | null;
	flag: string;
}

export const GROUPS = ['DAY', 'MODEL', 'PROVIDER'] as const;
export type Group = (typeof GROUPS)[number];
export const GROUP_LABELS: Record<Group, string> = {
	DAY: 'Day',
	MODEL: 'Model',
	PROVIDER: 'Provider'
};

export interface Filter {
	proxyKeyId: string;
	virtualModel: string;
}

export type AuthFetch = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;
export interface Timers {
	setInterval: (fn: () => void, ms: number) => unknown;
	clearInterval: (handle: unknown) => void;
}

type Section = 'records' | 'totals' | 'live';

interface GraphQLBody {
	data?: Record<string, unknown>;
	errors?: { message: string }[];
}

export const PAGE_SIZE = 50;
export const LIVE_INTERVAL_MS = 5000;

const REQUEST_FAILED = 'Request failed';
const HTTP_ERRORS: Record<number, string> = { 401: 'Signed out', 403: 'Access denied' };

const RECORDS_QUERY = `query($filter: UsageFilter, $limit: Int, $offset: Int) {
  usageRecords(filter: $filter, limit: $limit, offset: $offset) { total items {
    id createdAt proxyKeyName virtualModel providerName keyName model status durationMs
    inputTokens outputTokens cacheReadTokens cacheCreationTokens steps affinity } } }`;
const TOTALS_QUERY = `query($filter: UsageFilter, $groupBy: UsageGroup!) {
  usageTotals(filter: $filter, groupBy: $groupBy) { label requests inputTokens outputTokens
    cacheReadTokens cacheCreationTokens errors } }`;
const LIVE_QUERY =
	'{ liveStatus { providerId providerName keyId keyName enabled maxInFlight inFlight free cooldownUntil flag } }';

export type BadgeVariant = 'default' | 'secondary' | 'destructive' | 'outline';

export const tokenText = (n: number | null): string => (n === null ? '-' : String(n));

export const statusVariant = (status: number): BadgeVariant =>
	status === 499 ? 'secondary' : status >= 400 ? 'destructive' : 'default';

export function keyState(k: LiveKey): { label: string; variant: BadgeVariant } {
	if (!k.enabled) return { label: 'disabled', variant: 'secondary' };
	if (k.cooldownUntil) {
		const until = new SvelteDate(k.cooldownUntil).toLocaleTimeString([], { hour12: false });
		return { label: `cooling down until ${until}`, variant: 'destructive' };
	}
	return k.free === 0
		? { label: 'busy', variant: 'outline' }
		: { label: 'free', variant: 'default' };
}

export function createUsageStore(
	authFetch: AuthFetch = (input, init) => fetch(input, init),
	timers: Timers = {
		setInterval: (fn, ms) => setInterval(fn, ms),
		clearInterval: (handle) => clearInterval(handle as ReturnType<typeof setInterval>)
	},
	liveIntervalMs = LIVE_INTERVAL_MS
) {
	const proxyKeys = createProxyKeysStore(authFetch);
	const virtualModels = createVirtualModelsStore(authFetch);

	let records = $state<UsageRecord[]>([]);
	let total = $state(0);
	let page = $state(0);
	let filter = $state<Filter>({ proxyKeyId: '', virtualModel: '' });
	let groupBy = $state<Group>('DAY');
	let totals = $state<UsageTotal[]>([]);
	let live = $state<LiveKey[]>([]);
	const errors = $state<Record<Section, string>>({ records: '', totals: '', live: '' });
	const loading = $state<Record<Section, boolean>>({ records: false, totals: false, live: false });
	let liveTimer: unknown = null;
	// Only the latest request of a section may write state.
	const latest: Record<Section, number> = { records: 0, totals: 0, live: 0 };

	// Returns the GraphQL data, or null after recording why it failed.
	async function request(
		section: Section,
		query: string,
		variables?: Record<string, unknown>
	): Promise<Record<string, unknown> | null> {
		const id = ++latest[section];
		loading[section] = true;
		const res = await authFetch('/graphql', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ query, variables })
		}).catch(() => null);
		const body = res?.ok ? ((await res.json().catch(() => null)) as GraphQLBody | null) : null;
		if (id !== latest[section]) return null;
		loading[section] = false;
		if (!res?.ok) {
			errors[section] = (res && HTTP_ERRORS[res.status]) ?? REQUEST_FAILED;
			return null;
		}
		if (body?.errors?.length) {
			errors[section] = body.errors[0].message;
			return null;
		}
		errors[section] = '';
		return body?.data ?? null;
	}

	const filterVariable = () => ({
		proxyKeyId: filter.proxyKeyId || null,
		virtualModel: filter.virtualModel || null
	});

	async function loadRecords(): Promise<void> {
		const data = await request('records', RECORDS_QUERY, {
			filter: filterVariable(),
			limit: PAGE_SIZE,
			offset: page * PAGE_SIZE
		});
		if (!data) return;
		const result = data.usageRecords as { items: UsageRecord[]; total: number };
		records = result.items;
		total = result.total;
	}

	async function loadTotals(): Promise<void> {
		const data = await request('totals', TOTALS_QUERY, { filter: filterVariable(), groupBy });
		if (data) totals = data.usageTotals as UsageTotal[];
	}

	async function refreshLive(): Promise<void> {
		const data = await request('live', LIVE_QUERY);
		if (data) live = data.liveStatus as LiveKey[];
	}

	const reload = () => Promise.all([loadRecords(), loadTotals()]).then(() => undefined);

	const lastPage = () => Math.max(Math.ceil(total / PAGE_SIZE) - 1, 0);

	function stopLive(): void {
		if (liveTimer !== null) timers.clearInterval(liveTimer);
		liveTimer = null;
	}

	return {
		proxyKeys,
		virtualModels,
		get records() {
			return records;
		},
		get total() {
			return total;
		},
		get page() {
			return page;
		},
		get filter() {
			return filter;
		},
		get groupBy() {
			return groupBy;
		},
		get totals() {
			return totals;
		},
		get live() {
			return live;
		},
		get errors() {
			return errors;
		},
		get loading() {
			return loading;
		},
		get hasPrev() {
			return page > 0;
		},
		get hasNext() {
			return page < lastPage();
		},
		load: async () => {
			await Promise.all([proxyKeys.load(), virtualModels.load(), reload()]);
		},
		setFilter: (patch: Partial<Filter>) => {
			filter = { ...filter, ...patch };
			page = 0;
			return reload();
		},
		setPage: (index: number) => {
			page = Math.min(Math.max(index, 0), lastPage());
			return loadRecords();
		},
		setGroupBy: (group: Group) => {
			groupBy = group;
			return loadTotals();
		},
		refreshLive,
		startLive: () => {
			stopLive();
			void refreshLive();
			liveTimer = timers.setInterval(() => void refreshLive(), liveIntervalMs);
		},
		stopLive
	};
}
