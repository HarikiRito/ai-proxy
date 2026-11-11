import { createClient as workosCreateClient } from '@workos-inc/authkit-js';

export type AuthStatus = 'loading' | 'signedOut' | 'signedIn' | 'refused' | 'error';
export type AuthClient = Awaited<ReturnType<typeof workosCreateClient>>;

interface AuthDeps {
	createClient: typeof workosCreateClient;
	fetchFn: typeof fetch;
	origin: string;
	navigate: (path: string) => void;
}

interface PublicConfig {
	workosClientId: string;
	devMode: boolean;
}

const VIEWER_QUERY = JSON.stringify({ query: '{ viewer { email } }' });

export function createAuthStore({
	createClient = workosCreateClient,
	fetchFn = (input, init) => fetch(input, init),
	origin = window.location.origin,
	navigate = (path) => window.location.replace(path)
}: Partial<AuthDeps> = {}) {
	let status = $state<AuthStatus>('loading');
	let email = $state('');
	let client: AuthClient | null = null;

	const signedOutResponse = () => new Response(null, { status: 401 });

	// A fresh token per request; the token is never stored.
	async function authFetch(input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> {
		if (!client) return signedOutResponse();
		const token = await client.getAccessToken().catch(() => null);
		if (token === null) {
			status = 'signedOut';
			return signedOutResponse();
		}
		const headers = new Headers(init.headers);
		headers.set('Authorization', `Bearer ${token}`);
		const res = await fetchFn(input, { ...init, headers }).catch(
			() => new Response(null, { status: 503 })
		);
		if (res.status === 401) status = 'signedOut';
		return res;
	}

	async function verifyViewer(): Promise<AuthStatus> {
		const res = await authFetch('/graphql', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: VIEWER_QUERY
		});
		if (res.status === 401) return 'signedOut';
		if (res.status === 403) return 'refused';
		if (!res.ok) return 'error';
		const body = await res.json().catch(() => null);
		email = body?.data?.viewer?.email ?? '';
		return email ? 'signedIn' : 'error';
	}

	async function bootstrap(): Promise<AuthStatus> {
		const cfg: PublicConfig = await fetchFn('/config.json').then((res) => {
			if (!res.ok) throw new Error(`config.json ${res.status}`);
			return res.json();
		});
		client = await createClient(cfg.workosClientId, {
			devMode: cfg.devMode,
			redirectUri: `${origin}/auth/callback`,
			onRedirectCallback: () => navigate('/'),
			onRefreshFailure: () => {
				status = 'signedOut';
			}
		});
		return client.getUser() ? verifyViewer() : 'signedOut';
	}

	async function init(): Promise<void> {
		status = await bootstrap().catch((): AuthStatus => 'error');
	}

	async function signIn(): Promise<void> {
		await client?.signIn().catch(() => {
			status = 'error';
		});
	}

	function signOut(): void {
		client?.signOut({ returnTo: `${origin}/` });
	}

	return {
		get status() {
			return status;
		},
		get user() {
			return client?.getUser() ?? null;
		},
		get email() {
			return email;
		},
		init,
		signIn,
		signOut,
		authFetch
	};
}
