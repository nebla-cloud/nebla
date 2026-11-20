package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Hexeption/nebla/internal/cli/apiclient"
)

var (
	retryBaseDelay = 500 * time.Millisecond
	retryMaxDelay  = 5 * time.Second
	retryAfterMax  = time.Minute
	requestTimeout = 30 * time.Second
)

const (
	retryAttempts = 5
	perPage       = 50
)

// API is the generated client with retries, auth and the CLI's helpers.
type API struct {
	C *apiclient.ClientWithResponses
	// Timeout bounds Wait; zero means no limit.
	Timeout time.Duration
}

func newAPI(env Env, t Target) (*API, error) {
	ep, err := normalizeEndpoint(t.Endpoint)
	if err != nil {
		return nil, err
	}
	base := http.DefaultTransport
	if env.HTTPClient != nil && env.HTTPClient.Transport != nil {
		base = env.HTTPClient.Transport
	}
	hc := &http.Client{Transport: retryTransport{base: base}}
	token := t.Token
	c, err := apiclient.NewClientWithResponses(ep+"/v1",
		apiclient.WithHTTPClient(hc),
		apiclient.WithRequestEditorFn(func(_ context.Context, r *http.Request) error {
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("User-Agent", "nebla-cli/"+Version)
			return nil
		}))
	if err != nil {
		return nil, err
	}
	return &API{C: c}, nil
}

// retryTransport retries GETs on 429, 502, 503, 504 and network errors.
type retryTransport struct{ base http.RoundTripper }

func (t retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		ctx, cancel := context.WithTimeout(req.Context(), requestTimeout)
		res, err := t.base.RoundTrip(req.WithContext(ctx))
		if err != nil {
			cancel()
		} else {
			res.Body = cancelOnClose{res.Body, cancel}
		}
		if req.Method != http.MethodGet || attempt == retryAttempts-1 || req.Context().Err() != nil || !retryable(res, err) {
			return res, err
		}
		d := backoff(attempt, res)
		if res != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
			res.Body.Close()
		}
		timer := time.NewTimer(d)
		select {
		case <-timer.C:
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		}
	}
}

func retryable(res *http.Response, err error) bool {
	if err != nil {
		return true
	}
	switch res.StatusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func backoff(attempt int, res *http.Response) time.Duration {
	if res != nil {
		if s, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && s >= 0 {
			return min(time.Duration(s)*time.Second, retryAfterMax)
		}
	}
	d := retryBaseDelay << attempt
	if d > retryMaxDelay {
		d = retryMaxDelay
	}
	return d
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// APIError is an error response from the panel.
type APIError struct {
	Status        int
	Code, Message string
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

func apiErr(status int, body []byte) error {
	var e apiclient.Error
	if json.Unmarshal(body, &e) != nil || e.Error.Code == "" {
		return &APIError{Status: status, Message: fmt.Sprintf("unexpected status %d", status)}
	}
	return &APIError{Status: status, Code: e.Error.Code, Message: e.Error.Message}
}

// result returns v for a 2xx response and an APIError otherwise.
func result[T any](status int, body []byte, v *T) (*T, error) {
	if status < 200 || status > 299 {
		return nil, apiErr(status, body)
	}
	if v == nil {
		return nil, fmt.Errorf("unexpected response (status %d); is the endpoint a Nebla panel?", status)
	}
	return v, nil
}

func pageAll[T any](fetch func(page *int) ([]T, apiclient.Meta, error)) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		items, meta, err := fetch(&page)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(items) == 0 || page*perPage >= meta.Pagination.TotalEntries {
			return all, nil
		}
	}
}

// Servers lists every server matching params.
func (a *API) Servers(ctx context.Context, params apiclient.GetServersParams) ([]apiclient.Server, error) {
	pp := perPage
	params.PerPage = &pp
	return pageAll(func(page *int) ([]apiclient.Server, apiclient.Meta, error) {
		params.Page = page
		res, err := a.C.GetServersWithResponse(ctx, &params)
		if err != nil {
			return nil, apiclient.Meta{}, err
		}
		v, err := result(res.StatusCode(), res.Body, res.JSON200)
		if err != nil {
			return nil, apiclient.Meta{}, err
		}
		return v.Servers, v.Meta, nil
	})
}

// SSHKeys lists every SSH key.
func (a *API) SSHKeys(ctx context.Context) ([]apiclient.SshKey, error) {
	pp := perPage
	return pageAll(func(page *int) ([]apiclient.SshKey, apiclient.Meta, error) {
		res, err := a.C.GetSshKeysWithResponse(ctx, &apiclient.GetSshKeysParams{Page: page, PerPage: &pp})
		if err != nil {
			return nil, apiclient.Meta{}, err
		}
		v, err := result(res.StatusCode(), res.Body, res.JSON200)
		if err != nil {
			return nil, apiclient.Meta{}, err
		}
		return v.SshKeys, v.Meta, nil
	})
}

// Images lists every image, only those in location when it is set.
func (a *API) Images(ctx context.Context, location string) ([]apiclient.Image, error) {
	pp := perPage
	var loc *string
	if location != "" {
		loc = &location
	}
	return pageAll(func(page *int) ([]apiclient.Image, apiclient.Meta, error) {
		res, err := a.C.GetImagesWithResponse(ctx, &apiclient.GetImagesParams{Page: page, PerPage: &pp, Location: loc})
		if err != nil {
			return nil, apiclient.Meta{}, err
		}
		v, err := result(res.StatusCode(), res.Body, res.JSON200)
		if err != nil {
			return nil, apiclient.Meta{}, err
		}
		return v.Images, v.Meta, nil
	})
}

// ServerTypes lists every server type.
func (a *API) ServerTypes(ctx context.Context) ([]apiclient.ServerType, error) {
	pp := perPage
	return pageAll(func(page *int) ([]apiclient.ServerType, apiclient.Meta, error) {
		res, err := a.C.GetServerTypesWithResponse(ctx, &apiclient.GetServerTypesParams{Page: page, PerPage: &pp})
		if err != nil {
			return nil, apiclient.Meta{}, err
		}
		v, err := result(res.StatusCode(), res.Body, res.JSON200)
		if err != nil {
			return nil, apiclient.Meta{}, err
		}
		return v.ServerTypes, v.Meta, nil
	})
}

// Locations lists every location.
func (a *API) Locations(ctx context.Context) ([]apiclient.Location, error) {
	pp := perPage
	return pageAll(func(page *int) ([]apiclient.Location, apiclient.Meta, error) {
		res, err := a.C.GetLocationsWithResponse(ctx, &apiclient.GetLocationsParams{Page: page, PerPage: &pp})
		if err != nil {
			return nil, apiclient.Meta{}, err
		}
		v, err := result(res.StatusCode(), res.Body, res.JSON200)
		if err != nil {
			return nil, apiclient.Meta{}, err
		}
		return v.Locations, v.Meta, nil
	})
}

// resolve tries ref as an id when it is a positive integer, then as a name.
func resolve[T any](ref, kind string, byID func(int64) (*T, *http.Response, []byte, error), byName func(string) (*T, error)) (T, error) {
	var zero T
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil && id > 0 {
		v, res, b, err := byID(id)
		if err != nil {
			return zero, err
		}
		switch res.StatusCode {
		case http.StatusOK:
			if v == nil {
				return zero, fmt.Errorf("unexpected response from %s; is the endpoint a Nebla panel?", res.Request.URL)
			}
			return *v, nil
		case http.StatusNotFound:
		default:
			return zero, apiErr(res.StatusCode, b)
		}
	}
	v, err := byName(ref)
	if err != nil {
		return zero, err
	}
	if v == nil {
		return zero, fmt.Errorf("%s %q not found", kind, ref)
	}
	return *v, nil
}

func named[T any](items []T, name func(T) string, ref string) *T {
	for i := range items {
		if name(items[i]) == ref {
			return &items[i]
		}
	}
	return nil
}

// Server finds a server by id or name.
func (a *API) Server(ctx context.Context, ref string) (apiclient.Server, error) {
	return resolve(ref, "server", func(id int64) (*apiclient.Server, *http.Response, []byte, error) {
		res, err := a.C.GetServerWithResponse(ctx, id)
		if err != nil {
			return nil, nil, nil, err
		}
		return res.JSON200, res.HTTPResponse, res.Body, nil
	}, func(name string) (*apiclient.Server, error) {
		list, err := a.Servers(ctx, apiclient.GetServersParams{Name: &name})
		if err != nil {
			return nil, err
		}
		return named(list, func(s apiclient.Server) string { return s.Name }, name), nil
	})
}

// SSHKey finds an SSH key by id or name.
func (a *API) SSHKey(ctx context.Context, ref string) (apiclient.SshKey, error) {
	return resolve(ref, "SSH key", func(id int64) (*apiclient.SshKey, *http.Response, []byte, error) {
		res, err := a.C.GetSshKeyWithResponse(ctx, id)
		if err != nil {
			return nil, nil, nil, err
		}
		return res.JSON200, res.HTTPResponse, res.Body, nil
	}, func(name string) (*apiclient.SshKey, error) {
		list, err := a.SSHKeys(ctx)
		if err != nil {
			return nil, err
		}
		return named(list, func(k apiclient.SshKey) string { return k.Name }, name), nil
	})
}

// Image finds an image by id or name.
func (a *API) Image(ctx context.Context, ref string) (apiclient.Image, error) {
	return resolve(ref, "image", func(id int64) (*apiclient.Image, *http.Response, []byte, error) {
		res, err := a.C.GetImageWithResponse(ctx, id)
		if err != nil {
			return nil, nil, nil, err
		}
		return res.JSON200, res.HTTPResponse, res.Body, nil
	}, func(name string) (*apiclient.Image, error) {
		list, err := a.Images(ctx, "")
		if err != nil {
			return nil, err
		}
		return named(list, func(i apiclient.Image) string { return i.Name }, name), nil
	})
}

// ServerType finds a server type by id or name.
func (a *API) ServerType(ctx context.Context, ref string) (apiclient.ServerType, error) {
	return resolve(ref, "server type", func(id int64) (*apiclient.ServerType, *http.Response, []byte, error) {
		res, err := a.C.GetServerTypeWithResponse(ctx, id)
		if err != nil {
			return nil, nil, nil, err
		}
		return res.JSON200, res.HTTPResponse, res.Body, nil
	}, func(name string) (*apiclient.ServerType, error) {
		list, err := a.ServerTypes(ctx)
		if err != nil {
			return nil, err
		}
		return named(list, func(s apiclient.ServerType) string { return s.Name }, name), nil
	})
}

// Location finds a location by id or name.
func (a *API) Location(ctx context.Context, ref string) (apiclient.Location, error) {
	return resolve(ref, "location", func(id int64) (*apiclient.Location, *http.Response, []byte, error) {
		res, err := a.C.GetLocationWithResponse(ctx, id)
		if err != nil {
			return nil, nil, nil, err
		}
		return res.JSON200, res.HTTPResponse, res.Body, nil
	}, func(name string) (*apiclient.Location, error) {
		list, err := a.Locations(ctx)
		if err != nil {
			return nil, err
		}
		return named(list, func(l apiclient.Location) string { return l.Name }, name), nil
	})
}
