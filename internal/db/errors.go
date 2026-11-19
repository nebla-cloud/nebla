package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"net"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsTransient reports connection-class database errors worth retrying.
func IsTransient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code == "40001" || pe.Code == "40P01" || strings.HasPrefix(pe.Code, "08")
	}
	var ne net.Error
	return pgconn.SafeToRetry(err) || errors.Is(err, driver.ErrBadConn) || errors.As(err, &ne)
}
