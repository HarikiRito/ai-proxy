import assert from 'node:assert/strict';
import { test } from 'node:test';
import { BUILTIN_TOOL_NAMES } from '@anthropic-ai/claude-agent-sdk';
import {
	assistant,
	body,
	dirHeaders,
	messages,
	result,
	startApp,
	streamEvent,
	TOKEN,
	tokenHeaders
} from './helpers.ts';

const app = await startApp(() => messages(assistant('hi'), result()));
const last = () => app.calls.at(-1)!.options;

test('query options lock the SDK down', async () => {
	await app.post(
		body({
			system: [
				{ type: 'text', text: 'be brief' },
				{ type: 'text', text: 'ok' }
			]
		})
	);
	const o = last();
	assert.equal(o.model, 'claude-sonnet-4-5');
	assert.deepEqual(o.tools, []);
	assert.deepEqual(o.disallowedTools, [...BUILTIN_TOOL_NAMES]);
	assert.ok(o.disallowedTools.includes('Bash'));
	assert.equal(o.maxTurns, 1);
	assert.deepEqual(o.settingSources, []);
	assert.equal(o.strictMcpConfig, true);
	assert.equal('mcpServers' in o, false);
	assert.equal(o.systemPrompt, 'be brief\n\nok');
	assert.equal(o.includePartialMessages, false);
	assert.equal(o.env.CLAUDE_CODE_DISABLE_AUTO_MEMORY, '1');
});

test('no system -> empty string systemPrompt, stream -> partial messages', async () => {
	await (await app.post(body({ stream: true }))).text();
	assert.equal(last().systemPrompt, '');
	assert.equal(last().includePartialMessages, true);
});

test('setup_token env: only the token, inherited auth vars removed', async () => {
	const saved = { ...process.env };
	Object.assign(process.env, {
		ANTHROPIC_API_KEY: 'k',
		ANTHROPIC_AUTH_TOKEN: 't',
		ANTHROPIC_BASE_URL: 'u',
		CLAUDE_CODE_OAUTH_TOKEN: 'other-account',
		CLAUDE_CONFIG_DIR: '/other',
		CLAUDE_CODE_USE_BEDROCK: '1',
		KEEP_ME: '1'
	});
	try {
		await app.post(body(), tokenHeaders);
	} finally {
		process.env = saved;
	}
	const env = last().env;
	assert.equal(env.CLAUDE_CODE_OAUTH_TOKEN, TOKEN);
	assert.equal(env.KEEP_ME, '1');
	for (const k of [
		'ANTHROPIC_API_KEY',
		'ANTHROPIC_AUTH_TOKEN',
		'ANTHROPIC_BASE_URL',
		'CLAUDE_CONFIG_DIR',
		'CLAUDE_CODE_USE_BEDROCK'
	])
		assert.equal(k in env, false, k);
});

test('login_dir env: only the config dir', async () => {
	const saved = { ...process.env };
	Object.assign(process.env, {
		CLAUDE_CODE_OAUTH_TOKEN: 'other-account',
		CLAUDE_CONFIG_DIR: '/other'
	});
	try {
		await app.post(body(), dirHeaders);
	} finally {
		process.env = saved;
	}
	const env = last().env;
	assert.match(env.CLAUDE_CONFIG_DIR, /accounts-.*\/alice$/);
	assert.equal('CLAUDE_CODE_OAUTH_TOKEN' in env, false);
});

test('ignored parameters are accepted without effect', async () => {
	const before = JSON.stringify((await (await app.post(body())).json()) && last());
	const res = await app.post(
		body({
			max_tokens: 5,
			temperature: 0.1,
			top_p: 0.2,
			top_k: 3,
			stop_sequences: ['x'],
			metadata: { user_id: 'u' },
			cache_control: { type: 'ephemeral' }
		})
	);
	assert.equal(res.status, 200);
	assert.equal(JSON.stringify(last()), before);
});

test('client disconnect aborts the query', async () => {
	let signal!: AbortSignal;
	const hanging = await startApp(async function* ({ options }) {
		signal = options.abortController.signal;
		yield streamEvent({ type: 'message_start', message: {} });
		await new Promise((r) => signal.addEventListener('abort', r));
	});
	const ac = new AbortController();
	const res = await hanging.post(body({ stream: true }), tokenHeaders, ac.signal);
	await res.body!.getReader().read();
	assert.equal(signal.aborted, false);
	ac.abort();
	await new Promise((r) => setTimeout(r, 100));
	assert.equal(signal.aborted, true);
});
