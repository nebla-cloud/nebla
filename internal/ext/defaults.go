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

package ext

import "context"

type defaultRegistration struct{ settings SettingsReader }

func (r defaultRegistration) SelfSignupEnabled(ctx context.Context) bool {
	if r.settings == nil {
		return false
	}
	return r.settings.Bool(ctx, "self_signup", false)
}

func (defaultRegistration) OnUserCreated(context.Context, int64) error { return nil }

type noopSink struct{}

func (noopSink) Handle(context.Context, Event) error { return nil }

type defaultBranding struct{}

func (defaultBranding) Get(context.Context) BrandingInfo {
	return BrandingInfo{ProductName: "nebla"}
}

// Defaults returns the built-in extensions; settings may be nil.
func Defaults(settings SettingsReader) Extensions {
	return Extensions{
		Registration: defaultRegistration{settings: settings},
		BillingSink:  noopSink{},
		Branding:     defaultBranding{},
	}
}

// WithDefaults fills any nil extension in e from Defaults.
func (e Extensions) WithDefaults(settings SettingsReader) Extensions {
	d := Defaults(settings)
	if e.Registration == nil {
		e.Registration = d.Registration
	}
	if e.BillingSink == nil {
		e.BillingSink = d.BillingSink
	}
	if e.Branding == nil {
		e.Branding = d.Branding
	}
	return e
}
