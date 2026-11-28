// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/nebla-cloud/nebla/sdk/nebla"
)

func ExampleClient() {
	ctx := context.Background()
	c := nebla.NewClient(
		nebla.WithEndpoint("https://panel.example"),
		nebla.WithToken(os.Getenv("NEBLA_TOKEN")),
	)

	res, err := c.Servers.Create(ctx, nebla.ServerCreateOpts{
		Name:       "web-1",
		ServerType: "cx22",
		Image:      "ubuntu-24.04",
		Location:   "fsn1",
		SSHKeys:    []string{"deploy"},
	})
	if err != nil {
		log.Fatal(err)
	}
	err = c.Actions.WaitFor(ctx, []nebla.Action{res.Action}, nebla.OnProgress(func(a nebla.Action) {
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
