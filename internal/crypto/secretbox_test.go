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

package crypto_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/crypto"
	"github.com/stretchr/testify/require"
)

func TestSecretboxRoundTrip(t *testing.T) {
	_, err := crypto.NewBox([]byte("short"))
	require.Error(t, err)

	b, err := crypto.NewBox([]byte(strings.Repeat("k", 32)))
	require.NoError(t, err)
	sealed := b.Seal([]byte("hunter2"))
	require.False(t, bytes.Contains(sealed, []byte("hunter2")))
	require.NotEqual(t, sealed, b.Seal([]byte("hunter2")), "nonces differ")
	got, err := b.Open(sealed)
	require.NoError(t, err)
	require.Equal(t, "hunter2", string(got))

	other, err := crypto.NewBox([]byte(strings.Repeat("x", 32)))
	require.NoError(t, err)
	_, err = other.Open(sealed)
	require.ErrorIs(t, err, crypto.ErrDecrypt)
	_, err = b.Open(sealed[:10])
	require.ErrorIs(t, err, crypto.ErrDecrypt)
}
