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

package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
)

const unlockTimeout = 5 * time.Second

// TryAdvisoryLock takes the session advisory lock (ns, key) on its own connection, held until release.
// ok is false while another session holds it. release is safe to call more than once.
func TryAdvisoryLock(ctx context.Context, sqlDB *sql.DB, ns, key string) (release func(), ok bool, err error) {
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	if err := conn.QueryRowContext(ctx, `select pg_try_advisory_lock(hashtext($1), hashtext($2))`, ns, key).Scan(&ok); err != nil {
		discard(conn)
		return nil, false, err
	}
	if !ok {
		_ = conn.Close()
		return nil, false, nil
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
			defer cancel()
			var unlocked bool
			err := conn.QueryRowContext(ctx, `select pg_advisory_unlock(hashtext($1), hashtext($2))`, ns, key).Scan(&unlocked)
			if err != nil || !unlocked {
				discard(conn)
				return
			}
			_ = conn.Close()
		})
	}, true, nil
}

// discard closes the connection's Postgres session, so no lock it took outlives it in the pool.
func discard(conn *sql.Conn) {
	_ = conn.Raw(func(dc any) error {
		if c, ok := dc.(*stdlib.Conn); ok {
			ctx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
			defer cancel()
			_ = c.Conn().Close(ctx)
		}
		return driver.ErrBadConn
	})
	_ = conn.Close()
}
