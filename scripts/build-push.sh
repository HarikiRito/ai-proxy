#!/usr/bin/env bash
# Build api + sidecar images on h-wsl, push to the zot registry. Usage: scripts/build-push.sh <staging|prod> [git-sha]
# Run from any machine: re-execs on h-wsl over ssh. On h-wsl directly: ON_BUILD_HOST=1 scripts/build-push.sh <env> <full-sha>.
set -euo pipefail

ENV_NAME=${1:?usage: build-push.sh <staging|prod> [git-sha]}
[[ $ENV_NAME == staging || $ENV_NAME == prod ]] || { echo "env must be staging|prod" >&2; exit 2; }
REGISTRY=${REGISTRY:-h-wsl.tailc71524.ts.net:8443}
REPO_URL=${REPO_URL:-https://github.com/HarikiRito/ai-proxy.git}

if [[ ${ON_BUILD_HOST:-} != 1 ]]; then
  SHA=$(git rev-parse "${2:-HEAD}")
  exec ssh h-wsl "ON_BUILD_HOST=1 REGISTRY=$REGISTRY REPO_URL=$REPO_URL bash -s -- $ENV_NAME $SHA" < "$0"
fi

SHA=${2:?git sha required on h-wsl}
export PATH=$HOME/.local/bin:$PATH

WORK=$(mktemp -d)
export DOCKER_CONFIG=$WORK/docker
mkdir -p "$DOCKER_CONFIG"
trap 'docker logout "$REGISTRY" >/dev/null 2>&1 || true; rm -rf "$WORK"' EXIT

git -C "$WORK" init -q src
git -C "$WORK/src" -c credential.helper='!gh auth git-credential' fetch -q --depth 1 "$REPO_URL" "$SHA"
git -C "$WORK/src" checkout -q FETCH_HEAD
SHA=$(git -C "$WORK/src" rev-parse HEAD)

op read "op://Dev/Zot registry push/password" | docker login "$REGISTRY" -u "$(op read 'op://Dev/Zot registry push/username')" --password-stdin >/dev/null

API=$REGISTRY/ai-proxy
SIDECAR=$REGISTRY/ai-proxy-sidecar
cd "$WORK/src"
docker buildx build --platform linux/amd64 --push -t "$API:$SHA" -t "$API:$ENV_NAME-latest" .
docker buildx build --platform linux/amd64 --push -t "$SIDECAR:$SHA" -t "$SIDECAR:$ENV_NAME-latest" ./sidecar

echo "IMAGE_TAG=$SHA DEPLOY_ENV=$ENV_NAME"
