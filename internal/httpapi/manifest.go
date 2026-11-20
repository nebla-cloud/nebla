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
