// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
)

func TestLocations(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("GET", "/v1/locations", 200, "getLocations")
	f.handle("GET", "/v1/locations/1", 200, "getLocation")
	ls, meta, err := c.Locations.List(ctx, ListOpts{Page: 2, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := f.last()
	if r.Method != "GET" || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("per_page") != "10" {
		t.Fatalf("req %s %s", r.Method, r.URL)
	}
	if meta.TotalEntries != 1 || len(ls) != 1 || ls[0].Name != "lon1" || !ls[0].Networks.IPv4 || ls[0].Networks.IPv6 || !ls[0].SnapshotsAvailable || ls[0].BackupsAvailable {
		t.Fatalf("got %+v %+v", ls, meta)
	}
	all, err := c.Locations.All(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("all %v %v", all, err)
	}
	l, err := c.Locations.Get(ctx, 1)
	if err != nil || l.ID != 1 || l.Description != "Falkenstein" || !l.VolumesAvailable || !l.PrivateNetworksAvailable {
		t.Fatalf("get %+v %v", l, err)
	}
	l, err = c.Locations.GetByName(ctx, "lon1")
	if err != nil || l.ID != 1 {
		t.Fatalf("by name %+v %v", l, err)
	}
}

func TestServerTypes(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("GET", "/v1/server_types", 200, "getServerTypes")
	f.handle("GET", "/v1/server_types/3", 200, "getServerType")
	ts, meta, err := c.ServerTypes.List(ctx, ListOpts{})
	if err != nil {
		t.Fatal(err)
	}
	want := ServerType{ID: 3, Name: "small", Description: "Small", Cores: 2, MemoryMB: 4096, DiskGB: 40, Locations: []int64{1, 2}}
	if meta.TotalEntries != 1 || len(ts) != 1 || !reflect.DeepEqual(ts[0], want) {
		t.Fatalf("got %+v %+v", ts, meta)
	}
	all, err := c.ServerTypes.All(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("all %v %v", all, err)
	}
	g, err := c.ServerTypes.Get(ctx, 3)
	if err != nil || !reflect.DeepEqual(*g, want) {
		t.Fatalf("get %+v %v", g, err)
	}
	g, err = c.ServerTypes.GetByName(ctx, "small")
	if err != nil || g.ID != 3 {
		t.Fatalf("by name %+v %v", g, err)
	}
}

func TestImages(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("GET", "/v1/images", 200, "getImages")
	f.handle("GET", "/v1/images/5", 200, "getImage")
	is, _, err := c.Images.List(ctx, ImageListOpts{ListOpts: ListOpts{Page: 1}, Location: "lon1"})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := f.last()
	if r.URL.Query().Get("location") != "lon1" || r.URL.Query().Get("page") != "1" {
		t.Fatalf("query %s", r.URL.RawQuery)
	}
	want := Image{ID: 5, Name: "ubuntu-24.04", Description: "Ubuntu 24.04", OSFlavor: "ubuntu", OSVersion: "24.04", Locations: []int64{1}}
	if len(is) != 1 || !reflect.DeepEqual(is[0], want) {
		t.Fatalf("got %+v", is)
	}
	all, err := c.Images.All(ctx, ImageListOpts{Location: "lon1"})
	if err != nil || len(all) != 1 {
		t.Fatalf("all %v %v", all, err)
	}
	r, _ = f.last()
	if r.URL.Query().Get("location") != "lon1" || r.URL.Query().Get("per_page") != "50" {
		t.Fatalf("query %s", r.URL.RawQuery)
	}
	g, err := c.Images.Get(ctx, 5)
	if err != nil || !reflect.DeepEqual(*g, want) {
		t.Fatalf("get %+v %v", g, err)
	}
	g, err = c.Images.GetByName(ctx, "ubuntu-24.04")
	if err != nil || g.ID != 5 {
		t.Fatalf("by name %+v %v", g, err)
	}
}

func TestSSHKeys(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("GET", "/v1/ssh_keys", 200, "getSshKeys")
	f.handle("POST", "/v1/ssh_keys", 201, "postSshKeys")
	f.handle("GET", "/v1/ssh_keys/9", 200, "getSshKey")
	f.handle("PUT", "/v1/ssh_keys/9", 200, "putSshKey")
	f.handle("DELETE", "/v1/ssh_keys/9", 204, "")

	ks, meta, err := c.SSHKeys.List(ctx, ListOpts{})
	if err != nil || meta.TotalEntries != 1 || len(ks) != 1 || ks[0].Fingerprint != "aa:bb:cc:dd" || ks[0].Created.Year() != 2026 {
		t.Fatalf("list %+v %+v %v", ks, meta, err)
	}
	all, err := c.SSHKeys.All(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("all %v %v", all, err)
	}
	k, err := c.SSHKeys.Get(ctx, 9)
	if err != nil || k.Name != "laptop" || k.PublicKey != "ssh-ed25519 AAAAC3Nz test" {
		t.Fatalf("get %+v %v", k, err)
	}
	if k, err = c.SSHKeys.GetByName(ctx, "laptop"); err != nil || k.ID != 9 {
		t.Fatalf("by name %+v %v", k, err)
	}

	k, err = c.SSHKeys.Create(ctx, SSHKeyCreateOpts{Name: "laptop", PublicKey: "ssh-ed25519 AAAAC3Nz test"})
	if err != nil || k.ID != 9 {
		t.Fatalf("create %+v %v", k, err)
	}
	r, body := f.last()
	var got map[string]any
	_ = json.Unmarshal(body, &got)
	if r.Method != "POST" || len(got) != 2 || got["name"] != "laptop" || got["public_key"] != "ssh-ed25519 AAAAC3Nz test" {
		t.Fatalf("create req %s %s", r.Method, body)
	}

	k, err = c.SSHKeys.Update(ctx, 9, SSHKeyUpdateOpts{Name: Ptr("renamed")})
	if err != nil || k.ID != 9 {
		t.Fatalf("update %+v %v", k, err)
	}
	r, body = f.last()
	if r.Method != "PUT" || string(body) != `{"name":"renamed"}` {
		t.Fatalf("update req %s %s", r.Method, body)
	}

	if err := c.SSHKeys.Delete(ctx, 9); err != nil {
		t.Fatal(err)
	}
	if r, _ = f.last(); r.Method != "DELETE" || r.URL.Path != "/v1/ssh_keys/9" {
		t.Fatalf("delete req %s %s", r.Method, r.URL.Path)
	}
}

func TestGetByNameExactMatch(t *testing.T) {
	list := func(names ...string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			keys := []map[string]any{}
			for i, n := range names {
				keys = append(keys, map[string]any{"id": i + 1, "name": n, "public_key": "k", "fingerprint": "f", "created": "2026-01-02T03:04:05Z"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ssh_keys": keys, "meta": map[string]any{"pagination": map[string]any{"page": 1, "per_page": 50, "total_entries": len(keys)}}})
		}
	}
	c, f := newFake(t)
	f.handleFunc("GET", "/v1/ssh_keys", list("web-1", "Web"))
	if _, err := c.SSHKeys.GetByName(context.Background(), "web"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err %v", err)
	}
	f.handleFunc("GET", "/v1/ssh_keys", list("web", "web-1"))
	k, err := c.SSHKeys.GetByName(context.Background(), "web")
	if err != nil || k.ID != 1 || k.Name != "web" {
		t.Fatalf("got %+v %v", k, err)
	}
}
