package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/cli"
)

func main() {
	args := hostArgs(os.Args[1:])
	app := cli.NewApp(os.Stdout, os.Stderr)
	if err := app.Run(context.Background(), args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "rdev-host: %v\n", err)
		os.Exit(1)
	}
}

func hostArgs(args []string) []string {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return append([]string{"host", "serve"}, args...)
	}
	switch args[0] {
	case "host", "version", "help", "-h", "--help":
		return args
	case "serve", "install-service", "service-status", "service-control", "uninstall-service":
		return append([]string{"host"}, args...)
	default:
		return append([]string{"host", "serve"}, args...)
	}
}
