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

package sshkeys_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/sshkeys"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func setup(t *testing.T) (*sshkeys.Service, int64, int64) {
	t.Helper()
	client := testutil.NewEnt(t)
	ctx := context.Background()
	a := client.Project.Create().SetName("a").SaveX(ctx)
	b := client.Project.Create().SetName("b").SaveX(ctx)
	return sshkeys.New(client), a.ID, b.ID
}

func newKey(t *testing.T) (string, ssh.PublicKey) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pk, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pk))) + " me@laptop", pk
}

func requireCode(t *testing.T, err error, status int, code string) {
	t.Helper()
	var e apierr.E
	require.True(t, errors.As(err, &e), "want apierr, got %v", err)
	require.Equal(t, status, e.Status, e.Msg)
	require.Equal(t, code, e.Code, e.Msg)
}

func TestSSHKeyInvalid(t *testing.T) {
	ctx := context.Background()
	svc, p, _ := setup(t)
	good, _ := newKey(t)
	for _, k := range []string{"", "not a key", "ssh-ed25519 AAAAbad", good + "\n" + good} {
		_, err := svc.Create(ctx, p, "k", k)
		requireCode(t, err, 422, "invalid_input")
	}
	_, err := svc.Create(ctx, p, " ", good)
	requireCode(t, err, 422, "invalid_input")
}

func TestSSHKeyDuplicate(t *testing.T) {
	ctx := context.Background()
	svc, p, other := setup(t)
	k1, pk1 := newKey(t)
	k2, _ := newKey(t)

	key, err := svc.Create(ctx, p, "laptop", k1)
	require.NoError(t, err)
	require.Equal(t, ssh.FingerprintLegacyMD5(pk1), key.Fingerprint)
	require.Equal(t, k1, key.PublicKey)

	_, err = svc.Create(ctx, p, "laptop", k2)
	requireCode(t, err, 409, "uniqueness_error")
	_, err = svc.Create(ctx, p, "desktop", k1)
	requireCode(t, err, 409, "uniqueness_error")

	_, err = svc.Create(ctx, other, "laptop", k1)
	require.NoError(t, err, "keys are unique per project only")
}

func TestSSHKeyCRUD(t *testing.T) {
	ctx := context.Background()
	svc, p, other := setup(t)
	k1, _ := newKey(t)
	k2, _ := newKey(t)
	a, err := svc.Create(ctx, p, "a", k1)
	require.NoError(t, err)
	b, err := svc.Create(ctx, p, "b", k2)
	require.NoError(t, err)

	keys, total, err := svc.List(ctx, p, 1, 1)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, keys, 1)
	require.Equal(t, a.ID, keys[0].ID)

	_, err = svc.Get(ctx, other, a.ID)
	requireCode(t, err, 404, "not_found")
	_, err = svc.Rename(ctx, other, a.ID, "x")
	requireCode(t, err, 404, "not_found")
	requireCode(t, svc.Delete(ctx, other, a.ID), 404, "not_found")

	_, err = svc.Rename(ctx, p, a.ID, "b")
	requireCode(t, err, 409, "uniqueness_error")
	got, err := svc.Rename(ctx, p, a.ID, "renamed")
	require.NoError(t, err)
	require.Equal(t, "renamed", got.Name)

	many, err := svc.GetMany(ctx, p, []int64{b.ID, a.ID})
	require.NoError(t, err)
	require.Len(t, many, 2)
	_, err = svc.GetMany(ctx, other, []int64{a.ID})
	requireCode(t, err, 404, "not_found")

	require.NoError(t, svc.Delete(ctx, p, a.ID))
	_, err = svc.Get(ctx, p, a.ID)
	requireCode(t, err, 404, "not_found")
}
