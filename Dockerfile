FROM node:24-alpine AS web
RUN corepack enable
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml web/.npmrc ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.27.4-alpine AS builder
WORKDIR /src
COPY backend/go.mod backend/go.sum backend/
COPY web/go.mod web/
RUN cd backend && go mod download
COPY backend/ backend/
COPY web/embed.go web/
COPY --from=web /src/web/build web/build
RUN cd backend && CGO_ENABLED=0 go build -trimpath -o /out/api ./cmd/api

FROM alpine:3.23
RUN adduser -D -u 10001 app && mkdir /data && chown app /data
USER app
ENV DATABASE_URL="file:/data/app.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"
VOLUME /data
COPY --from=builder /out/api /usr/local/bin/api
EXPOSE 8080
CMD ["api"]
