package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/toolchain"
)

func (a App) toolchain(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing toolchain subcommand (available: plan, ensure)")
	}
	command := strings.TrimSpace(args[0])
	if command != "plan" && command != "ensure" {
		return fmt.Errorf("unknown toolchain subcommand %q", command)
	}
	fs := flag.NewFlagSet("toolchain "+command, flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	toolName := fs.String("tool", "", "agent toolchain: codex or claude-code")
	version := fs.String("version", "", "exact package version")
	policyFile := fs.String("policy-file", "", "trusted toolchain policy JSON file")
	root := fs.String("root", "", "optional user-scoped toolchain root")
	execute := fs.Bool("execute", false, "execute installation (required for ensure)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected toolchain arguments: %s", strings.Join(fs.Args(), " "))
	}
	if command == "ensure" && !*execute {
		return fmt.Errorf("toolchain ensure requires --execute")
	}
	if command == "plan" && *execute {
		return fmt.Errorf("toolchain plan does not accept --execute; use toolchain ensure --execute")
	}
	policy, err := readToolchainPolicyFile(*policyFile)
	if err != nil {
		return err
	}
	request, err := contracts.DecodeToolchainRequest(map[string]any{
		"schema_version": contracts.ToolchainRequestSchemaVersion,
		"tool":           *toolName,
		"version":        *version,
		"execute":        *execute,
		"policy":         policy,
	})
	if err != nil {
		return err
	}
	result, ensureErr := toolchain.Ensure(ctx, request, toolchain.Options{Root: *root})
	encoder := json.NewEncoder(a.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return err
	}
	return ensureErr
}

func readToolchainPolicyFile(path string) (any, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("--policy-file is required")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read toolchain policy: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	var policy any
	if err := decoder.Decode(&policy); err != nil {
		return nil, fmt.Errorf("decode toolchain policy: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode toolchain policy: trailing JSON value")
	}
	return policy, nil
}
