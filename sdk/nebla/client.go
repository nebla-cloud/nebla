// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/nebla-cloud/nebla/sdk/nebla/internal/wire"
)

// Version is the SDK version.
const Version = "0.4.0"

// Client talks to a Nebla panel.
type Client struct {
	endpoint  string
	token     string
	userAgent string
	hc        *http.Client
	retry     RetryPolicy
	httpc     *http.Client

	// Actions lists and waits for actions.
	Actions *ActionsService
}

// Option configures a Client.
type Option func(*Client)

// WithEndpoint sets the panel URL, e.g. https://panel.example.
func WithEndpoint(u string) Option { return func(c *Client) { c.endpoint = u } }

// WithToken sets the API token.
func WithToken(t string) Option { return func(c *Client) { c.token = t } }

// WithUserAgent prefixes the SDK user agent with p.
func WithUserAgent(p string) Option { return func(c *Client) { c.userAgent = p } }

// WithHTTPClient sets the HTTP client used for requests.
func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.hc = hc } }

// WithRetry sets the retry policy.
func WithRetry(p RetryPolicy) Option { return func(c *Client) { c.retry = p } }

// NewClient returns a Client configured by opts.
func NewClient(opts ...Option) *Client {
	c := &Client{retry: DefaultRetryPolicy}
	for _, o := range opts {
		o(c)
	}
	base := http.DefaultClient
	if c.hc != nil {
		base = c.hc
	}
	hc := *base
	rt := hc.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	hc.Transport = retryTransport{base: rt, policy: c.retry}
	c.httpc = &hc
	c.Actions = &ActionsService{c: c}
	return c
}

func (c *Client) agent() string {
	ua := "nebla-go/" + Version
	if c.userAgent != "" {
		ua = c.userAgent + " " + ua
	}
	return ua
}

func (c *Client) url(path string, query url.Values) string {
	base := strings.TrimRight(c.endpoint, "/")
	base = strings.TrimSuffix(base, "/v1")
	u := base + "/v1" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// do sends a request to /v1<path>, JSON-encoding body and decoding a 2xx reply into out.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	if c.endpoint == "" {
		return errors.New("nebla: endpoint is not set (use WithEndpoint)")
	}
	if c.token == "" {
		return errors.New("nebla: token is not set (use WithToken)")
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("nebla: encode request: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url(path, query), rdr)
	if err != nil {
		return fmt.Errorf("nebla: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", c.agent())
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return decodeError(res)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil
	}
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("nebla: read response: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("nebla: decode response (is the endpoint a Nebla panel?): %w", err)
	}
	return nil
}

func decodeError(res *http.Response) *Error {
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var w wire.Error
	if json.Unmarshal(data, &w) != nil || w.Error.Code == "" {
		return &Error{StatusCode: res.StatusCode, Message: fmt.Sprintf("unexpected status %d", res.StatusCode)}
	}
	return &Error{StatusCode: res.StatusCode, Code: ErrorCode(w.Error.Code), Message: w.Error.Message}
}
