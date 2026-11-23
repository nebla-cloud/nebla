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

package snapshots

// SetAfterStep makes the create workers built from o call f once each step is saved.
func SetAfterStep(o WorkerOptions, f func(step int)) WorkerOptions {
	o.afterStep = f
	return o
}

// SetAfterCreatePost makes the create workers built from o call f once the create POST returns.
func SetAfterCreatePost(o WorkerOptions, f func()) WorkerOptions {
	o.afterCreatePost = f
	return o
}
