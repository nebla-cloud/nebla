# Primary IPs

A primary IP is the public address a server uses as its own. It is a resource of your project, so you can keep the
address when its server is deleted, reserve one before you need it, and put it on a new server. DNS records, customer
allowlists and mail reputation can stay where they are while you replace the machine behind them.

For an address that moves between running servers, use a [floating IP](floating-ips.md). A primary IP changes only
while its server is stopped.

You can manage primary IPs on the Primary IPs page, in the Primary IPs section of a server's Overview tab, with the API, or with the
[CLI](cli.md) (`nebla primary-ip ...`).

## What it is

- Each primary IP is one address: an IPv4 address or an IPv6 address. The two families are separate resources, and a
  server has at most one of each.
- A server created without naming an address gets new primary IPs named `<server>-ipv4` and `<server>-ipv6`.
- Every primary IP counts against your project's IP quota (`max_ips`), together with floating IPs, whether or not it is
  assigned to a server. An unassigned address you keep still uses quota.
- A primary IP stays in one location.

## Keep an address when its server is deleted

Each primary IP has an auto-delete setting. It is on by default: deleting the server deletes the address and gives it
back to the location. Turn it off and the address stays in your project, unassigned, when the server goes away.

```sh
nebla primary-ip update web1-ipv4 --auto-delete=false
nebla server delete web1
```

You can also decide at delete time. The panel's delete dialog has a "Keep" checkbox for each of the server's IPs. On
the command line:

```sh
nebla server delete web1 --keep-ips          # keep every primary IP
nebla server delete web1 --keep-ip web1-ipv4 # keep only this one; repeat the flag for more
```

Both turn auto-delete off for the chosen IPs first and then delete the server, so the setting stays after the delete.
Kept IPs stay unassigned and count toward quota.

## Reserve an address

Create a primary IP before you have a server for it:

```sh
nebla primary-ip create --type ipv4 --location home --name web-ip --keep
```

It is unassigned and ready to use. A reserved IP has auto-delete on by default. If you use it at server create and
later delete that server, the address is deleted with it, unless you create it with `--keep` or turn auto-delete off
(`nebla primary-ip update web-ip --auto-delete=false`). Name, description and labels are optional; without a name it is named
`ip-<type>-<id>`.

Delete an address you no longer need. It has to be unassigned, and it goes back to the location:

```sh
nebla primary-ip delete web-ip
```

## Use an address at create

Pass the address to `server create` in place of a new one. It has to be unassigned and in the server's location, and
it has to be the right family:

```sh
nebla server create --name web2 --type small --image ubuntu --location home --ipv4 web-ip
nebla server create --name web3 --type small --image ubuntu --location home --ipv4 web-ip --ipv6 false
```

`--ipv4` and `--ipv6` take `true` for a new address, `false` for none, or the id or name of an existing primary IP.
`--without-ipv4` and `--with-ipv6` still work. A server can have no public IPv4 or IPv6 at all. In the panel, the create
form offers "New address" or "Use an existing IP" for each family.

The address has to be on the same network as the server's other primary IP, if it has one. If it isn't, create fails
with "This IP can't be used with that server; it's in a different network."

## Assign and unassign

Changing a server's primary IP works only while the server is stopped, because the network setup inside the server is
written when it starts. If the server is running, the request fails with "Stop the server first."

```sh
nebla server shutdown web2
nebla primary-ip unassign web2-ipv4
nebla primary-ip assign web-ip web2
nebla server poweron web2
```

`assign` and `unassign` wait for the change to finish, then print "The new address takes effect when the server
starts." The server keeps its old address until you start it.

- A server can't take a second primary IP of the same family. Unassign the first one.
- An address that is assigned to a server can't go to another server directly. Unassign it first.
- Unassigning is refused while floating IPs of the same family are assigned to the server. Unassign those first.
- After an unassign, the address shows status `releasing` until Nebla has taken it off the old server. It can't be
  assigned or deleted until then. If the server's host was unreachable, the release finishes on its own once the host
  is back.
- Unassigning a server's only IPv4 leaves it with no public IPv4. The panel warns about that: "This server will have no
  public IPv4."

### Servers created before IP changes were supported

Servers created before this feature don't re-read their network settings at boot. The first IP change of one of them
makes the guest treat the next start as a new machine, and the change also turns on boot-time network refresh for the
server. `assign` and `unassign` warn you before that first change:

> This server was created before IP changes were supported. Changing its IP re-runs its cloud-init user data and gives
> it new SSH host keys.

The CLI prints the warning on stderr, and with `-o json` it is in the `warnings` list of the output. Re-running user
data repeats whatever your first-boot scripts do, and new host keys make SSH clients report a changed key. Later IP
changes on the same server keep its identity, so they don't warn and have neither side effect. Rebuilding the server
also avoids the warning, because new and rebuilt servers pick up the change without either side effect.

A primary IP can be protected from deletion, which also keeps it when its server is deleted; see
[Protection](protection.md).

## Limits

- A server has at most one primary IP per family.
- A primary IP and its server have to be in the same location.
- Every primary IP counts against `max_ips`, assigned or not.
- Assign and unassign need the server to be stopped. Delete needs the address to be unassigned.
- While an action on a primary IP is running, another one is refused with "the primary IP has a running action".

## For people who read billing events

Nebla publishes `primary_ip.created` and `primary_ip.deleted` events for primary IPs, whether they came from
`primary-ip create`, from a server create, or from an automatic delete when a server is removed. Addresses that existed
before the upgrade to primary IPs have no `created` event. The upgrade converted them without publishing anything. They
do publish `deleted` when they are deleted, so a consumer should handle a `deleted` for an address it never saw
created.

## For admins

Primary IPs are drawn from the same pools as floating IPs. Pools that share a bridge and VLAN form one network. To use
an existing address on a server that also has the other family's address, both addresses have to be on the same
network. A server with no primary IPs accepts an address from any pool in its location, and moves its network
connection to that pool's network.
