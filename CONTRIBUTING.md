# Contributing to Nebla

Thanks for helping. This guide covers the development setup, the checks a pull request must pass, and the contributor
licence agreement.

## Development setup

You need:

- Go 1.27 (the version in `go.mod`)
- Docker, because the tests start Postgres with Testcontainers
- Node 26 and pnpm, for the web panel in `web/`
- The Atlas CLI, only if you change the database schema

```sh
make generate   # ent and oapi-codegen output
go vet ./...
make test       # go test ./...
make build      # bin/nebla-server

cd web && pnpm install
pnpm gen:check && pnpm lint && pnpm typecheck && pnpm test && pnpm build
pnpm e2e        # Playwright against a fake Proxmox
```

The end-to-end smoke test in `internal/e2e` creates real VMs. Run it only against a disposable or nested Proxmox VE. The
README lists the variables it needs.

## Making changes

- Open an issue first for anything larger than a small fix, so we can agree on the approach.
- API changes start in `api/openapi.yaml`. Run `make generate` afterwards.
- Schema changes go in the ent schema. Generate the migration with `make migrate-diff name=<name>` and never edit
  migrations by hand.
- Data-only migrations may be created with `atlas migrate new <name>` and written by hand; run `make migrate-hash` after.
- Add or update tests with every behaviour change.
- Keep code comments short: one line, or none when the code says it.

## Commits and pull requests

- Use [conventional commits](https://www.conventionalcommits.org/), for example `feat: add server rename` or
  `fix: free the IP when a create fails`. The pull request title follows the same format.
- Keep each pull request to one change. CI runs the Go checks, the web checks and the Playwright tests, and all of them must pass.
- Describe what changed and how you tested it.

## Contributor licence agreement

Before your first pull request is merged, you sign the [CLA](CLA.md). A bot comments on the pull request with
instructions, and you sign by replying with the sentence it gives you. You only sign once.

The CLA lets the project ship your contribution in the AGPL-3.0 core and in the commercial reseller module. Your
contribution stays available under AGPL-3.0 in this repository.

## Security

Never report vulnerabilities in public issues. See [SECURITY.md](SECURITY.md).
