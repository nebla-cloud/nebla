package db_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nebla-cloud/nebla/internal/db"
)

func TestIsTransient(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain", errors.New("boom"), false},
		{"bad conn", fmt.Errorf("query: %w", driver.ErrBadConn), true},
		{"net", &net.OpError{Op: "dial", Err: errors.New("refused")}, true},
		{"serialization", &pgconn.PgError{Code: "40001"}, true},
		{"deadlock", &pgconn.PgError{Code: "40P01"}, true},
		{"connection exception", &pgconn.PgError{Code: "08006"}, true},
		{"admin shutdown", &pgconn.PgError{Code: "57P01"}, false},
		{"unique", &pgconn.PgError{Code: "23505"}, false},
		{"not null", fmt.Errorf("insert: %w", &pgconn.PgError{Code: "23502"}), false},
		{"canceled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, false},
	}
	for _, c := range cases {
		if got := db.IsTransient(c.err); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
