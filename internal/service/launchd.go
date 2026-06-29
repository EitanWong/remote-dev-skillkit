package service

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"path/filepath"
	"strings"
)

const DefaultMacOSLaunchAgentLabel = "com.remote-dev-skillkit.host"

type LaunchAgentOptions struct {
	Label             string
	BinaryPath        string
	GatewayURL        string
	TicketCode        string
	ManifestURL       string
	IdentityStorePath string
	TrustStorePath    string
	NonceStorePath    string
	ApprovalStorePath string
	LogDir            string
	Transport         string
	LongPollTimeout   string
}

type LaunchAgent struct {
	Label            string   `json:"label"`
	ProgramArguments []string `json:"program_arguments"`
	StdoutPath       string   `json:"stdout_path"`
	StderrPath       string   `json:"stderr_path"`
	KeepAlive        bool     `json:"keep_alive"`
	RunAtLoad        bool     `json:"run_at_load"`
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
	return LaunchAgent{
		Label:            opts.Label,
		ProgramArguments: args,
		StdoutPath:       filepath.Join(opts.LogDir, opts.Label+".out.log"),
		StderrPath:       filepath.Join(opts.LogDir, opts.Label+".err.log"),
		KeepAlive:        true,
		RunAtLoad:        true,
	}, nil
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
