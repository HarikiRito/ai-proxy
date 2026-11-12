import { badRequest } from './errors.ts';

export type Block =
	| { type: 'text'; text: string }
	| { type: 'image'; source: { type: 'base64'; media_type: string; data: string } };

export type ParsedRequest = { model: string; system: string; stream: boolean; blocks: Block[] };

const isObject = (v: unknown): v is Record<string, any> =>
	typeof v === 'object' && v !== null && !Array.isArray(v);

function textOf(block: any, where: string): string {
	if (!isObject(block) || block.type !== 'text' || typeof block.text !== 'string')
		throw badRequest(`unsupported content block in ${where}`);
	return block.text;
}

function imageOf(block: Record<string, any>): Block {
	const s = block.source;
	if (isObject(s) && s.type === 'url') throw badRequest('send images as base64');
	if (
		!isObject(s) ||
		s.type !== 'base64' ||
		typeof s.media_type !== 'string' ||
		typeof s.data !== 'string'
	)
		throw badRequest('invalid image source');
	return { type: 'image', source: { type: 'base64', media_type: s.media_type, data: s.data } };
}

function systemText(system: unknown): string {
	if (system == null) return '';
	if (typeof system === 'string') return system;
	if (Array.isArray(system)) return system.map((b) => textOf(b, 'system')).join('\n\n');
	throw badRequest('invalid system');
}

// Append-only: messages[0..k] always render to the same blocks, so turn N is a prefix of turn N+1.
export function transcriptBlocks(messages: unknown[]): Block[] {
	const blocks: Block[] = [];
	messages.forEach((m, i) => {
		if (!isObject(m) || (m.role !== 'user' && m.role !== 'assistant'))
			throw badRequest('invalid message role');
		const content = typeof m.content === 'string' ? [{ type: 'text', text: m.content }] : m.content;
		if (!Array.isArray(content) || content.length === 0)
			throw badRequest('invalid message content');
		blocks.push({ type: 'text', text: `${i > 0 ? '\n\n' : ''}[${m.role}]\n` });
		content.forEach((b, j) => {
			if (isObject(b) && b.type === 'image') {
				if (m.role !== 'user') throw badRequest('images are only allowed in user messages');
				blocks.push(imageOf(b));
			} else {
				blocks.push({ type: 'text', text: (j > 0 ? '\n' : '') + textOf(b, 'messages') });
			}
		});
	});
	return blocks;
}

export const CUE = '\n\n[assistant]\n';

export function parseRequest(body: unknown): ParsedRequest {
	if (!isObject(body)) throw badRequest('body must be a JSON object');
	if (body.tools != null || body.tool_choice != null) throw badRequest('tools are not supported');
	if (typeof body.model !== 'string' || body.model === '') throw badRequest('model is required');
	if (!Array.isArray(body.messages) || body.messages.length === 0)
		throw badRequest('messages is required');
	const blocks = transcriptBlocks(body.messages);
	blocks.push({ type: 'text', text: CUE });
	return {
		model: body.model,
		system: systemText(body.system),
		stream: body.stream === true,
		blocks
	};
}
