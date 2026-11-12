import { randomBytes, randomUUID } from 'node:crypto';
import { statSync } from 'node:fs';
import http from 'node:http';
import path from 'node:path';
import { BUILTIN_TOOL_NAMES } from '@anthropic-ai/claude-agent-sdk';
import { ApiError, badRequest, classify, collect, envelope, newState } from './errors.ts';
import type { RunState } from './errors.ts';
import { parseRequest } from './request.ts';
import type { Block, ParsedRequest } from './request.ts';

export type QueryFn = (params: {
	prompt: AsyncIterable<unknown>;
	options: Record<string, any>;
}) => AsyncIterable<any>;

export type Deps = {
	query: QueryFn;
	accountsDir: string;
	log: (line: string) => void;
	now?: () => number;
};

type Auth = { secret?: string; env: Record<string, string> };

const MAX_BODY_BYTES = 32 * 1024 * 1024;
const ACCOUNT_NAME = /^[a-z0-9_-]{1,64}$/;
const STOP_REASONS = ['end_turn', 'max_tokens', 'stop_sequence', 'refusal', 'pause_turn'];

function authenticate(req: http.IncomingMessage, accountsDir: string): Auth {
	const mode = req.headers['x-claude-auth-mode'];
	const credential = req.headers['x-claude-credential'];
	const invalid = (m: string) => new ApiError(401, 'authentication_error', m);
	if (typeof credential !== 'string' || credential === '') throw invalid('missing credential');
	if (mode === 'setup_token')
		return { secret: credential, env: { CLAUDE_CODE_OAUTH_TOKEN: credential } };
	if (mode !== 'login_dir') throw invalid('invalid auth mode');
	if (!ACCOUNT_NAME.test(credential)) throw invalid('invalid account name');
	const dir = path.join(accountsDir, credential);
	if (!statSync(dir, { throwIfNoEntry: false })?.isDirectory())
		throw invalid('account directory not found');
	return { env: { CLAUDE_CONFIG_DIR: dir } };
}

function subprocessEnv(auth: Auth) {
	const env: Record<string, string | undefined> = { ...process.env };
	for (const k of Object.keys(env))
		if (/^ANTHROPIC_|^CLAUDE_CONFIG_DIR$|^CLAUDE_CODE_(OAUTH|USE_)/.test(k)) delete env[k];
	return { ...env, ...auth.env, CLAUDE_CODE_DISABLE_AUTO_MEMORY: '1' };
}

async function readJson(req: http.IncomingMessage): Promise<unknown> {
	const chunks: Buffer[] = [];
	let size = 0;
	for await (const chunk of req) {
		size += chunk.length;
		if (size > MAX_BODY_BYTES) throw badRequest('request body too large');
		chunks.push(chunk);
	}
	try {
		return JSON.parse(Buffer.concat(chunks).toString('utf8'));
	} catch {
		throw badRequest('invalid JSON');
	}
}

async function* singleUserMessage(blocks: Block[]) {
	yield {
		type: 'user',
		message: { role: 'user', content: blocks },
		parent_tool_use_id: null,
		session_id: randomUUID()
	};
}

const usageOf = (state: RunState) => {
	const u = state.result?.usage ?? {};
	return {
		input_tokens: u.input_tokens ?? 0,
		output_tokens: u.output_tokens ?? 0,
		cache_creation_input_tokens: u.cache_creation_input_tokens ?? 0,
		cache_read_input_tokens: u.cache_read_input_tokens ?? 0
	};
};

const stopReasonOf = (state: RunState) =>
	STOP_REASONS.includes(state.stopReason ?? '') ? state.stopReason! : 'end_turn';

const messageId = () => `msg_${randomBytes(12).toString('hex')}`;

function messageBody(state: RunState, model: string) {
	return {
		id: messageId(),
		type: 'message',
		role: 'assistant',
		model,
		content: [{ type: 'text', text: state.text || state.result?.result || '' }],
		stop_reason: stopReasonOf(state),
		stop_sequence: null,
		usage: usageOf(state)
	};
}

// Used when the SDK emitted no stream events: replay the final message as a stream.
function syntheticEvents(state: RunState, model: string) {
	const message = messageBody(state, model);
	const text = message.content[0].text;
	return [
		{ type: 'message_start', message: { ...message, content: [], stop_reason: null } },
		{ type: 'content_block_start', index: 0, content_block: { type: 'text', text: '' } },
		{ type: 'content_block_delta', index: 0, delta: { type: 'text_delta', text } },
		{ type: 'content_block_stop', index: 0 },
		{
			type: 'message_delta',
			delta: { stop_reason: message.stop_reason, stop_sequence: null },
			usage: message.usage
		},
		{ type: 'message_stop' }
	];
}

function sendJson(res: http.ServerResponse, status: number, body: unknown, headers = {}) {
	res.writeHead(status, { 'content-type': 'application/json', ...headers });
	res.end(JSON.stringify(body));
}

function sendError(res: http.ServerResponse, err: ApiError, secret?: string) {
	const message = secret ? err.message.replaceAll(secret, '[redacted]') : err.message;
	const headers = err.retryAfter === undefined ? {} : { 'retry-after': String(err.retryAfter) };
	sendJson(res, err.status, envelope(err.type, message), headers);
}

async function messages(req: http.IncomingMessage, res: http.ServerResponse, deps: Deps) {
	const auth = authenticate(req, deps.accountsDir);
	const parsed: ParsedRequest = parseRequest(await readJson(req));
	const now = deps.now ?? Date.now;

	const abortController = new AbortController();
	res.once('close', () => {
		if (!res.writableFinished) abortController.abort();
	});

	const state = newState();
	let started = false;
	let stopped = false;
	let thrown: unknown;
	const sse = (event: any) => {
		if (res.destroyed) return;
		if (!started) {
			res.writeHead(200, {
				'content-type': 'text/event-stream',
				'cache-control': 'no-cache',
				connection: 'keep-alive'
			});
			res.flushHeaders();
			started = true;
		}
		res.write(`event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`);
	};

	try {
		const q = deps.query({
			prompt: singleUserMessage(parsed.blocks),
			options: {
				model: parsed.model,
				systemPrompt: parsed.system,
				tools: [],
				disallowedTools: [...BUILTIN_TOOL_NAMES],
				maxTurns: 1,
				settingSources: [],
				strictMcpConfig: true,
				persistSession: false,
				includePartialMessages: parsed.stream,
				abortController,
				env: subprocessEnv(auth)
			}
		});
		for await (const m of q) {
			collect(state, m);
			if (parsed.stream && m.type === 'stream_event' && !state.limitRejected) {
				sse(m.event);
				stopped ||= m.event?.type === 'message_stop';
			}
			if (state.limitRejected && !started) break;
		}
	} catch (e) {
		thrown = e;
	} finally {
		abortController.abort();
	}

	if (res.destroyed) return;
	const err = classify(state, thrown, now());
	if (err && !started) return sendError(res, err, auth.secret);
	if (err) {
		const message = auth.secret ? err.message.replaceAll(auth.secret, '[redacted]') : err.message;
		res.write(`event: error\ndata: ${JSON.stringify(envelope(err.type, message))}\n\n`);
		return void res.end();
	}
	if (!parsed.stream) return sendJson(res, 200, messageBody(state, parsed.model));
	if (!started) syntheticEvents(state, parsed.model).forEach(sse);
	else if (!stopped) sse({ type: 'message_stop' });
	res.end();
}

export function createServer(deps: Deps) {
	return http.createServer(async (req, res) => {
		const id = `req_${randomUUID()}`;
		const start = Date.now();
		const url = (req.url ?? '').split('?')[0];
		res.once('close', () =>
			deps.log(
				JSON.stringify({
					id,
					method: req.method,
					path: url,
					status: res.statusCode,
					duration_ms: Date.now() - start
				})
			)
		);
		try {
			if (req.method === 'GET' && url === '/healthz') return sendJson(res, 200, { ok: true });
			if (req.method === 'POST' && url === '/v1/messages') return await messages(req, res, deps);
			sendError(res, new ApiError(404, 'not_found_error', 'not found'));
		} catch (e) {
			if (res.headersSent) return void res.destroy();
			sendError(res, e instanceof ApiError ? e : new ApiError(500, 'api_error', 'internal error'));
		}
	});
}
