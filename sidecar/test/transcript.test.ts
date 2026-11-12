import assert from 'node:assert/strict';
import { test } from 'node:test';
import { CUE, parseRequest, transcriptBlocks } from '../src/request.ts';

const text = (blocks: ReturnType<typeof transcriptBlocks>) =>
	blocks.map((b) => (b.type === 'text' ? b.text : '<image>')).join('');

const turns = [
	{ role: 'user', content: 'one' },
	{ role: 'assistant', content: [{ type: 'text', text: 'two' }] },
	{ role: 'user', content: 'three' },
	{ role: 'assistant', content: 'four' },
	{ role: 'user', content: 'five' }
];

test('deterministic and in order', () => {
	assert.equal(text(transcriptBlocks(turns)), text(transcriptBlocks(turns)));
	assert.equal(
		text(transcriptBlocks(turns)),
		'[user]\none\n\n[assistant]\ntwo\n\n[user]\nthree\n\n[assistant]\nfour\n\n[user]\nfive'
	);
});

test('turn N is a strict prefix of turn N+1', () => {
	for (let n = 1; n < turns.length; n++) {
		const a = text(transcriptBlocks(turns.slice(0, n)));
		const b = text(transcriptBlocks(turns.slice(0, n + 1)));
		assert.ok(b.startsWith(a) && b.length > a.length, `turn ${n}`);
	}
});

test('images stay in place and system stays out of the transcript', () => {
	const img = { type: 'image', source: { type: 'base64', media_type: 'image/png', data: 'AAAA' } };
	const parsed = parseRequest({
		model: 'm',
		system: 'SYS',
		messages: [
			{
				role: 'user',
				content: [{ type: 'text', text: 'before' }, img, { type: 'text', text: 'after' }]
			},
			{ role: 'assistant', content: 'reply' },
			{ role: 'user', content: 'next' }
		]
	});
	assert.equal(parsed.system, 'SYS');
	assert.equal(parsed.blocks.at(-1)!.type === 'text' && (parsed.blocks.at(-1) as any).text, CUE);
	assert.equal(text(parsed.blocks).includes('SYS'), false);
	assert.deepEqual(
		parsed.blocks.map((b) => b.type),
		['text', 'text', 'image', 'text', 'text', 'text', 'text', 'text', 'text']
	);
	assert.equal(
		text(parsed.blocks),
		`[user]\nbefore<image>\nafter\n\n[assistant]\nreply\n\n[user]\nnext${CUE}`
	);
	assert.deepEqual((parsed.blocks[2] as any).source, img.source);
});
