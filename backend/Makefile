MIGRATIONS = migrations
GOLANGCI_LINT = go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
GOVULNCHECK = go run golang.org/x/vuln/cmd/govulncheck@v1.9.0

-include .env
DB_FILE ?= data/app.db
DATABASE_URL ?= file:$(DB_FILE)?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate
export

.PHONY: run generate migrate migrate-diff lint fmt test generate-check check build web

run: migrate
	go run github.com/air-verse/air@latest

generate:
	go generate ./...

# apply pending migrations to $(DB_FILE), creating it when missing (needs the atlas CLI)
migrate:
	mkdir -p "$(dir $(DB_FILE))"
	atlas migrate apply --url "sqlite://$(DB_FILE)?_fk=1" --dir file://$(MIGRATIONS)

# make migrate-diff NAME=add_thing: diff the ent schema against a scratch db replay
migrate-diff:
	rm -f scratch.db
	DATABASE_URL='file:scratch.db?_fk=1&_pragma=foreign_keys(1)' go run ./cmd/migrate diff $(NAME)
	rm -f scratch.db

lint:
	$(GOLANGCI_LINT) run ./...

fmt:
	$(GOLANGCI_LINT) fmt ./...

test:
	go test -race ./...

# fails when generated code or go.mod/go.sum differ from the committed tree
generate-check: generate
	go mod tidy -diff
	git diff --exit-code
	test -z "$$(git ls-files --others --exclude-standard internal/ent internal/graph)"

# build the SPA into the dir embedded by ../web/embed.go
web:
	cd ../web && pnpm install --frozen-lockfile && pnpm build && touch build/.gitkeep

build:
	go build -o bin/api ./cmd/api

check: lint
	$(GOLANGCI_LINT) fmt --diff
	$(MAKE) generate-check
	$(MAKE) test
	$(GOVULNCHECK) ./...
	go build ./...
