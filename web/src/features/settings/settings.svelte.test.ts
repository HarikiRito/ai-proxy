import { describe, expect, it, vi } from 'vitest';
import { createSettingsStore, type Settings } from './settings.svelte';

const values = (over: Partial<Settings> = {}): Settings => ({
	maxBodyBytes: 26214400,
	requestTimeoutSeconds: 600,
	queueWaitSeconds: 30,
	defaultCooldownSeconds: 60,
	...over
});

function setup(current: Settings, override?: (op: string) => Response | undefined) {
	const calls: { op: string; variables?: Record<string, unknown> }[] = [];
	const authFetch = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
		const { query, variables } = JSON.parse(init?.body as string);
		const op = /\b(updateSettings|settings)\b/.exec(query)?.[1] ?? '';
		calls.push({ op, variables });
		const data =
			op === 'updateSettings' ? { updateSettings: variables.input } : { settings: current };
		return Promise.resolve(override?.(op) ?? Response.json({ data }));
	});
	return { store: createSettingsStore(authFetch), calls };
}

describe('load', () => {
	it('exposes the settings', async () => {
		const { store } = setup(values({ queueWaitSeconds: 5 }));
		await store.load();
		expect(store.settings).toEqual(values({ queueWaitSeconds: 5 }));
		expect(store.error).toBe('');
		expect(store.loading).toBe(false);
	});

	it.each([
		[401, 'Signed out'],
		[403, 'Access denied'],
		[500, 'Request failed']
	])('maps HTTP %i to %j and never rejects', async (status, message) => {
		const { store } = setup(values(), () => new Response('', { status }));
		await store.load();
		expect(store.error).toBe(message);
		expect(store.settings).toBeNull();
	});

	it('survives network failure', async () => {
		const store = createSettingsStore(() => Promise.reject(new Error('offline')));
		await store.load();
		expect(store.error).toBe('Request failed');
	});
});

describe('save', () => {
	it('sends the input and adopts the returned settings', async () => {
		const { store, calls } = setup(values());
		const next = values({ requestTimeoutSeconds: 120 });
		expect(await store.save(next)).toBe(true);
		expect(calls[0]).toEqual({ op: 'updateSettings', variables: { input: next } });
		expect(store.settings).toEqual(next);
	});

	it('shows the server validation message and keeps the old settings', async () => {
		const { store } = setup(values(), (op) =>
			op === 'updateSettings'
				? Response.json({ errors: [{ message: 'queue wait seconds must be between 1 and 600' }] })
				: undefined
		);
		await store.load();
		expect(await store.save(values({ queueWaitSeconds: 0 }))).toBe(false);
		expect(store.error).toBe('queue wait seconds must be between 1 and 600');
		expect(store.settings).toEqual(values());
	});

	it('clears a previous error on the next attempt', async () => {
		let fail = true;
		const { store } = setup(values(), () => (fail ? new Response('', { status: 500 }) : undefined));
		await store.save(values());
		expect(store.error).toBe('Request failed');
		fail = false;
		expect(await store.save(values())).toBe(true);
		expect(store.error).toBe('');
	});
});
