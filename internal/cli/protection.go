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

package cli

import (
	"strings"

	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
	"github.com/spf13/pflag"
)

// protectionLine is the describe text for a resource's protection flags; rebuild is nil for resources without one.
func protectionLine(del bool, rebuild *bool) string {
	var on []string
	if del {
		on = append(on, "delete")
	}
	if rebuild != nil && *rebuild {
		on = append(on, "rebuild")
	}
	if len(on) == 0 {
		return "none"
	}
	return strings.Join(on, ", ")
}

func addProtectDeleteFlag(f *pflag.FlagSet, v *bool) {
	f.BoolVar(v, "protect-delete", false, "refuse deleting it while on; --protect-delete=false turns it off")
}

// protectionInput is the request's protection, set only when --protect-delete was passed.
func protectionInput(f *pflag.FlagSet, del bool) *apiclient.ProtectionInput {
	if !f.Changed("protect-delete") {
		return nil
	}
	return &apiclient.ProtectionInput{Delete: &del}
}

func addServerProtectFlags(f *pflag.FlagSet, del, rebuild *bool) {
	addProtectDeleteFlag(f, del)
	f.BoolVar(rebuild, "protect-rebuild", false, "refuse rebuilds and backup restores while on; --protect-rebuild=false turns it off")
}

// serverProtectionInput is the request's protection, with only the flags that were passed.
func serverProtectionInput(f *pflag.FlagSet, del, rebuild bool) *apiclient.ServerProtectionInput {
	var in apiclient.ServerProtectionInput
	if f.Changed("protect-delete") {
		in.Delete = &del
	}
	if f.Changed("protect-rebuild") {
		in.Rebuild = &rebuild
	}
	if in.Delete == nil && in.Rebuild == nil {
		return nil
	}
	return &in
}
