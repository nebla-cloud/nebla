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
	"net/http"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent/ipaddress"
	"github.com/nebla-cloud/nebla/internal/primaryips"
	"github.com/nebla-cloud/nebla/internal/protection"
	"github.com/stretchr/testify/require"
)

func (e *env) busy(t *testing.T, id int64) {
	t.Helper()
	_, err := actions.Create(context.Background(), e.App.Ent, e.Project, primaryips.KindAssign, []actions.Resource{{ID: id, Type: primaryips.ResourceType}})
	require.NoError(t, err)
}

func (e *env) protect(t *testing.T, id int64) {
	t.Helper()
	e.App.Ent.IPAddress.UpdateOneID(id).SetDeleteProtected(true).ExecX(context.Background())
}

func TestPIPDelete_Protected(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	p := e.create(t, primaryips.CreateInput{})
	e.protect(t, p.ID)
	require.ErrorIs(t, e.svc.Delete(ctx, e.Project, p.ID), protection.ErrDelete)
	require.True(t, e.App.Ent.IPAddress.Query().Where(ipaddress.ID(p.ID)).ExistX(ctx))
}

func TestPIPDelete_ProtectedBeatsBusy(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	p := e.create(t, primaryips.CreateInput{})
	e.protect(t, p.ID)
	e.busy(t, p.ID)
	require.ErrorIs(t, e.svc.Delete(ctx, e.Project, p.ID), protection.ErrDelete)
}

func TestPIPUpdate_ProtectionOnlyWhileBusy(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	p := e.create(t, primaryips.CreateInput{})
	e.busy(t, p.ID)
	got, err := e.svc.Update(ctx, e.Project, p.ID, primaryips.UpdateInput{DeleteProtected: new(true)})
	require.NoError(t, err)
	require.True(t, got.DeleteProtected)
	require.True(t, e.App.Ent.IPAddress.GetX(ctx, p.ID).DeleteProtected)
}

func TestPIPUpdate_MixedWhileBusyRefused(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	p := e.create(t, primaryips.CreateInput{})
	e.busy(t, p.ID)
	_, err := e.svc.Update(ctx, e.Project, p.ID, primaryips.UpdateInput{AutoDelete: new(true), DeleteProtected: new(true)})
	requireAPIErr(t, err, http.StatusConflict, "locked", "the primary IP has a running action")
	require.False(t, e.App.Ent.IPAddress.GetX(ctx, p.ID).DeleteProtected)
}

func TestPIPCreate_WithProtection(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	p := e.create(t, primaryips.CreateInput{DeleteProtected: true})
	require.True(t, p.DeleteProtected)
	require.True(t, e.App.Ent.IPAddress.GetX(ctx, p.ID).DeleteProtected)
}
