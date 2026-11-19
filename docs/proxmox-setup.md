# Proxmox VE setup

Nebla talks only to the Proxmox VE API. It never uses SSH and installs nothing on the host. Each Nebla location points at
one Proxmox cluster (or standalone node) through an API token. The steps below run once per cluster, in a root shell on
any node.

Nebla supports Proxmox VE 8 and 9.

## 1. Role

Create a role named `Nebla` with exactly these privileges.

On **PVE 8**:

```sh
pveum role add Nebla --privs "VM.Allocate VM.Clone VM.Config.CDROM VM.Config.CPU VM.Config.Disk VM.Config.Memory VM.Config.Network VM.Config.Options VM.PowerMgmt VM.Console VM.Audit VM.Monitor Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit Sys.Audit SDN.Use"
```

On **PVE 9**, `VM.Monitor` no longer exists. Use `VM.GuestAgent.Audit` in its place:

```sh
pveum role add Nebla --privs "VM.Allocate VM.Clone VM.Config.CDROM VM.Config.CPU VM.Config.Disk VM.Config.Memory VM.Config.Network VM.Config.Options VM.PowerMgmt VM.Console VM.Audit VM.GuestAgent.Audit Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit Sys.Audit SDN.Use"
```

## 2. User, permissions and API token

```sh
pveum user add nebla@pve --comment "Nebla panel"
pveum aclmod / --users nebla@pve --roles Nebla
pveum user token add nebla@pve panel --privsep 0
```

`--privsep 0` turns privilege separation off, so the token gets the user's `Nebla` role on `/`. The last command prints
the token once:

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

Servers are full clones of a template VM. Nebla attaches its own NoCloud seed ISO as `ide2`, so do **not** add a Proxmox
cloud-init drive to the template, and leave `ide2` free.

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

Notes:

- Nebla sets cores, memory, the network card, tags and `agent=1` on every clone, and grows the disk to the server type's
  size. Keep the template disk no larger than your smallest server type, because disks never shrink.
- The genericcloud image doesn't ship the QEMU guest agent. Install it in your image or with user data
  (`packages: [qemu-guest-agent]`) if you want it. Power actions and metrics work without it.
- Nebla tags its VMs `nebla` and `nb-srv-<id>`. It never touches VMs without the `nebla` tag.

## 6. Register the template as an image

An image template is a `(location, vmid, node)` triple:

- **Template on shared storage** (Ceph, NFS and so on): omit `node`. Any node in the location can clone it.
- **Template on local storage**, such as `local-lvm`: set `node` to the node that holds it. Servers from that template
  are placed on that node only. A template without a node must sit on shared storage, otherwise the API returns
  `template_not_shared`.

For several nodes with local storage, create the template once per node, with a different VMID on each, and list each as
its own template entry.

## 7. Networking

IP pools name the bridge (`vmbr0`, for example) and an optional VLAN tag. Nebla enables the VM firewall with IP and MAC
filtering on each server's `net0`. It only takes effect when the firewall is enabled at the datacenter level
(Datacenter > Firewall > Options). Make sure your own datacenter rules still allow the traffic you expect.
