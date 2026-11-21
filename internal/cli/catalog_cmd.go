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
	"fmt"
	"strconv"

	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

func id64(v int64) string { return strconv.FormatInt(v, 10) }

// listCmd builds a list command that prints items as a table or as objects.
func listCmd[T any](a *app, use, short string, headers []string, extra func(*cobra.Command), fetch func(*cobra.Command, *API) ([]T, error), row func(T) []string) *cobra.Command {
	var noHeader bool
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			api, err := a.api()
			if err != nil {
				return err
			}
			items, err := fetch(cmd, api)
			if err != nil {
				return err
			}
			p := a.printer()
			if a.flags.output != "table" {
				if items == nil {
					items = []T{}
				}
				return p.Object(items)
			}
			p.NoHeader = noHeader
			rows := make([][]string, 0, len(items))
			for _, it := range items {
				rows = append(rows, row(it))
			}
			p.Table(headers, rows)
			return nil
		},
	}
	cmd.Flags().BoolVar(&noHeader, "no-header", false, "omit the table header")
	if extra != nil {
		extra(cmd)
	}
	return cmd
}

func memoryString(mb int) string {
	if mb%1024 == 0 {
		return fmt.Sprintf("%d GB", mb/1024)
	}
	return fmt.Sprintf("%d MB", mb)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func newImageCmd(a *app) *cobra.Command {
	var location string
	cmd := &cobra.Command{Use: "image", Short: "Browse images"}
	cmd.AddCommand(listCmd(a, "list", "List images", []string{"ID", "NAME", "OS", "VERSION", "DESCRIPTION"},
		func(c *cobra.Command) {
			c.Flags().StringVar(&location, "location", "", "only images available in this location")
		},
		func(c *cobra.Command, api *API) ([]apiclient.Image, error) {
			return api.Images(c.Context(), location)
		},
		func(i apiclient.Image) []string {
			return []string{id64(i.Id), i.Name, i.OsFlavor, i.OsVersion, i.Description}
		}))
	return cmd
}

func newServerTypeCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "server-type", Short: "Browse server types"}
	cmd.AddCommand(listCmd(a, "list", "List server types", []string{"ID", "NAME", "CORES", "MEMORY", "DISK", "DEPRECATED"}, nil,
		func(c *cobra.Command, api *API) ([]apiclient.ServerType, error) { return api.ServerTypes(c.Context()) },
		func(s apiclient.ServerType) []string {
			return []string{id64(s.Id), s.Name, strconv.Itoa(s.Cores), memoryString(s.MemoryMb), fmt.Sprintf("%d GB", s.DiskGb), yesNo(s.Deprecated)}
		}))
	return cmd
}

func newLocationCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "location", Short: "Browse locations"}
	cmd.AddCommand(listCmd(a, "list", "List locations", []string{"ID", "NAME", "DESCRIPTION"}, nil,
		func(c *cobra.Command, api *API) ([]apiclient.Location, error) { return api.Locations(c.Context()) },
		func(l apiclient.Location) []string { return []string{id64(l.Id), l.Name, l.Description} }))
	return cmd
}
