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
)

func (s *server) GetUiManifest(ctx context.Context, _ apigen.GetUiManifestRequestObject) (apigen.GetUiManifestResponseObject, error) {
	b := s.deps.Ext.Branding.Get(ctx)
	out := apigen.UiManifest{AdminSections: []struct {
		Id    string `json:"id"`
		Path  string `json:"path"`
		Title string `json:"title"`
	}{}}
	if o := s.deps.Config.PasskeyOrigin(); o != "" && s.passkeys.Enabled() {
		out.PasskeyOrigin = &o
	}
	setup, err := s.auth.SetupRequired(ctx)
	if err != nil {
		return nil, err
	}
	out.SetupRequired = setup
	out.SignupEnabled = s.deps.Ext.SelfSignupEnabled(ctx)
	out.Branding.ProductName = b.ProductName
	out.Branding.LogoUrl = b.LogoURL
	out.Branding.PrimaryColor = b.PrimaryColor
	for _, a := range s.deps.Ext.Admin {
		for _, sec := range a.Sections() {
			out.AdminSections = append(out.AdminSections, struct {
				Id    string `json:"id"`
				Path  string `json:"path"`
				Title string `json:"title"`
			}{sec.ID, sec.Path, sec.Title})
		}
	}
	return apigen.GetUiManifest200JSONResponse(out), nil
}
