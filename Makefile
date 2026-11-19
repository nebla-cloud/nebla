.PHONY: generate test build

generate:
	go tool sqlc generate
	@if [ -f api/openapi.yaml ]; then go tool oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml; fi

test:
	go test ./...

build:
	go build -o bin/nebla-server ./cmd/nebla-server
