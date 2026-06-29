package hostrunner

import (
	"fmt"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/shelladapter"
)

type Result struct {
	ArtifactContent string `json:"artifact_content"`
}

func RunDevJob(hostID string, trust model.TrustBundle, job model.Job, now time.Time) (Result, error) {
	return runDevJob(hostID, trust, job, now)
}

func RunDevJobWithTrustBundle(hostID string, trustBundle model.SignedTrustBundle, job model.Job, now time.Time) (Result, error) {
	if job.Envelope == nil {
		return Result{}, fmt.Errorf("job envelope is required")
	}
	trust, err := trustBundle.ActiveTrustBundle(job.Envelope.SigningKeyID, now)
	if err != nil {
		return Result{}, err
	}
	return runDevJob(hostID, trust, job, now)
}

func runDevJob(hostID string, trust model.TrustBundle, job model.Job, now time.Time) (Result, error) {
	if job.Envelope == nil {
		return Result{}, fmt.Errorf("job envelope is required")
	}
	envelope := *job.Envelope
	if envelope.HostID != hostID || job.HostID != hostID {
		return Result{}, fmt.Errorf("job is not assigned to host")
	}
	if envelope.SigningKeyID != trust.SigningKeyID {
		return Result{}, fmt.Errorf("signing key mismatch")
	}
	publicKey, err := trust.Ed25519PublicKey()
	if err != nil {
		return Result{}, err
	}
	if err := envelope.VerifyForHost(publicKey, hostID, now); err != nil {
		return Result{}, err
	}
	if envelope.Adapter != "shell" {
		return Result{}, fmt.Errorf("unsupported dev adapter %q", envelope.Adapter)
	}
	if !hasCapability(envelope.Capabilities, "shell.user") {
		return Result{}, fmt.Errorf("missing shell.user capability")
	}
	if envelope.Workspace.Root == "" {
		return Result{}, fmt.Errorf("workspace root is required")
	}
	execution, err := shelladapter.Execute(shelladapter.Spec{
		WorkspaceRoot:      envelope.Workspace.Root,
		WriteScope:         envelope.Workspace.WriteScope,
		Argv:               stringSliceValue(envelope.Payload, "argv"),
		AllowCommands:      stringSliceValue(envelope.Payload, "allow_commands"),
		MaxDurationSeconds: envelope.Limits.MaxDurationSeconds,
		MaxOutputBytes:     envelope.Limits.MaxOutputBytes,
	})
	result := Result{ArtifactContent: execution.ArtifactContent()}
	if err != nil {
		return result, err
	}
	return result, nil
}

func hasCapability(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func stringSliceValue(values map[string]any, key string) []string {
	value, ok := values[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && text != "" {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}
