package powershelladapter

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/shelladapter"
)

const ResultSchemaVersion = "rdev.powershell-result.v1"

type Spec struct {
	WorkspaceRoot      string
	WriteScope         []string
	Command            string
	PowerShellCommand  string
	AllowCommands      []string
	MaxDurationSeconds int
	MaxOutputBytes     int
}

type Result struct {
	Adapter           string   `json:"adapter"`
	PowerShellCommand string   `json:"powershell_command"`
	Command           string   `json:"command"`
	Argv              []string `json:"argv"`
	WorkspaceRoot     string   `json:"workspace_root"`
	ExitCode          int      `json:"exit_code"`
	Stdout            string   `json:"stdout,omitempty"`
	Stderr            string   `json:"stderr,omitempty"`
	TimedOut          bool     `json:"timed_out"`
	Canceled          bool     `json:"canceled"`
	OutputTruncated   bool     `json:"output_truncated"`
	StartedAt         string   `json:"started_at"`
	EndedAt           string   `json:"ended_at"`
	DurationMillis    int64    `json:"duration_millis"`
}

type ResultArtifact struct {
	SchemaVersion     string         `json:"schema_version"`
	Adapter           string         `json:"adapter"`
	PowerShellCommand string         `json:"powershell_command"`
	Command           string         `json:"command"`
	Argv              []string       `json:"argv"`
	WorkspaceRoot     string         `json:"workspace_root"`
	ExitCode          int            `json:"exit_code"`
	Stdout            string         `json:"stdout,omitempty"`
	Stderr            string         `json:"stderr,omitempty"`
	TimedOut          bool           `json:"timed_out"`
	Canceled          bool           `json:"canceled"`
	OutputTruncated   bool           `json:"output_truncated"`
	StartedAt         string         `json:"started_at"`
	EndedAt           string         `json:"ended_at"`
	DurationMillis    int64          `json:"duration_millis"`
	Redacted          bool           `json:"redacted"`
	RedactionRules    []string       `json:"redaction_rules"`
	RedactionCounts   map[string]int `json:"redaction_counts,omitempty"`
}

func Execute(spec Spec) (Result, error) {
	return ExecuteContext(context.Background(), spec)
}

func ExecuteContext(ctx context.Context, spec Spec) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	command := strings.TrimSpace(spec.Command)
	if command == "" {
		return Result{}, fmt.Errorf("powershell command is required")
	}
	powershellCommand, err := resolvePowerShellCommand(spec.PowerShellCommand)
	if err != nil {
		return Result{}, err
	}
	// Prepend a UTF-8 console encoding directive so that localised strings
	// (e.g. Chinese Windows `ver` output) are captured without garbling.
	// $OutputEncoding sets the encoding for pipeline bytes; ConsoleEncoding
	// sets it for native stdout/stderr captured by rdev.
	const utf8Prefix = "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; $OutputEncoding = [System.Text.Encoding]::UTF8; "
	argv := []string{powershellCommand, "-NoProfile", "-NonInteractive", "-Command", utf8Prefix + command}
	execution, err := shelladapter.ExecuteContext(ctx, shelladapter.Spec{
		WorkspaceRoot:      spec.WorkspaceRoot,
		WriteScope:         spec.WriteScope,
		Argv:               argv,
		AllowCommands:      spec.AllowCommands,
		MaxDurationSeconds: spec.MaxDurationSeconds,
		MaxOutputBytes:     spec.MaxOutputBytes,
	})
	if execution.Adapter == "" {
		return Result{}, err
	}
	result := Result{
		Adapter:           "powershell",
		PowerShellCommand: powershellCommand,
		Command:           command,
		Argv:              argv,
		WorkspaceRoot:     execution.WorkspaceRoot,
		ExitCode:          execution.ExitCode,
		Stdout:            execution.Stdout,
		Stderr:            execution.Stderr,
		TimedOut:          execution.TimedOut,
		Canceled:          execution.Canceled,
		OutputTruncated:   execution.OutputTruncated,
		StartedAt:         execution.StartedAt,
		EndedAt:           execution.EndedAt,
		DurationMillis:    execution.DurationMillis,
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

func (r Result) ArtifactContent() string {
	if r.Adapter == "" {
		return ""
	}
	content, err := json.MarshalIndent(r.Artifact(), "", "  ")
	if err != nil {
		return ""
	}
	return string(content)
}

func (r Result) Artifact() ResultArtifact {
	redactor := shelladapter.NewArtifactRedactor()
	argv := make([]string, 0, len(r.Argv))
	for _, arg := range r.Argv {
		argv = append(argv, redactor.Redact(arg))
	}
	return ResultArtifact{
		SchemaVersion:     ResultSchemaVersion,
		Adapter:           r.Adapter,
		PowerShellCommand: redactor.Redact(r.PowerShellCommand),
		Command:           redactor.Redact(r.Command),
		Argv:              argv,
		WorkspaceRoot:     r.WorkspaceRoot,
		ExitCode:          r.ExitCode,
		Stdout:            redactor.Redact(r.Stdout),
		Stderr:            redactor.Redact(r.Stderr),
		TimedOut:          r.TimedOut,
		Canceled:          r.Canceled,
		OutputTruncated:   r.OutputTruncated,
		StartedAt:         r.StartedAt,
		EndedAt:           r.EndedAt,
		DurationMillis:    r.DurationMillis,
		Redacted:          redactor.Redacted(),
		RedactionRules:    shelladapter.RedactionRuleNames(),
		RedactionCounts:   redactor.Counts(),
	}
}

func resolvePowerShellCommand(override string) (string, error) {
	override = strings.TrimSpace(override)
	if override != "" {
		return override, nil
	}
	for _, name := range []string{"pwsh", "powershell", "powershell.exe"} {
		if _, err := exec.LookPath(name); err == nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("powershell executable is required")
}
