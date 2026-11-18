# Deploy (Dokploy)

## Env

| Env | Required | Meaning |
| --- | --- | --- |
| `DATABASE_URL` | yes | SQLite DSN; compose sets it for the `api` service, do not set it in Dokploy |
| `WORKOS_CLIENT_ID` | yes | WorkOS client id (`client_...`) |
| `WORKOS_API_KEY` | yes | WorkOS secret key (`sk_...`) |
| `WORKOS_ISSUER` | yes | exact `iss` of access tokens, e.g. `https://api.workos.com/user_management/<client_id>` |
| `OWNER_EMAIL` | yes | only email allowed into the admin UI, verified in WorkOS |
| `MASTER_KEY` | yes | base64 of 32 bytes: `openssl rand -base64 32`. Encrypts provider keys at rest |
| `WORKOS_DEV_MODE` | no | `true` or `false` (anything else fails startup), default `false`. `true` stores the refresh token in localStorage; needed unless a same-site custom auth domain exists |
| `IMAGE_TAG` | yes | git sha printed by `scripts/build-push.sh`; compose pulls `ai-proxy:<sha>` and `ai-proxy-sidecar:<sha>` from the zot registry, never builds |
| `REGISTRY` | no | default `container.harikidev.win` (anonymous pull off: deploy host needs `docker login`) |
| `PORT` | no | listen port, default `8080`; compose sets `8080` |
| `CORS_ORIGINS` | no | comma separated allowed origins; compose does not pass it, so the SPA is same-origin only |
| `DEBUG` | no | `true` for debug logging, default `false`; compose does not pass it |

The api exits at startup if a required var is missing.

## Dokploy

1. Create a Compose app from this repository, branch `main`, compose path `docker-compose.yml`.
2. Autodeploy off. Images are not built by Dokploy; a push does not produce a new image.
3. Set the env vars above in the app's Environment tab.
4. Domain: service `api`, port `8080`, HTTPS on. The `sidecar` has no published port.
5. Provider form for Claude subscription accounts: sidecar address `http://sidecar:3000`.

## Images (zot registry)

Never build on the deploy host. Images are built on `h-wsl` (linux/amd64) and pushed to `container.harikidev.win` (zot, R2-backed, anonymous pull off).

1. `scripts/build-push.sh staging [git-sha]` (default HEAD). Pushes `ai-proxy:<sha>`, `ai-proxy-sidecar:<sha>` and `staging-latest` tags. Prints `IMAGE_TAG=<sha> DEPLOY_ENV=staging`.
2. Dokploy Environment: set `IMAGE_TAG=<sha>`.
3. Deploy.
4. Tags older than 72h are deleted except `*-latest` and `*-deployed`. Rollback within 72h: set `IMAGE_TAG` to the previous sha, deploy; older: rebuild.

Creds: 1Password item `nlsnuiubtnqzjneo3n7afapqcq` (`Zot registry (public) push/pull`, vault Dev). Deploy host: `docker login container.harikidev.win` with the pull creds.

Local build (no registry): `IMAGE_TAG=local docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build --wait`.

## WorkOS (production)

Dashboard, for the real origin `https://<domain>`:

- Redirect URI: `https://<domain>/auth/callback`
- Sign-in endpoint (initiate login URL): `https://<domain>/auth/login`
- Sign-out redirect: `https://<domain>/`
- Web origin (CORS): `https://<domain>`. Remove any other CORS entry (e.g. localhost); compose sets no `CORS_ORIGINS`.
- Sign-up disabled; invite the `OWNER_EMAIL` account only.

## Backup

Everything lives in two volumes (names are prefixed by the Dokploy project, find them with `docker volume ls`).

- `data`: SQLite db. Consistent copy while the api runs:

  ```sh
  docker run --rm -v <project>_data:/data -v "$PWD":/backup alpine:3.23 \
    sh -c 'apk add -q sqlite && sqlite3 /data/app.db ".backup /backup/app-$(date +%F).db"'
  ```

  Restore: stop the stack, copy the file to `app.db` in the volume (`chown 10001:10001`), start.
- `sidecar-data`: Claude account credentials. Exclude it from backups, or encrypt the backup target. There is no built-in encryption.
- `MASTER_KEY`: losing or changing it makes stored provider keys unrecoverable. Keep a copy in a password manager.

## First run

1. Deploy; wait for `migrate` to exit 0 and `api`/`sidecar` to be healthy.
2. `curl https://<domain>/healthz` returns 200.
3. Sign in at `https://<domain>` as `OWNER_EMAIL`.
4. Add a provider and a virtual model; create a proxy key.
5. Run the smoke test:

   ```sh
   BASE_URL=https://<domain> PROXY_KEY=<key> scripts/smoke.sh   # STREAM=1 adds a stream check, MODEL=<id> picks the model
   ```
