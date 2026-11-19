package auth

import (
	"context"
	"crypto/rand"
	"strings"
	"time"

	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/apitoken"
)

const (
	APITokenPrefix = "nb_"
	apiTokenLen    = 40
	base62         = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	PermRead      = "read"
	PermReadWrite = "read_write"
)

// APIToken is the project and permission an API token authenticates as.
type APIToken struct {
	ID         int64
	ProjectID  int64
	Permission string
}

// NewAPIToken returns a fresh nb_ token and its SHA-256 hash.
func NewAPIToken() (token string, hash []byte, err error) {
	var b strings.Builder
	b.WriteString(APITokenPrefix)
	buf := make([]byte, 64)
	for b.Len() < len(APITokenPrefix)+apiTokenLen {
		if _, err := rand.Read(buf); err != nil {
			return "", nil, err
		}
		for _, c := range buf {
			// 248 is the largest multiple of 62 below 256, so this stays unbiased.
			if c < 248 && b.Len() < len(APITokenPrefix)+apiTokenLen {
				b.WriteByte(base62[c%62])
			}
		}
	}
	token = b.String()
	return token, hashToken(token), nil
}

// HashSecret returns the SHA-256 used to store tokens.
func HashSecret(s string) []byte { return hashToken(s) }

// AuthenticateToken resolves a plaintext API token; unknown or revoked tokens are unauthorized.
func (s *Service) AuthenticateToken(ctx context.Context, token string) (APIToken, error) {
	if !strings.HasPrefix(token, APITokenPrefix) || len(token) != len(APITokenPrefix)+apiTokenLen {
		return APIToken{}, errUnauthorized
	}
	row, err := s.db.APIToken.Query().Where(apitoken.TokenHash(hashToken(token))).Only(ctx)
	if ent.IsNotFound(err) {
		return APIToken{}, errUnauthorized
	}
	if err != nil {
		return APIToken{}, err
	}
	// Coarse last-used tracking: at most one write per token per minute.
	now := time.Now()
	_ = s.db.APIToken.Update().
		Where(apitoken.ID(row.ID), apitoken.Or(apitoken.LastUsedAtIsNil(), apitoken.LastUsedAtLT(now.Add(-time.Minute)))).
		SetLastUsedAt(now).Exec(ctx)
	return APIToken{ID: row.ID, ProjectID: row.ProjectID, Permission: row.Permission}, nil
}
