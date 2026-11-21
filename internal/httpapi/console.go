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

package httpapi

import (
	"context"

	"github.com/nebla-cloud/nebla/internal/apigen"
	"github.com/nebla-cloud/nebla/internal/console"
)

// RequestServerConsole returns only wss_url and password; it is not an Action.
func (s *server) RequestServerConsole(ctx context.Context, req apigen.RequestServerConsoleRequestObject) (apigen.RequestServerConsoleResponseObject, error) {
	sc, err := mustScope(ctx)
	if err != nil {
		return nil, err
	}
	g, err := s.console.Request(ctx, sc.ProjectID, req.Id, console.Principal{UserID: sc.UserID, SessionHash: sc.SessionHash, TokenID: sc.TokenID})
	if err != nil {
		return nil, err
	}
	return apigen.RequestServerConsole201JSONResponse{WssUrl: g.URL, Password: g.Password}, nil
}
