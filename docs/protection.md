# Protection

Protection stops a resource from being deleted, or a server from being rebuilt, until you turn it off. It guards
against slips, like a wrong click or a script line aimed at the wrong thing. It is not a permission: anyone who can
delete the resource can also turn protection off.

## What each flag blocks

| Flag | Where | What it blocks |
| --- | --- | --- |
| Delete | Servers, volumes, floating IPs, primary IPs, snapshots | Deleting the resource |
| Rebuild | Servers | Rebuilding the server, from an image or from a snapshot, and restoring it from a backup |

Rebuild protection covers all three because each one replaces the server's disk. Backups themselves can't be
protected: they rotate and go away with their server.

A refused call returns `409` with the code `protected` and the message "Turn off delete protection first." or "Turn off
rebuild protection first." Delete protection applies in every status, so a snapshot in `error` has to be unprotected
before you can delete it. Deleting a project needs an empty project, so protected resources block it too.

## Who can change it

Owners, admins and members of the project can turn either flag on or off. Read-only members and read-only tokens can't.

## In the panel

On a server, volume, floating IP, primary IP or snapshot page, choose Protection in the page header. Tick Delete (and
Rebuild on a server), then Save. The button shows a closed lock while anything is protected.

While Delete is on, the Delete button stays visible but disabled, with a tooltip saying why. On a server, Rebuild and
each backup's Restore are disabled the same way while Rebuild is on, and so is a snapshot's Restore on the server's
Snapshots tab. Lists show a small lock before the name of anything that has delete protection.

You can change protection while an action is running on the resource.

## With the API

Every server, volume, floating IP, primary IP and snapshot has a `protection` object. Servers have `delete` and
`rebuild`; the others have `delete` only.

```json
{ "protection": { "delete": true, "rebuild": false } }
```

Send `protection` on the resource's `PUT` to change it. A field you leave out keeps its current value, so
`{"protection": {"delete": false}}` leaves rebuild protection alone. A `PUT` that sends only `protection` works in any
status. The create requests take the same object, and a missing field means off.

## With the CLI

```sh
nebla volume create --name data --size 50 --protect-delete
nebla volume update data --protect-delete=false
nebla server update web1 --protect-delete --protect-rebuild
```

`server create` and `server update` take `--protect-delete` and `--protect-rebuild`. `volume`, `floating-ip`,
`primary-ip` and `snapshot` take `--protect-delete` on `create` and `update`. A flag is sent only when you pass it, so
`--protect-delete=false` turns protection off, and an update without the flags leaves it alone. `describe` shows a
`Protection:` line: `none`, `delete` or `delete, rebuild`.

## Protected primary IPs when a server is deleted

A primary IP with auto-delete on is normally deleted along with its server. If it also has delete protection, the server
delete goes ahead and the IP is unassigned and kept, as if auto-delete were off. The panel's delete dialog shows it as
"Protected, kept", and `nebla server delete` lists it with the kept addresses. See [Primary IPs](primary-ips.md).
