package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/nebla-cloud/nebla/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Run(ctx, os.Args[1:], cli.DefaultEnv())
	stop()
	os.Exit(code)
}
