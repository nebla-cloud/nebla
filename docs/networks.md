# Private networks

A private network lets servers in the same location talk to each other over their own addresses, apart from the public
network. Use it for a database that only your web servers should reach, or for traffic between servers that you don't
want on the internet.

You can manage private networks on the Networks page, on a server's page, with the API, or with the [CLI](cli.md)
(`nebla network ...`).

## What it is

- A network belongs to your project and lives in one location. Only servers in that location can join it.
- It is IPv4 only. You pick a range, or leave it empty and get the first free `10.N.0.0/16`.
- A range is inside `10.0.0.0/8`, `172.16.0.0/12` or `192.168.0.0/16`, from `/8` to `/28`. It can't overlap the
  location's public addresses or another network of yours in the same location.
- Each server gets one address per network. The network address, the broadcast address and the first host are never
  handed out.
- Traffic on a private network stays inside the location.

## Create

On the Networks page, choose Create, then pick a name and a location. The range is optional.

```sh
nebla network create --name backend --location home
nebla network create --name db --location home --ip-range 192.168.50.0/24
```

If the location has no private networks free right now, creating fails with "No private networks are available in this
location right now." Ask your administrator to add more. A location that doesn't offer private networks says so when
you pick it.

## Attach at create or later

A server can be on up to 3 private networks. Pick networks when you create the server, or attach it later from the
server's page.

```sh
nebla server create --name db1 --type small --image debian-13 --location home --network backend
nebla server create --name db2 --type small --image debian-13 --location home --network backend=10.0.0.5
nebla server attach-to-network web1 --network backend
nebla server attach-to-network web1 --network db --ip 192.168.50.10
```

Without an address, the server gets the lowest free one. If the address you ask for isn't usable, you get "That address
isn't usable on this network." If another server has it, you get "That address is already used on this network."

## Run the commands

Attaching connects the server at the network level. The operating system inside the server also has to use the new
interface. New and rebuilt servers set this up automatically. For a running server, run these commands. The panel shows
them after you attach, and `nebla server attach-to-network` prints them. They look like this (the panel fills in the
address and picks commands that fit the server's operating system):

```sh
sudo ip addr add 10.0.0.5/16 dev eth1
sudo ip link set eth1 up
```

Nebla also updates the server's cloud-init seed, so most servers keep the address from the next reboot on. If the server
is stopped, it gets the address when it starts and you don't need to run anything.

Servers from before IP changes were supported (see [Primary IPs](primary-ips.md)) don't read the updated seed on boot.
For these, the commands don't survive a reboot, so the panel and the CLI also print a second command that makes the
setting permanent. If such a server is stopped, run both after starting it.

The first private network is `eth1`, the second `eth2`, the third `eth3`.

## Detach and delete

Detach a server from a network on the server's page, or with the CLI:

```sh
nebla server detach-from-network web1 --network backend
```

For servers from before IP changes were supported, the panel and the CLI print a command that removes the saved
configuration from the server. If an IP change later moves the server to the new behaviour, remove
`/etc/netplan/60-nebla-net-<id>.yaml` yourself after detaching. Other servers drop the interface's setup at the next boot. The address becomes free for
other servers after a short wait.

To delete a network, detach every server first. Otherwise deleting fails with "Detach all servers from this network
first." followed by the server names.

```sh
nebla network delete backend
```

## Limits

- 3 private networks per server.
- A project has a limit on its number of networks, shown on the project page. It counts networks in all locations.
- A server joins networks only in its own location.
- Firewalls you create filter the public network only; they don't apply to private networks.
- A network can be marked as not taking new servers while the location repairs it: "This network can't take new servers
  right now." Servers already on it keep working.
- If an administrator turns isolation off in a location, creating and attaching pause there: "Private networks are
  paused in this location." Existing networks, detaching and deleting keep working.
- The size of one packet on a private network can be smaller than on the public one (1450 bytes in some locations).
  The panel sets this on the server's interface for you.
