import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { test } from 'node:test';

const entry = new URL('../src/server.ts', import.meta.url).pathname;
const cleanEnv = () => {
	const env: NodeJS.ProcessEnv = { ...process.env, PORT: '0' };
	delete env.ANTHROPIC_API_KEY;
	delete env.ANTHROPIC_AUTH_TOKEN;
	return env;
};

for (const name of ['ANTHROPIC_API_KEY', 'ANTHROPIC_AUTH_TOKEN']) {
	test(`refuses to start when ${name} is set`, () => {
		const r = spawnSync('node', [entry], {
			env: { ...cleanEnv(), [name]: 'x' },
			encoding: 'utf8',
			timeout: 20000
		});
		assert.notEqual(r.status, 0);
		assert.match(r.stderr, new RegExp(name));
	});
}

test('starts and serves /healthz when no api credentials are set', async () => {
	const child = spawn('node', [entry], { env: cleanEnv() });
	try {
		const port = await new Promise<number>((resolve, reject) => {
			child.once('exit', (code) => reject(new Error(`exited ${code}`)));
			child.stdout.on('data', (d) => {
				const m = /"port":(\d+)/.exec(String(d));
				if (m) resolve(Number(m[1]));
			});
		});
		const res = await fetch(`http://127.0.0.1:${port}/healthz`);
		assert.equal(res.status, 200);
		assert.deepEqual(await res.json(), { ok: true });
	} finally {
		child.kill();
	}
});
