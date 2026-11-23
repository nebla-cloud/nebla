# Floating IPs

A floating IP is an address that belongs to your project, not to a server. Point DNS at it once, then move it to a
replacement or standby server without changing DNS. It stays yours until you delete it, and only one server uses it at
a time.

You can manage floating IPs on the Floating IPs page, on a server's page, with the API, or with the [CLI](cli.md)
(`nebla floating-ip ...`).

## What it is

- Each floating IP is one address: an IPv4 address (`/32`) or an IPv6 address (`/128`).
- It comes from the same pool as the primary IPs of the location you pick.
- It counts against your project's IP quota (`max_ips`) together with primary IPs, whether or not it is assigned.
- Deleting a server doesn't delete its floating IPs. They stay in your project, unassigned, and keep counting toward
  the quota.

## Create

On the Floating IPs page, choose Create, pick IPv4 or IPv6, and pick either a location or a server. With a server, the
floating IP is created in the server's location and assigned to it right away. Name, description and labels are
optional; without a name, one is made from the address.

```sh
nebla floating-ip create --type ipv4 --location home --name web-vip
nebla floating-ip create --type ipv6 --server web1
```

Set exactly one of `--location` and `--server`.

## Assign and run the commands

A floating IP goes only to a server in the same location, and the server needs a primary address of the same family
(IPv4 or IPv6) on the same network as the floating IP. If it doesn't, assigning fails with "This floating IP can't be
used with that server; it's in a different network."

```sh
nebla floating-ip assign web-vip web1
```

Assigning makes the address reachable at the network level. The operating system inside the server also has to use it.
New and rebuilt servers set this up automatically. For a running server, run these commands. The panel shows them after
you assign, and `nebla floating-ip assign` and `nebla floating-ip describe` print them. They look like this for
IPv4 (the panel fills in the address and picks commands that fit the server's operating system):

```sh
sudo ip addr add 203.0.113.10/32 dev eth0
sudo apt-get install -y iputils-arping
sudo arping -U -c 3 -I eth0 203.0.113.10
```

The `arping` line announces the address so the network learns where it moved. For IPv6 the commands are:

```sh
sudo sysctl -w net.ipv6.conf.eth0.ndisc_notify=1
sudo ip -6 addr add 2001:db8::10/128 dev eth0 preferred_lft 0
```

These commands don't survive a reboot. The panel and the CLI also print a second command that makes the address
permanent. On Ubuntu and Debian it writes a file under `/etc/netplan/` and runs `netplan apply`. On other systems, add
the address to `eth0` in your network configuration.

## Move

Assign a floating IP that already has a server to another server. Nebla takes the address away from the first server,
then gives it to the second, so the two never hold it at once. The panel shows the commands for the new server, plus
the commands that remove the address from the old one. Run those on the old server too, or it keeps the address on
its own interface.

If the address is still being released from the previous server, assigning fails with "The address is still being
released from the previous server." That happens when the previous server's host couldn't be reached. The release
finishes on its own once the host is back; try again later. While it is releasing, the floating IP shows status
`releasing`.

## Unassign and delete

Unassign takes the address off its server and keeps it in your project:

```sh
nebla floating-ip unassign web-vip
```

Delete works on assigned and unassigned floating IPs. It unassigns first, waits for the release, and then gives the
address back to the pool. Until then the floating IP shows status `deleting`.

```sh
nebla floating-ip delete web-vip
```

After you unassign or delete, run the removal commands on the server so it stops using the address. A project that
still has floating IPs can't be deleted.

## Limits

- A server can have at most 10 floating IPs.
- Floating IPs count against `max_ips` together with primary IPs, assigned or not.
- A floating IP and its server must be in the same location, on the same network.
- A floating IP is assigned to one server at a time. Assigning it to the server that already has it, or unassigning an
  unassigned one, is refused.
- While an action on a floating IP is running, another one is refused with "the floating IP has a running action".
