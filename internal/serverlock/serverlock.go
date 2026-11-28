// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

// Package serverlock locks a server row against other server actions.
package serverlock

import (
	"context"
	"encoding/json"
	"net/http"

	"entgo.io/ent/dialect/sql"
	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/predicate"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/firewalls"
	"github.com/nebla-cloud/nebla/internal/jobs"
)

// KindCreateBackup is the action command of a running backup, which locks the server.
const KindCreateBackup = "create_backup"

var (
	// ErrLocked is the 409 for a server with another running action.
	ErrLocked = apierr.New(http.StatusConflict, "locked", "the server has a running action")
	// ErrBackingUp is the 409 for a server whose running action is a backup.
	ErrBackingUp = apierr.New(http.StatusConflict, "backup_in_progress", "A backup of this server is running")
)

// ActionsOf matches the project's actions whose resources include the server.
func ActionsOf(projectID, serverID int64) predicate.Action {
	doc, _ := json.Marshal([]actions.Resource{{ID: serverID, Type: jobs.ServerResource}})
	return action.And(action.ProjectID(projectID), func(s *sql.Selector) {
		s.Where(sql.P(func(b *sql.Builder) {
			b.Ident(s.C(action.FieldResources)).WriteString(" @> ").Arg(string(doc)).WriteString("::jsonb")
		}))
	})
}

// Running leaves out firewall applies, which run alongside other server work.
var Running = action.And(action.StatusEQ(action.StatusRunning), action.CommandNEQ(firewalls.KindApply))

// HasRunningAction reports whether any running action other than a firewall apply lists the server.
func HasRunningAction(ctx context.Context, c *ent.Client, projectID, serverID int64) (bool, error) {
	return c.Action.Query().Where(ActionsOf(projectID, serverID), Running).Exist(ctx)
}

// LockRow locks the project's server row FOR UPDATE; a missing row gives notFound.
func LockRow(ctx context.Context, tx *ent.Client, projectID, id int64, notFound error) (*ent.Server, error) {
	row, err := tx.Server.Query().Where(server.ID(id), server.ProjectID(projectID)).ForUpdate().Only(ctx)
	if ent.IsNotFound(err) {
		return nil, notFound
	}
	return row, err
}

// CheckBusy gives ErrBackingUp for a running backup and ErrLocked for any other running action.
func CheckBusy(ctx context.Context, tx *ent.Client, projectID, id int64) error {
	busy, err := HasRunningAction(ctx, tx, projectID, id)
	if err != nil || !busy {
		return err
	}
	backingUp, err := tx.Action.Query().Where(ActionsOf(projectID, id), action.StatusEQ(action.StatusRunning),
		action.Command(KindCreateBackup)).Exist(ctx)
	if err != nil {
		return err
	}
	if backingUp {
		return ErrBackingUp
	}
	return ErrLocked
}

// Lock is LockRow followed by CheckBusy.
func Lock(ctx context.Context, tx *ent.Client, projectID, id int64, notFound error) (*ent.Server, error) {
	row, err := LockRow(ctx, tx, projectID, id, notFound)
	if err != nil {
		return nil, err
	}
	if err := CheckBusy(ctx, tx, projectID, id); err != nil {
		return nil, err
	}
	return row, nil
}
