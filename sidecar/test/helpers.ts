import { mkdirSync, mkdtempSync } from 'node:fs';
import type { AddressInfo } from 'node:net';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { after } from 'node:test';
import { createServer } from '../src/app.ts';
import type { QueryFn } from '../src/app.ts';

export type Call = { prompt: AsyncIterable<any>; options: Record<string, any> };
export type Script = (call: Call) => AsyncIterable<any>;

export const TOKEN = 'sk-ant-oat01-SECRET-TOKEN-1234';
export const PROMPT = 'tell me the PROMPT-CANARY-42 story';

export const assistant = (text: string, extra: Record<string, any> = {}) => ({
	type: 'assistant',
	message: { model: 'claude-test', content: [{ type: 'text', text }], stop_reason: null },
	...extra
});

export const result = (text = 'ok', extra: Record<string, any> = {}) => ({
	type: 'result',
	subtype: 'success',
	is_error: false,
	result: text,
	usage: {
		input_tokens: 11,
		output_tokens: 7,
		cache_creation_input_tokens: 3,
		cache_read_input_tokens: 5
	},
	...extra
});

export const streamEvent = (event: Record<string, any>) => ({ type: 'stream_event', event });

export async function* messages(...items: any[]) {
	yield* items;
}

export const body = (extra: Record<string, any> = {}) => ({
	model: 'claude-sonnet-4-5',
	messages: [{ role: 'user', content: PROMPT }],
	...extra
});

export async function startApp(script: Script) {
	const calls: Call[] = [];
	const logs: string[] = [];
	const accountsDir = mkdtempSync(path.join(tmpdir(), 'accounts-'));
	mkdirSync(path.join(accountsDir, 'alice'));
	const query: QueryFn = (call) => {
		calls.push(call);
		return script(call);
	};
	const server = createServer({ query, accountsDir, log: (l) => logs.push(l) });
	await new Promise<void>((r) => server.listen(0, '127.0.0.1', r));
	after(() => server.close());
	const url = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;

	const post = (
		json: unknown,
		headers: Record<string, string> = tokenHeaders,
		signal?: AbortSignal
	) =>
		fetch(`${url}/v1/messages`, {
			method: 'POST',
			headers: { 'content-type': 'application/json', ...headers },
			body: JSON.stringify(json),
			signal
		});
	return { url, calls, logs, post };
}

export const tokenHeaders = { 'x-claude-auth-mode': 'setup_token', 'x-claude-credential': TOKEN };
export const dirHeaders = { 'x-claude-auth-mode': 'login_dir', 'x-claude-credential': 'alice' };

export async function sse(res: Response) {
	const text = await res.text();
	return text
		.split('\n\n')
		.filter(Boolean)
		.map((chunk) => {
			const [ev, data] = chunk.split('\n');
			return { event: ev.slice(7), data: JSON.parse(data.slice(6)) };
		});
}
