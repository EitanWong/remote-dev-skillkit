package hostrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/codexadapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/powershelladapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/shelladapter"
	"github.com/EitanWong/remote-dev-skillkit/pkg/adapterkit"
)

type adapterExecution struct {
	ArtifactContent       string
	RuntimeFixtureContent string
}

func executeJobAdapter(ctx context.Context, envelope model.JobEnvelope, captureRuntimeFixture bool, releaseWorkspaceLock func()) (adapterExecution, error) {
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
		JobID:         envelope.JobID,
		WorkspaceRoot: envelope.Workspace.Root,
		Intent:        envelope.Intent,
		Payload:       envelope.Payload,
		Limits: adapterkit.RuntimeLimits{
			MaxDurationSeconds: envelope.Limits.MaxDurationSeconds,
			MaxOutputBytes:     envelope.Limits.MaxOutputBytes,
		},
		Approvals: envelope.ApprovalsRequired,
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
	envelope             model.JobEnvelope
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
			fmt.Sprintf("approvals_required:%d", len(a.envelope.ApprovalsRequired)),
			"implicit_approval_preflight:passed",
		},
		Detail: "hostrunner completed explicit and implicit approval checks before adapter execution",
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

func executeJobAdapterDirect(ctx context.Context, envelope model.JobEnvelope) (string, error) {
	switch envelope.Adapter {
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

func adapterDenial(job model.Job, adapter string, err error) (denialSpec, bool) {
	switch adapter {
	case "codex":
		return codexDenial(job, err)
	case "powershell":
		return powershellDenial(job, err)
	default:
		return shellDenial(job, err)
	}
}

func resultSchemaForAdapter(adapter string) string {
	switch adapter {
	case "codex":
		return codexadapter.ResultSchemaVersion
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
