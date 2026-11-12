export interface Settings {
	maxBodyBytes: number;
	requestTimeoutSeconds: number;
	queueWaitSeconds: number;
	defaultCooldownSeconds: number;
}

export type AuthFetch = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

interface GraphQLBody {
	data?: Record<string, unknown>;
	errors?: { message: string }[];
}

const REQUEST_FAILED = 'Request failed';
const HTTP_ERRORS: Record<number, string> = { 401: 'Signed out', 403: 'Access denied' };

const FIELDS = 'maxBodyBytes requestTimeoutSeconds queueWaitSeconds defaultCooldownSeconds';
const LOAD_QUERY = `{ settings { ${FIELDS} } }`;
const UPDATE_MUTATION = `mutation($input: SettingsInput!) { updateSettings(input: $input) { ${FIELDS} } }`;

export function createSettingsStore(authFetch: AuthFetch = (input, init) => fetch(input, init)) {
	let settings = $state<Settings | null>(null);
	let error = $state('');
	let loading = $state(false);

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
		const data = await request(LOAD_QUERY);
		if (data) {
			settings = data.settings as Settings;
			error = '';
		}
		loading = false;
	}

	async function save(input: Settings): Promise<boolean> {
		error = '';
		const data = await request(UPDATE_MUTATION, { input });
		if (!data) return false;
		settings = data.updateSettings as Settings;
		return true;
	}

	return {
		get settings() {
			return settings;
		},
		get error() {
			return error;
		},
		get loading() {
			return loading;
		},
		load,
		save
	};
}
