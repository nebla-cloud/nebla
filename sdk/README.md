# Nebla Go SDK

Go client for the [Nebla](https://github.com/nebla-cloud/nebla) panel API.
It covers the operations available to project tokens.

## Install

```sh
go get github.com/nebla-cloud/nebla/sdk
```

The import path is `github.com/nebla-cloud/nebla/sdk/nebla`.

## Quick start

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/nebla-cloud/nebla/sdk/nebla"
)

func main() {
	ctx := context.Background()
	c := nebla.NewClient(
		nebla.WithEndpoint("https://panel.example"),
		nebla.WithToken(os.Getenv("NEBLA_TOKEN")),
	)

	res, err := c.Servers.Create(ctx, nebla.ServerCreateOpts{
		Name:       "web-1",
		ServerType: "small",
		Image:      "ubuntu-24.04",
		Location:   "lon1",
		SSHKeys:    []string{"deploy"},
	})
	if err != nil {
		log.Fatal(err)
	}
	err = c.Actions.WaitFor(ctx, append([]nebla.Action{res.Action}, res.NextActions...), nebla.OnProgress(func(a nebla.Action) {
		fmt.Printf("%s: %d%%\n", a.Command, a.Progress)
	}))
	if err != nil {
		var ae *nebla.ActionError
		if errors.As(err, &ae) {
			log.Fatalf("action %d failed: %s", ae.Action.ID, ae.Message)
		}
		log.Fatal(err)
	}

	servers, err := c.Servers.All(ctx, nebla.ServerListOpts{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("servers:", len(servers))

	if _, err := c.Servers.GetByName(ctx, "missing"); errors.Is(err, nebla.ErrNotFound) {
		fmt.Println("no server named missing")
	}
}
```

`WaitFor` polls each action in order until it finishes. The poll interval starts
at 1 second and grows to 5 seconds. `PollInterval(d)` fixes it, and
`OnProgress(fn)` is called with each polled state.

## Pagination

Each resource has `List`, which returns one page and its `Meta`, and `All`,
which fetches every page:

```go
page, meta, err := c.Servers.List(ctx, nebla.ServerListOpts{
	ListOpts: nebla.ListOpts{Page: 2, PerPage: 25},
})
fmt.Println(len(page), meta.TotalEntries)
```

`All` requests 50 items per page and stops at the first empty page or once
the total is reached.

## Errors

API errors are returned as `*nebla.Error`, with `StatusCode`, `Code` and
`Message` fields. Check them with:

- `errors.Is(err, nebla.ErrNotFound)`, which matches any error with that code.
- `nebla.IsCode(err, nebla.CodeNameTaken, ...)`, which matches one of several codes.

An action that finishes with status `error` makes `WaitFor` and `Wait` return
a `*nebla.ActionError`, which carries the `Action` and its failure `Code` and
`Message`. Use `errors.As` to read it.

## Retries

By default the client makes up to 5 attempts with exponential backoff, starting
at 500ms and capped at 5s. It retries:

- any method on HTTP 429, honouring `Retry-After` up to one minute;
- GET requests on HTTP 502, 503 and 504, and on transport errors.

Request bodies are buffered, so a retried request sends the same bytes. To
change the policy, pass `WithRetry`. A `MaxAttempts` of 1 or less disables
retries:

```go
c := nebla.NewClient(
	nebla.WithEndpoint("https://panel.example"),
	nebla.WithToken(token),
	nebla.WithRetry(nebla.RetryPolicy{MaxAttempts: 1}),
)
```

## Configuration

- `WithEndpoint` sets the panel URL, for example `https://panel.example`.
  A trailing slash or a `/v1` suffix is accepted.
- `WithToken` sets the API token.
- `WithUserAgent` adds a prefix to the User-Agent header. The header always ends
  with `nebla-go/<version>`.
- `WithHTTPClient` replaces the `*http.Client` used for requests.

## License

Apache License 2.0. See [LICENSE](LICENSE).
