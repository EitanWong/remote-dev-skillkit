package service

import (
	"fmt"
	"strings"
)

const DefaultWindowsServiceName = "RemoteDevSkillkitHost"

type WindowsServiceOptions struct {
	ServiceName              string
	DisplayName              string
	Description              string
	BinaryPath               string
	GatewayURL               string
	TicketCode               string
	ManifestURL              string
	IdentityStorePath        string
	TrustStorePath           string
	NonceStorePath           string
	ApprovalStorePath        string
	WorkspaceLockStorePath   string
	ReleaseBundlePath        string
	ReleaseRootPublicKey     string
	ReleaseRequiredArtifacts []string
	Transport                string
	LongPollTimeout          string
}

type WindowsService struct {
	ServiceName string     `json:"service_name"`
	DisplayName string     `json:"display_name"`
	Description string     `json:"description"`
	Args        []string   `json:"args"`
	BinPath     string     `json:"bin_path"`
	Commands    [][]string `json:"commands"`
	Shell       []string   `json:"shell"`
	StartType   string     `json:"start_type"`
}

type WindowsServiceStatus struct {
	ServiceName string     `json:"service_name"`
	Commands    [][]string `json:"commands"`
	Shell       []string   `json:"shell"`
}

type WindowsServiceControlOptions struct {
	Action      string
	ServiceName string
}

type WindowsServiceControlPlan struct {
	Action      string     `json:"action"`
	ServiceName string     `json:"service_name"`
	Commands    [][]string `json:"commands"`
	Shell       []string   `json:"shell"`
}

func NewWindowsService(opts WindowsServiceOptions) (WindowsService, error) {
	if opts.ServiceName == "" {
		opts.ServiceName = DefaultWindowsServiceName
	}
	if opts.DisplayName == "" {
		opts.DisplayName = "Remote Dev Skillkit Host"
	}
	if opts.Description == "" {
		opts.Description = "Remote Dev Skillkit managed host"
	}
	if opts.Transport == "" {
		opts.Transport = "long-poll"
	}
	if opts.LongPollTimeout == "" {
		opts.LongPollTimeout = "25s"
	}
	if err := validateWindowsServiceOptions(opts); err != nil {
		return WindowsService{}, err
	}
	args := []string{
		opts.BinaryPath,
		"host", "serve",
		"--mode", "managed",
		"--once=false",
		"--transport", opts.Transport,
		"--long-poll-timeout", opts.LongPollTimeout,
	}
	if opts.GatewayURL != "" {
		args = append(args, "--gateway", opts.GatewayURL)
	}
	if opts.TicketCode != "" {
		args = append(args, "--ticket-code", opts.TicketCode)
	}
	if opts.ManifestURL != "" {
		args = append(args, "--manifest-url", opts.ManifestURL)
	}
	if opts.IdentityStorePath != "" {
		args = append(args, "--identity-store", opts.IdentityStorePath)
	}
	if opts.TrustStorePath != "" {
		args = append(args, "--trust-store", opts.TrustStorePath)
	}
	if opts.NonceStorePath != "" {
		args = append(args, "--nonce-store", opts.NonceStorePath)
	}
	if opts.ApprovalStorePath != "" {
		args = append(args, "--approval-store", opts.ApprovalStorePath)
	}
	if opts.WorkspaceLockStorePath != "" {
		args = append(args, "--workspace-lock-store", opts.WorkspaceLockStorePath)
	}
	appendReleaseGateArgs(&args, opts.ReleaseBundlePath, opts.ReleaseRootPublicKey, opts.ReleaseRequiredArtifacts)
	binPath := windowsServiceBinPath(args)
	commands := [][]string{
		{"sc.exe", "create", opts.ServiceName, "binPath=", binPath, "start=", "demand", "DisplayName=", opts.DisplayName},
		{"sc.exe", "description", opts.ServiceName, opts.Description},
	}
	return WindowsService{
		ServiceName: opts.ServiceName,
		DisplayName: opts.DisplayName,
		Description: opts.Description,
		Args:        args,
		BinPath:     binPath,
		Commands:    commands,
		Shell:       shellCommands(commands),
		StartType:   "demand",
	}, nil
}

func NewWindowsServiceStatus(serviceName string) (WindowsServiceStatus, error) {
	if serviceName == "" {
		serviceName = DefaultWindowsServiceName
	}
	if !validWindowsServiceName(serviceName) {
		return WindowsServiceStatus{}, fmt.Errorf("invalid Windows service name %q", serviceName)
	}
	commands := [][]string{
		{"sc.exe", "query", serviceName},
		{"sc.exe", "qc", serviceName},
	}
	return WindowsServiceStatus{
		ServiceName: serviceName,
		Commands:    commands,
		Shell:       shellCommands(commands),
	}, nil
}

func NewWindowsServiceControlPlan(opts WindowsServiceControlOptions) (WindowsServiceControlPlan, error) {
	if opts.ServiceName == "" {
		opts.ServiceName = DefaultWindowsServiceName
	}
	if !validWindowsServiceName(opts.ServiceName) {
		return WindowsServiceControlPlan{}, fmt.Errorf("invalid Windows service name %q", opts.ServiceName)
	}
	var commands [][]string
	switch opts.Action {
	case "start":
		commands = append(commands, []string{"sc.exe", "start", opts.ServiceName})
	case "stop":
		commands = append(commands, []string{"sc.exe", "stop", opts.ServiceName})
	case "inspect":
		commands = append(commands, []string{"sc.exe", "query", opts.ServiceName}, []string{"sc.exe", "qc", opts.ServiceName})
	default:
		return WindowsServiceControlPlan{}, fmt.Errorf("unsupported Windows service action %q", opts.Action)
	}
	return WindowsServiceControlPlan{
		Action:      opts.Action,
		ServiceName: opts.ServiceName,
		Commands:    commands,
		Shell:       shellCommands(commands),
	}, nil
}

func NewWindowsServiceUninstallPlan(serviceName string) (WindowsServiceControlPlan, error) {
	if serviceName == "" {
		serviceName = DefaultWindowsServiceName
	}
	if !validWindowsServiceName(serviceName) {
		return WindowsServiceControlPlan{}, fmt.Errorf("invalid Windows service name %q", serviceName)
	}
	commands := [][]string{
		{"sc.exe", "stop", serviceName},
		{"sc.exe", "delete", serviceName},
	}
	return WindowsServiceControlPlan{
		Action:      "uninstall",
		ServiceName: serviceName,
		Commands:    commands,
		Shell:       shellCommands(commands),
	}, nil
}

func validateWindowsServiceOptions(opts WindowsServiceOptions) error {
	if !validWindowsServiceName(opts.ServiceName) {
		return fmt.Errorf("invalid Windows service name %q", opts.ServiceName)
	}
	if strings.TrimSpace(opts.BinaryPath) == "" {
		return fmt.Errorf("binary path is required")
	}
	if opts.TicketCode == "" && opts.ManifestURL == "" {
		return fmt.Errorf("ticket code or manifest URL is required")
	}
	if opts.TicketCode != "" && opts.GatewayURL == "" {
		return fmt.Errorf("gateway URL is required with ticket code")
	}
	if opts.TicketCode != "" && opts.ManifestURL != "" {
		return fmt.Errorf("ticket code and manifest URL are mutually exclusive")
	}
	if opts.Transport != "long-poll" && opts.Transport != "poll" {
		return fmt.Errorf("unsupported transport %q", opts.Transport)
	}
	if err := validateReleaseGateOptions(opts.ReleaseBundlePath, opts.ReleaseRootPublicKey, opts.ReleaseRequiredArtifacts); err != nil {
		return err
	}
	return nil
}

func validWindowsServiceName(name string) bool {
	if name == "" || len(name) > 80 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func windowsServiceBinPath(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, windowsQuoteArg(arg))
	}
	return strings.Join(quoted, " ")
}

func windowsQuoteArg(arg string) string {
	if arg == "" {
		return `""`
	}
	if !strings.ContainsAny(arg, " \t\"") {
		return arg
	}
	return `"` + strings.ReplaceAll(arg, `"`, `\"`) + `"`
}

func shellCommands(commands [][]string) []string {
	shell := make([]string, 0, len(commands))
	for _, command := range commands {
		shell = append(shell, shellCommand(command))
	}
	return shell
}
