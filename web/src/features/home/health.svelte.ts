export type HealthStatus = 'checking' | 'healthy' | 'unhealthy';

export function createHealthStore(fetchFn: typeof fetch = (input, init) => fetch(input, init)) {
	let status = $state<HealthStatus>('checking');

	async function check(): Promise<void> {
		status = await fetchFn('/healthz')
			.then((res): HealthStatus => (res.ok ? 'healthy' : 'unhealthy'))
			.catch((): HealthStatus => 'unhealthy');
	}

	return {
		get status() {
			return status;
		},
		check
	};
}
