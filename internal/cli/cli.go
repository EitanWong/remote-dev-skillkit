package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/buildinfo"
	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostcap"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostcmd"
	"github.com/EitanWong/remote-dev-skillkit/internal/httpapi"
	"github.com/EitanWong/remote-dev-skillkit/internal/mcpstdio"
	"github.com/EitanWong/remote-dev-skillkit/internal/operatorauth"
)

type App struct {
	Stdout io.Writer
	Stderr io.Writer
}

func NewApp(stdout, stderr io.Writer) App {
	return App{Stdout: stdout, Stderr: stderr}
}

func (a App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		a.printUsage()
		return nil
	}
	if len(args) > 1 && isHelpArg(args[1]) {
		a.printCommandUsage(args[0])
		return nil
	}

	switch args[0] {
	case "version":
		return a.version(args[1:])
	case "doctor":
		return a.doctor(ctx, args[1:])
	case "git":
		return a.git(ctx, args[1:])
	case "mcp":
		return a.mcp(ctx, args[1:])
	case "host":
		return hostcmd.New(a.Stdout, a.Stderr).Run(ctx, args[1:])
	case "gateway":
		return a.gateway(ctx, args[1:])
	case "toolchain":
		return a.toolchain(ctx, args[1:])
	default:
		return fmt.Errorf("unknown command %q; available commands: version, doctor, git, mcp, host, gateway, toolchain", args[0])
	}
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "-h" || arg == "--help"
}

func (a App) version(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	info := runtimeInfo()
	if *jsonOut {
		return writeJSON(a.Stdout, info)
	}
	_, err := fmt.Fprintf(a.Stdout, "%s %s\n", buildinfo.Name, buildinfo.Version)
	return err
}

func (a App) doctor(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	return writeJSON(a.Stdout, map[string]any{
		"schema_version":      "rdev.doctor.v2",
		"ok":                  true,
		"rdev":                runtimeInfo(),
		"host_capabilities":   hostcap.Detect(ctx),
		"protocol":            "rdev.session.v1",
		"recommended_actions": []string{"Use rdev mcp serve for session control.", "Use rdev host serve --join-code <code> --gateway <https-url> to join a session."},
	})
}

func runtimeInfo() map[string]any {
	executable, err := os.Executable()
	if err != nil {
		executable = ""
	}
	return map[string]any{
		"schema_version":        "rdev.runtime-info.v2",
		"name":                  buildinfo.Name,
		"version":               buildinfo.Version,
		"commit":                buildinfo.Commit,
		"build_time":            buildinfo.BuildTime,
		"source_root":           buildinfo.SourceRoot,
		"current_executable":    executable,
		"goos":                  runtime.GOOS,
		"goarch":                runtime.GOARCH,
		"session_protocol_only": true,
	}
}

func (a App) mcp(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing mcp subcommand")
	}
	switch args[0] {
	case "tools":
		return writeJSON(a.Stdout, map[string]any{
			"schema_version": "rdev.mcp-tools.v2",
			"version":        buildinfo.Version,
			"tools":          contracts.Tools(),
		})
	case "serve":
		fs := flag.NewFlagSet("mcp serve", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		remoteGateway := fs.String("gateway-url", "", "optional HTTPS session gateway URL")
		operatorTokenFile := fs.String("operator-token-file", "", "optional protected operator bearer token file for the configured gateway")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		gw := gateway.NewMemoryGateway()
		if strings.TrimSpace(*remoteGateway) == "" {
			return mcpstdio.NewServer(gw).Serve(ctx, os.Stdin, a.Stdout)
		}
		token, err := readProtectedTokenFile(mcpOperatorTokenFilePath(*operatorTokenFile))
		if err != nil {
			return err
		}
		return mcpstdio.NewServerWithRemoteGatewayAndOperatorToken(gw, *remoteGateway, token).Serve(ctx, os.Stdin, a.Stdout)
	default:
		return fmt.Errorf("unknown mcp subcommand %q", args[0])
	}
}

func mcpOperatorTokenFilePath(explicit string) string {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return explicit
	}
	return strings.TrimSpace(os.Getenv("RDEV_GATEWAY_OPERATOR_TOKEN_FILE"))
}

func readProtectedTokenFile(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read operator token file: %w", err)
	}
	if token := strings.TrimSpace(string(content)); token != "" {
		return token, nil
	}
	return "", fmt.Errorf("operator token file is empty")
}

func (a App) gateway(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing gateway subcommand")
	}
	if args[0] != "serve" {
		return fmt.Errorf("unknown gateway subcommand %q", args[0])
	}
	fs := flag.NewFlagSet("gateway serve", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	addr := fs.String("addr", "127.0.0.1:8787", "loopback listen address")
	dev := fs.Bool("dev", false, "mark the local gateway as development mode")
	operatorAuthFile := fs.String("operator-auth-file", "", "optional protected operator-auth principals file")
	stateFile := fs.String("state-file", "", "protected persistent gateway state file; requires --signing-key-file and --operator-auth-file")
	signingKeyFile := fs.String("signing-key-file", "", "protected persistent gateway Ed25519 signing key file; requires --state-file")
	signingKeyID := fs.String("signing-key-id", defaultManagedGatewaySigningKeyID, "gateway signing key id for persistent state")
	publicBaseURL := fs.String("public-base-url", "", "optional public HTTPS base URL for browser handoff links")
	windowsAMD64HostBinary := fs.String("windows-amd64-host-binary", "", "optional current rdev-host.exe artifact for browser handoffs")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := requireLoopbackAddress(*addr); err != nil {
		return err
	}
	runtime, err := newGatewayRuntime(*stateFile, *signingKeyFile, *signingKeyID, *operatorAuthFile)
	if err != nil {
		return err
	}
	webHandoffOptions, webHandoffEnabled, err := gatewayWebHandoffOptions(*publicBaseURL, *windowsAMD64HostBinary)
	if err != nil {
		return err
	}
	handler, operatorAuthEnabled, err := gatewayHandlerWithRuntime(runtime.Gateway, runtime.StateStore, *operatorAuthFile, webHandoffOptions)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	defer listener.Close()

	server := &http.Server{Handler: handler}
	if err := writeJSON(a.Stdout, map[string]any{
		"schema_version":        "rdev.gateway-ready.v2",
		"url":                   "http://" + listener.Addr().String(),
		"mode":                  map[bool]string{true: "development", false: "local"}[*dev],
		"operator_auth_enabled": operatorAuthEnabled,
		"persistence_enabled":   runtime.Persistent,
		"web_handoff_enabled":   webHandoffEnabled,
		"protocol":              "rdev.session.v1",
	}); err != nil {
		return err
	}
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		err := <-result
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func gatewayWebHandoffOptions(publicBaseURL, windowsAMD64HostBinary string) (httpapi.WebHandoffOptions, bool, error) {
	publicBaseURL = strings.TrimSpace(publicBaseURL)
	windowsAMD64HostBinary = strings.TrimSpace(windowsAMD64HostBinary)
	if publicBaseURL == "" && windowsAMD64HostBinary == "" {
		return httpapi.WebHandoffOptions{}, false, nil
	}
	if publicBaseURL == "" || windowsAMD64HostBinary == "" {
		return httpapi.WebHandoffOptions{}, false, fmt.Errorf("--public-base-url and --windows-amd64-host-binary must be supplied together")
	}
	asset, err := httpapi.LoadWindowsAMD64WebHandoffAsset(windowsAMD64HostBinary)
	if err != nil {
		return httpapi.WebHandoffOptions{}, false, err
	}
	return httpapi.WebHandoffOptions{PublicBaseURL: publicBaseURL, WindowsAMD64: asset}, true, nil
}

func gatewayHandler(operatorAuthFile string) (http.Handler, bool, error) {
	return gatewayHandlerWithWebHandoff(operatorAuthFile, httpapi.WebHandoffOptions{})
}

func gatewayHandlerWithWebHandoff(operatorAuthFile string, webHandoffOptions httpapi.WebHandoffOptions) (http.Handler, bool, error) {
	return gatewayHandlerWithRuntime(gateway.NewMemoryGateway(), nil, operatorAuthFile, webHandoffOptions)
}

func gatewayHandlerWithRuntime(gw *gateway.MemoryGateway, stateStore gateway.StateStore, operatorAuthFile string, webHandoffOptions httpapi.WebHandoffOptions) (http.Handler, bool, error) {
	if gw == nil {
		return nil, false, fmt.Errorf("gateway runtime is required")
	}
	operatorAuthFile = strings.TrimSpace(operatorAuthFile)
	var server httpapi.Server
	operatorAuthEnabled := operatorAuthFile != ""
	if operatorAuthFile == "" {
		server = httpapi.NewServerWithStateStore(gw, stateStore)
	} else {
		auth, _, err := operatorauth.Load(operatorAuthFile)
		if err != nil {
			return nil, false, fmt.Errorf("load operator auth file: %w", err)
		}
		server = httpapi.NewServerWithOperatorAuthAndStateStore(gw, stateStore, auth)
	}
	if strings.TrimSpace(webHandoffOptions.PublicBaseURL) != "" || len(webHandoffOptions.WindowsAMD64.Content) > 0 {
		configured, err := server.WithWebHandoff(webHandoffOptions)
		if err != nil {
			return nil, false, err
		}
		server = configured
	}
	return server.Handler(), operatorAuthEnabled, nil
}

func requireLoopbackAddress(addr string) error {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return fmt.Errorf("gateway address must be host:port: %w", err)
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("gateway serve accepts loopback addresses only")
}

func (a App) printUsage() {
	_, _ = fmt.Fprintln(a.Stdout, "rdev — session-native remote development toolkit")
	_, _ = fmt.Fprintln(a.Stdout, "")
	_, _ = fmt.Fprintln(a.Stdout, "commands:")
	_, _ = fmt.Fprintln(a.Stdout, "  version    print version and build metadata")
	_, _ = fmt.Fprintln(a.Stdout, "  doctor     inspect this host's agent capabilities")
	_, _ = fmt.Fprintln(a.Stdout, "  git        enforce repository git workflow policy (policy check, pr plan)")
	_, _ = fmt.Fprintln(a.Stdout, "  mcp        list or serve the Agent MCP surface")
	_, _ = fmt.Fprintln(a.Stdout, "  host       join a Control Plane session as a target host")
	_, _ = fmt.Fprintln(a.Stdout, "  gateway    serve the Control Plane gateway (loopback dev or operator-managed)")
	_, _ = fmt.Fprintln(a.Stdout, "  toolchain  plan or install agent toolchains (codex, claude-code)")
	_, _ = fmt.Fprintln(a.Stdout, "")
	_, _ = fmt.Fprintln(a.Stdout, "examples:")
	_, _ = fmt.Fprintln(a.Stdout, "  rdev mcp tools                                list the Agent MCP tools")
	_, _ = fmt.Fprintln(a.Stdout, "  rdev gateway serve --dev                      start a loopback development gateway")
	_, _ = fmt.Fprintln(a.Stdout, "  rdev host serve --join-code CODE --gateway URL join a session as a target host")
}

func (a App) printCommandUsage(command string) {
	switch command {
	case "mcp":
		_, _ = fmt.Fprintln(a.Stdout, "rdev mcp tools | rdev mcp serve [--gateway-url URL --operator-token-file PATH]")
	case "host":
		_, _ = fmt.Fprintln(a.Stdout, "rdev host serve --join-code CODE --gateway URL")
	case "gateway":
		_, _ = fmt.Fprintln(a.Stdout, "rdev gateway serve [--addr 127.0.0.1:8787] [--dev] [--operator-auth-file PATH] [--state-file PATH --signing-key-file PATH] [--public-base-url HTTPS_URL --windows-amd64-host-binary PATH]")
	default:
		a.printUsage()
	}
}

func writeJSON(out io.Writer, payload any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func Main() {
	app := NewApp(os.Stdout, os.Stderr)
	if err := app.Run(context.Background(), os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		_, _ = fmt.Fprintf(os.Stderr, "rdev: %v\n", err)
		os.Exit(hostcmd.ExitCode(err))
	}
}
