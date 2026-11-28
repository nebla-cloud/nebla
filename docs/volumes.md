# Volumes

A volume is extra disk space you attach to a server. It keeps its data when you detach it, rebuild the server or delete
the server, and you can attach it to another server in the same location later. Volumes count against your project's
quota.

You can manage volumes on the Volumes page, on a server's Volumes tab, with the API, or with the
[CLI](cli.md) (`nebla volume ...`).

## Before you start

- A volume and its server must be in the same location. In some locations a volume also stays on one host, so it can
  only attach to servers on that host. Servers it can't attach to are greyed out with the reason.
- A server holds at most 14 volumes.
- Some servers can't take a volume while running. If attaching fails with "Power off the server to attach the volume."
  or detaching fails with "Power off the server to detach the volume.", power the server off and try again.
- If Volumes doesn't show up for a location, your panel admin hasn't set volumes up there yet.

## Find the disk

Linux names disks like `/dev/vdb` and `/dev/vdc` in the order they were attached, so the name can change after a
detach, a reboot or a rebuild. Don't use these names. Every volume has a stable path instead:

```sh
/dev/disk/by-id/virtio-nb-vol-<id>
```

`<id>` is the volume ID. The panel shows the full path on the volume and on the server's Volumes tab. To list the
volumes a server can see:

```sh
ls -l /dev/disk/by-id/ | grep nb-vol
```

## Format a new volume

A new volume is empty. Make a filesystem on it once. This erases whatever is on the volume:

```sh
sudo mkfs.ext4 -L data /dev/disk/by-id/virtio-nb-vol-42
```

`-L data` gives the filesystem the label `data`. For XFS, use `sudo mkfs.xfs -L data /dev/disk/by-id/virtio-nb-vol-42`.

## Mount it

```sh
sudo mkdir -p /mnt/data
sudo mount /dev/disk/by-id/virtio-nb-vol-42 /mnt/data
```

To mount it at every boot, add a line to `/etc/fstab`. Use the filesystem's label or UUID, not `/dev/vdX`:

```
LABEL=data  /mnt/data  ext4  defaults,nofail  0  2
```

For XFS, change the type to `xfs`:

```
LABEL=data  /mnt/data  xfs  defaults,nofail  0  2
```

Find the UUID with `sudo blkid /dev/disk/by-id/virtio-nb-vol-42`, and use `UUID=<uuid>` in place of `LABEL=data` if you
prefer. Keep `nofail`: without it the server stops at boot when the volume is detached. Check the line with
`sudo mount -a` before you reboot.

## Make it bigger

Volumes only grow. Resize the volume in the panel or with `nebla volume resize <volume> --size <GB>`. If it's attached,
the server sees the new size within a few seconds. If it's detached, the new size applies when you next attach it.

Then grow the filesystem to fill the disk. Both commands work while the volume is mounted:

```sh
sudo resize2fs /dev/disk/by-id/virtio-nb-vol-42   # ext4
sudo xfs_growfs /mnt/data                          # XFS, takes the mount point
```

If you made a partition on the volume instead of formatting the whole disk, grow the partition first, for example with
`growpart` from the `cloud-guest-utils` package.

## Detach it

Stop anything using the volume and unmount it first, otherwise data still in memory can be lost:

```sh
sudo umount /mnt/data
```

Then detach it in the panel or with `nebla volume detach <volume>`. If the volume has an `/etc/fstab` line, `nofail`
lets the server boot without it. Remove the line if the volume won't come back.

A volume can be protected from deletion; see [Protection](protection.md).

## Delete it

Detach the volume first. Deleting erases its data for good.
