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

package floatingips_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/floatingips"
	"github.com/nebla-cloud/nebla/internal/protection"
	"github.com/stretchr/testify/require"
)

func (e *env) spare(t *testing.T) floatingips.FloatingIP {
	t.Helper()
	f, _, err := e.svc.Create(context.Background(), e.Project, floatingips.CreateInput{Name: "spare", Family: "ipv4", Location: "eu-1"})
	require.NoError(t, err)
	return f
}

func (e *env) busy(t *testing.T, id int64) {
	t.Helper()
	_, err := actions.Create(context.Background(), e.App.Ent, e.Project, floatingips.KindAssign, []actions.Resource{{ID: id, Type: floatingips.ResourceType}})
	require.NoError(t, err)
}

func TestFIPDelete_Protected(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	f := e.spare(t)
	e.App.Ent.IPAddress.UpdateOneID(f.ID).SetDeleteProtected(true).ExecX(ctx)
	before := e.App.Ent.Action.Query().CountX(ctx)
	_, err := e.svc.Delete(ctx, e.Project, f.ID)
	require.ErrorIs(t, err, protection.ErrDelete)
	require.False(t, e.row(t, f.ID).Deleting)
	require.Equal(t, before, e.App.Ent.Action.Query().CountX(ctx))
}

func TestFIPDelete_ProtectedBeatsBusy(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	f := e.spare(t)
	e.App.Ent.IPAddress.UpdateOneID(f.ID).SetDeleteProtected(true).ExecX(ctx)
	e.busy(t, f.ID)
	_, err := e.svc.Delete(ctx, e.Project, f.ID)
	require.ErrorIs(t, err, protection.ErrDelete)
}

func TestFIPUpdate_ProtectionOnlyWhileBusy(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	f := e.spare(t)
	e.busy(t, f.ID)
	got, err := e.svc.Update(ctx, e.Project, f.ID, floatingips.UpdateInput{DeleteProtected: new(true)})
	require.NoError(t, err)
	require.True(t, got.DeleteProtected)
	require.True(t, e.row(t, f.ID).DeleteProtected)
}

func TestFIPUpdate_MixedWhileBusyRefused(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	f := e.spare(t)
	e.busy(t, f.ID)
	_, err := e.svc.Update(ctx, e.Project, f.ID, floatingips.UpdateInput{Name: new("renamed"), DeleteProtected: new(true)})
	requireAPIErr(t, err, http.StatusConflict, "locked", msgBusy)
	r := e.row(t, f.ID)
	require.Equal(t, "spare", r.Name)
	require.False(t, r.DeleteProtected)
}

func TestFIPCreate_WithProtection(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	f, _, err := e.svc.Create(ctx, e.Project, floatingips.CreateInput{Family: "ipv4", Location: "eu-1", DeleteProtected: true})
	require.NoError(t, err)
	require.True(t, f.DeleteProtected)
	require.True(t, e.row(t, f.ID).DeleteProtected)
}
