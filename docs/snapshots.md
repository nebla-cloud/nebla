# Snapshots

A snapshot is a saved copy of a server's boot disk. You can build new servers from it, or put the server back to how it
was (restore). A snapshot keeps existing when its server is deleted, and it counts against your project's quota.

You can manage snapshots on the Snapshots page, on a server's Snapshots tab, with the API, or with the
[CLI](cli.md) (`nebla snapshot ...`).

## What a snapshot is

- It saves the boot disk only. Volumes are never included; they stay separate and keep their own data.
- It is a saved copy, not a rollback point. Taking one doesn't change the server, and later changes to the server don't
  change the snapshot.
- It lives in the same location as its server and can't be moved or copied to another one.
- Snapshots can be taken in a location while at least one of its hosts is online. When none is, the panel hides
  Snapshots for that location and new snapshot requests are refused until a host is back.

## Take a snapshot

Open the server's Snapshots tab and choose Take snapshot. Give it a name, and a description if you like.

A running server is shut down cleanly, copied and started again, so expect it to be offline for a few minutes. The
panel warns you before you confirm. A server that is off stays off.

If the server hasn't shut down after 3 minutes, no snapshot is taken and the server keeps running. Nebla keeps watching
for a few more minutes, and if the server powers off late it starts it again. To go on anyway,
tick "Force stop if it doesn't shut down" (`--force-stop` in the CLI). Nebla then powers the server off, which can lose
data that was still in memory.

Nebla reserves the disk's real size from your quota before it touches the server, so a snapshot that doesn't fit fails
straight away with the server still running.

## Build a server from a snapshot

When you create a server, pick a snapshot under Snapshots in the image step (`--snapshot` in the CLI). The new server
gets a new identity: new SSH host keys, and its user data runs again.

- Server types with a disk smaller than the snapshot are greyed out with the reason.
- Where a location keeps snapshots on storage that belongs to one host, servers built from a snapshot run on that host.
  An admin can point the location at shared storage to lift that.

You can also rebuild an existing server in the same location from a snapshot, from its Rebuild tab or with
`nebla server rebuild <server> --snapshot <snapshot>`.

## Restore a server

Restore rebuilds a snapshot's own server from it. Everything that changed on the boot disk since the snapshot is lost.
Volumes stay attached, with their current data.

A restore keeps the machine's identity: same SSH host keys, and user data doesn't run again. The exception is a network
change. If you added or removed a floating IP, or the server's network settings changed in any other way since the
snapshot, the server can't re-apply its network settings under the old identity. It then comes back with new SSH host
keys and runs its user data again. The Restore dialog and the CLI tell you before you confirm.

A snapshot whose server was deleted can't be restored, but you can still build a new server from it.

## Quota

Snapshots count against two limits: the number of snapshots and their total size in GB. A snapshot is counted from the
moment you ask for it, even while it's being taken. Your admin sets the limits per project.

## Delete a snapshot

Delete it from the snapshot's page, or with `nebla snapshot delete <snapshot>`. Type its name to confirm. A snapshot
can't be deleted while a server is being built from it; try again when that finishes. Deleting a project needs all its
snapshots deleted first.

A snapshot can be protected from deletion; see [Protection](protection.md).

## For admins

Snapshots show up in Proxmox as template VMs named `nebla-snap-<id>`, tagged `nebla-snapshot`. Don't edit, move,
delete or start from them: Nebla manages them, and a snapshot whose template is gone can't be used. See
[Proxmox setup: Snapshots](proxmox-setup.md#12-snapshots).
