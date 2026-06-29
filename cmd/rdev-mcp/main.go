package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/cli"
)

func main() {
	args := mcpArgs(os.Args[1:])
	app := cli.NewApp(os.Stdout, os.Stderr)
	if err := app.Run(context.Background(), args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "rdev-mcp: %v\n", err)
		os.Exit(1)
	}
}

func mcpArgs(args []string) []string {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return append([]string{"mcp", "serve"}, args...)
	}
	switch args[0] {
	case "mcp", "version", "help", "-h", "--help":
		return args
	case "serve", "tools":
		return append([]string{"mcp"}, args...)
	default:
		return append([]string{"mcp", "serve"}, args...)
	}
}
