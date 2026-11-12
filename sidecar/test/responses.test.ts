import assert from 'node:assert/strict';
import { test } from 'node:test';
import { assistant, body, messages, result, sse, startApp, streamEvent } from './helpers.ts';

test('non-stream: message shape and usage mapping', async () => {
	const app = await startApp(() =>
		messages(assistant('hello '), assistant('world'), result('hello world'))
	);
	const res = await app.post(body());
	assert.equal(res.status, 200);
	assert.match(res.headers.get('content-type')!, /application\/json/);
	const json = (await res.json()) as any;
	assert.match(json.id, /^msg_/);
	assert.equal(json.type, 'message');
	assert.equal(json.role, 'assistant');
	assert.equal(json.model, 'claude-sonnet-4-5');
	assert.deepEqual(json.content, [{ type: 'text', text: 'hello world' }]);
	assert.equal(json.stop_reason, 'end_turn');
	assert.equal(json.stop_sequence, null);
	assert.deepEqual(json.usage, {
		input_tokens: 11,
		output_tokens: 7,
		cache_creation_input_tokens: 3,
		cache_read_input_tokens: 5
	});
});

test('non-stream: assistant stop_reason max_tokens is kept', async () => {
	const app = await startApp(() =>
		messages(
			{ ...assistant('cut'), message: { ...assistant('cut').message, stop_reason: 'max_tokens' } },
			result('cut')
		)
	);
	assert.equal(((await (await app.post(body())).json()) as any).stop_reason, 'max_tokens');
});

const events = [
	streamEvent({
		type: 'message_start',
		message: { id: 'm1', usage: { input_tokens: 9, cache_read_input_tokens: 4 } }
	}),
	streamEvent({ type: 'content_block_start', index: 0, content_block: { type: 'text', text: '' } }),
	streamEvent({ type: 'content_block_delta', index: 0, delta: { type: 'text_delta', text: 'hi' } }),
	streamEvent({ type: 'content_block_stop', index: 0 }),
	streamEvent({
		type: 'message_delta',
		delta: { stop_reason: 'end_turn' },
		usage: { output_tokens: 2 }
	})
];

test('stream: valid SSE sequence, message_stop synthesized when missing', async () => {
	const app = await startApp(() => messages(...events, assistant('hi'), result('hi')));
	const res = await app.post(body({ stream: true }));
	assert.equal(res.status, 200);
	assert.match(res.headers.get('content-type')!, /text\/event-stream/);
	const out = await sse(res);
	assert.deepEqual(
		out.map((e) => e.event),
		[
			'message_start',
			'content_block_start',
			'content_block_delta',
			'content_block_stop',
			'message_delta',
			'message_stop'
		]
	);
	for (const e of out) assert.equal(e.data.type, e.event);
	assert.equal(out[0].data.message.usage.cache_read_input_tokens, 4);
});

test('stream: SDK message_stop is not duplicated', async () => {
	const app = await startApp(() =>
		messages(...events, streamEvent({ type: 'message_stop' }), result('hi'))
	);
	const out = await sse(await app.post(body({ stream: true })));
	assert.equal(out.filter((e) => e.event === 'message_stop').length, 1);
});

test('stream: no SDK stream events -> final message replayed as a valid stream', async () => {
	const app = await startApp(() => messages(assistant('hi'), result('hi')));
	const out = await sse(await app.post(body({ stream: true })));
	assert.equal(out[0].event, 'message_start');
	assert.equal(out[0].data.message.usage.cache_read_input_tokens, 5);
	assert.equal(out.at(-1)!.event, 'message_stop');
	assert.equal(out.find((e) => e.event === 'content_block_delta')!.data.delta.text, 'hi');
});

test('stream: each event is flushed before the next is produced', async () => {
	let release!: () => void;
	const gate = new Promise<void>((r) => (release = r));
	const app = await startApp(async function* () {
		yield events[0];
		await gate;
		yield* events.slice(1);
		yield result('hi');
	});
	const res = await app.post(body({ stream: true }));
	const reader = res.body!.getReader();
	const first = new TextDecoder().decode((await reader.read()).value);
	assert.match(first, /^event: message_start\n/);
	release();
	let rest = '';
	for (let r = await reader.read(); !r.done; r = await reader.read())
		rest += new TextDecoder().decode(r.value);
	assert.match(rest, /event: message_stop/);
});
