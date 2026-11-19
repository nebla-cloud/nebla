# Proxmox VE setup

Nebla talks only to the Proxmox VE API. It never uses SSH and installs nothing on the host. Each Nebla location points at
one Proxmox cluster (or standalone node) through an API token. The steps below run once per cluster, in a root shell on
any node.

Nebla supports Proxmox VE 8 and 9.

## 1. Roles

Nebla uses two roles: `Nebla` on `/` for VMs, and `NeblaISO` on the ISO storage only, so it can delete the cloud-init
seed ISOs it uploads.

On **PVE 8**:

```sh
pveum role add Nebla --privs "VM.Allocate VM.Clone VM.Config.CDROM VM.Config.CPU VM.Config.Disk VM.Config.Memory VM.Config.Network VM.Config.Options VM.Config.Cloudinit VM.PowerMgmt VM.Console VM.Audit VM.Monitor Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit Sys.Audit SDN.Use"
```

On **PVE 9**, `VM.Monitor` no longer exists. Use `VM.GuestAgent.Audit` in its place:

```sh
pveum role add Nebla --privs "VM.Allocate VM.Clone VM.Config.CDROM VM.Config.CPU VM.Config.Disk VM.Config.Memory VM.Config.Network VM.Config.Options VM.Config.Cloudinit VM.PowerMgmt VM.Console VM.Audit VM.GuestAgent.Audit Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit Sys.Audit SDN.Use"
```

`VM.Config.Cloudinit` lets Nebla remove a Proxmox cloud-init drive that a cloned template carries. That drive would clash
with Nebla's own seed ISO, because both use the `cidata` label.

On both versions, also create the ISO cleanup role:

```sh
pveum role add NeblaISO --privs "Datastore.Allocate Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit"
```

Proxmox only lets a user delete an ISO volume with `Datastore.Allocate` on its storage. Without it, deleting or
rebuilding a server fails and seed ISOs pile up. `Datastore.Allocate` also lets the token delete any other volume on that
storage, so grant it on the ISO storage only (step 2), never on `/`. A storage dedicated to Nebla's ISOs keeps this tight.

A role on a more specific path such as `/storage/<iso_storage>` replaces the inherited `/` grant on that path, it doesn't
add to it. So `NeblaISO` repeats every `Datastore.*` privilege of the main role: `Datastore.AllocateTemplate` for ISO
uploads, plus the rest, which also covers an ISO storage that doubles as the disk storage.

## 2. User, permissions and API token

```sh
pveum user add nebla@pve --comment "Nebla panel"
pveum aclmod / --users nebla@pve --roles Nebla
pveum aclmod /storage/<iso_storage> --users nebla@pve --roles NeblaISO
pveum user token add nebla@pve panel --privsep 0
```

Replace `<iso_storage>` with the location's `iso_storage`, for example `local`. `--privsep 0` turns privilege
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
is renewed, update the location with the new fingerprint.

## 4. Storages

Each location names two storages:

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

- Nebla sets cores, memory, the network card, tags and `agent=1` on every clone, and grows the disk to the server type's
  size. Keep the template disk no larger than your smallest server type, because disks never shrink.
- Nebla tags its VMs `nebla` and `nb-srv-<id>`. It never touches VMs without the `nebla` tag.
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
