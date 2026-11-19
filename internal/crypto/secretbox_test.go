package crypto_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Hexeption/nebla/internal/crypto"
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
