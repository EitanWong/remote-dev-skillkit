package hostrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/acpxadapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/claudecodeadapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/codexadapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/desktopadapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/fileadapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/powershelladapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/shelladapter"
	"github.com/EitanWong/remote-dev-skillkit/pkg/adapterkit"
)

type adapterExecution struct {
	ArtifactContent       string
	RuntimeFixtureContent string
}

func executeJobAdapter(ctx context.Context, envelope taskEnvelope, captureRuntimeFixture bool, releaseWorkspaceLock func()) (adapterExecution, error) {
	if !captureRuntimeFixture {
		artifact, err := executeJobAdapterDirect(ctx, envelope)
		return adapterExecution{ArtifactContent: artifact}, err
	}
	runtimeAdapter := &hostRuntimeAdapter{
		envelope:             envelope,
		releaseWorkspaceLock: releaseWorkspaceLock,
	}
	fixture, err := adapterkit.RunLifecycle(ctx, runtimeAdapter, adapterkit.RuntimeRequest{
		Adapter:       envelope.Adapter,
		TaskID:        envelope.TaskID,
		WorkspaceRoot: envelope.Workspace.Root,
		Intent:        envelope.Intent,
		Payload:       envelope.Payload,
		Limits: adapterkit.RuntimeLimits{
			MaxDurationSeconds: envelope.Limits.MaxDurationSeconds,
			MaxOutputBytes:     envelope.Limits.MaxOutputBytes,
		},
		Authorizations: envelope.InterruptsRequired,
	})
	fixtureContent, fixtureErr := fixture.JSON()
	if fixtureErr != nil {
		err = errors.Join(err, fixtureErr)
	}
	artifact := runtimeAdapter.artifactContent
	if artifact == "" && len(fixture.ResultArtifact) > 0 {
		artifact = formatRawArtifact(fixture.ResultArtifact)
	}
	return adapterExecution{
		ArtifactContent:       artifact,
		RuntimeFixtureContent: string(fixtureContent),
	}, err
}

type hostRuntimeAdapter struct {
	envelope             taskEnvelope
	releaseWorkspaceLock func()
	artifactContent      string
	resultSchema         string
}

func (a *hostRuntimeAdapter) Detect(context.Context, adapterkit.RuntimeRequest) (adapterkit.RuntimePhaseOutput, error) {
	return adapterkit.RuntimePhaseOutput{
		Evidence: []string{
			"adapter:" + a.envelope.Adapter,
			"capabilities:" + strings.Join(a.envelope.Capabilities, ","),
		},
		Detail: "hostrunner selected a built-in adapter after signed-envelope validation",
	}, nil
}

func (a *hostRuntimeAdapter) Plan(context.Context, adapterkit.RuntimeRequest) (adapterkit.RuntimePhaseOutput, error) {
	return adapterkit.RuntimePhaseOutput{
		Evidence: []string{
			"intent:" + a.envelope.Intent,
			fmt.Sprintf("interrupts_required:%d", len(a.envelope.InterruptsRequired)),
			"implicit_interrupt_preflight:passed",
		},
		Detail: "hostrunner completed explicit and implicit interrupt checks before adapter execution",
	}, nil
}

func (a *hostRuntimeAdapter) Prepare(context.Context, adapterkit.RuntimeRequest) (adapterkit.RuntimePhaseOutput, error) {
	return adapterkit.RuntimePhaseOutput{
		Evidence: []string{
			"workspace:" + a.envelope.Workspace.Root,
			"workspace_boundary:validated",
			"workspace_lock:acquired_or_not_configured",
		},
		Detail: "hostrunner validated workspace policy and acquired the configured workspace lock before runtime execution",
	}, nil
}

func (a *hostRuntimeAdapter) Run(ctx context.Context, _ adapterkit.RuntimeRequest) (adapterkit.RuntimePhaseOutput, error) {
	artifact, err := executeJobAdapterDirect(ctx, a.envelope)
	a.artifactContent = artifact
	a.resultSchema = resultSchemaForAdapter(a.envelope.Adapter)
	output := adapterkit.RuntimePhaseOutput{
		Evidence:       []string{"adapter_executed:" + a.envelope.Adapter},
		Detail:         "built-in hostrunner adapter executed under runtime lifecycle supervision",
		ArtifactSchema: a.resultSchema,
	}
	if artifact != "" && err != nil {
		output.ResultArtifact = rawArtifact(artifact)
	}
	if err != nil {
		return output, normalizeRuntimeAdapterError(err)
	}
	return output, nil
}

func (a *hostRuntimeAdapter) Collect(context.Context, adapterkit.RuntimeRequest) (adapterkit.RuntimePhaseOutput, error) {
	if strings.TrimSpace(a.artifactContent) == "" {
		return adapterkit.RuntimePhaseOutput{
			Evidence: []string{"result_artifact:missing"},
			Detail:   "adapter did not produce a result artifact",
		}, fmt.Errorf("adapter result artifact is missing")
	}
	return adapterkit.RuntimePhaseOutput{
		Evidence:       []string{"result_artifact:collected", "schema:" + a.resultSchema},
		Detail:         "runtime lifecycle collected the built-in adapter result artifact",
		ArtifactSchema: a.resultSchema,
		ResultArtifact: rawArtifact(a.artifactContent),
	}, nil
}

func (a *hostRuntimeAdapter) Cleanup(context.Context, adapterkit.RuntimeRequest) (adapterkit.RuntimePhaseOutput, error) {
	if a.releaseWorkspaceLock != nil {
		a.releaseWorkspaceLock()
	}
	return adapterkit.RuntimePhaseOutput{
		Evidence: []string{"workspace_lock:released"},
		Detail:   "hostrunner cleanup released the workspace lock through the runtime lifecycle",
	}, nil
}

func executeJobAdapterDirect(ctx context.Context, envelope taskEnvelope) (string, error) {
	switch envelope.Adapter {
	case "acpx":
		execution, err := acpxadapter.ExecuteContext(ctx, acpxadapter.Spec{
			WorkspaceRoot:             envelope.Workspace.Root,
			WriteScope:                envelope.Workspace.WriteScope,
			Prompt:                    stringValue(envelope.Payload, "prompt", envelope.Intent),
			AcpxCommand:               stringValue(envelope.Payload, "acpx_command", ""),
			AcpxAgent:                 stringValue(envelope.Payload, "acpx_agent", ""),
			AcpxArgs:                  stringSliceValue(envelope.Payload, "acpx_args"),
			VerificationCommands:      stringMatrixValue(envelope.Payload, "verification_commands"),
			AllowVerificationCommands: stringSliceValue(envelope.Payload, "allow_verification_commands"),
			MaxDurationSeconds:        envelope.Limits.MaxDurationSeconds,
			MaxOutputBytes:            envelope.Limits.MaxOutputBytes,
		})
		return execution.ArtifactContent(), err
	case "claude-code":
		execution, err := claudecodeadapter.ExecuteContext(ctx, claudecodeadapter.Spec{
			WorkspaceRoot:             envelope.Workspace.Root,
			WriteScope:                envelope.Workspace.WriteScope,
			Prompt:                    stringValue(envelope.Payload, "prompt", envelope.Intent),
			ClaudeCodeCommand:         stringValue(envelope.Payload, "claude_code_command", stringValue(envelope.Payload, "claude_command", "")),
			ClaudeCodeArgs:            stringSliceValue(envelope.Payload, "claude_code_args"),
			VerificationCommands:      stringMatrixValue(envelope.Payload, "verification_commands"),
			AllowVerificationCommands: stringSliceValue(envelope.Payload, "allow_verification_commands"),
			MaxDurationSeconds:        envelope.Limits.MaxDurationSeconds,
			MaxOutputBytes:            envelope.Limits.MaxOutputBytes,
		})
		return execution.ArtifactContent(), err
	case "codex":
		execution, err := codexadapter.ExecuteContext(ctx, codexadapter.Spec{
			WorkspaceRoot:             envelope.Workspace.Root,
			WriteScope:                envelope.Workspace.WriteScope,
			Prompt:                    stringValue(envelope.Payload, "prompt", envelope.Intent),
			CodexCommand:              stringValue(envelope.Payload, "codex_command", ""),
			CodexArgs:                 stringSliceValue(envelope.Payload, "codex_args"),
			VerificationCommands:      stringMatrixValue(envelope.Payload, "verification_commands"),
			AllowVerificationCommands: stringSliceValue(envelope.Payload, "allow_verification_commands"),
			MaxDurationSeconds:        envelope.Limits.MaxDurationSeconds,
			MaxOutputBytes:            envelope.Limits.MaxOutputBytes,
		})
		return execution.ArtifactContent(), err
	case "desktop":
		action := desktopadapter.NormalizeAction(stringValue(envelope.Payload, "action", ""))
		outputPath := stringValue(envelope.Payload, "output_path", "")
		if outputPath == "" && (action == "screen.screenshot" || action == "screen.record") {
			extension := "png"
			if action == "screen.record" {
				extension = "zip"
			}
			outputPath = ".rdev/desktop-artifacts/" + safeArtifactTaskID(envelope.TaskID) + "." + extension
		}
		execution, err := desktopadapter.ExecuteContext(ctx, desktopadapter.Spec{
			Action:             action,
			URL:                stringValue(envelope.Payload, "url", ""),
			App:                stringValue(envelope.Payload, "app", ""),
			WindowID:           stringValue(envelope.Payload, "window_id", ""),
			Title:              stringValue(envelope.Payload, "title", ""),
			Text:               stringValue(envelope.Payload, "text", ""),
			X:                  intValue(envelope.Payload, "x", 0),
			Y:                  intValue(envelope.Payload, "y", 0),
			Width:              intValue(envelope.Payload, "width", 0),
			Height:             intValue(envelope.Payload, "height", 0),
			Button:             stringValue(envelope.Payload, "button", ""),
			Frames:             intValue(envelope.Payload, "frames", 0),
			IntervalMillis:     intValue(envelope.Payload, "interval_millis", 0),
			MaxDurationSeconds: envelope.Limits.MaxDurationSeconds,
			MaxOutputBytes:     envelope.Limits.MaxOutputBytes,
			WorkspaceRoot:      envelope.Workspace.Root,
			OutputPath:         outputPath,
		})
		return execution.ArtifactContent(), err
	case "file":
		execution, err := fileadapter.ExecuteContext(ctx, fileadapter.Spec{
			WorkspaceRoot:      envelope.Workspace.Root,
			WriteScope:         envelope.Workspace.WriteScope,
			Action:             stringValue(envelope.Payload, "action", ""),
			Path:               stringValue(envelope.Payload, "path", ""),
			Content:            stringValue(envelope.Payload, "content", ""),
			Encoding:           stringValue(envelope.Payload, "encoding", ""),
			ExpectedBytes:      intValue(envelope.Payload, "expected_bytes", 0),
			ExpectedSHA256:     stringValue(envelope.Payload, "expected_sha256", ""),
			MaxBytes:           intValue(envelope.Payload, "max_bytes", envelope.Limits.MaxOutputBytes),
			Offset:             int64Value(envelope.Payload, "offset", 0),
			ChunkBytes:         intValue(envelope.Payload, "chunk_bytes", 0),
			MaxDurationSeconds: envelope.Limits.MaxDurationSeconds,
			MaxOutputBytes:     envelope.Limits.MaxOutputBytes,
		})
		return execution.ArtifactContent(), err
	case "powershell":
		execution, err := powershelladapter.ExecuteContext(ctx, powershelladapter.Spec{
			WorkspaceRoot:      envelope.Workspace.Root,
			WriteScope:         envelope.Workspace.WriteScope,
			Command:            stringValue(envelope.Payload, "command", stringValue(envelope.Payload, "script", "")),
			PowerShellCommand:  stringValue(envelope.Payload, "powershell_command", ""),
			AllowCommands:      stringSliceValue(envelope.Payload, "allow_commands"),
			MaxDurationSeconds: envelope.Limits.MaxDurationSeconds,
			MaxOutputBytes:     envelope.Limits.MaxOutputBytes,
		})
		return execution.ArtifactContent(), err
	default:
		execution, err := shelladapter.ExecuteContext(ctx, shelladapter.Spec{
			WorkspaceRoot:      envelope.Workspace.Root,
			WriteScope:         envelope.Workspace.WriteScope,
			Argv:               stringSliceValue(envelope.Payload, "argv"),
			AllowCommands:      stringSliceValue(envelope.Payload, "allow_commands"),
			MaxDurationSeconds: envelope.Limits.MaxDurationSeconds,
			MaxOutputBytes:     envelope.Limits.MaxOutputBytes,
		})
		return execution.ArtifactContent(), err
	}
}

func safeArtifactTaskID(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "desktop-artifact"
	}
	var b strings.Builder
	for _, r := range taskID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "desktop-artifact"
	}
	return b.String()
}

func adapterDenial(adapter string, err error) (denialSpec, bool) {
	switch adapter {
	case "acpx":
		return acpxDenial(err)
	case "claude-code":
		return claudeCodeDenial(err)
	case "codex":
		return codexDenial(err)
	case "powershell":
		return powershellDenial(err)
	default:
		return shellDenial(err)
	}
}

func resultSchemaForAdapter(adapter string) string {
	switch adapter {
	case "acpx":
		return acpxadapter.ResultSchemaVersion
	case "claude-code":
		return claudecodeadapter.ResultSchemaVersion
	case "codex":
		return codexadapter.ResultSchemaVersion
	case "desktop":
		return desktopadapter.ResultSchemaVersion
	case "file":
		return fileadapter.ResultSchemaVersion
	case "powershell":
		return powershelladapter.ResultSchemaVersion
	default:
		return shelladapter.ResultSchemaVersion
	}
}

func rawArtifact(content string) json.RawMessage {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func formatRawArtifact(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	content, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(content)
}

func normalizeRuntimeAdapterError(err error) error {
	if err == nil {
		return nil
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "canceled"):
		return errors.Join(context.Canceled, err)
	case strings.Contains(text, "timed out"):
		return errors.Join(context.DeadlineExceeded, err)
	default:
		return err
	}
}
