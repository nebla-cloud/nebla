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
