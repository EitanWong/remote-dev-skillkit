package hostrunner

import (
	"fmt"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

type Result struct {
	ArtifactContent string `json:"artifact_content"`
}

func RunDevJob(hostID string, trust model.TrustBundle, job model.Job, now time.Time) (Result, error) {
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
	return Result{
		ArtifactContent: fmt.Sprintf("dev host accepted job %s in workspace %s", job.ID, envelope.Workspace.Root),
	}, nil
}

func hasCapability(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
