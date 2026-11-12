import { query } from '@anthropic-ai/claude-agent-sdk';
import { createServer } from './app.ts';
import type { QueryFn } from './app.ts';

for (const name of ['ANTHROPIC_API_KEY', 'ANTHROPIC_AUTH_TOKEN']) {
	if (process.env[name]) {
		console.error(`${name} is set: refusing to start (it would bypass the subscription accounts)`);
		process.exit(1);
	}
}

const server = createServer({
	query: query as unknown as QueryFn,
	accountsDir: process.env.CLAUDE_ACCOUNTS_DIR || '/data/accounts',
	log: console.log
});

server.listen(Number(process.env.PORT ?? 3000), '0.0.0.0', () => {
	const { port } = server.address() as { port: number };
	console.log(JSON.stringify({ msg: 'listening', port }));
});

process.on('SIGTERM', () => process.exit(0));
