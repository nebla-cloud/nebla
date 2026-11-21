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
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"go.yaml.in/yaml/v4"
)

// Printer writes command results in the chosen output format.
type Printer struct {
	Format   string
	Out      io.Writer
	NoHeader bool
}

// Table writes rows as aligned columns under upper-case headers.
func (p Printer) Table(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(p.Out, 0, 8, 2, ' ', 0)
	if !p.NoHeader {
		fmt.Fprintln(w, strings.ToUpper(strings.Join(headers, "\t")))
	}
	for _, r := range rows {
		fmt.Fprintln(w, strings.Join(r, "\t"))
	}
	w.Flush()
}

// Object writes v as YAML when the format is yaml, otherwise as indented JSON.
func (p Printer) Object(v any) error {
	if p.Format != "yaml" {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(p.Out, "%s\n", b)
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var doc any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return err
	}
	enc := yaml.NewEncoder(p.Out)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return err
	}
	return enc.Close()
}

// KV writes a key/value block.
func (p Printer) KV(pairs [][2]string) {
	w := tabwriter.NewWriter(p.Out, 0, 8, 2, ' ', 0)
	for _, kv := range pairs {
		fmt.Fprintf(w, "%s:\t%s\n", kv[0], kv[1])
	}
	w.Flush()
}

func age(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", max(int(d.Seconds()), 0))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
