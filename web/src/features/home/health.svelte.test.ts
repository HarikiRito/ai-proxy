import { describe, expect, it, vi } from 'vitest';
import { createHealthStore } from './health.svelte';

const respond = (status: number) => () => Promise.resolve(new Response('{}', { status }));

describe('createHealthStore', () => {
	it('is checking until the first probe resolves', () => {
		expect(createHealthStore(respond(200)).status).toBe('checking');
	});

	it('is healthy when /healthz answers 2xx', async () => {
		const store = createHealthStore(respond(200));
		await store.check();
		expect(store.status).toBe('healthy');
	});

	it('is unhealthy when /healthz answers 503', async () => {
		const store = createHealthStore(respond(503));
		await store.check();
		expect(store.status).toBe('unhealthy');
	});

	it('is unhealthy when the request fails', async () => {
		const store = createHealthStore(() => Promise.reject(new TypeError('network down')));
		await store.check();
		expect(store.status).toBe('unhealthy');
	});

	it('probes the same-origin /healthz path', async () => {
		const fetchFn = vi.fn(() => Promise.resolve(new Response('{}')));
		await createHealthStore(fetchFn).check();
		expect(fetchFn).toHaveBeenCalledWith('/healthz');
	});
});
