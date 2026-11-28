// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
)

func TestListPage(t *testing.T) {
	c, f := newFake(t)
	f.handle("GET", "/v1/actions", 200, "getActions")
	got, meta, err := c.Actions.List(context.Background(), ActionListOpts{ListOpts: ListOpts{Page: 2, PerPage: 25}, Status: []ActionStatus{ActionStatusError}, Sort: "id:asc"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != 42 || got[0].Resources[0].Type != "server" || got[1].Error.Code != "timed_out" || got[1].Finished != nil {
		t.Fatalf("got %+v", got)
	}
	if meta != (Meta{Page: 2, PerPage: 25, TotalEntries: 27}) {
		t.Fatalf("meta %+v", meta)
	}
	r, _ := f.last()
	q := r.URL.Query()
	if q.Get("page") != "2" || q.Get("per_page") != "25" || q.Get("status") != "error" || q.Get("sort") != "id:asc" {
		t.Fatalf("query %v", q)
	}
	if _, _, err := c.Actions.List(context.Background(), ActionListOpts{}); err != nil {
		t.Fatal(err)
	}
	r, _ = f.last()
	if r.URL.RawQuery != "" {
		t.Fatalf("expected empty query, got %q", r.URL.RawQuery)
	}
}

func pagedHandler(total, perPage int, short bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		n := 0
		if !short || page == 1 {
			n = min(perPage, max(0, total-(page-1)*perPage))
		}
		acts := []map[string]any{}
		for i := 0; i < n; i++ {
			acts = append(acts, map[string]any{"id": (page-1)*perPage + i + 1, "command": "x", "status": "success"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"actions": acts, "meta": map[string]any{"pagination": map[string]any{"page": page, "per_page": perPage, "total_entries": total}}})
	}
}

func TestAllWalksPages(t *testing.T) {
	c, f := newFake(t)
	f.handleFunc("GET", "/v1/actions", pagedHandler(120, 50, false))
	got, err := c.Actions.All(context.Background(), ActionListOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 120 || f.count() != 3 {
		t.Fatalf("got %d items in %d requests", len(got), f.count())
	}
	r, _ := f.last()
	if r.URL.Query().Get("per_page") != "50" || r.URL.Query().Get("page") != "3" {
		t.Fatalf("query %v", r.URL.Query())
	}
}

func TestAllStopsOnShortPage(t *testing.T) {
	c, f := newFake(t)
	f.handleFunc("GET", "/v1/actions", pagedHandler(500, 50, true))
	got, err := c.Actions.All(context.Background(), ActionListOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 50 || f.count() != 2 {
		t.Fatalf("got %d items in %d requests", len(got), f.count())
	}
}

func TestGetByName(t *testing.T) {
	all := func(context.Context) ([]string, error) { return []string{"web-1", "web"}, nil }
	id := func(s string) string { return s }
	got, err := getByName(context.Background(), all, "web", id, "server")
	if err != nil || *got != "web" {
		t.Fatalf("got %v, %v", got, err)
	}
	_, err = getByName(context.Background(), all, "we", id, "server")
	var ae *Error
	if !errors.As(err, &ae) || ae.StatusCode != 404 || !errors.Is(err, ErrNotFound) || ae.Message != "server \"we\" not found" {
		t.Fatalf("err %v", err)
	}
}
