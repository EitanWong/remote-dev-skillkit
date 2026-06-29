package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/cli"
)

func main() {
	args := gatewayArgs(os.Args[1:])
	app := cli.NewApp(os.Stdout, os.Stderr)
	if err := app.Run(context.Background(), args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "rdev-gateway: %v\n", err)
		os.Exit(1)
	}
}

func gatewayArgs(args []string) []string {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return append([]string{"gateway", "serve"}, args...)
	}
	switch args[0] {
	case "gateway", "version", "help", "-h", "--help":
		return args
	case "serve":
		return append([]string{"gateway"}, args...)
	default:
		return append([]string{"gateway", "serve"}, args...)
	}
}
