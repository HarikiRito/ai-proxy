import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
	assistant,
	body,
	messages,
	PROMPT,
	result,
	sse,
	startApp,
	streamEvent,
	TOKEN,
	tokenHeaders,
	dirHeaders
} from './helpers.ts';

const failing = (assistantExtra: Record<string, any>, text: string, resultExtra = {}) =>
	messages(assistant(text, assistantExtra), result(text, { is_error: true, ...resultExtra }));

async function expectFailure(
	script: AsyncIterable<any>,
	status: number,
	type: string,
	retryAfter?: string
) {
	const app = await startApp(() => script);
	const res = await app.post(body());
	assert.equal(res.status, status);
	assert.equal(res.headers.get('retry-after'), retryAfter ?? null);
	const json = (await res.json()) as any;
	assert.deepEqual(Object.keys(json), ['type', 'error']);
	assert.equal(json.type, 'error');
	assert.equal(json.error.type, type);
	assert.equal(typeof json.error.message, 'string');
}

const inSeconds = (s: number) => Math.floor(Date.now() / 1000) + s;
const retryNear = async (resetsAt: number, expected: number) => {
	const app = await startApp(() =>
		messages({ type: 'rate_limit_event', rate_limit_info: { status: 'rejected', resetsAt } })
	);
	const res = await app.post(body());
	assert.equal(res.status, 429);
	assert.ok(Math.abs(Number(res.headers.get('retry-after')) - expected) <= 2);
};

test('limit (a): rate_limit_event rejected, resetsAt in seconds and ms', async () => {
	await retryNear(inSeconds(600), 600);
	await retryNear(inSeconds(600) * 1000, 600);
});

test('limit (a): past reset -> 1, far reset capped at 86400', async () => {
	await retryNear(inSeconds(-100), 1);
	await retryNear(inSeconds(10 * 86400), 86400);
});

test('limit (a): no reset time -> 1800', () =>
	expectFailure(
		messages({ type: 'rate_limit_event', rate_limit_info: { status: 'rejected' } }),
		429,
		'rate_limit_error',
		'1800'
	));

test('limit (a): allowed_warning is not a limit', async () => {
	const app = await startApp(() =>
		messages(
			{ type: 'rate_limit_event', rate_limit_info: { status: 'allowed_warning' } },
			assistant('fine'),
			result('fine')
		)
	);
	assert.equal((await app.post(body())).status, 200);
});

test('limit (b): assistant error rate_limit', () =>
	expectFailure(failing({ error: 'rate_limit' }, 'slow down'), 429, 'rate_limit_error', '1800'));

test('limit (c): short text matches', async () => {
	await expectFailure(
		failing({}, "You've hit your session limit · resets 3:45pm"),
		429,
		'rate_limit_error',
		'1800'
	);
	await expectFailure(
		messages(
			assistant("You've hit your weekly limit", {
				message: {
					model: '<synthetic>',
					content: [{ type: 'text', text: "You've hit your weekly limit" }]
				}
			}),
			result('', {})
		),
		429,
		'rate_limit_error',
		'1800'
	);
});

test('long or successful normal text containing the phrase is not a limit', async () => {
	const long = `${'a '.repeat(200)} you hit your usage limit`;
	const a = await startApp(() => messages(assistant(long), result(long)));
	assert.equal((await a.post(body())).status, 200);
	const short = 'If you hit your rate limit, wait.';
	const b = await startApp(() => messages(assistant(short), result(short)));
	assert.equal((await b.post(body())).status, 200);
	await expectFailure(failing({}, long), 500, 'api_error');
});

test('auth: authentication_failed and auth texts -> 401', async () => {
	await expectFailure(
		failing({ error: 'authentication_failed' }, 'x'),
		401,
		'authentication_error'
	);
	for (const text of [
		'Not logged in · Please run /login',
		'Failed to authenticate: OAuth session expired and could not be refreshed',
		'OAuth token revoked',
		'Invalid API key'
	])
		await expectFailure(failing({}, text), 401, 'authentication_error');
});

test('auth: SDK throws after an auth error result', () =>
	expectFailure(
		(async function* () {
			yield assistant('Failed to authenticate. API Error: 401', { error: 'authentication_failed' });
			yield result('Failed to authenticate', { is_error: true });
			throw new Error('Claude Code returned an error result: Failed to authenticate');
		})(),
		401,
		'authentication_error'
	));

test('permission and overload mapping', async () => {
	await expectFailure(failing({ error: 'oauth_org_not_allowed' }, 'no'), 403, 'permission_error');
	await expectFailure(failing({ error: 'account_on_hold' }, 'held'), 403, 'permission_error');
	await expectFailure(failing({ error: 'overloaded' }, 'busy'), 529, 'overloaded_error');
	await expectFailure(failing({ error: 'server_error' }, 'boom'), 500, 'api_error');
});

test('SDK throwing with no output -> 500 api_error', () =>
	expectFailure(
		(async function* () {
			throw new Error('spawn failed');
		})(),
		500,
		'api_error'
	));

test('mid-stream failure -> event: error then end', async () => {
	const app = await startApp(() =>
		messages(
			streamEvent({ type: 'message_start', message: { usage: {} } }),
			assistant('part', { error: 'overloaded' }),
			result('overloaded', { is_error: true })
		)
	);
	const res = await app.post(body({ stream: true }));
	assert.equal(res.status, 200);
	const out = await sse(res);
	assert.deepEqual(
		out.map((e) => e.event),
		['message_start', 'error']
	);
	assert.deepEqual(Object.keys(out[1].data), ['type', 'error']);
	assert.equal(out[1].data.error.type, 'overloaded_error');
});

test('stream failure before the first event -> plain HTTP error', async () => {
	const app = await startApp(() => failing({ error: 'authentication_failed' }, 'bad'));
	const res = await app.post(body({ stream: true }));
	assert.equal(res.status, 401);
});

test('credential echoed by the SDK is redacted from the response', async () => {
	const app = await startApp(() => failing({}, `Invalid API key ${TOKEN}`));
	const res = await app.post(body());
	assert.equal(res.status, 401);
	assert.equal((await res.text()).includes(TOKEN), false);
});

test('logs never contain the credential or the prompt', async () => {
	const scripts = [
		messages(assistant('reply'), result('reply')),
		failing({ error: 'authentication_failed' }, `Invalid API key ${TOKEN} ${PROMPT}`),
		messages({ type: 'rate_limit_event', rate_limit_info: { status: 'rejected' } }),
		(async function* () {
			throw new Error(`boom ${TOKEN} ${PROMPT}`);
		})()
	];
	for (const script of scripts) {
		const app = await startApp(() => script);
		for (const headers of [tokenHeaders, dirHeaders])
			await (await app.post(body({ stream: true }), headers)).text();
		await app.post({ ...body(), tools: [{}] });
		await app.post(body(), { ...tokenHeaders, 'x-claude-credential': '' });
		await new Promise((r) => setTimeout(r, 20));
		assert.ok(app.logs.length >= 4);
		const logs = app.logs.join('\n');
		assert.equal(logs.includes(TOKEN), false);
		assert.equal(logs.includes('PROMPT-CANARY'), false);
		assert.match(
			app.logs[0],
			/"method":"POST".*"path":"\/v1\/messages".*"status":\d+.*"duration_ms":\d+/
		);
	}
});
