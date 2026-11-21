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

package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/argon2"
)

const (
	MinPasswordLen = 10
	argonMem       = 64 * 1024
	argonTime      = 3
	argonThreads   = 2
	argonKeyLen    = 32
	argonSaltLen   = 16
)

var b64 = base64.RawStdEncoding

var (
	hashMu       sync.RWMutex
	hashSem      = make(chan struct{}, 4)
	hashInflight atomic.Int64
	hashPeak     atomic.Int64
)

// setHashConcurrency swaps the hashing semaphore and returns the old size (tests).
func setHashConcurrency(n int) int {
	hashMu.Lock()
	defer hashMu.Unlock()
	prev := cap(hashSem)
	hashSem = make(chan struct{}, n)
	hashPeak.Store(0)
	return prev
}

// acquireHash waits for a hashing slot so argon2 memory stays bounded.
func acquireHash(ctx context.Context) (func(), error) {
	hashMu.RLock()
	sem := hashSem
	hashMu.RUnlock()
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	n := hashInflight.Add(1)
	for p := hashPeak.Load(); n > p && !hashPeak.CompareAndSwap(p, n); p = hashPeak.Load() {
	}
	return func() {
		hashInflight.Add(-1)
		<-sem
	}, nil
}

func idKey(ctx context.Context, pw, salt []byte, t, m uint32, p uint8, n uint32) ([]byte, error) {
	release, err := acquireHash(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return argon2.IDKey(pw, salt, t, m, p, n), nil
}

// HashPassword returns a PHC-encoded argon2id hash.
func HashPassword(ctx context.Context, pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := idKey(ctx, []byte(pw), salt, argonTime, argonMem, argonThreads, argonKeyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMem, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword checks pw against an encoded hash in constant time.
func VerifyPassword(ctx context.Context, encoded, pw string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var v int
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := idKey(ctx, []byte(pw), salt, t, m, p, uint32(len(want)))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}
