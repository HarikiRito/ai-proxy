import assert from 'node:assert/strict';
import { test } from 'node:test';
import { body, dirHeaders, messages, startApp, tokenHeaders } from './helpers.ts';

const app = await startApp(() => messages());

const expectError = async (res: Response, status: number, type: string, message?: RegExp) => {
	assert.equal(res.status, status);
	const json = (await res.json()) as any;
	assert.equal(json.type, 'error');
	assert.equal(json.error.type, type);
	if (message) assert.match(json.error.message, message);
};

const auth401 = (headers: Record<string, string>) => async () =>
	expectError(await app.post(body(), headers), 401, 'authentication_error');

test('missing headers -> 401', auth401({}));
test('missing credential -> 401', auth401({ 'x-claude-auth-mode': 'setup_token' }));
test('invalid mode -> 401', auth401({ 'x-claude-auth-mode': 'nope', 'x-claude-credential': 'x' }));
test('bad dir name -> 401', auth401({ ...dirHeaders, 'x-claude-credential': '../etc' }));
test('uppercase dir name -> 401', auth401({ ...dirHeaders, 'x-claude-credential': 'Alice' }));

test('missing dir -> 401', async () => {
	const res = await app.post(body(), { ...dirHeaders, 'x-claude-credential': 'bob' });
	await expectError(res, 401, 'authentication_error', /account directory not found/);
});

const bad400 = (extra: Record<string, any>, message: RegExp) => async () =>
	expectError(await app.post(body(extra), tokenHeaders), 400, 'invalid_request_error', message);

test('tools -> 400', bad400({ tools: [] }, /tools are not supported/));
test('tool_choice -> 400', bad400({ tool_choice: { type: 'auto' } }, /tools are not supported/));
test(
	'url image -> 400',
	bad400(
		{
			messages: [
				{
					role: 'user',
					content: [{ type: 'image', source: { type: 'url', url: 'https://x/y.png' } }]
				}
			]
		},
		/send images as base64/
	)
);
test(
	'unknown block -> 400',
	bad400({ messages: [{ role: 'user', content: [{ type: 'tool_use' }] }] }, /unsupported/)
);
test('no messages -> 400', bad400({ messages: [] }, /messages/));

test('invalid json -> 400', async () => {
	const res = await fetch(`${app.url}/v1/messages`, {
		method: 'POST',
		headers: tokenHeaders,
		body: '{'
	});
	await expectError(res, 400, 'invalid_request_error');
});

test('nothing reaches the SDK on validation failure', () => assert.equal(app.calls.length, 0));
