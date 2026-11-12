export class ApiError extends Error {
	status: number;
	type: string;
	retryAfter?: number;
	constructor(status: number, type: string, message: string, retryAfter?: number) {
		super(message);
		this.status = status;
		this.type = type;
		this.retryAfter = retryAfter;
	}
}

export const badRequest = (message: string) => new ApiError(400, 'invalid_request_error', message);

export const envelope = (type: string, message: string) => ({
	type: 'error',
	error: { type, message }
});

export const DEFAULT_RETRY_SECONDS = 1800;
const MAX_RETRY_SECONDS = 86400;

// Everything the SDK reported for one query, filled by collect().
export type RunState = {
	limitRejected: boolean;
	resetsAt?: number;
	assistantError?: string;
	synthetic: boolean;
	text: string;
	stopReason?: string;
	result?: any;
};

export const newState = (): RunState => ({ limitRejected: false, synthetic: false, text: '' });

export function collect(state: RunState, m: any) {
	if (m.type === 'rate_limit_event' && m.rate_limit_info?.status === 'rejected') {
		state.limitRejected = true;
		state.resetsAt = m.rate_limit_info.resetsAt;
	} else if (m.type === 'assistant') {
		state.assistantError ??= m.error;
		state.synthetic ||= m.message?.model === '<synthetic>';
		state.stopReason = m.message?.stop_reason ?? state.stopReason;
		for (const b of m.message?.content ?? []) if (b.type === 'text') state.text += b.text;
	} else if (m.type === 'result') {
		state.result = m;
	}
}

function retryAfter(resetsAt: number | undefined, nowMs: number) {
	if (typeof resetsAt !== 'number' || !Number.isFinite(resetsAt)) return DEFAULT_RETRY_SECONDS;
	const resetMs = resetsAt > 1e12 ? resetsAt : resetsAt * 1000;
	return Math.min(MAX_RETRY_SECONDS, Math.max(1, Math.ceil((resetMs - nowMs) / 1000)));
}

const LIMIT_TEXT = /hit your .*limit/i;
const AUTH_TEXT =
	/not logged in|please run \/login|failed to authenticate|oauth (session|token).*(expired|revoked)|token revoked|invalid api key/i;
const MAX_SIGNAL_TEXT = 300;

// null = the query succeeded.
export function classify(state: RunState, thrown: unknown, nowMs: number): ApiError | null {
	const result = state.result;
	const ok =
		result?.subtype === 'success' &&
		!result.is_error &&
		!state.assistantError &&
		!state.limitRejected &&
		!state.synthetic;
	if (ok) return null;

	const texts = [result?.result, state.text, thrown instanceof Error ? thrown.message : ''];
	const signal = [
		...new Set(texts.filter((t) => typeof t === 'string' && t !== '' && t.length < MAX_SIGNAL_TEXT))
	].join(' ');
	const err = state.assistantError;
	const message = (fallback: string) => signal || fallback;

	if (state.limitRejected)
		return new ApiError(
			429,
			'rate_limit_error',
			message('rate limited'),
			retryAfter(state.resetsAt, nowMs)
		);
	if (err === 'rate_limit' || LIMIT_TEXT.test(signal))
		return new ApiError(429, 'rate_limit_error', message('rate limited'), DEFAULT_RETRY_SECONDS);
	if (err === 'authentication_failed' || AUTH_TEXT.test(signal))
		return new ApiError(401, 'authentication_error', message('authentication failed'));
	if (err === 'oauth_org_not_allowed' || err === 'account_on_hold')
		return new ApiError(403, 'permission_error', message(err));
	if (err === 'overloaded') return new ApiError(529, 'overloaded_error', message('overloaded'));
	return new ApiError(500, 'api_error', message('upstream request failed'));
}
