# Backups

A backup is a copy of a server's boot disk, stored away from the host the server runs on. Turn backups on and Nebla
backs the server up once a day and keeps the last few. If something goes wrong, you can roll the server back to any of
them.

You can manage backups on a server's Backups tab, with the API, or with the [CLI](cli.md) (`nebla server backup ...`).

## What it covers

- A backup covers the boot disk only. [Volumes](volumes.md) aren't included, so back up their data yourself.
- Restoring puts the boot disk back as it was. Anything written to it since the backup is lost.
- A restore keeps the server's addresses, type, volumes and firewalls as they are now. Attached volumes stay attached
  and untouched.
- Deleting a server deletes its backups too. The delete dialog tells you how many.

## Backups and snapshots

| | Backup | Snapshot |
| --- | --- | --- |
| What | A copy of the boot disk, kept off the host | A saved boot disk you can build new servers from |
| Made | Daily on a schedule, or with Back up now | When you ask |
| Used for | Rolling this server back in place | Making new servers |
| Lifetime | Rotates, keeping the last N. Deleted with the server | Kept until you delete it, even after the server is gone |

## Turn them on

On the server's Backups tab, switch on Daily backups and choose how many to Keep, from 1 to 14. The default is 7. The
tab shows the next and last backup times in your timezone. You can also turn backups on when you create a server, with
Enable daily backups.

```sh
nebla server create --name web1 --type small --image ubuntu --location eu-1 --ssh-key laptop --backups --backup-keep 5
nebla server backup enable web1 --keep 7
nebla server backup disable web1
```

Turning backups off keeps the backups you already have. You can still restore and delete them one at a time. They go
when you delete the server.

If the Backups tab says "Backups are turned off for this location", your panel admin hasn't set up backup storage
there. Existing backups stay restorable.

## When they run

Your panel admin sets a backup window for each location, in UTC. Nebla spreads servers across the window, so each
server has its own start time. A server is backed up once a day. Only one backup runs at a time on a host, so a backup
can start a little later than its time and still count for that day. A backup that is waiting shows as "Waiting for its
turn". It doesn't lock the server. Only while the backup is actually running, other actions on the server fail with
`backup_in_progress`. Try again when it finishes.

Back up now makes a backup right away, or as soon as the host is free. It is only offered while backups are on.

```sh
nebla server backup now web1
```

Scheduled and manual backups share one pool. After each successful backup, Nebla deletes the oldest ones beyond Keep.

## Quota

Each backup the server may keep counts against your project's quota, whether or not it exists yet. A server with backups
on reserves its Keep number. A server with backups off counts the backups it still holds. Turning backups on, or
raising Keep, fails with `quota_exceeded` when the total would go over the project's limit. Because the room is
reserved up front, a nightly backup never fails for quota.

The default limit for new projects is 35 backups. Your admin can change it.

## List and delete

```sh
nebla server backup list web1
nebla server backup delete web1 42
```

Deleting a backup is permanent. You can't delete a backup while it is being restored.

## Restore

Choose Restore on a backup, then type the server's name to confirm. Nebla doesn't take another backup first, so back up
now if you might want the current disk back.

```sh
nebla server backup restore web1 42
```

While the restore runs, other actions on the server are refused. The server starts again when it finishes.

### Identity

If the server's addresses haven't changed since the backup, it comes back as it was. SSH host keys and cloud-init state
are the same. If its addresses changed, the server is treated as new: cloud-init applies the network again, SSH host
keys are regenerated, and user data runs again. The restore dialog and the CLI say so before you confirm.

### After a rebuild

A rebuild replaces the operating system, and backups taken before it stay. Restoring one of those brings back the old
operating system. The server's image, as shown in the panel, doesn't change when you restore, so it can name an image
the disk no longer runs.

## If a location's storage changes

Each backup remembers where it was stored. If your admin switches a location to different storage, existing backups
still restore and delete. Only new backups go to the new storage. If your admin clears the storage, scheduled backups
stop.
