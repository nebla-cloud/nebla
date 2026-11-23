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

package networks

import "github.com/nebla-cloud/nebla/internal/ent"

// SetDiscoverHook installs f as Discover's phase hook and returns a restore func.
func SetDiscoverHook(f func(phase string)) func() {
	discoverHook = f
	return func() { discoverHook = nil }
}

// SetNetRefresh overrides the net_refresh flag lookup and returns a restore func.
func SetNetRefresh(v bool) func() {
	netRefreshFn = func(*ent.Server) bool { return v }
	return func() { netRefreshFn = netRefresh }
}
