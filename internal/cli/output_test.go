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
	if !strings.HasPrefix(b.String(), "{\n  \"created\"") || !strings.Contains(b.String(), `"public_net": {`) || !strings.HasSuffix(b.String(), "}\n") {
		t.Fatalf("json=%s", b.String())
	}
	b.Reset()
	if err := (Printer{Format: "yaml", Out: &b}).Object(s); err != nil {
		t.Fatal(err)
	}
	y := b.String()
	if !strings.Contains(y, "public_net:\n  ipv4:\n    ip: 10.0.0.10\n") || strings.Contains(y, "PublicNet") || !strings.Contains(y, "id: 7\n") {
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
