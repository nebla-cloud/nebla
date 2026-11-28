// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

// Protection is a resource's delete protection; deletes fail with ErrProtected while Delete is true.
type Protection struct {
	Delete bool
}

// ServerProtection is a server's protection; Rebuild also refuses backup restores.
type ServerProtection struct {
	Delete  bool
	Rebuild bool
}

// ProtectionOpts sets delete protection; a nil field keeps its current value (false on create).
type ProtectionOpts struct {
	Delete *bool `json:"delete,omitempty"`
}

// ServerProtectionOpts sets a server's protection; a nil field keeps its current value (false on create).
type ServerProtectionOpts struct {
	Delete  *bool `json:"delete,omitempty"`
	Rebuild *bool `json:"rebuild,omitempty"`
}
