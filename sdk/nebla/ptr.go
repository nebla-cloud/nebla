// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

// Ptr returns a pointer to v.
func Ptr[T any](v T) *T { return &v }
