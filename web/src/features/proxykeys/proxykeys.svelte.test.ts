import { describe, expect, it, vi } from 'vitest';
import { createProxyKeysStore, type ProxyKey, parseModels } from './proxykeys.svelte';

const SECRET = 'sk-apx-DISTINCTIVE-secret';

const proxyKey = (over: Partial<ProxyKey> = {}): ProxyKey => ({
	id: 'k1',
	name: 'ci',
	allowedModels: [],
	createdAt: '2026-11-11T10:00:00Z',
	lastUsedAt: null,
	revokedAt: null,
	status: 'active',
	...over
});

const OPS = /\b(createProxyKey|setProxyKeyAllowedModels|revokeProxyKey|proxyKeys)\b/;

function setup(keys: ProxyKey[] = [], override?: (op: string) => Response | undefined) {
	const calls: { op: string; variables: Record<string, unknown> }[] = [];
	const authFetch = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
		const { query, variables } = JSON.parse(init?.body as string);
		const op = OPS.exec(query)?.[1] ?? '';
		calls.push({ op, variables });
		const data =
			op === 'proxyKeys'
				? { proxyKeys: keys }
				: op === 'createProxyKey'
					? { createProxyKey: { secret: SECRET, key: { id: 'k2' } } }
					: { [op]: { id: 'x' } };
		return Promise.resolve(override?.(op) ?? Response.json({ data }));
	});
	const writeText = vi.fn((_text: string) => Promise.resolve());
	return { store: createProxyKeysStore(authFetch, { writeText }), calls, writeText };
}

describe('parseModels', () => {
	it.each([
		['fast\nsmart', ['fast', 'smart']],
		['fast, smart ,,\n  ', ['fast', 'smart']],
		['a,a\na', ['a']],
		['', []],
		['   \n , ', []]
	])('%j -> %j', (text, expected) => {
		expect(parseModels(text)).toEqual(expected);
	});
});

describe('load', () => {
	it('exposes the key list', async () => {
		const { store } = setup([proxyKey()]);
		await store.load();
		expect(store.keys.map((k) => k.name)).toEqual(['ci']);
		expect(store.error).toBe('');
	});

	it.each([
		[401, 'Signed out'],
		[403, 'Access denied'],
		[500, 'Request failed']
	])('maps HTTP %i to %j and never rejects', async (status, message) => {
		const { store } = setup([], () => new Response('', { status }));
		await store.load();
		expect(store.error).toBe(message);
	});

	it('shows GraphQL error messages and survives network failure', async () => {
		const gqlError = setup([], () => Response.json({ errors: [{ message: 'nope' }] }));
		await gqlError.store.load();
		expect(gqlError.store.error).toBe('nope');

		const store = createProxyKeysStore(() => Promise.reject(new Error('offline')));
		await store.load();
		expect(store.error).toBe('Request failed');
	});
});

describe('create', () => {
	it('sends the input, reveals the secret and reloads the list', async () => {
		const { store, calls } = setup([proxyKey()]);
		const ok = await store.create('ci', ['fast']);
		expect(ok).toBe(true);
		expect(calls[0]).toEqual({
			op: 'createProxyKey',
			variables: { input: { name: 'ci', allowedModels: ['fast'] } }
		});
		expect(calls.at(-1)?.op).toBe('proxyKeys');
		expect(store.revealed).toEqual({ name: 'ci', secret: SECRET });
		expect(store.keys).toHaveLength(1);
	});

	it('keeps the secret out of the list state', async () => {
		const { store } = setup([proxyKey()]);
		await store.create('ci', []);
		expect(JSON.stringify(store.keys)).not.toContain(SECRET);
	});

	it('reveals nothing and reports failure on error', async () => {
		const { store } = setup([], (op) =>
			op === 'createProxyKey'
				? Response.json({ errors: [{ message: 'name already exists' }] })
				: undefined
		);
		expect(await store.create('ci', [])).toBe(false);
		expect(store.revealed).toBeNull();
		expect(store.error).toBe('name already exists');
	});
});

describe('reveal', () => {
	it('dismissReveal clears the secret and the copied flag', async () => {
		const { store } = setup();
		await store.create('ci', []);
		await store.copy();
		store.dismissReveal();
		expect(store.revealed).toBeNull();
		expect(store.copied).toBe(false);
	});

	it('copy writes the exact secret through the injected clipboard', async () => {
		const { store, writeText } = setup();
		await store.create('ci', []);
		expect(await store.copy()).toBe(true);
		expect(writeText).toHaveBeenCalledWith(SECRET);
		expect(store.copied).toBe(true);
	});

	it('copy reports failure without rejecting when the clipboard throws', async () => {
		const store = createProxyKeysStore(
			() =>
				Promise.resolve(
					Response.json({ data: { createProxyKey: { secret: SECRET }, proxyKeys: [] } })
				),
			{ writeText: () => Promise.reject(new Error('denied')) }
		);
		await store.create('ci', []);
		expect(await store.copy()).toBe(false);
		expect(store.copied).toBe(false);
	});

	it('copy resolves with an error when the clipboard throws synchronously', async () => {
		const store = createProxyKeysStore(
			() =>
				Promise.resolve(
					Response.json({ data: { createProxyKey: { secret: SECRET }, proxyKeys: [] } })
				),
			{
				writeText: () => {
					throw new TypeError('clipboard unavailable');
				}
			}
		);
		await store.create('ci', []);
		expect(await store.copy()).toBe(false);
		expect(store.copied).toBe(false);
		expect(store.error).toBe('Copy failed, select and copy the key manually');
	});

	it('copy without a revealed key does nothing', async () => {
		const { store, writeText } = setup();
		expect(await store.copy()).toBe(false);
		expect(writeText).not.toHaveBeenCalled();
	});
});

describe('mutations', () => {
	it('revoke sends the id and reloads', async () => {
		const { store, calls } = setup([proxyKey()]);
		expect(await store.revoke('k1')).toBe(true);
		expect(calls.map((c) => c.op)).toEqual(['revokeProxyKey', 'proxyKeys']);
		expect(calls[0].variables).toEqual({ id: 'k1' });
	});

	it('setAllowedModels sends the models and reloads', async () => {
		const { store, calls } = setup([proxyKey()]);
		expect(await store.setAllowedModels('k1', ['a', 'b'])).toBe(true);
		expect(calls.map((c) => c.op)).toEqual(['setProxyKeyAllowedModels', 'proxyKeys']);
		expect(calls[0].variables).toEqual({ id: 'k1', models: ['a', 'b'] });
	});

	it('records errors and returns false', async () => {
		const { store, calls } = setup([], () => new Response('', { status: 403 }));
		expect(await store.revoke('k1')).toBe(false);
		expect(store.error).toBe('Access denied');
		expect(calls).toHaveLength(1);
	});
});
