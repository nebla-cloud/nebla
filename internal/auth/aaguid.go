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
	_ "embed"
	"encoding/json"

	"github.com/google/uuid"
)

// Provider names by AAGUID, from github.com/passkeydeveloper/passkey-authenticator-aaguids.
//
//go:embed aaguids.json
var aaguidsJSON []byte

var aaguidNames = func() map[string]string {
	m := map[string]string{}
	if err := json.Unmarshal(aaguidsJSON, &m); err != nil {
		panic(err)
	}
	return m
}()

// providerName names a passkey after its provider, or "Passkey" when unknown.
func providerName(aaguid []byte) string {
	id, err := uuid.FromBytes(aaguid)
	if err != nil {
		return "Passkey"
	}
	if n, ok := aaguidNames[id.String()]; ok {
		return n
	}
	return "Passkey"
}
