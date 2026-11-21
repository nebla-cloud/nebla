package volumes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/servers"
)

// Commands are the volume action commands; the server rescue leaves them to RescueOrphaned.
var Commands = []string{KindCreateVolume, KindAttachVolume, KindDetachVolume, KindResizeVolume, KindDeleteVolume}

// RescueOrphaned fails orphaned volume actions and repairs volume rows no running action will settle.
// A create orphaned after its disk was allocated publishes volume.created on bus, which may be nil.
// It returns how many actions it failed.
func RescueOrphaned(ctx context.Context, c *ent.Client, sqlDB *sql.DB, bus servers.Publisher) (int, error) {
	n := 0
	var created []int64
	err := db.WithTx(ctx, sqlDB, func(tx *sql.Tx, etx *ent.Client) error {
		n, created = 0, nil
		q, err := tx.QueryContext(ctx, `
			update actions a set status = 'error', error_code = $1, error_message = $2, finished = now()
			where a.command = any($3) and `+jobs.Orphaned+`
			returning a.command, (select j.args from river_job j where j.id = a.river_job_id)`,
			jobs.OrphanedCode, jobs.OrphanedMessage, Commands)
		if err != nil {
			return fmt.Errorf("fail orphaned volume actions: %w", err)
		}
		var resizes []ResizeVolumeArgs
		for q.Next() {
			var command string
			var args []byte
			if err := q.Scan(&command, &args); err != nil {
				q.Close()
				return err
			}
			n++
			var a ResizeVolumeArgs
			if command == KindResizeVolume && args != nil && json.Unmarshal(args, &a) == nil {
				resizes = append(resizes, a)
			}
			var ca CreateVolumeArgs
			if command == KindCreateVolume && args != nil && json.Unmarshal(args, &ca) == nil {
				created = append(created, ca.VolumeID)
			}
		}
		if err := errors.Join(q.Err(), q.Close()); err != nil {
			return err
		}
		for _, a := range resizes {
			// A lost resize of a detached volume never touched the disk, so its quota goes back.
			// An attached one may have grown; it keeps size_gb rather than guessing without Proxmox.
			err := etx.Volume.Update().Where(volume.ID(a.VolumeID), volume.SizeGB(a.SizeGB), volume.AppliedGBLT(a.SizeGB),
				volume.ServerIDIsNil()).
				SetSizeGB(a.PrevSizeGB).Exec(ctx)
			if err != nil {
				return err
			}
		}
		return repairRows(ctx, etx)
	})
	if err != nil || bus == nil || len(created) == 0 {
		return n, err
	}
	rows, err := c.Volume.Query().Where(volume.IDIn(created...), volume.VolidNEQ(""),
		volume.StatusIn(volume.StatusAvailable, volume.StatusAttached)).All(ctx)
	if err != nil {
		return n, err
	}
	o := &WorkerOptions{Bus: bus}
	for _, r := range rows {
		o.publish(ctx, ext.VolumeCreated, r, r.SizeGB, r.ServerID != nil)
	}
	return n, nil
}

// repairRows settles volumes stuck mid-action: creating becomes error, deleting available, and a saved attachment attached.
func repairRows(ctx context.Context, etx *ent.Client) error {
	rows, err := etx.Volume.Query().Where(volume.Or(
		volume.StatusIn(volume.StatusCreating, volume.StatusDeleting),
		volume.And(volume.StatusEQ(volume.StatusAvailable), volume.ServerIDNotNil()),
	)).Order(ent.Asc(volume.FieldID)).ForUpdate().All(ctx)
	if err != nil {
		return err
	}
	for _, r := range rows {
		busy, err := etx.Action.Query().Where(ActionsOf(r.ProjectID, r.ID), action.StatusEQ(action.StatusRunning)).Exist(ctx)
		if err != nil {
			return err
		}
		if busy {
			continue
		}
		st := volume.StatusAttached
		switch r.Status {
		case volume.StatusCreating:
			st = volume.StatusError
		case volume.StatusDeleting:
			st = volume.StatusAvailable
			if r.Volid == "" {
				st = volume.StatusError
			}
		}
		if err := etx.Volume.UpdateOneID(r.ID).SetStatus(st).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}
