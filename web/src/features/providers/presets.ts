export type ProviderKind = 'openai_compatible' | 'anthropic' | 'claude_subscription';

export interface Preset {
	id: string;
	label: string;
	kind: ProviderKind;
	baseUrl: string;
	cacheAffinitySeconds: number;
}

export const DEFAULT_CACHE_AFFINITY_SECONDS = 600;
export const ANTHROPIC_CACHE_AFFINITY_SECONDS = 300;

export const PRESETS: Preset[] = [
	{
		id: 'openai',
		label: 'OpenAI',
		kind: 'openai_compatible',
		baseUrl: 'https://api.openai.com/v1',
		cacheAffinitySeconds: DEFAULT_CACHE_AFFINITY_SECONDS
	},
	{
		id: 'anthropic',
		label: 'Anthropic',
		kind: 'anthropic',
		baseUrl: 'https://api.anthropic.com',
		cacheAffinitySeconds: ANTHROPIC_CACHE_AFFINITY_SECONDS
	},
	{
		id: 'groq',
		label: 'Groq',
		kind: 'openai_compatible',
		baseUrl: 'https://api.groq.com/openai/v1',
		cacheAffinitySeconds: DEFAULT_CACHE_AFFINITY_SECONDS
	},
	{
		id: 'together',
		label: 'Together',
		kind: 'openai_compatible',
		baseUrl: 'https://api.together.ai/v1',
		cacheAffinitySeconds: DEFAULT_CACHE_AFFINITY_SECONDS
	},
	{
		id: 'fireworks',
		label: 'Fireworks',
		kind: 'openai_compatible',
		baseUrl: 'https://api.fireworks.ai/inference/v1',
		cacheAffinitySeconds: DEFAULT_CACHE_AFFINITY_SECONDS
	},
	{
		id: 'deepseek',
		label: 'DeepSeek',
		kind: 'openai_compatible',
		baseUrl: 'https://api.deepseek.com',
		cacheAffinitySeconds: DEFAULT_CACHE_AFFINITY_SECONDS
	},
	{
		id: 'gemini',
		label: 'Gemini',
		kind: 'openai_compatible',
		baseUrl: 'https://generativelanguage.googleapis.com/v1beta/openai/',
		cacheAffinitySeconds: DEFAULT_CACHE_AFFINITY_SECONDS
	},
	{
		id: 'openrouter',
		label: 'OpenRouter',
		kind: 'openai_compatible',
		baseUrl: 'https://openrouter.ai/api/v1',
		cacheAffinitySeconds: DEFAULT_CACHE_AFFINITY_SECONDS
	}
];
