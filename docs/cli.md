# nebla CLI

`nebla` is a command line client for the Nebla panel. It talks to the panel API with a project token.

## Install

From a checkout:

```sh
go install ./cmd/nebla
```

Or run `make build`, which writes `bin/nebla`.

## Contexts

A context is a panel endpoint plus a project token. Contexts are stored in `cli.yaml` under your user config directory
(`~/.config/nebla` on Linux, `~/Library/Application Support/nebla` on macOS). The file is mode 0600 and the token is
never printed after it is stored.

```sh
nebla context create home --endpoint https://panel.example.com   # asks for the token
nebla context list
nebla context use home
nebla context delete home
```

`context create` checks the endpoint and token before saving. The first context becomes active; pass `--activate` to
switch to a later one. When stdin is not a terminal, the token is read from `NEBLA_TOKEN` or from one line of stdin.

### Which context is used

The first match wins:

1. `--context NAME`
2. `NEBLA_ENDPOINT` and `NEBLA_TOKEN`, both set. The config file is not read. Use this in CI.
3. `NEBLA_CONTEXT`
4. The active context in the config file

If only one of `NEBLA_ENDPOINT` and `NEBLA_TOKEN` is set, commands fail with `NEBLA_ENDPOINT and NEBLA_TOKEN must be set
together`. A lone `NEBLA_TOKEN` is only read by `context create`.

`--config PATH` or `NEBLA_CONFIG` points the CLI at a different config file.

## Commands

```sh
# SSH keys
nebla ssh-key create --name laptop --public-key-from-file ~/.ssh/id_ed25519.pub
nebla ssh-key list
nebla ssh-key delete laptop

# Catalog
nebla image list
nebla server-type list
nebla location list

# Servers
nebla server create --name web1 --type small --image ubuntu --location eu-1 --ssh-key laptop
nebla server list --status running
nebla server describe web1
nebla server rename web1 web2
nebla server poweroff web2        # also: poweron, reboot, reset, shutdown
nebla server rebuild web2 --image debian
nebla server change-type web2 --type medium --upgrade-disk
nebla server ssh web2 -u admin -p 2222 -- -A
nebla server delete web2

nebla version
nebla completion zsh              # also: bash, fish, powershell
```

Servers, keys and other objects can be named by id or by name. A number is tried as an id first, then as a name.

`server create` also takes `--user-data-from-file`, `--label key=value`, `--without-ipv4`, `--with-ipv6` and
`--start-after-create=false`. `server list` takes `--selector key=value`.

## Waiting for actions

Commands that start an action (create, delete, power, rebuild, change-type) wait until it finishes. Progress goes to
stderr, so stdout stays clean for `-o json`.

- `--no-wait` prints the action id and returns.
- `--timeout 5m` stops waiting after that long. The default is to wait forever. The action keeps running on the panel.
- Ctrl-C stops waiting and exits with 130. The CLI prints the action id.

## Output

`-o table` (default), `-o json` or `-o yaml`. List commands print all pages. Add `--no-header` to a list to drop the
table header.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Any error, including a failed action |
| 130 | Interrupted while waiting for an action |

## server ssh

`server ssh <server>` runs your system `ssh`. It uses the server's IPv4 address, or IPv6 with `--ipv6` or when the
server has no IPv4. Arguments after `--` are ssh options, placed before the host (for example `-- -A -L 8080:localhost:80`). They are not a remote command. Without `-u`, the user comes from the image's OS:

| Image OS | User |
| --- | --- |
| `debian` | `debian` |
| `ubuntu` | `ubuntu` |
| `rocky` | `rocky` |
| `almalinux`, `alma` | `almalinux` |
| `fedora` | `fedora` |
| `centos` | `cloud-user` |
| anything else | `root` |

Cloud-init installs the project's SSH keys for that user.
