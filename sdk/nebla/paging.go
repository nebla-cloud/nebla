// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

const allPerPage = 50

// ListOpts selects a page of a list; zero values are unset.
type ListOpts struct {
	Page, PerPage int
}

// Meta is the pagination metadata of a list response.
type Meta struct {
	Page, PerPage, TotalEntries int
}

func (o ListOpts) values() url.Values {
	q := url.Values{}
	if o.Page > 0 {
		q.Set("page", strconv.Itoa(o.Page))
	}
	if o.PerPage > 0 {
		q.Set("per_page", strconv.Itoa(o.PerPage))
	}
	return q
}

type wireMeta struct {
	Pagination struct {
		Page         int `json:"page"`
		PerPage      int `json:"per_page"`
		TotalEntries int `json:"total_entries"`
	} `json:"pagination"`
}

// listPage fetches one page of {<key>: [...], meta} and converts the items.
func listPage[W, T any](ctx context.Context, c *Client, path string, q url.Values, key string, conv func(W) T) ([]T, Meta, error) {
	var raw map[string]json.RawMessage
	if err := c.do(ctx, "GET", path, q, nil, &raw); err != nil {
		return nil, Meta{}, err
	}
	var items []W
	if r, ok := raw[key]; ok {
		if err := json.Unmarshal(r, &items); err != nil {
			return nil, Meta{}, fmt.Errorf("nebla: decode %s: %w", key, err)
		}
	}
	var wm wireMeta
	if r, ok := raw["meta"]; ok {
		if err := json.Unmarshal(r, &wm); err != nil {
			return nil, Meta{}, fmt.Errorf("nebla: decode meta: %w", err)
		}
	}
	out := make([]T, len(items))
	for i, it := range items {
		out[i] = conv(it)
	}
	p := wm.Pagination
	return out, Meta{Page: p.Page, PerPage: p.PerPage, TotalEntries: p.TotalEntries}, nil
}

// listAll walks every page; an empty or short page, or reaching total_entries, ends the walk.
func listAll[W, T any](ctx context.Context, c *Client, path string, q url.Values, key string, conv func(W) T) ([]T, error) {
	q = cloneValues(q)
	q.Set("per_page", strconv.Itoa(allPerPage))
	var all []T
	for page := 1; ; page++ {
		q.Set("page", strconv.Itoa(page))
		items, meta, err := listPage(ctx, c, path, q, key, conv)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		per := allPerPage
		if meta.PerPage > 0 && meta.PerPage < per {
			per = meta.PerPage
		}
		if len(items) == 0 || len(items) < per || page*per >= meta.TotalEntries {
			return all, nil
		}
	}
}

func cloneValues(q url.Values) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// getByName returns the exact-name match from all, or a not-found *Error.
func getByName[T any](ctx context.Context, all func(ctx context.Context) ([]T, error), name string, nameOf func(T) string, kind string) (*T, error) {
	items, err := all(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if nameOf(items[i]) == name {
			return &items[i], nil
		}
	}
	return nil, &Error{StatusCode: 404, Code: CodeNotFound, Message: fmt.Sprintf("%s %q not found", kind, name)}
}
