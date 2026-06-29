package service

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const DefaultMacOSLaunchAgentLabel = "com.remote-dev-skillkit.host"

type LaunchAgentOptions struct {
	Label                  string
	BinaryPath             string
	GatewayURL             string
	TicketCode             string
	ManifestURL            string
	IdentityStorePath      string
	TrustStorePath         string
	NonceStorePath         string
	ApprovalStorePath      string
	WorkspaceLockStorePath string
	LogDir                 string
	Transport              string
	LongPollTimeout        string
}

type LaunchAgent struct {
	Label            string   `json:"label"`
	ProgramArguments []string `json:"program_arguments"`
	StdoutPath       string   `json:"stdout_path"`
	StderrPath       string   `json:"stderr_path"`
	KeepAlive        bool     `json:"keep_alive"`
	RunAtLoad        bool     `json:"run_at_load"`
}

type LaunchAgentStatus struct {
	PlistPath        string   `json:"plist_path"`
	Exists           bool     `json:"exists"`
	Label            string   `json:"label,omitempty"`
	ProgramArguments []string `json:"program_arguments,omitempty"`
	StdoutPath       string   `json:"stdout_path,omitempty"`
	StderrPath       string   `json:"stderr_path,omitempty"`
	KeepAlive        bool     `json:"keep_alive,omitempty"`
	RunAtLoad        bool     `json:"run_at_load,omitempty"`
	Mode             string   `json:"mode,omitempty"`
	SizeBytes        int64    `json:"size_bytes,omitempty"`
}

type LaunchAgentControlOptions struct {
	Action    string
	Label     string
	PlistPath string
	Domain    string
}

type LaunchAgentControlPlan struct {
	Action    string   `json:"action"`
	Label     string   `json:"label"`
	PlistPath string   `json:"plist_path"`
	Domain    string   `json:"domain"`
	Argv      []string `json:"argv"`
	Shell     string   `json:"shell"`
}

func NewMacOSLaunchAgent(opts LaunchAgentOptions) (LaunchAgent, error) {
	if opts.Label == "" {
		opts.Label = DefaultMacOSLaunchAgentLabel
	}
	if opts.Transport == "" {
		opts.Transport = "long-poll"
	}
	if opts.LongPollTimeout == "" {
		opts.LongPollTimeout = "25s"
	}
	if opts.LogDir == "" {
		opts.LogDir = "/tmp"
	}
	if err := validateLaunchAgentOptions(opts); err != nil {
		return LaunchAgent{}, err
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
	return LaunchAgent{
		Label:            opts.Label,
		ProgramArguments: args,
		StdoutPath:       filepath.Join(opts.LogDir, opts.Label+".out.log"),
		StderrPath:       filepath.Join(opts.LogDir, opts.Label+".err.log"),
		KeepAlive:        true,
		RunAtLoad:        true,
	}, nil
}

func NewMacOSLaunchAgentControlPlan(opts LaunchAgentControlOptions) (LaunchAgentControlPlan, error) {
	if opts.Label == "" {
		opts.Label = DefaultMacOSLaunchAgentLabel
	}
	if opts.Domain == "" {
		opts.Domain = "gui/$(id -u)"
	}
	switch opts.Action {
	case "start", "stop":
		if strings.TrimSpace(opts.PlistPath) == "" {
			return LaunchAgentControlPlan{}, fmt.Errorf("plist path is required for %s", opts.Action)
		}
	case "inspect":
	default:
		return LaunchAgentControlPlan{}, fmt.Errorf("unsupported launchctl action %q", opts.Action)
	}
	plan := LaunchAgentControlPlan{
		Action:    opts.Action,
		Label:     opts.Label,
		PlistPath: opts.PlistPath,
		Domain:    opts.Domain,
	}
	switch opts.Action {
	case "start":
		plan.Argv = []string{"launchctl", "bootstrap", opts.Domain, opts.PlistPath}
	case "stop":
		plan.Argv = []string{"launchctl", "bootout", opts.Domain, opts.PlistPath}
	case "inspect":
		plan.Argv = []string{"launchctl", "print", opts.Domain + "/" + opts.Label}
	}
	plan.Shell = shellCommand(plan.Argv)
	return plan, nil
}

func DefaultMacOSLaunchAgentPath(homeDir, label string) string {
	if label == "" {
		label = DefaultMacOSLaunchAgentLabel
	}
	return filepath.Join(homeDir, "Library", "LaunchAgents", label+".plist")
}

func InspectMacOSLaunchAgent(path string) (LaunchAgentStatus, error) {
	if path == "" {
		return LaunchAgentStatus{}, fmt.Errorf("plist path is required")
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return LaunchAgentStatus{PlistPath: path, Exists: false}, nil
	}
	if err != nil {
		return LaunchAgentStatus{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return LaunchAgentStatus{}, err
	}
	agent, err := ParseMacOSLaunchAgent(content)
	if err != nil {
		return LaunchAgentStatus{}, err
	}
	return LaunchAgentStatus{
		PlistPath:        path,
		Exists:           true,
		Label:            agent.Label,
		ProgramArguments: agent.ProgramArguments,
		StdoutPath:       agent.StdoutPath,
		StderrPath:       agent.StderrPath,
		KeepAlive:        agent.KeepAlive,
		RunAtLoad:        agent.RunAtLoad,
		Mode:             fmt.Sprintf("%04o", info.Mode().Perm()),
		SizeBytes:        info.Size(),
	}, nil
}

func ParseMacOSLaunchAgent(content []byte) (LaunchAgent, error) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	var agent LaunchAgent
	var currentElement string
	var pendingKey string
	var inProgramArguments bool
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return LaunchAgent{}, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			currentElement = value.Name.Local
			if value.Name.Local == "array" && pendingKey == "ProgramArguments" {
				inProgramArguments = true
				pendingKey = ""
			}
			if value.Name.Local == "true" || value.Name.Local == "false" {
				boolValue := value.Name.Local == "true"
				switch pendingKey {
				case "KeepAlive":
					agent.KeepAlive = boolValue
				case "RunAtLoad":
					agent.RunAtLoad = boolValue
				}
				pendingKey = ""
			}
		case xml.EndElement:
			if value.Name.Local == "array" {
				inProgramArguments = false
			}
			currentElement = ""
		case xml.CharData:
			text := strings.TrimSpace(string(value))
			if text == "" {
				continue
			}
			switch currentElement {
			case "key":
				pendingKey = text
			case "string":
				if inProgramArguments {
					agent.ProgramArguments = append(agent.ProgramArguments, text)
					continue
				}
				switch pendingKey {
				case "Label":
					agent.Label = text
				case "StandardOutPath":
					agent.StdoutPath = text
				case "StandardErrorPath":
					agent.StderrPath = text
				}
				pendingKey = ""
			}
		}
	}
	if agent.Label == "" {
		return LaunchAgent{}, fmt.Errorf("launch agent label is required")
	}
	return agent, nil
}

func RenderMacOSLaunchAgent(agent LaunchAgent) ([]byte, error) {
	if agent.Label == "" {
		return nil, fmt.Errorf("label is required")
	}
	if len(agent.ProgramArguments) == 0 {
		return nil, fmt.Errorf("program arguments are required")
	}
	var buffer bytes.Buffer
	buffer.WriteString(xml.Header)
	buffer.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "https://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	enc := xml.NewEncoder(&buffer)
	enc.Indent("", "  ")
	if err := enc.EncodeToken(xml.StartElement{Name: xml.Name{Local: "plist"}, Attr: []xml.Attr{{Name: xml.Name{Local: "version"}, Value: "1.0"}}}); err != nil {
		return nil, err
	}
	if err := enc.EncodeToken(xml.StartElement{Name: xml.Name{Local: "dict"}}); err != nil {
		return nil, err
	}
	writeStringKey := func(key, value string) error {
		if err := encodeSimpleElement(enc, "key", key); err != nil {
			return err
		}
		return encodeSimpleElement(enc, "string", value)
	}
	writeBoolKey := func(key string, value bool) error {
		if err := encodeSimpleElement(enc, "key", key); err != nil {
			return err
		}
		tag := "false"
		if value {
			tag = "true"
		}
		return enc.EncodeToken(xml.StartElement{Name: xml.Name{Local: tag}})
	}
	if err := writeStringKey("Label", agent.Label); err != nil {
		return nil, err
	}
	if err := encodeSimpleElement(enc, "key", "ProgramArguments"); err != nil {
		return nil, err
	}
	if err := enc.EncodeToken(xml.StartElement{Name: xml.Name{Local: "array"}}); err != nil {
		return nil, err
	}
	for _, arg := range agent.ProgramArguments {
		if err := encodeSimpleElement(enc, "string", arg); err != nil {
			return nil, err
		}
	}
	if err := enc.EncodeToken(xml.EndElement{Name: xml.Name{Local: "array"}}); err != nil {
		return nil, err
	}
	if err := writeBoolKey("RunAtLoad", agent.RunAtLoad); err != nil {
		return nil, err
	}
	if err := enc.EncodeToken(xml.EndElement{Name: xml.Name{Local: boolTag(agent.RunAtLoad)}}); err != nil {
		return nil, err
	}
	if err := writeBoolKey("KeepAlive", agent.KeepAlive); err != nil {
		return nil, err
	}
	if err := enc.EncodeToken(xml.EndElement{Name: xml.Name{Local: boolTag(agent.KeepAlive)}}); err != nil {
		return nil, err
	}
	if agent.StdoutPath != "" {
		if err := writeStringKey("StandardOutPath", agent.StdoutPath); err != nil {
			return nil, err
		}
	}
	if agent.StderrPath != "" {
		if err := writeStringKey("StandardErrorPath", agent.StderrPath); err != nil {
			return nil, err
		}
	}
	if err := enc.EncodeToken(xml.EndElement{Name: xml.Name{Local: "dict"}}); err != nil {
		return nil, err
	}
	if err := enc.EncodeToken(xml.EndElement{Name: xml.Name{Local: "plist"}}); err != nil {
		return nil, err
	}
	if err := enc.Flush(); err != nil {
		return nil, err
	}
	buffer.WriteByte('\n')
	return buffer.Bytes(), nil
}

func validateLaunchAgentOptions(opts LaunchAgentOptions) error {
	if !validLaunchdLabel(opts.Label) {
		return fmt.Errorf("invalid launchd label %q", opts.Label)
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
	return nil
}

func validLaunchdLabel(label string) bool {
	if label == "" || strings.HasPrefix(label, ".") || strings.HasSuffix(label, ".") {
		return false
	}
	for _, part := range strings.Split(label, ".") {
		if part == "" {
			return false
		}
	}
	for _, r := range label {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func encodeSimpleElement(enc *xml.Encoder, name, value string) error {
	start := xml.StartElement{Name: xml.Name{Local: name}}
	if err := enc.EncodeToken(start); err != nil {
		return err
	}
	if err := enc.EncodeToken(xml.CharData([]byte(value))); err != nil {
		return err
	}
	return enc.EncodeToken(xml.EndElement{Name: start.Name})
}

func boolTag(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func shellCommand(argv []string) string {
	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if value == "gui/$(id -u)" {
		return value
	}
	if safeShellToken(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func safeShellToken(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '/', r == '.', r == '_', r == '-', r == ':', r == '$', r == '(', r == ')':
		default:
			return false
		}
	}
	return value != ""
}
