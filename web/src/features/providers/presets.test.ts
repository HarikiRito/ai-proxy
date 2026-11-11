import { describe, expect, it } from 'vitest';
import { PRESETS } from './presets';

describe('PRESETS', () => {
	it('lists the eight providers with their base URLs, kinds and cache affinity', () => {
		expect(
			PRESETS.map(({ id, kind, baseUrl, cacheAffinitySeconds }) => [
				id,
				kind,
				baseUrl,
				cacheAffinitySeconds
			])
		).toEqual([
			['openai', 'openai_compatible', 'https://api.openai.com/v1', 600],
			['anthropic', 'anthropic', 'https://api.anthropic.com', 300],
			['groq', 'openai_compatible', 'https://api.groq.com/openai/v1', 600],
			['together', 'openai_compatible', 'https://api.together.ai/v1', 600],
			['fireworks', 'openai_compatible', 'https://api.fireworks.ai/inference/v1', 600],
			['deepseek', 'openai_compatible', 'https://api.deepseek.com', 600],
			[
				'gemini',
				'openai_compatible',
				'https://generativelanguage.googleapis.com/v1beta/openai/',
				600
			],
			['openrouter', 'openai_compatible', 'https://openrouter.ai/api/v1', 600]
		]);
	});
});
