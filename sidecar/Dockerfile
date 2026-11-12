FROM node:24-slim
RUN corepack enable
WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile --prod \
	&& ln -s "$(find /app/node_modules/.pnpm -path '*claude-agent-sdk-linux-*/claude' | head -1)" /usr/local/bin/claude \
	&& claude --version
COPY src/ src/
RUN mkdir -p /data/accounts && chown -R node:node /data
USER node
ENV CLAUDE_ACCOUNTS_DIR=/data/accounts
VOLUME /data
EXPOSE 3000
HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=5 \
	CMD ["node", "-e", "fetch('http://127.0.0.1:3000/healthz').then(r => process.exit(r.ok ? 0 : 1), () => process.exit(1))"]
CMD ["node", "src/server.ts"]
