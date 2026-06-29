package hostrunner

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/hostnonce"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/shelladapter"
)

const (
	DenialSchemaVersion           = "rdev.host-denial.v1"
	ApprovalRequiredSchemaVersion = "rdev.approval-required.v1"
)

type Result struct {
	ArtifactContent string `json:"artifact_content"`
}

type Options struct {
	IdentityFingerprint string
	NonceStore          hostnonce.Store
}

type ApprovalRequired struct {
	SchemaVersion     string   `json:"schema_version"`
	Code              string   `json:"code"`
	Summary           string   `json:"summary"`
	Detail            string   `json:"detail,omitempty"`
	JobID             string   `json:"job_id,omitempty"`
	HostID            string   `json:"host_id,omitempty"`
	Adapter           string   `json:"adapter,omitempty"`
	RequiredApprovals []string `json:"required_approvals"`
	ApprovedApprovals []string `json:"approved_approvals,omitempty"`
	ApprovalTokenIDs  []string `json:"approval_token_ids,omitempty"`
	Retryable         bool     `json:"retryable"`
}

type ApprovalRequiredError struct {
	Explanation ApprovalRequired
}

func (e ApprovalRequiredError) Error() string {
	if e.Explanation.Summary != "" {
		return e.Explanation.Summary
	}
	return e.Explanation.Code
}

type DenialExplanation struct {
	SchemaVersion string `json:"schema_version"`
	Code          string `json:"code"`
	Summary       string `json:"summary"`
	Detail        string `json:"detail,omitempty"`
	JobID         string `json:"job_id,omitempty"`
	HostID        string `json:"host_id,omitempty"`
	Adapter       string `json:"adapter,omitempty"`
	Capability    string `json:"capability,omitempty"`
	Retryable     bool   `json:"retryable"`
}

type DenialError struct {
	Explanation DenialExplanation
	Cause       error
}

func (e DenialError) Error() string {
	if e.Explanation.Summary != "" {
		return e.Explanation.Summary
	}
	return e.Explanation.Code
}

func (e DenialError) Unwrap() error {
	return e.Cause
}

func RunDevJob(hostID string, trust model.TrustBundle, job model.Job, now time.Time) (Result, error) {
	return runDevJob(hostID, trust, job, now, Options{})
}

func RunDevJobForIdentity(hostID, identityFingerprint string, trust model.TrustBundle, job model.Job, now time.Time) (Result, error) {
	return runDevJob(hostID, trust, job, now, Options{IdentityFingerprint: identityFingerprint})
}

func RunDevJobWithOptions(hostID string, trust model.TrustBundle, job model.Job, now time.Time, opts Options) (Result, error) {
	return runDevJob(hostID, trust, job, now, opts)
}

func RunDevJobWithTrustBundle(hostID string, trustBundle model.SignedTrustBundle, job model.Job, now time.Time) (Result, error) {
	return RunDevJobWithTrustBundleOptions(hostID, trustBundle, job, now, Options{})
}

func RunDevJobWithTrustBundleForIdentity(hostID, identityFingerprint string, trustBundle model.SignedTrustBundle, job model.Job, now time.Time) (Result, error) {
	return RunDevJobWithTrustBundleOptions(hostID, trustBundle, job, now, Options{IdentityFingerprint: identityFingerprint})
}

func RunDevJobWithTrustBundleOptions(hostID string, trustBundle model.SignedTrustBundle, job model.Job, now time.Time, opts Options) (Result, error) {
	if job.Envelope == nil {
		return deny(job, denialSpec{
			Code:      "job_envelope_required",
			Summary:   "Job envelope is required before host execution.",
			Detail:    "The host received a job without the signed executable envelope.",
			Retryable: false,
		}, fmt.Errorf("job envelope is required"))
	}
	trust, err := trustBundle.ActiveTrustBundle(job.Envelope.SigningKeyID, now)
	if err != nil {
		return deny(job, trustBundleDenial(job, err), err)
	}
	return runDevJob(hostID, trust, job, now, opts)
}

func runDevJob(hostID string, trust model.TrustBundle, job model.Job, now time.Time, opts Options) (Result, error) {
	if job.Envelope == nil {
		return deny(job, denialSpec{
			Code:      "job_envelope_required",
			Summary:   "Job envelope is required before host execution.",
			Detail:    "The host received a job without the signed executable envelope.",
			Retryable: false,
		}, fmt.Errorf("job envelope is required"))
	}
	envelope := *job.Envelope
	if envelope.HostID != hostID || job.HostID != hostID {
		return deny(job, denialSpec{
			Code:      "wrong_host",
			Summary:   "Job is not assigned to this host.",
			Detail:    "The job or signed envelope host id did not match the executing host.",
			Retryable: false,
		}, fmt.Errorf("job is not assigned to host"))
	}
	if opts.IdentityFingerprint != "" && envelope.HostIdentityFingerprint != opts.IdentityFingerprint {
		return deny(job, denialSpec{
			Code:      "host_identity_mismatch",
			Summary:   "Host identity fingerprint did not match the signed job envelope.",
			Detail:    "The local host identity is not the identity named by the gateway-signed job.",
			Retryable: false,
		}, fmt.Errorf("host identity fingerprint mismatch"))
	}
	if envelope.SigningKeyID != trust.SigningKeyID {
		return deny(job, denialSpec{
			Code:      "signing_key_mismatch",
			Summary:   "Job signing key did not match the trusted key.",
			Detail:    "The host refused to verify a job signed by a different key id than the provided trust bundle.",
			Retryable: false,
		}, fmt.Errorf("signing key mismatch"))
	}
	publicKey, err := trust.Ed25519PublicKey()
	if err != nil {
		return deny(job, denialSpec{
			Code:      "trust_public_key_invalid",
			Summary:   "Trusted gateway public key is invalid.",
			Detail:    err.Error(),
			Retryable: false,
		}, err)
	}
	if err := envelope.VerifyForHost(publicKey, hostID, now); err != nil {
		return deny(job, envelopeDenial(job, err), err)
	}
	if opts.NonceStore != nil {
		if err := opts.NonceStore.Remember(hostnonce.Entry{
			JobID:     envelope.JobID,
			HostID:    envelope.HostID,
			Nonce:     envelope.Nonce,
			ExpiresAt: envelope.ExpiresAt,
		}, now); err != nil {
			return deny(job, nonceDenial(job, err), err)
		}
	}
	approved, tokenIDs, missing, err := verifyApprovalTokens(trust, envelope, now)
	if err != nil {
		return deny(job, approvalTokenDenial(job, err), err)
	}
	if len(missing) > 0 {
		return requireApproval(job, missing, approved, tokenIDs)
	}
	if envelope.Adapter != "shell" {
		return deny(job, denialSpec{
			Code:      "unsupported_adapter",
			Summary:   "Requested adapter is not supported by this host runner.",
			Detail:    fmt.Sprintf("Adapter %q is not available in the current host runner.", envelope.Adapter),
			Adapter:   envelope.Adapter,
			Retryable: true,
		}, fmt.Errorf("unsupported dev adapter %q", envelope.Adapter))
	}
	if !hasCapability(envelope.Capabilities, "shell.user") {
		return deny(job, denialSpec{
			Code:       "missing_capability",
			Summary:    "Job is missing the shell.user capability.",
			Detail:     "The host requires shell.user before running the shell adapter.",
			Adapter:    envelope.Adapter,
			Capability: "shell.user",
			Retryable:  true,
		}, fmt.Errorf("missing shell.user capability"))
	}
	if envelope.Workspace.Root == "" {
		return deny(job, denialSpec{
			Code:      "workspace_required",
			Summary:   "Workspace root is required for shell execution.",
			Detail:    "The shell adapter only runs inside an explicit workspace root.",
			Adapter:   envelope.Adapter,
			Retryable: true,
		}, fmt.Errorf("workspace root is required"))
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
		if denial, ok := shellDenial(job, err); ok {
			return deny(job, denial, err)
		}
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

func verifyApprovalTokens(trust model.TrustBundle, envelope model.JobEnvelope, now time.Time) ([]string, []string, []string, error) {
	approved := make([]string, 0, len(envelope.ApprovalTokens))
	tokenIDs := make([]string, 0, len(envelope.ApprovalTokens))
	approvedSet := make(map[string]struct{}, len(envelope.ApprovalTokens))
	for _, token := range envelope.ApprovalTokens {
		if err := token.Verify(trust, envelope.JobID, envelope.HostID, token.Operation, now); err != nil {
			return nil, nil, nil, err
		}
		if strings.TrimSpace(token.Operation) != "" {
			if _, exists := approvedSet[token.Operation]; !exists {
				approved = append(approved, token.Operation)
				approvedSet[token.Operation] = struct{}{}
			}
		}
		if strings.TrimSpace(token.TokenID) != "" {
			tokenIDs = append(tokenIDs, token.TokenID)
		}
	}
	var missing []string
	for _, value := range envelope.ApprovalsRequired {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := approvedSet[value]; !ok {
			missing = append(missing, value)
		}
	}
	return approved, tokenIDs, missing, nil
}

func requireApproval(job model.Job, missing, approved, tokenIDs []string) (Result, error) {
	explanation := ApprovalRequired{
		SchemaVersion:     ApprovalRequiredSchemaVersion,
		Code:              "approval_required",
		Summary:           "Job requires approval before host execution.",
		Detail:            "The signed job envelope lists required approvals that have not been satisfied.",
		JobID:             job.ID,
		HostID:            job.HostID,
		RequiredApprovals: append([]string(nil), missing...),
		ApprovedApprovals: append([]string(nil), approved...),
		ApprovalTokenIDs:  append([]string(nil), tokenIDs...),
		Retryable:         true,
	}
	if job.Envelope != nil {
		if explanation.JobID == "" {
			explanation.JobID = job.Envelope.JobID
		}
		if explanation.HostID == "" {
			explanation.HostID = job.Envelope.HostID
		}
		explanation.Adapter = job.Envelope.Adapter
	}
	content, _ := json.MarshalIndent(explanation, "", "  ")
	return Result{ArtifactContent: string(content)}, ApprovalRequiredError{Explanation: explanation}
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

type denialSpec struct {
	Code       string
	Summary    string
	Detail     string
	Adapter    string
	Capability string
	Retryable  bool
}

func deny(job model.Job, spec denialSpec, cause error) (Result, error) {
	explanation := DenialExplanation{
		SchemaVersion: DenialSchemaVersion,
		Code:          spec.Code,
		Summary:       spec.Summary,
		Detail:        spec.Detail,
		JobID:         job.ID,
		HostID:        job.HostID,
		Adapter:       spec.Adapter,
		Capability:    spec.Capability,
		Retryable:     spec.Retryable,
	}
	if job.Envelope != nil {
		if explanation.JobID == "" {
			explanation.JobID = job.Envelope.JobID
		}
		if explanation.HostID == "" {
			explanation.HostID = job.Envelope.HostID
		}
		if explanation.Adapter == "" {
			explanation.Adapter = job.Envelope.Adapter
		}
	}
	if explanation.Detail == "" && cause != nil {
		explanation.Detail = cause.Error()
	}
	content, _ := json.MarshalIndent(explanation, "", "  ")
	return Result{ArtifactContent: string(content)}, DenialError{
		Explanation: explanation,
		Cause:       cause,
	}
}

func envelopeDenial(job model.Job, err error) denialSpec {
	switch {
	case errors.Is(err, model.ErrEnvelopeExpired):
		return denialSpec{
			Code:      "envelope_expired",
			Summary:   "Job envelope has expired.",
			Detail:    err.Error(),
			Retryable: true,
		}
	case errors.Is(err, model.ErrEnvelopeSignature):
		return denialSpec{
			Code:      "envelope_signature_invalid",
			Summary:   "Job envelope signature is invalid.",
			Detail:    err.Error(),
			Retryable: false,
		}
	case errors.Is(err, model.ErrEnvelopeInvalid):
		return denialSpec{
			Code:      "envelope_invalid",
			Summary:   "Job envelope is invalid.",
			Detail:    err.Error(),
			Retryable: true,
		}
	default:
		return denialSpec{
			Code:      "envelope_invalid",
			Summary:   "Job envelope failed host verification.",
			Detail:    err.Error(),
			Retryable: false,
		}
	}
}

func trustBundleDenial(job model.Job, err error) denialSpec {
	code := "trust_bundle_invalid"
	summary := "Signed trust bundle could not authorize the job signing key."
	if errors.Is(err, model.ErrTrustKeyRevoked) {
		code = "trust_bundle_revoked"
		summary = "Job signing key has been revoked."
	}
	return denialSpec{
		Code:      code,
		Summary:   summary,
		Detail:    err.Error(),
		Retryable: false,
	}
}

func nonceDenial(job model.Job, err error) denialSpec {
	code := "nonce_replay"
	summary := "Job envelope nonce was already used."
	if !strings.Contains(strings.ToLower(err.Error()), "replay") {
		code = "nonce_store_error"
		summary = "Host nonce replay store rejected the job."
	}
	return denialSpec{
		Code:      code,
		Summary:   summary,
		Detail:    err.Error(),
		Retryable: false,
	}
}

func approvalTokenDenial(job model.Job, err error) denialSpec {
	switch {
	case errors.Is(err, model.ErrApprovalTokenExpired):
		return denialSpec{
			Code:      "approval_token_expired",
			Summary:   "Approval token has expired.",
			Detail:    err.Error(),
			Retryable: true,
		}
	case errors.Is(err, model.ErrApprovalTokenConsumed):
		return denialSpec{
			Code:      "approval_token_consumed",
			Summary:   "Approval token has already been consumed.",
			Detail:    err.Error(),
			Retryable: true,
		}
	case errors.Is(err, model.ErrApprovalTokenSignature):
		return denialSpec{
			Code:      "approval_token_signature_invalid",
			Summary:   "Approval token signature is invalid.",
			Detail:    err.Error(),
			Retryable: false,
		}
	case errors.Is(err, model.ErrApprovalTokenInvalid):
		return denialSpec{
			Code:      "approval_token_invalid",
			Summary:   "Approval token is invalid for this job.",
			Detail:    err.Error(),
			Retryable: true,
		}
	default:
		return denialSpec{
			Code:      "approval_token_invalid",
			Summary:   "Approval token failed host verification.",
			Detail:    err.Error(),
			Retryable: false,
		}
	}
}

func shellDenial(job model.Job, err error) (denialSpec, bool) {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "not allowlisted"):
		return denialSpec{
			Code:      "command_not_allowlisted",
			Summary:   "Shell command is not allowlisted.",
			Detail:    err.Error(),
			Adapter:   "shell",
			Retryable: true,
		}, true
	case strings.Contains(text, "escapes workspace root"):
		return denialSpec{
			Code:      "workspace_escape",
			Summary:   "Requested write scope escapes the workspace root.",
			Detail:    err.Error(),
			Adapter:   "shell",
			Retryable: true,
		}, true
	case strings.Contains(text, "workspace root is required"):
		return denialSpec{
			Code:      "workspace_required",
			Summary:   "Workspace root is required for shell execution.",
			Detail:    err.Error(),
			Adapter:   "shell",
			Retryable: true,
		}, true
	case strings.Contains(text, "workspace root must"):
		return denialSpec{
			Code:      "workspace_invalid",
			Summary:   "Workspace root is invalid.",
			Detail:    err.Error(),
			Adapter:   "shell",
			Retryable: true,
		}, true
	default:
		return denialSpec{}, false
	}
}
