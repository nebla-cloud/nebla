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

package mail

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	s, err := New("", "https://panel.example.com")
	require.NoError(t, err)
	require.IsType(t, Noop{}, s)

	s, err = New("smtps://u:p%40ss@mail.example.com", "https://panel.example.com:8443")
	require.NoError(t, err)
	m := s.(*SMTP)
	require.Equal(t, "465", m.port)
	require.Equal(t, "p@ss", m.pass)
	require.Equal(t, "nebla@panel.example.com", m.from)

	s, err = New("smtp://mail.example.com:2525?from=ops@example.com", "")
	require.NoError(t, err)
	require.Equal(t, "ops@example.com", s.(*SMTP).from)

	_, err = New("http://mail.example.com", "")
	require.Error(t, err)
	require.Error(t, s.Send(context.Background(), "a@example.com\r\nBcc: x", "s", "b"))
}
