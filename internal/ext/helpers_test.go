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

package ext_test

import (
	"context"
	"sync/atomic"

	"github.com/nebla-cloud/nebla/internal/ext"
)

type blockSink struct {
	release chan struct{}
	called  atomic.Bool
}

func (s *blockSink) Handle(context.Context, ext.Event) error {
	<-s.release
	s.called.Store(true)
	return nil
}

type staticSettings map[string]bool

func (m staticSettings) Bool(_ context.Context, key string, def bool) bool {
	if v, ok := m[key]; ok {
		return v
	}
	return def
}
