import { describe, expect, it, vi } from 'vitest';
import {
	createUsageStore,
	keyState,
	type LiveKey,
	statusVariant,
	type Timers,
	tokenText,
	type UsageRecord
} from './usage.svelte';

const record = (over: Partial<UsageRecord> = {}): UsageRecord => ({
	id: 'r1',
	createdAt: '2026-11-11T10:00:00Z',
	proxyKeyName: 'ci',
	virtualModel: 'fast',
	providerName: 'p',
	keyName: 'k',
	model: 'm',
	status: 200,
	durationMs: 12,
	inputTokens: null,
	outputTokens: 5,
	cacheReadTokens: null,
	cacheCreationTokens: null,
	steps: 1,
	affinity: null,
	...over
});

const OPS = /\b(usageRecords|usageTotals|liveStatus|proxyKeys|virtualModels)\b/;

function setup(total = 120, override?: (op: string) => Response | undefined) {
	const calls: { op: string; variables: Record<string, unknown> }[] = [];
	const authFetch = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
		const { query, variables } = JSON.parse(init?.body as string);
		const op = OPS.exec(query)?.[1] ?? '';
		calls.push({ op, variables });
		const data: Record<string, unknown> = {
			usageRecords: { total, items: [record()] },
			usageTotals: [],
			liveStatus: [],
			proxyKeys: [],
			virtualModels: [],
			providers: []
		};
		return Promise.resolve(override?.(op) ?? Response.json({ data }));
	});
	const handles: { fn: () => void; ms: number; cleared: boolean }[] = [];
	const timers: Timers = {
		setInterval: (fn, ms) => handles.push({ fn, ms, cleared: false }) - 1,
		clearInterval: (h) => {
			handles[h as number].cleared = true;
		}
	};
	const ofOp = (op: string) => calls.filter((c) => c.op === op);
	return { store: createUsageStore(authFetch, timers), calls, ofOp, handles };
}

describe('records', () => {
	it('loads the first page with limit and offset, preserving null tokens', async () => {
		const { store, ofOp } = setup();
		await store.load();
		expect(ofOp('usageRecords')[0].variables).toEqual({
			filter: { proxyKeyId: null, virtualModel: null },
			limit: 50,
			offset: 0
		});
		expect(store.total).toBe(120);
		expect(store.records[0].inputTokens).toBeNull();
		expect(store.records[0].outputTokens).toBe(5);
	});

	it('pages within bounds', async () => {
		const { store, ofOp } = setup(120);
		await store.load();
		expect(store.hasPrev).toBe(false);
		await store.setPage(1);
		expect(ofOp('usageRecords').at(-1)?.variables.offset).toBe(50);
		await store.setPage(99);
		expect(store.page).toBe(2);
		expect(store.hasNext).toBe(false);
		await store.setPage(-3);
		expect(store.page).toBe(0);
	});

	it('resets to the first page and reloads records and totals on filter change', async () => {
		const { store, ofOp } = setup(120);
		await store.load();
		await store.setPage(2);
		await store.setFilter({ proxyKeyId: 'k1' });
		expect(store.page).toBe(0);
		expect(ofOp('usageRecords').at(-1)?.variables).toMatchObject({
			filter: { proxyKeyId: 'k1', virtualModel: null },
			offset: 0
		});
		expect(ofOp('usageTotals').at(-1)?.variables.filter).toEqual({
			proxyKeyId: 'k1',
			virtualModel: null
		});
	});

	it('ignores a response superseded by a newer request', async () => {
		const answers: ((r: Response) => void)[] = [];
		const authFetch = vi.fn(() => new Promise<Response>((resolve) => answers.push(resolve)));
		const store = createUsageStore(authFetch);
		const stale = store.setPage(0);
		const fresh = store.setPage(0);
		const reply = (total: number) =>
			Response.json({ data: { usageRecords: { total, items: [] } } });
		answers[1](reply(2));
		await fresh;
		answers[0](reply(1));
		await stale;
		expect(store.total).toBe(2);
	});
});

describe('totals', () => {
	it('sends the groupBy variable', async () => {
		const { store, ofOp } = setup();
		await store.setGroupBy('MODEL');
		expect(store.groupBy).toBe('MODEL');
		expect(ofOp('usageTotals')[0].variables.groupBy).toBe('MODEL');
	});
});

describe('errors', () => {
	it.each([
		['GraphQL error', () => Response.json({ errors: [{ message: 'nope' }] }), 'nope'],
		['HTTP 401', () => new Response('', { status: 401 }), 'Signed out'],
		['HTTP 500', () => new Response('', { status: 500 }), 'Request failed']
	])('%s sets the section error without throwing', async (_name, respond, message) => {
		const { store } = setup(1, (op) => (op === 'usageRecords' ? respond() : undefined));
		await store.load();
		expect(store.errors.records).toBe(message);
		expect(store.errors.totals).toBe('');
	});

	it('survives network failure', async () => {
		const store = createUsageStore(() => Promise.reject(new Error('offline')));
		await store.refreshLive();
		expect(store.errors.live).toBe('Request failed');
	});
});

describe('live status', () => {
	it('polls every 5s and stops cleanly', async () => {
		const { store, ofOp, handles } = setup();
		store.startLive();
		expect(handles).toHaveLength(1);
		expect(handles[0].ms).toBe(5000);
		await vi.waitFor(() => expect(ofOp('liveStatus')).toHaveLength(1));
		handles[0].fn();
		await vi.waitFor(() => expect(ofOp('liveStatus')).toHaveLength(2));
		store.stopLive();
		expect(handles[0].cleared).toBe(true);
	});

	it('restarting does not leak the previous timer', () => {
		const { store, handles } = setup();
		store.startLive();
		store.startLive();
		expect(handles.map((h) => h.cleared)).toEqual([true, false]);
	});

	it('stores the snapshot', async () => {
		const key: LiveKey = {
			providerId: 'p',
			providerName: 'P',
			keyId: 'k',
			keyName: 'K',
			enabled: true,
			maxInFlight: 2,
			inFlight: 1,
			free: 1,
			cooldownUntil: null,
			flag: ''
		};
		const { store } = setup(1, (op) =>
			op === 'liveStatus' ? Response.json({ data: { liveStatus: [key] } }) : undefined
		);
		await store.refreshLive();
		expect(store.live).toEqual([key]);
	});
});

describe('helpers', () => {
	it('renders null tokens as a dash and keeps zero', () => {
		expect([tokenText(null), tokenText(0), tokenText(7)]).toEqual(['-', '0', '7']);
	});

	it('maps status to badge variants', () => {
		expect([200, 499, 429, 502].map(statusVariant)).toEqual([
			'default',
			'secondary',
			'destructive',
			'destructive'
		]);
	});

	it('derives key state with disabled first, then cooldown, busy, free', () => {
		const base: LiveKey = {
			providerId: 'p',
			providerName: 'P',
			keyId: 'k',
			keyName: 'K',
			enabled: true,
			maxInFlight: 2,
			inFlight: 0,
			free: 2,
			cooldownUntil: null,
			flag: ''
		};
		expect(keyState({ ...base, enabled: false, cooldownUntil: '2026-11-11T10:00:00Z' }).label).toBe(
			'disabled'
		);
		expect(keyState({ ...base, cooldownUntil: '2026-11-11T10:00:00Z' }).label).toMatch(
			/^cooling down until \d\d:\d\d:\d\d$/
		);
		expect(keyState({ ...base, free: 0 }).label).toBe('busy');
		expect(keyState(base).label).toBe('free');
	});
});
