//go:build !windows

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/EitanWong/remote-dev-skillkit/internal/bootstrapcmd"
)

func main() {
	app := bootstrapcmd.App{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
	if err := app.Run(context.Background(), os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
