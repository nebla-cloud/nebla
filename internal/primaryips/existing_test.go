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

package primaryips_test

import (
	"context"
	"database/sql"
	"net/http"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/primaryips"
	"github.com/stretchr/testify/require"
)

func (e *env) lockForCreate(id int64, family string) error {
	return db.WithTx(context.Background(), e.App.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		_, err := primaryips.LockForCreate(context.Background(), etx, e.Project, id, e.Location, family)
		return err
	})
}

func TestLockForCreateRefusals(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	web := e.RunningServer("web")

	ok := e.create(t, primaryips.CreateInput{})
	require.NoError(t, e.lockForCreate(ok.ID, "ipv4"))

	v6 := e.create(t, primaryips.CreateInput{Type: "ipv6"})
	requireAPIErr(t, e.lockForCreate(v6.ID, "ipv4"), http.StatusUnprocessableEntity, "invalid_input", "That IP isn't an IPv4 address.")

	deleting := e.create(t, primaryips.CreateInput{})
	e.App.Ent.IPAddress.UpdateOneID(deleting.ID).SetDeleting(true).ExecX(ctx)
	requireAPIErr(t, e.lockForCreate(deleting.ID, "ipv4"), http.StatusConflict, "locked", "The address is still being released from the previous server.")

	releasing := e.create(t, primaryips.CreateInput{})
	e.App.Ent.IPAddress.UpdateOneID(releasing.ID).SetReleasingFromID(web.ServerID).ExecX(ctx)
	requireAPIErr(t, e.lockForCreate(releasing.ID, "ipv4"), http.StatusConflict, "locked", "The address is still being released from the previous server.")

	busy := e.create(t, primaryips.CreateInput{})
	_, err := actions.Create(ctx, e.App.Ent, e.Project, primaryips.KindAssign, []actions.Resource{{ID: busy.ID, Type: primaryips.ResourceType}})
	require.NoError(t, err)
	requireAPIErr(t, e.lockForCreate(busy.ID, "ipv4"), http.StatusConflict, "locked", "the primary IP has a running action")
}
