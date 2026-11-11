import { LoginRequiredError } from '@workos-inc/authkit-js';
import { describe, expect, it, vi } from 'vitest';
import { type AuthClient, createAuthStore } from './auth.svelte';

const origin = 'https://app.test';
const user = { id: 'user_1', email: 'owner@example.com' };

interface ClientOptions {
	onRedirectCallback?: () => void;
	onRefreshFailure?: () => void;
}

function fakeClient(overrides: Partial<AuthClient> = {}): AuthClient {
	let n = 0;
	return {
		getUser: () => user,
		getAccessToken: () => Promise.resolve(`token-${++n}`),
		signIn: () => Promise.resolve(),
		signOut: () => undefined,
		...overrides
	} as unknown as AuthClient;
}

function setup(opts: {
	client?: AuthClient;
	graphql?: () => Promise<Response>;
	config?: Response | Promise<Response>;
}) {
	const client = opts.client ?? fakeClient();
	const createClient = vi.fn((_id: string, _o?: ClientOptions) => Promise.resolve(client));
	const fetchFn = vi.fn((input: RequestInfo | URL, _init?: RequestInit) =>
		input === '/config.json'
			? Promise.resolve(opts.config ?? Response.json({ workosClientId: 'client_x', devMode: true }))
			: (opts.graphql ?? (() => Promise.resolve(viewer())))()
	);
	const navigate = vi.fn();
	const store = createAuthStore({
		createClient: createClient as never,
		fetchFn: fetchFn,
		origin,
		navigate
	});
	return { store, client, createClient, fetchFn, navigate };
}

const viewer = (email = user.email) => Response.json({ data: { viewer: { email } } });

describe('createAuthStore.init', () => {
	it('starts loading', () => {
		expect(setup({}).store.status).toBe('loading');
	});

	it('is signedOut when the client has no user', async () => {
		const { store, fetchFn } = setup({ client: fakeClient({ getUser: () => null }) });
		await store.init();
		expect(store.status).toBe('signedOut');
		expect(fetchFn).not.toHaveBeenCalledWith('/graphql', expect.anything());
	});

	it('is signedIn with the viewer email when the viewer query answers 200', async () => {
		const { store } = setup({});
		await store.init();
		expect(store.status).toBe('signedIn');
		expect(store.email).toBe('owner@example.com');
		expect(store.user).toEqual(user);
	});

	it('is refused on 403 and exposes no email', async () => {
		const { store } = setup({
			graphql: () => Promise.resolve(Response.json({ error: 'forbidden' }, { status: 403 }))
		});
		await store.init();
		expect(store.status).toBe('refused');
		expect(store.email).toBe('');
	});

	it('is signedOut on 401', async () => {
		const { store } = setup({
			graphql: () => Promise.resolve(new Response('{}', { status: 401 }))
		});
		await store.init();
		expect(store.status).toBe('signedOut');
	});

	it('is signedOut when the token cannot be obtained (LoginRequiredError)', async () => {
		const { store } = setup({
			client: fakeClient({ getAccessToken: () => Promise.reject(new LoginRequiredError()) })
		});
		await store.init();
		expect(store.status).toBe('signedOut');
	});

	it('is error on 503', async () => {
		const { store } = setup({
			graphql: () => Promise.resolve(new Response('{}', { status: 503 }))
		});
		await store.init();
		expect(store.status).toBe('error');
	});

	it('never rejects when config, client creation or the network fails', async () => {
		const badConfig = setup({ config: new Response('nope', { status: 500 }) });
		await badConfig.store.init();
		expect(badConfig.store.status).toBe('error');

		const down = setup({ graphql: () => Promise.reject(new TypeError('network down')) });
		await down.store.init();
		expect(down.store.status).toBe('error');

		const failing = createAuthStore({
			createClient: () => Promise.reject(new Error('boom')),
			fetchFn: () => Promise.resolve(Response.json({ workosClientId: 'c', devMode: false })),
			origin,
			navigate: vi.fn()
		});
		await failing.init();
		expect(failing.status).toBe('error');
	});

	it('creates the client from /config.json with callback uri and navigates home after redirect', async () => {
		const { store, createClient, navigate } = setup({});
		await store.init();
		const [clientId, options] = createClient.mock.calls[0] as [string, ClientOptions];
		expect(clientId).toBe('client_x');
		expect(options).toMatchObject({ devMode: true, redirectUri: `${origin}/auth/callback` });
		options.onRedirectCallback?.();
		expect(navigate).toHaveBeenCalledWith('/');
	});

	it('goes signedOut when the SDK reports a refresh failure', async () => {
		const { store, createClient } = setup({});
		await store.init();
		expect(store.status).toBe('signedIn');
		const [, options] = createClient.mock.calls[0] as [string, ClientOptions];
		options.onRefreshFailure?.();
		expect(store.status).toBe('signedOut');
	});
});

describe('createAuthStore actions', () => {
	it('signOut returns to the app root', async () => {
		const signOut = vi.fn();
		const { store } = setup({ client: fakeClient({ signOut }) });
		await store.init();
		store.signOut();
		expect(signOut).toHaveBeenCalledWith({ returnTo: `${origin}/` });
	});

	it('signIn delegates to the client and never rejects', async () => {
		const signIn = vi.fn(() => Promise.reject(new Error('redirect failed')));
		const { store } = setup({ client: fakeClient({ signIn }) });
		await store.init();
		await store.signIn();
		expect(signIn).toHaveBeenCalledOnce();
		expect(store.status).toBe('error');
	});

	it('authFetch attaches a fresh token per call and never caches it', async () => {
		const { store, fetchFn } = setup({});
		await store.init();
		fetchFn.mockClear();
		await store.authFetch('/graphql');
		await store.authFetch('/graphql');
		const auth = fetchFn.mock.calls.map(([, init]) =>
			new Headers(init?.headers).get('Authorization')
		);
		expect(auth).toEqual(['Bearer token-2', 'Bearer token-3']);
	});

	it('authFetch maps 401 to signedOut', async () => {
		let status = 200;
		const { store } = setup({
			graphql: () => Promise.resolve(status === 200 ? viewer() : new Response('{}', { status }))
		});
		await store.init();
		expect(store.status).toBe('signedIn');
		status = 401;
		const res = await store.authFetch('/graphql');
		expect(res.status).toBe(401);
		expect(store.status).toBe('signedOut');
	});
});
