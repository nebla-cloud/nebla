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

package floatingips

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestLockErr(t *testing.T) {
	for _, code := range []string{"55P03", "40P01"} {
		require.Equal(t, errBusy, lockErr(fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: code})), code)
	}
	other := &pgconn.PgError{Code: "23505"}
	require.ErrorIs(t, lockErr(other), other)
	plain := errors.New("boom")
	require.Equal(t, plain, lockErr(plain))
	require.NoError(t, lockErr(nil))
}
