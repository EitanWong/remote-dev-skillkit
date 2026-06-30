package service

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const DefaultLinuxSystemdUnitName = "remote-dev-skillkit-host.service"

type SystemdUserServiceOptions struct {
	UnitName                 string
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
	LogDir                   string
	Transport                string
	LongPollTimeout          string
	RestartSec               string
}

type SystemdUserService struct {
	UnitName        string   `json:"unit_name"`
	Description     string   `json:"description"`
	ExecStart       []string `json:"exec_start"`
	StandardOutput  string   `json:"standard_output,omitempty"`
	StandardError   string   `json:"standard_error,omitempty"`
	Restart         string   `json:"restart"`
	RestartSec      string   `json:"restart_sec"`
	WantedBy        string   `json:"wanted_by"`
	NoNewPrivileges bool     `json:"no_new_privileges"`
	PrivateTmp      bool     `json:"private_tmp"`
}

type SystemdUserServiceStatus struct {
	UnitPath        string `json:"unit_path"`
	Exists          bool   `json:"exists"`
	UnitName        string `json:"unit_name,omitempty"`
	Description     string `json:"description,omitempty"`
	ExecStart       string `json:"exec_start,omitempty"`
	StandardOutput  string `json:"standard_output,omitempty"`
	StandardError   string `json:"standard_error,omitempty"`
	Restart         string `json:"restart,omitempty"`
	RestartSec      string `json:"restart_sec,omitempty"`
	WantedBy        string `json:"wanted_by,omitempty"`
	NoNewPrivileges bool   `json:"no_new_privileges,omitempty"`
	PrivateTmp      bool   `json:"private_tmp,omitempty"`
	Mode            string `json:"mode,omitempty"`
	SizeBytes       int64  `json:"size_bytes,omitempty"`
}

type SystemdControlOptions struct {
	Action   string
	UnitName string
	User     bool
}

type SystemdControlPlan struct {
	Action   string     `json:"action"`
	UnitName string     `json:"unit_name"`
	User     bool       `json:"user"`
	Commands [][]string `json:"commands"`
	Shell    []string   `json:"shell"`
}

func NewLinuxSystemdUserService(opts SystemdUserServiceOptions) (SystemdUserService, error) {
	if opts.UnitName == "" {
		opts.UnitName = DefaultLinuxSystemdUnitName
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
	if opts.RestartSec == "" {
		opts.RestartSec = "5s"
	}
	if err := validateSystemdUserServiceOptions(opts); err != nil {
		return SystemdUserService{}, err
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
	service := SystemdUserService{
		UnitName:        opts.UnitName,
		Description:     opts.Description,
		ExecStart:       args,
		Restart:         "on-failure",
		RestartSec:      opts.RestartSec,
		WantedBy:        "default.target",
		NoNewPrivileges: true,
		PrivateTmp:      true,
	}
	if opts.LogDir != "" {
		service.StandardOutput = "append:" + filepath.Join(opts.LogDir, strings.TrimSuffix(opts.UnitName, ".service")+".out.log")
		service.StandardError = "append:" + filepath.Join(opts.LogDir, strings.TrimSuffix(opts.UnitName, ".service")+".err.log")
	}
	return service, nil
}

func RenderLinuxSystemdUserService(unit SystemdUserService) ([]byte, error) {
	if unit.UnitName == "" {
		return nil, fmt.Errorf("unit name is required")
	}
	if len(unit.ExecStart) == 0 {
		return nil, fmt.Errorf("exec start is required")
	}
	if unit.Description == "" {
		unit.Description = "Remote Dev Skillkit managed host"
	}
	if unit.Restart == "" {
		unit.Restart = "on-failure"
	}
	if unit.RestartSec == "" {
		unit.RestartSec = "5s"
	}
	if unit.WantedBy == "" {
		unit.WantedBy = "default.target"
	}
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=" + escapeSystemdValue(unit.Description) + "\n")
	b.WriteString("After=network-online.target\n")
	b.WriteString("Wants=network-online.target\n")
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	b.WriteString("ExecStart=" + systemdExecStart(unit.ExecStart) + "\n")
	b.WriteString("Restart=" + escapeSystemdValue(unit.Restart) + "\n")
	b.WriteString("RestartSec=" + escapeSystemdValue(unit.RestartSec) + "\n")
	if unit.NoNewPrivileges {
		b.WriteString("NoNewPrivileges=true\n")
	}
	if unit.PrivateTmp {
		b.WriteString("PrivateTmp=true\n")
	}
	if unit.StandardOutput != "" {
		b.WriteString("StandardOutput=" + escapeSystemdValue(unit.StandardOutput) + "\n")
	}
	if unit.StandardError != "" {
		b.WriteString("StandardError=" + escapeSystemdValue(unit.StandardError) + "\n")
	}
	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=" + escapeSystemdValue(unit.WantedBy) + "\n")
	return []byte(b.String()), nil
}

func InspectLinuxSystemdUserService(path string) (SystemdUserServiceStatus, error) {
	if path == "" {
		return SystemdUserServiceStatus{}, fmt.Errorf("unit path is required")
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return SystemdUserServiceStatus{UnitPath: path, Exists: false, UnitName: filepath.Base(path)}, nil
	}
	if err != nil {
		return SystemdUserServiceStatus{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return SystemdUserServiceStatus{}, err
	}
	parsed := parseSystemdUnit(content)
	status := SystemdUserServiceStatus{
		UnitPath:        path,
		Exists:          true,
		UnitName:        filepath.Base(path),
		Description:     parsed["Unit.Description"],
		ExecStart:       parsed["Service.ExecStart"],
		StandardOutput:  parsed["Service.StandardOutput"],
		StandardError:   parsed["Service.StandardError"],
		Restart:         parsed["Service.Restart"],
		RestartSec:      parsed["Service.RestartSec"],
		WantedBy:        parsed["Install.WantedBy"],
		NoNewPrivileges: strings.EqualFold(parsed["Service.NoNewPrivileges"], "true"),
		PrivateTmp:      strings.EqualFold(parsed["Service.PrivateTmp"], "true"),
		Mode:            fmt.Sprintf("%04o", info.Mode().Perm()),
		SizeBytes:       info.Size(),
	}
	return status, nil
}

func NewLinuxSystemdControlPlan(opts SystemdControlOptions) (SystemdControlPlan, error) {
	if opts.UnitName == "" {
		opts.UnitName = DefaultLinuxSystemdUnitName
	}
	if !validSystemdUnitName(opts.UnitName) {
		return SystemdControlPlan{}, fmt.Errorf("invalid systemd unit name %q", opts.UnitName)
	}
	commands := [][]string{}
	switch opts.Action {
	case "start":
		commands = append(commands,
			systemctlCommand(opts.User, "daemon-reload"),
			systemctlCommand(opts.User, "enable", "--now", opts.UnitName),
		)
	case "stop":
		commands = append(commands, systemctlCommand(opts.User, "disable", "--now", opts.UnitName))
	case "inspect":
		commands = append(commands, systemctlCommand(opts.User, "status", opts.UnitName))
	default:
		return SystemdControlPlan{}, fmt.Errorf("unsupported systemd action %q", opts.Action)
	}
	shell := make([]string, 0, len(commands))
	for _, command := range commands {
		shell = append(shell, shellCommand(command))
	}
	return SystemdControlPlan{
		Action:   opts.Action,
		UnitName: opts.UnitName,
		User:     opts.User,
		Commands: commands,
		Shell:    shell,
	}, nil
}

func DefaultLinuxSystemdUserUnitPath(homeDir, unitName string) string {
	if unitName == "" {
		unitName = DefaultLinuxSystemdUnitName
	}
	return filepath.Join(homeDir, ".config", "systemd", "user", unitName)
}

func validateSystemdUserServiceOptions(opts SystemdUserServiceOptions) error {
	if !validSystemdUnitName(opts.UnitName) {
		return fmt.Errorf("invalid systemd unit name %q", opts.UnitName)
	}
	if opts.BinaryPath == "" || !filepath.IsAbs(opts.BinaryPath) {
		return fmt.Errorf("binary path must be absolute")
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

func validSystemdUnitName(unitName string) bool {
	if !strings.HasSuffix(unitName, ".service") || strings.Contains(unitName, "/") || strings.Contains(unitName, "\\") {
		return false
	}
	for _, r := range unitName {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-', r == '@':
		default:
			return false
		}
	}
	return len(unitName) > len(".service")
}

func systemctlCommand(user bool, args ...string) []string {
	command := []string{"systemctl"}
	if user {
		command = append(command, "--user")
	}
	return append(command, args...)
}

func systemdExecStart(argv []string) string {
	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		parts = append(parts, systemdQuoteArg(arg))
	}
	return strings.Join(parts, " ")
}

func systemdQuoteArg(arg string) string {
	if arg == "" || strings.ContainsAny(arg, " \t\n\"'\\") {
		return strconv.Quote(arg)
	}
	return arg
}

func escapeSystemdValue(value string) string {
	return strings.ReplaceAll(value, "\n", " ")
}

func parseSystemdUnit(content []byte) map[string]string {
	result := map[string]string{}
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || section == "" {
			continue
		}
		result[section+"."+strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return result
}
