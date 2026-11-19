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
