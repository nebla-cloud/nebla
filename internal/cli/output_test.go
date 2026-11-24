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
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/cli/apiclient"
)

func TestTableAndNoHeader(t *testing.T) {
	var b bytes.Buffer
	rows := [][]string{{"1", "web"}, {"22", "db-primary"}}
	Printer{Format: "table", Out: &b}.Table([]string{"ID", "NAME"}, rows)
	want := "ID  NAME\n1   web\n22  db-primary\n"
	if b.String() != want {
		t.Fatalf("got %q want %q", b.String(), want)
	}
	b.Reset()
	Printer{Format: "table", Out: &b, NoHeader: true}.Table([]string{"ID", "NAME"}, rows)
	if b.String() != "1   web\n22  db-primary\n" {
		t.Fatalf("got %q", b.String())
	}
	b.Reset()
	Printer{Format: "table", Out: &b}.KV([][2]string{{"ID", "1"}, {"Name", "web"}})
	if b.String() != "ID:    1\nName:  web\n" {
		t.Fatalf("got %q", b.String())
	}
}

func TestObjectJSONAndYAML(t *testing.T) {
	s := apiclient.Server{Id: 7, Name: "web"}
	s.PublicNet.Ipv4 = &apiclient.ServerIp{Ip: "10.0.0.10"}
	var b bytes.Buffer
	if err := (Printer{Format: "json", Out: &b}).Object(s); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(b.String(), "{\n  \"backups\"") || !strings.Contains(b.String(), `"public_net": {`) || !strings.HasSuffix(b.String(), "}\n") {
		t.Fatalf("json=%s", b.String())
	}
	b.Reset()
	if err := (Printer{Format: "yaml", Out: &b}).Object(s); err != nil {
		t.Fatal(err)
	}
	y := b.String()
	if !strings.Contains(y, "public_net:\n  floating_ips: null\n  ipv4:\n    id: 0\n    ip: 10.0.0.10\n") || strings.Contains(y, "PublicNet") || !strings.Contains(y, "id: 7\n") {
		t.Fatalf("yaml=%s", y)
	}
}

func TestAge(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{
		45 * time.Second:               "45s",
		12*time.Minute + 5*time.Second: "12m",
		3*time.Hour + 59*time.Minute:   "3h",
		5*24*time.Hour + time.Hour:     "5d",
		-time.Minute:                   "0s",
	} {
		if got := age(now.Add(-d), now); got != want {
			t.Errorf("age(%s)=%q want %q", d, got, want)
		}
	}
}
