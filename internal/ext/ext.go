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

// Package ext defines the extension points the panel exposes to add-on modules.
package ext

import "context"

// SettingsReader reads admin settings.
type SettingsReader interface {
	Bool(ctx context.Context, key string, def bool) bool
}

type Registration interface {
	SelfSignupEnabled(ctx context.Context) bool
	OnUserCreated(ctx context.Context, userID int64) error
}

type BillingSink interface {
	Handle(ctx context.Context, e Event) error
}

type BrandingInfo struct {
	ProductName  string
	LogoURL      string
	PrimaryColor string
}

type Branding interface {
	Get(ctx context.Context) BrandingInfo
}

type AdminSection struct {
	ID    string
	Title string
	Path  string
}

type AdminExtension interface {
	Sections() []AdminSection
}

type Extensions struct {
	Registration
	BillingSink
	Branding
	Admin []AdminExtension
}
