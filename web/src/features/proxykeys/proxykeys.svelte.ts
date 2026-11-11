export interface ProxyKey {
	id: string;
	name: string;
	allowedModels: string[];
	createdAt: string;
	lastUsedAt: string | null;
	revokedAt: string | null;
	status: 'active' | 'revoked';
}

export interface Revealed {
	name: string;
	secret: string;
}

export type AuthFetch = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;
export interface Clipboard {
	writeText: (text: string) => Promise<void>;
}

interface GraphQLBody {
	data?: Record<string, unknown>;
	errors?: { message: string }[];
}

const COPY_FAILED = 'Copy failed, select and copy the key manually';
const REQUEST_FAILED = 'Request failed';
const HTTP_ERRORS: Record<number, string> = { 401: 'Signed out', 403: 'Access denied' };

const LIST_QUERY = '{ proxyKeys { id name allowedModels createdAt lastUsedAt revokedAt status } }';

const MUTATIONS = {
	createProxyKey:
		'mutation($input: CreateProxyKeyInput!) { createProxyKey(input: $input) { secret key { id } } }',
	setProxyKeyAllowedModels:
		'mutation($id: ID!, $models: [String!]!) { setProxyKeyAllowedModels(id: $id, models: $models) { id } }',
	revokeProxyKey: 'mutation($id: ID!) { revokeProxyKey(id: $id) { id } }'
} as const;

// Splits on newlines and commas, trims, drops empties and duplicates.
export const parseModels = (text: string): string[] =>
	text
		.split(/[\n,]/)
		.map((m) => m.trim())
		.filter((m, i, all) => m !== '' && all.indexOf(m) === i);

export function createProxyKeysStore(
	authFetch: AuthFetch = (input, init) => fetch(input, init),
	clipboard: Clipboard = { writeText: (text) => navigator.clipboard.writeText(text) }
) {
	let keys = $state<ProxyKey[]>([]);
	let error = $state('');
	let loading = $state(false);
	let revealed = $state<Revealed | null>(null);
	let copied = $state(false);

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
			keys = data.proxyKeys as ProxyKey[];
			error = '';
		}
		loading = false;
	}

	async function mutate(
		name: keyof typeof MUTATIONS,
		variables: Record<string, unknown>
	): Promise<Record<string, unknown> | null> {
		error = '';
		const data = await request(MUTATIONS[name], variables);
		if (data) await load();
		return data;
	}

	async function create(name: string, allowedModels: string[]): Promise<boolean> {
		const data = await mutate('createProxyKey', { input: { name, allowedModels } });
		if (!data) return false;
		const created = data.createProxyKey as { secret: string };
		revealed = { name, secret: created.secret };
		copied = false;
		return true;
	}

	async function copy(): Promise<boolean> {
		if (!revealed) return false;
		const secret = revealed.secret;
		copied = await Promise.resolve()
			.then(() => clipboard.writeText(secret))
			.then(() => true)
			.catch(() => false);
		error = copied ? '' : COPY_FAILED;
		return copied;
	}

	return {
		get keys() {
			return keys;
		},
		get error() {
			return error;
		},
		get loading() {
			return loading;
		},
		get revealed() {
			return revealed;
		},
		get copied() {
			return copied;
		},
		load,
		create,
		copy,
		dismissReveal: () => {
			revealed = null;
			copied = false;
		},
		clearError: () => {
			error = '';
		},
		setAllowedModels: async (id: string, models: string[]) =>
			(await mutate('setProxyKeyAllowedModels', { id, models })) !== null,
		revoke: async (id: string) => (await mutate('revokeProxyKey', { id })) !== null
	};
}
