# Proxmox VE setup

Nebla talks only to the Proxmox VE API. It never uses SSH. Nebla installs nothing on the host; the optional setup script is
one-shot and leaves only the roles, user, ACLs and token Proxmox stores. Each Nebla location points at one Proxmox
cluster (or standalone node) through an API token. Setup runs once per cluster, in a root shell on any node.

Nebla supports Proxmox VE 8 and 9.

## Quick setup

In the panel, open Locations, then Connect a location. Enter a name and choose Get setup command. The page shows one
command:

```sh
curl -fsSL '<panel>/v1/enroll.sh' | NEBLA_TOKEN=nbe_... bash
```

Run it as root on any node of the cluster. The command is valid for 30 minutes and works once. Choose New command on the
page to get another. You can read the script first at `<panel>/v1/enroll.sh`. It is plain text.

The script needs root and Proxmox VE 8 or 9, and refuses anything else. It checks with the panel before it changes
anything. Then it:

- creates or resets the roles `Nebla` and `NeblaISO`, and `NeblaBackup` when you pass `--backup-storage` (see
  [Backups](#10-backups)). `Nebla` gets the privileges from step 1 for your PVE version, plus
  `Sys.AccessNetwork` and `VM.Config.HWType` when this install knows them.
- creates the user `nebla@pve` if it is missing.
- grants `Nebla` on `/`, and `NeblaISO` on every storage whose content includes `iso`, `import` or `images` (`images`
  for volumes, see [Volumes](#9-volumes)).
- creates a new API token named `nebla@pve!nebla-<location>-<6 chars>`, with privilege separation off. It never removes
  a token, and it refuses to run if that token already exists.
- reports the token, the certificate fingerprint of every node (`pveproxy-ssl.pem`, else `pve-ssl.pem`), and the
  cluster and storage list.

To also prepare private networks, add `NEBLA_SDN_VNETS=<n>` to the command (see [Private networks](#11-private-networks)).
It is off by default.

It writes no files and leaves nothing running. The roles are reset to the exact privilege list each time, so privileges
you added to `Nebla` or `NeblaISO` by hand are removed on a re-run.

The report goes back to the enrollment page. You have 24 hours after the node reports to finish there; after that,
cancel and run setup again. Then pick the Proxmox URL (prefilled from the node's IP, port 8006), the disk
storage (one with `images` content), the ISO storage, and optionally an import storage. Only storages that got
`NeblaISO` are offered for ISO and import. The panel connects to the URL and checks that the certificate it sees is one
the node reported. It pins that certificate.

`NeblaISO` lets the token delete any volume on its storage (see step 1). The script doesn't know yet which storages
you'll pick, so it grants it on every candidate. After you finish, the page lists optional cleanup commands for the
storages the location doesn't use. It keeps the ISO and import storages, and the volume storage (every node's disk
storage when the location has no volume storage of its own):

```sh
pveum acl delete /storage/<id> --users nebla@pve --roles NeblaISO
```

### If the node can't reach the panel

Open "The node can't reach this panel?" on the enrollment page. It gives an inline script that makes no network calls.
Run it on the node. It prints one line starting with `nebla-enroll:v1:`. Paste that line into the page. The line
contains the token secret, so clear your terminal scrollback afterwards. The online script prints the same line if it
can't send its report.

### Re-run setup

Use Re-run setup on the location page to run the same flow again for an existing location, for example after a
certificate renewal. The location switches to the new token and the refreshed fingerprints when you finish. Until then
the old token keeps working. The finish form starts from the location's current URL and storages. If a re-run is
already in progress, Re-run setup opens it instead of starting another. After a re-run the page shows the command that removes the old token:

```sh
pveum user token remove nebla@pve <old token>
```

### Removal

To remove everything the script created:

```sh
pveum user delete nebla@pve
pveum role delete Nebla
pveum role delete NeblaISO
```

Deleting the user removes its tokens and ACLs.

## Manual setup

Steps 1 to 3 do by hand what the script does. Use them if you can't run the script.

## 1. Roles

Nebla uses two roles: `Nebla` on `/` for VMs, and `NeblaISO` on single storages: the ISO storage, so it can delete the
cloud-init seed ISOs it uploads, plus the import and volume storages (step 8 and [Volumes](#9-volumes)).

On **PVE 8**:

```sh
pveum role add Nebla --privs "VM.Allocate VM.Clone VM.Config.CDROM VM.Config.CPU VM.Config.Disk VM.Config.Memory VM.Config.Network VM.Config.Options VM.Config.Cloudinit VM.PowerMgmt VM.Console VM.Backup VM.Audit VM.Monitor Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit Sys.Audit SDN.Use SDN.Audit"
```

On **PVE 9**, `VM.Monitor` no longer exists. Use `VM.GuestAgent.Audit` in its place:

```sh
pveum role add Nebla --privs "VM.Allocate VM.Clone VM.Config.CDROM VM.Config.CPU VM.Config.Disk VM.Config.Memory VM.Config.Network VM.Config.Options VM.Config.Cloudinit VM.PowerMgmt VM.Console VM.Backup VM.Audit VM.GuestAgent.Audit Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit Sys.Audit SDN.Use SDN.Audit"
```

`SDN.Audit` is read-only. Nebla uses it to see which zones and VNets exist (see [Private networks](#11-private-networks)).
The setup script grants it. If you set up before private networks existed, the location page shows a "needs setup"
banner. Add the privilege to the existing role:

```sh
pveum role modify Nebla --append 1 --privs SDN.Audit
```

`VM.Config.Cloudinit` lets Nebla remove a Proxmox cloud-init drive that a cloned template carries. That drive would clash
with Nebla's own seed ISO, because both use the `cidata` label.

On both versions, also create the ISO cleanup role:

```sh
pveum role add NeblaISO --privs "Datastore.Allocate Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit"
```

Proxmox only lets a user delete an ISO volume with `Datastore.Allocate` on its storage. Without it, deleting or
rebuilding a server fails and seed ISOs pile up. `Datastore.Allocate` also lets the token delete any other volume on that
storage, so grant it only on the storages that need it (step 2), never on `/`. A storage dedicated to Nebla's ISOs keeps this tight.

A role on a more specific path such as `/storage/<iso_storage>` replaces the inherited `/` grant on that path, it doesn't
add to it. So `NeblaISO` repeats every `Datastore.*` privilege of the main role: `Datastore.AllocateTemplate` for ISO
uploads, plus the rest, which also covers an ISO storage that doubles as the disk storage.

## 2. User, permissions and API token

```sh
pveum user add nebla@pve --comment "Nebla panel"
pveum aclmod / --users nebla@pve --roles Nebla
pveum aclmod /storage/<iso_storage> --users nebla@pve --roles NeblaISO
pveum aclmod /storage/<volume_storage> --users nebla@pve --roles NeblaISO
pveum user token add nebla@pve panel --privsep 0
```

Replace `<iso_storage>` with the location's `iso_storage`, for example `local`, and `<volume_storage>` with the storage
volumes go to (see [Volumes](#9-volumes)). Skip the volume line if you don't want volumes in this location. `--privsep 0` turns privilege
separation off, so the token gets the user's permissions. The last command prints the token once:

- the token ID is `nebla@pve!panel`
- the `value` is the token secret

Store the secret somewhere safe. Nebla seals it at rest with `PANEL_SECRET_KEY`.

## 3. TLS fingerprint

Nebla pins the SHA-256 fingerprint of the certificate the API serves. Read it on the node the location URL points at:

```sh
pvenode cert info
```

Use the `fingerprint` of the certificate that `pveproxy` serves. That's `pveproxy-ssl.pem` if you uploaded a custom
certificate or use ACME, and `pve-ssl.pem` otherwise. Paste it, in colon-hex form, into the location's
`ca_fingerprint`. Every node has its own certificate, so use the fingerprint of the node in the URL. If the certificate
is renewed, update the location with the new fingerprint, or use Re-run setup on the location page to refresh it.

## 4. Storages

Each location names two required storages:

- `disk_storage` holds server disks. It must support the `images` content type, for example `local-lvm`, a ZFS pool, or
  Ceph RBD.
- `iso_storage` holds the per-server cloud-init seed ISOs Nebla uploads. It must support the `iso` content type, for
  example `local`.

List what each node has:

```sh
pvesm status --content images
pvesm status --content iso
```

To add `iso` content to an existing directory storage, for example `local`:

```sh
pvesm set local --content iso,vztmpl,backup
```

Keep the content types the storage already had in that list. A single node can use a different disk storage through the
node's `disk_storage_override` in the admin API.

Two more are optional: `import_storage` for built-in OS images (step 8) and `volume_storage` for volumes (section 9).

## 5. Cloud-init template (Debian 13)

Servers are full clones of a template VM. Nebla attaches its own NoCloud seed ISO as `ide2`. A Proxmox cloud-init drive
isn't needed. If the template has one, on any slot, Nebla removes it from each clone.

```sh
cd /var/tmp
wget https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2

qm create 9000 --name debian-13 --ostype l26 --cores 1 --memory 1024 \
  --net0 virtio,bridge=vmbr0 --scsihw virtio-scsi-single
qm set 9000 --scsi0 local-lvm:0,import-from=/var/tmp/debian-13-genericcloud-amd64.qcow2
qm set 9000 --boot order=scsi0
qm set 9000 --agent enabled=1
qm template 9000
```

On older setups without `import-from`, import the disk first and then attach it:

```sh
qm importdisk 9000 /var/tmp/debian-13-genericcloud-amd64.qcow2 local-lvm
qm set 9000 --scsi0 local-lvm:vm-9000-disk-0
```

Template requirements:

- **A VGA display.** A serial-only display (`vga: serial0`) leaves the browser console blank. Nebla doesn't change
  `vga`, so fix the template: `qm set <tpl> --vga std`.
- **The QEMU guest agent is recommended.** The genericcloud image doesn't ship it. Install it in your image or with user
  data (`packages: [qemu-guest-agent]`). Power actions and metrics work without it.

Notes:

- Nebla sets cores, memory, the network card and tags on every clone, and grows the disk to the server type's
  size. Keep the template disk no larger than your smallest server type, because disks never shrink.
- Nebla tags its server VMs `nebla` and `nb-srv-<id>`, and the catalog templates it builds `nebla-tpl`. It only touches
  VMs tagged `nebla` (servers) or `nebla-tpl` (catalog templates it built).
- Run one Nebla install per Proxmox cluster. Before reinstalling Nebla with a fresh database, remove the VMs tagged
  `nebla`, because the new install reuses server ids.

## 6. Register the template as an image

An image template is a `(location, vmid, node)` triple:

- **Template on shared storage** (Ceph, NFS and so on): omit `node`. Any node in the location can clone it.
- **Template on local storage**, such as `local-lvm`: set `node` to the node that holds it. Servers from that template
  are placed on that node only. A template without a node must sit on shared storage, otherwise the API returns
  `template_not_shared`.

For several nodes with local storage, create the template once per node, with a different VMID on each, and list each as
its own template entry.

## 7. Networking

IP pools name the bridge (`vmbr0`, for example) and an optional VLAN tag. Nebla enables the VM firewall on each server
with IP and MAC filtering on `net0`, and sets its input and output policies to `ACCEPT`. The filters stop a server from
using addresses it wasn't given; they don't block traffic to or from its own addresses.

Enable the firewall at the datacenter level (Datacenter > Firewall > Options), otherwise the VM firewall and its filters
don't apply. Datacenter rules govern traffic to the Proxmox hosts, not to the VMs. Before enabling it, make sure those
rules still allow your own access to the hosts (SSH and the web UI on port 8006).

User firewalls (the rules people manage on a project's Firewalls page) need the datacenter firewall on as well. Nebla
writes each rule directly into the server's own VM firewall rules, on its public network interface. It doesn't use Proxmox
security groups and needs no extra privileges. The location page in the admin area warns when the
datacenter firewall is off. Proxmox loads rule changes on a cycle of about 10 seconds, so Nebla waits about 12 seconds
before the first start of a server that has firewalls, so the rules are in place when it boots.

Floating IPs can move between servers. IPv4 moves rely on Proxmox's ebtables ARP filtering, which is on by default
(`ebtables: 1` in the datacenter firewall options). Keep it on.

## 8. Built-in OS images

Nebla can build its own cloud-init templates from official cloud images, so you don't have to run step 5 by hand. An
admin enables an image such as `debian-13` for a location, and Nebla downloads it to the nodes and builds the templates.
Hand-registered images (step 6) keep working on every Proxmox version.

Requirements:

- **PVE 8.4 or later** on every node that should build templates. The build downloads the image into an `import`
  storage, which older versions don't support. Nebla checks this per node.
- **An import storage.** Set the location's `import_storage` to a file storage whose content types include `import`.
  To add it to an existing directory storage, for example `local`:

  ```sh
  pvesm set local --content iso,vztmpl,backup,import
  ```

  Keep the content types the storage already had in that list. Leaving `import_storage` empty turns built-in images off
  for the location.
- **`Sys.AccessNetwork` and `VM.Config.HWType`** for the token's user. `Sys.AccessNetwork` lets Proxmox start a
  download from a URL; Proxmox versions without this privilege need `Sys.Modify` on `/` instead. `VM.Config.HWType`
  lets Nebla set the guest OS type and SCSI controller when it creates the template VM. The simplest option is to add
  both to the existing role:

  ```sh
  pveum role modify Nebla --append 1 --privs "Sys.AccessNetwork VM.Config.HWType"
  ```

  If you grant `Sys.AccessNetwork` on `/nodes` through a separate role instead, include `Sys.Audit` in that role as well. An ACL on
  `/nodes` replaces the privileges the user inherits from `/` at `/nodes/*`. Without `Sys.Audit` there, Proxmox stops
  reporting node CPU and memory, which Nebla reads.
- **`NeblaISO` on the import storage.** Nebla downloads the image, builds the template from it, then deletes the
  downloaded file. Deleting needs `Datastore.Allocate` on the storage:

  ```sh
  pveum aclmod /storage/<import_storage> --users nebla@pve --roles NeblaISO
  ```

  If the import storage is also the ISO storage, step 2 already covers it.
- **Outbound internet.** Each node that builds a template must reach the image's download site over HTTPS. The panel
  needs outbound HTTPS too, to look up the newest image and its checksum.

Catalog templates are tagged `nebla-tpl`, never `nebla`, and Nebla only touches templates it built. Servers created from
them run with the QEMU guest agent disabled, because the images don't ship it. Power actions use ACPI, and metrics work
without it.

## 9. Volumes

Volumes are disks that project members create, attach to servers, grow and delete on their own. Each one lives on the
location's volume storage. The [volumes guide](volumes.md) covers the user side.

**Where volumes go.** The location's `volume_storage` (Volume storage on the location form) names a storage with
`images` content. Left empty, it means each node's disk storage, `disk_storage_override` included. On a storage that
only one node has, such as `local-lvm`, a volume stays on the node it was created on and only attaches to servers there.
A shared storage such as Ceph RBD has no such limit. A node where the storage doesn't exist simply can't hold volumes.

**Permission.** Proxmox only deletes a volume for a token with `Datastore.Allocate` on its storage, so grant `NeblaISO`
on the volume storage, or on every node's disk storage when `volume_storage` is empty:

```sh
pveum acl modify /storage/<id> --users nebla@pve --roles NeblaISO
```

The setup script does this for you. The token already holds `VM.Allocate` on `/`, so this adds little: Nebla only
deletes disks whose name it gave them. The grant is per storage, never on `/`.

Nebla checks the grant with Proxmox when nodes sync and when the location is saved. Until every storage volumes would use
has it, the location page says "Volumes need one more permission" with the command to run, and members don't see
volumes in that location. Locations connected before volumes existed show this too. Re-run setup or run the command,
then choose Sync nodes.

**How volumes look in Proxmox.** A volume is a disk named `vm-999999999-nb-vol-<id>` on the volume storage, owned by
VMID 999999999. That VMID is a placeholder; no VM with it exists. Never create a VM with ID 999999999: Proxmox would
count every volume as that VM's disk, and deleting the VM would delete them all.

An attached volume shows up in the server VM's config as `virtio1` to `virtio15`, with `serial=nb-vol-<id>` and
`backup=0`. `backup=0` keeps volumes out of vzdump backups of the server. Destroying the server VM in Proxmox leaves the volume in place, because the VM doesn't own it.

Don't rename, move or delete these disks by hand. Nebla only touches disks with this exact name, and it marks a volume
whose disk disappeared as missing.

## 10. Backups

Backups are daily copies of a server's boot disk that go to a Proxmox Backup Server (PBS). Each location has its own
backup storage. The [backups guide](backups.md) covers the user side.

**Add the storage.** Add the PBS datastore to Proxmox as a storage of type `pbs`, in Datacenter > Storage or with
`pvesm add pbs`. Nebla doesn't create it and never holds PBS credentials.

**Grant access.** Run setup again with the storage ID. On the location page, use Re-run setup and add the flag to the
command:

```sh
curl -fsSL '<panel>/v1/enroll.sh' | NEBLA_TOKEN=nbe_... bash -s -- --backup-storage <id>
```

The script checks that the storage exists and is of type `pbs`, then grants the access below. Running it again with the flag
grants `NeblaBackup` again.

**Pick it.** Finishing a setup run that had the flag sets the location's Backup storage to that ID. To change or clear
it, or for a location you aren't re-enrolling, choose Edit connection on the location page and set Backup storage there.
Set the Backup window (UTC) too. It defaults to 02:00 to 06:00 and may cross midnight. Nebla spreads servers across it. Leave Backup
storage blank to turn backups off for the location. Existing backups stay restorable.

**Roles.** Backups need two things:

- `Nebla` gets `VM.Backup`, so it can back up and restore its servers' VMs. The script adds it to the role. By hand:

  ```sh
  pveum role modify Nebla --append 1 --privs "VM.Backup"
  ```

- `NeblaBackup` has `Datastore.AllocateSpace` and `Datastore.Audit`, granted on `/storage/<backup_storage>` only. It lets
  Nebla write, list and delete backups on that storage. It doesn't need `Datastore.Allocate`.

  ```sh
  pveum role add NeblaBackup --privs "Datastore.AllocateSpace Datastore.Audit"
  pveum acl modify /storage/<id> --users nebla@pve --roles NeblaBackup
  ```

Nebla checks the access with Proxmox when nodes sync and when the location is saved. Until it passes, the location page
says "Backups need one more permission" with the commands to run, and members don't see backups in that location. Run
them, then choose Sync nodes.

**What Nebla does on the PBS.** Each backup is a `vzdump` of one server in snapshot mode. It carries the note
`nebla server <server id> backup <backup id>`, so you can tell Nebla's backups apart in the PBS interface. Nebla only
deletes backups it recorded itself. It never prunes a group, so your own backup jobs on the same datastore, and backups
of an earlier server with the same VMID, are safe.

**Housekeeping stays with you.** Nebla doesn't run garbage collection or verify jobs on the PBS. Schedule them there.
Deleting a backup frees its space only after a garbage collection. If the datastore has a prune job, leave backups with
a note starting `nebla server` out of it, or keep it looser than the members' Keep setting. Never prune or delete
those backups by hand. Nebla would mark them missing, and a server could lose the backups the member counts on.

**Changing the storage.** Each backup remembers its storage. After you change a location's Backup storage, old backups
stay restorable and deletable on the old one, so keep it connected until they rotate out. The location page shows how
many backups remain on an earlier storage.

**Limits.** Backups count against the project quota `max_backups` (default 35, `0` means unlimited). To stop a project
using backups, set a small limit. See `PANEL_DEFAULT_QUOTA` in the README. Backups cover the boot disk only. Volumes
carry `backup=0`, so vzdump skips them.

## 11. Private networks

Private networks let project members connect servers in one location over their own addresses. Nebla uses Proxmox SDN
for it: you create a zone and a pool of VNets once, and Nebla hands one VNet to each network a member creates and
attaches servers with `SDN.Use`. Nothing reloads networking when a member creates a network. The pool can run out; then
you add more VNets. A location without a zone doesn't offer private networks. The [networks guide](networks.md) covers
the user side.

**Permission.** The role needs `SDN.Audit` (the commands in step 1 and the setup script add it). If it is missing, the
location page says "Needs setup" and shows the command.

**With the script.** Run it with `NEBLA_SDN_VNETS` set to the number of VNets to create (1 to 200):

```sh
curl -fsSL '<panel>/v1/enroll.sh' | NEBLA_TOKEN=nbe_... NEBLA_SDN_VNETS=20 bash
```

It also reads `NEBLA_SDN_ZONE` (default `nebla`, 1 to 8 lowercase letters or digits, starting with a letter) and
`NEBLA_SDN_PREFIX` (default `nb`, 1 to 4 lowercase letters or digits, starting with a letter). It creates the zone if it
is missing: `simple` on a single node, `vxlan` with every node's IP as `peers` on a cluster. It creates the VNets
`<prefix>0001` up to the number you gave, skipping any that exist, and applies SDN. Running it again creates nothing that exists and applies SDN again. Run again with a larger number
to add more. On VXLAN it prints the firewall rules below. It stops if a VNet with one of those names exists in another zone.

**By hand.** The same steps, on one node:

```sh
# a single node: a Simple zone
pvesh create /cluster/sdn/zones --zone nebla --type simple --ipam pve
# a cluster: a VXLAN zone, peers are all node IPs
pvesh create /cluster/sdn/zones --zone nebla --type vxlan --peers 10.0.0.1,10.0.0.2,10.0.0.3

# the VNets: names are at most 8 characters, a prefix plus a number
pvesh create /cluster/sdn/vnets --vnet nb0001 --zone nebla
pvesh create /cluster/sdn/vnets --vnet nb0002 --zone nebla

# apply
pvesh set /cluster/sdn
```

Don't give VNets subnets or VLAN awareness, and don't use them for anything else: Nebla marks such a VNet as unusable
and shows the reason on the location page. A Simple zone only works on a single node. Don't share one zone between two
Nebla locations.

**In Nebla.** On the location page set the SDN zone and the VNet prefix (default `nb`). Nebla lists the zone's VNets
whose names start with the prefix and shows "Private networks: N available, M in use, C cooling, U unusable". The zone
and prefix can't change while any VNet is in use or cooling. A VNet whose last network was deleted cools down until no
VM config names it, then is free again. The MTU comes from the zone's `mtu` when set, else 1450 for VXLAN and 1500 for
Simple.

**Adding more VNets later.** Create more with the same prefix, for example `nb0021` to `nb0040`, and run
`pvesh set /cluster/sdn`. Nebla finds them on its next pass or when you save the location.

**VXLAN host firewall.** VXLAN uses UDP 4789, and Linux accepts it from any sender. On every node, add two host
firewall rules (Datacenter > Firewall, or the node's firewall), in this order: accept UDP 4789 from the other cluster
nodes, then drop it from everyone else. The drop rule makes this hold whatever the input policy is.

```
IN ACCEPT -source 10.0.0.1,10.0.0.2,10.0.0.3 -p udp -dport 4789
IN DROP -p udp -dport 4789
```

No earlier rule may accept UDP 4789, for example a broad allow for your LAN. The first matching rule wins.

Nebla also drops VXLAN traffic out of every server's public interface, so a server can't speak VXLAN to the hosts.
VXLAN is implemented and tested with fakes only; it hasn't run on a real multi-node cluster yet.

**Keep the datacenter firewall on.** Isolation between servers relies on the VM firewall. If the datacenter firewall is
off, the location shows "Isolation off", and creating networks and attaching servers pause there until you turn it back
on (see step 7).
