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
