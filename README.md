<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
    <img src="docs/assets/logo-light.svg" alt="Nebla" height="64">
  </picture>
</p>

<p align="center">
  <a href="https://github.com/nebla-cloud/nebla/actions/workflows/ci.yml"><img src="https://github.com/nebla-cloud/nebla/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-AGPL--3.0-15181D" alt="License: AGPL-3.0"></a>
  <a href="https://github.com/nebla-cloud/nebla/releases"><img src="https://img.shields.io/github/v/release/nebla-cloud/nebla?include_prereleases&color=15181D" alt="Latest release"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/nebla-cloud/nebla?color=15181D" alt="Go version"></a>
</p>

# Nebla

Nebla is an open-source cloud console for Proxmox VE. Users get projects, API tokens, SSH keys and servers with fixed
types, images and IP pools, with quotas and tenant isolation built in. Nebla drives Proxmox only through its API, runs as
one container next to Postgres and installs nothing on the host. The core is AGPL-3.0 with no limit on nodes. A web
panel ships in the binary, and the `nebla` CLI works from a terminal (see [Status](#status)). More at [nebla.dev](https://nebla.dev).

## Quick start

1. Prepare Proxmox: create the role, user, API token and a cloud-init template as described in
   [docs/proxmox-setup.md](docs/proxmox-setup.md).
2. Configure and start the panel:

   ```sh
   cp deploy/.env.example deploy/.env
   # Fill in PANEL_BASE_URL, PANEL_SECRET_KEY (openssl rand -base64 48) and POSTGRES_PASSWORD.
   docker compose -f deploy/compose.yaml up -d
   curl -fs localhost:8080/v1/healthz
   ```

   This runs the published image `ghcr.io/nebla-cloud/nebla:0.2`. To upgrade to a later minor release, change the
   tag in `deploy/compose.yaml` (or set `NEBLA_IMAGE` in `deploy/.env`), then run
   `docker compose -f deploy/compose.yaml pull` and `up -d` again. To build from your checkout instead, run
   `NEBLA_IMAGE=nebla:dev docker compose -f deploy/compose.yaml up -d --build`.

3. Open the panel at your `PANEL_BASE_URL` (`http://localhost:8080` for a local run), create the first admin account,
   and follow Get Nebla ready. It walks you through connecting Proxmox, turning on OS images, adding server types and
   adding an IP pool. The API is described in [api/openapi.yaml](api/openapi.yaml).
4. To use the panel from a terminal, install the CLI with `brew install nebla-cloud/tap/nebla` or a binary from
   [GitHub Releases](https://github.com/nebla-cloud/nebla/releases). See the [CLI guide](docs/cli.md#install).

## Configuration

The panel reads its settings from environment variables. With compose they go in `deploy/.env`.

### Required

- **`PANEL_DATABASE_URL`**: Postgres URL. Compose sets it for the bundled database.
- **`PANEL_BASE_URL`**: Public URL of the panel. It builds console URLs, is the allowed websocket origin, and its
  scheme and host are the trusted origin for API requests.
  - Passkeys need it on https with a domain name (or `http://localhost`).
  - Changing its host makes existing passkeys stop working, though passwords still work.
- **`PANEL_SECRET_KEY`**: At least 32 bytes. Seals Proxmox token secrets. Generate it with `openssl rand -base64 48`
  and keep it: losing it makes stored secrets unreadable.

### Optional

| Variable | Default |
| --- | --- |
| `PANEL_LISTEN` | `:8080` |
| `PANEL_SMTP_URL` | none |
| `PANEL_TRUSTED_PROXIES` | none |
| `PANEL_DEFAULT_QUOTA` | `servers=5,vcpu=8,memory_mb=16384,disk_gb=200,ips=5` |
| `PANEL_MAX_PROJECTS_PER_USER` | `3` |

- **`PANEL_LISTEN`**: HTTP listen address.
- **`PANEL_SMTP_URL`**: `smtp://user:pass@host:587?from=panel@example.com` or `smtps://...`. Without it, invite mails
  are not sent.
- **`PANEL_TRUSTED_PROXIES`**: Comma-separated CIDRs of reverse proxies. When a request comes from one, the client IP
  for rate limits is the rightmost `X-Forwarded-For` address that isn't a trusted proxy. Leave it empty when nothing
  sits in front of the panel.
- **`PANEL_DEFAULT_QUOTA`**: Quota given to projects created by non-admin users, as comma-separated `key=value` pairs.
  Keys left out keep their default, and `0` in a field means unlimited. Projects created by panel admins are always
  unlimited, and admins can change any project's quota.
- **`PANEL_MAX_PROJECTS_PER_USER`**: How many projects a non-admin user may own. `0` means unlimited. Panel admins are
  never capped.

### Healthcheck

`nebla-server healthcheck` exits 0 when `/v1/healthz` on the local listen port answers 200. The image and compose use it
as their healthcheck.

## Development

```sh
make generate   # ent and oapi-codegen output
make test       # go test ./..., needs Docker for the Postgres test containers
make build      # bin/nebla-server
docker build -t nebla:dev .
```

The web panel is a Vue app in [web](web), embedded in the binary at build time. It needs Node 26 or newer and pnpm.
`go build` works without Node: the binary then serves a placeholder page instead of the panel.

```sh
cd web && pnpm install
pnpm dev     # Vite dev server, proxies API calls to the panel on :8080
pnpm test    # unit tests
pnpm build   # type-checks and writes web/dist, which the Go build embeds
pnpm e2e     # Playwright against a fake Proxmox
```

Run the panel on `:8080` first (the compose setup above works) so `pnpm dev` has an API to talk to.

The end-to-end smoke test in [internal/e2e](internal/e2e/smoke_test.go) creates real VMs, so point it only at a
disposable or nested Proxmox VE. It skips unless `E2E_PVE_URL` is set, and then needs `E2E_PVE_TOKEN_ID`,
`E2E_PVE_SECRET`, `E2E_PVE_FINGERPRINT` (or `E2E_PVE_INSECURE=1`), `E2E_TEMPLATE_VMID`, `E2E_NODE`, `E2E_POOL_CIDR`,
`E2E_GATEWAY`, `E2E_RANGE` (`<start>-<end>`) and `E2E_BRIDGE`. `E2E_DISK_STORAGE` and `E2E_ISO_STORAGE` default to
`local-lvm` and `local`.

```sh
go test -tags e2e -timeout 60m -v ./internal/e2e/
```

## Status

The API, jobs, reconciler, console proxy and metrics are done, and the web panel covers them:

- first-admin setup, login with passwords, TOTP and passkeys, and account settings
- projects, members and invites, API tokens and SSH keys
- servers: create, power actions, rebuild, change type, the web console, metrics graphs and activity
- firewalls: inbound and outbound rules, applied to servers directly or by label
- the admin area: one-line Proxmox setup, locations, OS images, server types, IP pools, users, projects, settings and
  activity

The `nebla` CLI is described in the [CLI guide](docs/cli.md).

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) for the development setup and pull request flow. Contributors sign the
[CLA](CLA.md) once, before their first pull request is merged. Report security issues as described in
[SECURITY.md](SECURITY.md).

## License

[AGPL-3.0](LICENSE).
