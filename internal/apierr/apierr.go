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

// Package apierr holds the error type domain code returns to the HTTP layer.
package apierr

import "fmt"

type E struct {
	Status int
	Code   string
	Msg    string
}

func (e E) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Msg) }

func New(status int, code, msg string) E { return E{Status: status, Code: code, Msg: msg} }
