# sidecar

Node service running the unmodified official Claude Agent SDK. Serves the owner's Claude subscription accounts to the Go proxy as a Claude-shaped upstream (Anthropic Messages API subset). Internal only: compose publishes no port. Owner-only use; subscription use through third-party tooling is a gray area.

## Env

- `PORT` (3000), listens on 0.0.0.0
- `CLAUDE_ACCOUNTS_DIR` (`/data/accounts`): one login directory per account
- `ANTHROPIC_API_KEY` / `ANTHROPIC_AUTH_TOKEN` must be unset: the service exits at startup otherwise

## API

- `GET /healthz` -> `200 {"ok":true}`
- `POST /v1/messages`, headers (missing/invalid -> 401 `authentication_error`):
  - `x-claude-auth-mode: setup_token`, `x-claude-credential: <token>` (from `claude setup-token`)
  - `x-claude-auth-mode: login_dir`, `x-claude-credential: <name>` (`^[a-z0-9_-]{1,64}$`, directory `$CLAUDE_ACCOUNTS_DIR/<name>` must exist)
  - the credential is passed only to that request's Claude subprocess env
- Body: `model`, `system` (string or text blocks), `messages` (text and base64 image blocks), `stream`
- `max_tokens`, `temperature`, `top_p`, `top_k`, `stop_sequences`, `metadata`, `cache_control` and `anthropic-*` headers are accepted and ignored (the SDK has no sampling parameters)
- `tools`/`tool_choice` -> 400, URL images -> 400 `send images as base64`, unknown blocks -> 400
- The conversation is flattened into one append-only `[user]`/`[assistant]` transcript (final cue `[assistant]`) so prompt caching keeps hitting; `system` goes to the SDK system prompt
- Non-stream: Anthropic message JSON with `usage` incl. cache tokens. `stream:true`: SSE of the SDK stream events, `message_stop` added if missing; failure after the first event -> `event: error`
- Errors: `{"type":"error","error":{"type":...,"message":...}}`: 400 `invalid_request_error`, 401 `authentication_error`, 403 `permission_error` (`oauth_org_not_allowed`, `account_on_hold`), 429 `rate_limit_error` + `Retry-After`, 529 `overloaded_error`, 500 `api_error`
- Limits are detected from a rejected `rate_limit_event`, an assistant `rate_limit` error, or a short CLI error text matching `hit your .*limit`. `Retry-After` = reset time from the event; the CLI's text reset time has no timezone and is not parsed, so it falls back to 1800 seconds. Capped at 86400.
- Logs: one JSON line per request (id, method, path, status, duration); never headers, credentials, prompts or replies. The SDK session is not persisted.

## Accounts

Login directory, once per account:

```sh
docker compose exec -it sidecar sh -c 'mkdir -p /data/accounts/<name> && CLAUDE_CONFIG_DIR=/data/accounts/<name> claude auth login'
```

`claude` in the image is the SDK's bundled binary, `/app/node_modules/.pnpm/@anthropic-ai+claude-agent-sdk-linux-<arch>@<version>/node_modules/@anthropic-ai/claude-agent-sdk-linux-<arch>/claude`. Token alternative: run `claude setup-token` on any machine and send it as the credential.

## Develop

```sh
pnpm install --frozen-lockfile && pnpm test && pnpm typecheck && pnpm format:check
```
