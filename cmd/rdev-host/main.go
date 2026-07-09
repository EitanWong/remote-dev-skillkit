package main

import (
	"context"
	"fmt"
	"os"

	"github.com/EitanWong/remote-dev-skillkit/internal/hostcmd"
)

func main() {
	app := hostcmd.New(os.Stdout, os.Stderr)
	if err := app.Run(context.Background(), os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "rdev-host: %v\n", err)
		os.Exit(1)
	}
}
