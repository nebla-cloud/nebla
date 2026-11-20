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
one container next to Postgres and installs nothing on the host. The core is AGPL-3.0 with no limit on nodes. The web
panel and the `nebla` CLI are in progress (see [Status](#status)). More at [nebla.dev](https://nebla.dev).

## Quick start

1. Prepare Proxmox: create the role, user, API token and a cloud-init template as described in
   [docs/proxmox-setup.md](docs/proxmox-setup.md).
2. Configure and start the panel:

   ```sh
   cp deploy/.env.example deploy/.env
   # Fill in PANEL_BASE_URL, PANEL_SECRET_KEY (openssl rand -base64 48) and POSTGRES_PASSWORD.
   docker compose -f deploy/compose.yaml up -d --build
   curl -fs localhost:8080/v1/healthz
   ```

3. Create the first admin with `POST /v1/setup`, then add a location, server types, images and IP pools under
   `/v1/admin`. The API is described in [api/openapi.yaml](api/openapi.yaml).
4. To use the panel from a terminal, see the [CLI guide](docs/cli.md).

## Configuration

| Variable | Required | Default | Meaning |
| --- | --- | --- | --- |
| `PANEL_DATABASE_URL` | yes | | Postgres URL. Compose sets it for the bundled database. |
| `PANEL_BASE_URL` | yes | | Public URL of the panel. It builds console URLs, is the allowed websocket origin, and its scheme and host are the trusted origin for API requests. |
| `PANEL_SECRET_KEY` | yes | | At least 32 bytes. Seals Proxmox token secrets. Generate it with `openssl rand -base64 48` and keep it: losing it makes stored secrets unreadable. |
| `PANEL_LISTEN` | no | `:8080` | HTTP listen address. |
| `PANEL_SMTP_URL` | no | | `smtp://user:pass@host:587?from=panel@example.com` or `smtps://...`. Without it, invite mails are not sent. |
| `PANEL_TRUSTED_PROXIES` | no | | Comma-separated CIDRs of reverse proxies. When a request comes from one, the client IP for rate limits is the rightmost `X-Forwarded-For` address that isn't a trusted proxy. Leave it empty when nothing sits in front of the panel. |
| `PANEL_DEFAULT_QUOTA` | no | `servers=5,vcpu=8,memory_mb=16384,disk_gb=200,ips=5` | Quota given to projects created by non-admin users, as comma-separated `key=value` pairs. Keys left out keep their default. `0` in a field means unlimited. Projects created by panel admins are always unlimited, and admins can change any project's quota. |
| `PANEL_MAX_PROJECTS_PER_USER` | no | `3` | How many projects a non-admin user may own. `0` means unlimited. Panel admins are never capped. |

`nebla-server healthcheck` exits 0 when `/v1/healthz` on the local listen port answers 200. The image and compose use it
as their healthcheck.

## Development

```sh
make generate   # ent and oapi-codegen output
make test       # go test ./..., needs Docker for the Postgres test containers
make build      # bin/nebla-server
docker build -t nebla:dev .
```

The end-to-end smoke test in [internal/e2e](internal/e2e/smoke_test.go) creates real VMs, so point it only at a
disposable or nested Proxmox VE. It skips unless `E2E_PVE_URL` is set, and then needs `E2E_PVE_TOKEN_ID`,
`E2E_PVE_SECRET`, `E2E_PVE_FINGERPRINT` (or `E2E_PVE_INSECURE=1`), `E2E_TEMPLATE_VMID`, `E2E_NODE`, `E2E_POOL_CIDR`,
`E2E_GATEWAY`, `E2E_RANGE` (`<start>-<end>`) and `E2E_BRIDGE`. `E2E_DISK_STORAGE` and `E2E_ISO_STORAGE` default to
`local-lvm` and `local`.

```sh
go test -tags e2e -timeout 60m -v ./internal/e2e/
```

## Status

Backend v1: the API, jobs, reconciler, console proxy and metrics. The web UI and the `nebla` CLI come next.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) for the development setup and pull request flow. Contributors sign the
[CLA](CLA.md) once, before their first pull request is merged. Report security issues as described in
[SECURITY.md](SECURITY.md).

## License

[AGPL-3.0](LICENSE).
