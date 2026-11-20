# Security policy

## Reporting a vulnerability

Report vulnerabilities privately through GitHub's
[private vulnerability reporting](https://github.com/nebla-cloud/nebla/security/advisories/new) (the Security tab,
then "Report a vulnerability"). Do not open a public issue, discussion or pull request for a security problem.

Please include:

- the Nebla version or commit
- what an attacker can do, and what access they need first
- steps to reproduce, or a proof of concept

You will get an acknowledgement, and updates as the report is investigated and fixed. Once a fix is released, the
advisory is published with credit to you, unless you prefer not to be named.

## Supported versions

Security fixes go into the latest release. Upgrade to it before reporting, if you can.

## Scope

In scope: the Nebla server, its API, the web panel, the CLI, and the container image built from this repository.

Out of scope: Proxmox VE itself, and installs where the Proxmox API token has more privileges than
[docs/proxmox-setup.md](docs/proxmox-setup.md) describes.
