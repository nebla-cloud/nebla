# nebla CLI

`nebla` is a command line client for the Nebla panel. It talks to the panel API with a project token.

## Install

With Homebrew on macOS or Linux:

```sh
brew install nebla-cloud/tap/nebla
```

Or download the archive for your platform from [GitHub Releases](https://github.com/nebla-cloud/nebla/releases),
along with `checksums.txt`. Builds exist for Linux, macOS and Windows on amd64 and arm64. Check the download, then put
`nebla` on your `PATH`:

```sh
sha256sum --ignore-missing -c checksums.txt    # macOS: shasum -a 256 --ignore-missing -c checksums.txt
tar -xzf nebla_*_linux_amd64.tar.gz nebla
sudo install nebla /usr/local/bin/
```

The binaries are not signed. If you download by hand on macOS, run `xattr -d com.apple.quarantine nebla` before the
first run. Homebrew does this for you.

With Go:

```sh
go install github.com/nebla-cloud/nebla/cmd/nebla@latest
```

From a checkout, run `make build`, which writes `bin/nebla`.

The CLI and the panel share version numbers. A CLI works best with a panel of the same version. `nebla version` prints
the version, commit and build date.

## Contexts

A context is a panel endpoint plus a project token. Contexts are stored in `cli.yaml` under your user config directory
(`~/.config/nebla` on Linux, `~/Library/Application Support/nebla` on macOS). The file is mode 0600 and the token is
never printed after it is stored. The file is not locked, so avoid running `context` commands concurrently.

```sh
nebla context create home --endpoint https://panel.example.com   # asks for the token
nebla context list
nebla context use home
nebla context delete home
```

`context create` checks the endpoint and token before saving. The first context becomes active; pass `--activate` to
switch to a later one. A 401 from the panel names the context and endpoint in the error, so you know which one to
recreate. Ctrl-C at the prompts exits with 130. When stdin is not a terminal, the token is read from `NEBLA_TOKEN` or from one line of stdin.

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
nebla server backup enable web2 --keep 7   # also: disable, now, list, delete, restore

# Firewalls
nebla firewall create --name web --label env=prod
nebla firewall list
nebla firewall add-rule web --direction in --protocol tcp --port 22 --source-ips 0.0.0.0/0
nebla firewall apply-to-resource web --type server --server web2
nebla firewall apply-to-resource web --type label_selector --label-selector role=web
nebla firewall delete web

# Floating IPs
nebla floating-ip create --type ipv4 --location home --name web-vip
nebla floating-ip create --type ipv6 --server web1
nebla floating-ip list --server web1
nebla floating-ip assign web-vip web2      # prints the commands to run on the server
nebla floating-ip describe web-vip
nebla floating-ip unassign web-vip
nebla floating-ip delete web-vip

# Private networks
nebla network create --name backend --location home            # the first free 10.N.0.0/16
nebla network create --name db --location home --ip-range 192.168.50.0/24
nebla network list --location home
nebla network describe backend
nebla network update backend --name internal
nebla server create --name db1 --type small --image debian-13 --location home --network backend=10.0.0.5
nebla server attach-to-network web1 --network backend    # prints the commands to run on the server
nebla server detach-from-network web1 --network backend
nebla network delete backend

nebla version
nebla completion zsh              # also: bash, fish, powershell
```

Servers, keys and other objects can be named by id or by name. A number is tried as an id first, then as a name.

Firewalls are allow-lists that filter the public network only. A server can have up to five firewalls and
250 rules across them. IPv4 and IPv6 portions of a rule count separately, and long address lists can
split into multiple rules.

Backups copy a server's boot disk every day and restore it in place; see [Backups](backups.md).

A floating IP is one address that you can move between servers in its location. `assign` and `create --server` wait,
then print the commands to run inside the server; see [Floating IPs](floating-ips.md).

A private network connects servers in one location over their own addresses, apart from the public network. Attach
and detach wait, then print the commands to run inside the server; see [Private networks](networks.md). `--ip` on
`attach-to-network` picks the address, and `--network` on `server create` takes a network and an optional `name=ip`
(repeatable, up to 3).

`server create` also takes `--user-data-from-file`, `--label key=value`, `--without-ipv4`, `--with-ipv6` and
`--start-after-create=false`. `server list` takes `--selector key=value`.

## Waiting for actions

Commands that start an action (create, delete, power, rebuild, change-type) wait until it finishes. Progress goes to
stderr, so stdout stays clean for `-o json`.

- `--no-wait` prints the action id and returns.
- `--timeout 5m` stops waiting after that long. The default is to wait forever. The action keeps running on the panel.
  Both flags exist only on the commands that wait.
- Ctrl-C stops waiting and exits with 130. The CLI prints the action id.

## Output

`-o table` (default), `-o json` or `-o yaml`. List commands print all pages. Add `--no-header` to a list to drop the
table header.

With `-o json` or `-o yaml`, `context list` prints an array of `name`, `endpoint` and `active` (never the token),
`context create` prints that object for the new context, and `ssh-key delete` prints the deleted key.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Any error, including a failed action |
| 130 | Interrupted while waiting for an action |

## server ssh

`server ssh <server>` runs your system `ssh`. It uses the server's IPv4 address, or IPv6 with `--ipv6` or when the
server has no IPv4. Arguments after `--` are ssh options, placed before the host (for example `-- -A -L 8080:localhost:80`). They are not a remote command: a first argument that does not start with `-` is rejected. Without `-u`, the user comes from the image's OS:

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
