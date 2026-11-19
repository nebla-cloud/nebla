// Package crypto seals small secrets at rest with NaCl secretbox.
package crypto

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"

	"golang.org/x/crypto/nacl/secretbox"
)

const nonceSize = 24

// MinSecretLen matches the PANEL_SECRET_KEY minimum.
const MinSecretLen = 32

var ErrDecrypt = errors.New("crypto: cannot decrypt secret")

// Box seals values with a key derived from the panel secret.
type Box struct{ key [32]byte }

// NewBox derives the secretbox key from secret with HKDF-SHA256.
func NewBox(secret []byte) (*Box, error) {
	if len(secret) < MinSecretLen {
		return nil, fmt.Errorf("crypto: secret key must be at least %d bytes", MinSecretLen)
	}
	k, err := hkdf.Key(sha256.New, secret, nil, "nebla secretbox v1", 32)
	if err != nil {
		return nil, err
	}
	b := &Box{}
	copy(b.key[:], k)
	return b, nil
}

// Seal returns nonce || ciphertext.
func (b *Box) Seal(plain []byte) []byte {
	var nonce [nonceSize]byte
	rand.Read(nonce[:])
	return secretbox.Seal(nonce[:], plain, &nonce, &b.key)
}

func (b *Box) Open(sealed []byte) ([]byte, error) {
	if len(sealed) < nonceSize+secretbox.Overhead {
		return nil, ErrDecrypt
	}
	var nonce [nonceSize]byte
	copy(nonce[:], sealed[:nonceSize])
	out, ok := secretbox.Open(nil, sealed[nonceSize:], &nonce, &b.key)
	if !ok {
		return nil, ErrDecrypt
	}
	return out, nil
}
