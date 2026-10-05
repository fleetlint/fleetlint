// Command fleetlint checks a repository's setup against a catalog of rules.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/fleetlint/fleetlint/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	code, err := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "fleetlint:", err)
	}
	os.Exit(code)
}
