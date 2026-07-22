//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"syscall"

	"github.com/EitanWong/remote-dev-skillkit/internal/bootstrapcmd/windowsentry"
)

func main() {
	app := windowsentry.App{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
	if err := app.Run(context.Background(), os.Args[1:]); err != nil {
		_, _ = os.Stderr.WriteString("rdev-bootstrap layered-run failed\n")
		if errors.Is(err, syscall.ERROR_ALREADY_EXISTS) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
