.PHONY: generate migrate-diff migrate-hash migrate-validate test build web

MIGRATIONS_DIR := file://internal/db/migrations?format=goose
ATLAS_DEV_URL := docker://postgres/17/dev?search_path=public

generate:
	go generate ./internal/ent
	@if [ -f api/openapi.yaml ]; then go tool oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml; fi
	@if command -v pnpm >/dev/null 2>&1; then cd web && pnpm gen; fi

web:
	cd web && pnpm install --frozen-lockfile && pnpm build

# Writes a goose-format migration from the ent schema; needs the atlas CLI and Docker.
migrate-diff:
	@test -n "$(name)" || (echo "usage: make migrate-diff name=<name>" && exit 1)
	atlas migrate diff $(name) --dir "$(MIGRATIONS_DIR)" --to "ent://internal/ent/schema" --dev-url "$(ATLAS_DEV_URL)"

# Rewrites atlas.sum after a migration file changes.
migrate-hash:
	atlas migrate hash --dir "$(MIGRATIONS_DIR)"

# migrate lint is Atlas Pro only, so validate replays the dir on a dev database instead.
migrate-validate:
	atlas migrate validate --dir "$(MIGRATIONS_DIR)" --dev-url "$(ATLAS_DEV_URL)"

test:
	go test ./...

build:
	go build -o bin/nebla-server ./cmd/nebla-server
