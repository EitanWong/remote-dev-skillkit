package hostrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/hostapproval"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostnonce"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

const (
	DenialSchemaVersion           = "rdev.host-denial.v1"
	ApprovalRequiredSchemaVersion = "rdev.approval-required.v1"
)

type Result struct {
	ArtifactContent       string `json:"artifact_content"`
	RuntimeFixtureContent string `json:"runtime_fixture_content,omitempty"`
}

type Options struct {
	IdentityFingerprint   string
	NonceStore            hostnonce.Store
	ApprovalStore         hostapproval.Store
	WorkspaceLockStore    string
	WorkspaceLockTTL      time.Duration
	CaptureRuntimeFixture bool
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
	return RunDevJobWithOptionsContext(context.Background(), hostID, trust, job, now, Options{})
}

func RunDevJobForIdentity(hostID, identityFingerprint string, trust model.TrustBundle, job model.Job, now time.Time) (Result, error) {
	return RunDevJobWithOptionsContext(context.Background(), hostID, trust, job, now, Options{IdentityFingerprint: identityFingerprint})
}

func RunDevJobWithOptions(hostID string, trust model.TrustBundle, job model.Job, now time.Time, opts Options) (Result, error) {
	return RunDevJobWithOptionsContext(context.Background(), hostID, trust, job, now, opts)
}

func RunDevJobWithOptionsContext(ctx context.Context, hostID string, trust model.TrustBundle, job model.Job, now time.Time, opts Options) (Result, error) {
	return runDevJob(ctx, hostID, trust, job, now, opts)
}

func RunDevJobWithTrustBundle(hostID string, trustBundle model.SignedTrustBundle, job model.Job, now time.Time) (Result, error) {
	return RunDevJobWithTrustBundleOptionsContext(context.Background(), hostID, trustBundle, job, now, Options{})
}

func RunDevJobWithTrustBundleForIdentity(hostID, identityFingerprint string, trustBundle model.SignedTrustBundle, job model.Job, now time.Time) (Result, error) {
	return RunDevJobWithTrustBundleOptionsContext(context.Background(), hostID, trustBundle, job, now, Options{IdentityFingerprint: identityFingerprint})
}

func RunDevJobWithTrustBundleOptions(hostID string, trustBundle model.SignedTrustBundle, job model.Job, now time.Time, opts Options) (Result, error) {
	return RunDevJobWithTrustBundleOptionsContext(context.Background(), hostID, trustBundle, job, now, opts)
}

func RunDevJobWithTrustBundleOptionsContext(ctx context.Context, hostID string, trustBundle model.SignedTrustBundle, job model.Job, now time.Time, opts Options) (Result, error) {
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
	return runDevJob(ctx, hostID, trust, job, now, opts)
}

func runDevJob(ctx context.Context, hostID string, trust model.TrustBundle, job model.Job, now time.Time, opts Options) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
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
	if implicitMissing := missingImplicitApprovals(envelope, approved); len(implicitMissing) > 0 {
		return requireApproval(job, implicitMissing, approved, tokenIDs)
	}
	if envelope.Adapter != "shell" && envelope.Adapter != "codex" && envelope.Adapter != "powershell" {
		return deny(job, denialSpec{
			Code:      "unsupported_adapter",
			Summary:   "Requested adapter is not supported by this host runner.",
			Detail:    fmt.Sprintf("Adapter %q is not available in the current host runner.", envelope.Adapter),
			Adapter:   envelope.Adapter,
			Retryable: true,
		}, fmt.Errorf("unsupported dev adapter %q", envelope.Adapter))
	}
	if envelope.Workspace.Root == "" {
		return deny(job, denialSpec{
			Code:      "workspace_required",
			Summary:   "Workspace root is required for adapter execution.",
			Detail:    "Host adapters only run inside an explicit workspace root.",
			Adapter:   envelope.Adapter,
			Retryable: true,
		}, fmt.Errorf("workspace root is required"))
	}
	if missing := missingAdapterCapability(envelope); missing != "" {
		return deny(job, denialSpec{
			Code:       "missing_capability",
			Summary:    fmt.Sprintf("Job is missing the %s capability.", missing),
			Detail:     fmt.Sprintf("The host requires %s before running the %s adapter.", missing, envelope.Adapter),
			Adapter:    envelope.Adapter,
			Capability: missing,
			Retryable:  true,
		}, fmt.Errorf("missing %s capability", missing))
	}
	releaseWorkspaceLock, err := acquireWorkspaceLock(hostID, envelope, opts, now)
	if err != nil {
		return deny(job, workspaceLockDenial(err), err)
	}
	releasedWorkspaceLock := false
	releaseWorkspaceLockOnce := func() {
		if releasedWorkspaceLock {
			return
		}
		releasedWorkspaceLock = true
		releaseWorkspaceLock()
	}
	defer releaseWorkspaceLockOnce()
	if opts.ApprovalStore != nil {
		if err := consumeApprovalTokens(opts.ApprovalStore, envelope.ApprovalTokens, now); err != nil {
			return deny(job, approvalTokenDenial(job, err), err)
		}
	}
	execution, err := executeJobAdapter(ctx, envelope, opts.CaptureRuntimeFixture, releaseWorkspaceLockOnce)
	result := Result{
		ArtifactContent:       execution.ArtifactContent,
		RuntimeFixtureContent: execution.RuntimeFixtureContent,
	}
	if err != nil {
		if denial, ok := adapterDenial(job, envelope.Adapter, err); ok {
			return deny(job, denial, err)
		}
		return result, err
	}
	return result, nil
}

func missingAdapterCapability(envelope model.JobEnvelope) string {
	switch envelope.Adapter {
	case "shell":
		if !hasCapability(envelope.Capabilities, "shell.user") {
			return "shell.user"
		}
	case "powershell":
		if !hasCapability(envelope.Capabilities, "powershell.user") {
			return "powershell.user"
		}
	case "codex":
		if !hasCapability(envelope.Capabilities, "codex.run") {
			return "codex.run"
		}
		if !hasCapability(envelope.Capabilities, "git.diff") {
			return "git.diff"
		}
	}
	return ""
}

func acquireWorkspaceLock(hostID string, envelope model.JobEnvelope, opts Options, now time.Time) (func(), error) {
	if strings.TrimSpace(opts.WorkspaceLockStore) == "" {
		return func() {}, nil
	}
	ttl := opts.WorkspaceLockTTL
	if ttl <= 0 {
		ttl = workspace.DefaultLockTTL
	}
	store := workspace.NewFileLockStore(opts.WorkspaceLockStore)
	lock, err := store.Acquire(workspace.LockOptions{
		RepoRoot:     envelope.Workspace.Root,
		HostID:       hostID,
		JobID:        envelope.JobID,
		WorktreePath: envelope.Workspace.Root,
		BaseRef:      "",
		Branch:       envelope.Workspace.Branch,
		OwnerAdapter: envelope.Adapter,
		TTL:          ttl,
	}, now)
	if err != nil {
		return nil, err
	}
	return func() {
		_, _, _ = store.Release(lock.RepoRoot, envelope.JobID, false)
	}, nil
}

func consumeApprovalTokens(store hostapproval.Store, tokens []model.ApprovalToken, now time.Time) error {
	for _, token := range tokens {
		if err := store.Consume(token, now); err != nil {
			return err
		}
	}
	return nil
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

func missingImplicitApprovals(envelope model.JobEnvelope, approved []string) []string {
	approvedSet := make(map[string]struct{}, len(approved))
	for _, value := range approved {
		value = strings.TrimSpace(value)
		if value != "" {
			approvedSet[value] = struct{}{}
		}
	}
	var missing []string
	for _, operation := range implicitRiskApprovals(envelope) {
		if _, ok := approvedSet[operation]; ok {
			continue
		}
		missing = append(missing, operation)
	}
	return missing
}

func implicitRiskApprovals(envelope model.JobEnvelope) []string {
	seen := map[string]struct{}{}
	var operations []string
	add := func(operation string) {
		operation = normalizeRiskOperation(operation)
		if operation == "" {
			return
		}
		if _, ok := seen[operation]; ok {
			return
		}
		seen[operation] = struct{}{}
		operations = append(operations, operation)
	}
	for _, key := range []string{"external_actions", "dangerous_actions", "approval_actions", "requested_approvals"} {
		for _, action := range stringSliceValue(envelope.Payload, key) {
			add(action)
		}
	}
	var textValues []string
	switch envelope.Adapter {
	case "codex":
		textValues = codexRiskText(envelope)
	case "shell", "powershell":
		textValues = commandRiskText(envelope)
	default:
		return operations
	}
	text := strings.ToLower(strings.Join(textValues, "\n"))
	for _, rule := range []struct {
		operation string
		needles   []string
	}{
		{
			operation: "git.push",
			needles:   []string{"git push", "push to origin", "push changes", "push branch"},
		},
		{
			operation: "git.merge",
			needles:   []string{"git merge", "merge into main", "merge into master", "merge branch"},
		},
		{
			operation: "deploy.run",
			needles:   []string{"deploy", "deployment", "vercel --prod", "wrangler deploy", "fly deploy", "railway up"},
		},
		{
			operation: "publish.run",
			needles:   []string{"npm publish", "cargo publish", "twine upload", "gh release create", "publish package", "publish release"},
		},
		{
			operation: "credential.change",
			needles:   []string{"rotate credential", "change credential", "update secret", "modify secret", "write api key", "gh auth", "aws configure", "security add-generic-password"},
		},
		{
			operation: "service.manage",
			needles:   []string{"systemctl", "launchctl", "install service", "restart service", "modify service", "set-service", "new-service", "start-service", "stop-service", "restart-service", "register-scheduledtask", "service restart", "service stop", "service start"},
		},
		{
			operation: "package.install",
			needles:   []string{"brew install", "apt install", "apt-get install", "dnf install", "yum install", "pacman -s", "choco install", "winget install", "scoop install", "pip install", "npm install -g", "cargo install", "package install"},
		},
		{
			operation: "elevation.request",
			needles:   []string{"sudo ", "sudo\t", "pkexec", "runas", "start-process", "-verb runas", "elevation", "administrator privilege"},
		},
		{
			operation: "gui.control",
			needles:   []string{"xdotool", "cliclick", "system events", "sendkeys", "setcursorpos", "gui control", "control gui"},
		},
		{
			operation: "elevation.request",
			needles:   []string{"set-executionpolicy"},
		},
	} {
		for _, needle := range rule.needles {
			if strings.Contains(text, needle) {
				add(rule.operation)
				break
			}
		}
	}
	return operations
}

func codexRiskText(envelope model.JobEnvelope) []string {
	values := []string{envelope.Intent, stringValue(envelope.Payload, "prompt", "")}
	values = append(values, stringSliceValue(envelope.Payload, "codex_args")...)
	for _, command := range stringMatrixValue(envelope.Payload, "verification_commands") {
		values = append(values, strings.Join(command, " "))
	}
	return values
}

func commandRiskText(envelope model.JobEnvelope) []string {
	values := []string{envelope.Intent}
	argv := stringSliceValue(envelope.Payload, "argv")
	if len(argv) > 0 {
		values = append(values, strings.Join(argv, " "))
	}
	values = append(values, stringValue(envelope.Payload, "command", ""))
	values = append(values, stringValue(envelope.Payload, "script", ""))
	values = append(values, stringValue(envelope.Payload, "powershell_command", ""))
	return values
}

func normalizeRiskOperation(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", ".")
	value = strings.ReplaceAll(value, "-", ".")
	switch value {
	case "git.push", "push", "git push":
		return "git.push"
	case "git.merge", "merge", "git merge":
		return "git.merge"
	case "deploy", "deployment", "deploy.run", "run.deploy":
		return "deploy.run"
	case "publish", "publish.run", "run.publish":
		return "publish.run"
	case "credential", "credentials", "credential.change", "credentials.change", "secret", "secret.change":
		return "credential.change"
	case "service", "service.manage", "manage.service", "service.change":
		return "service.manage"
	case "package", "package.install", "package.install.requiresapproval", "install.package":
		return "package.install"
	case "elevation", "elevation.request", "admin", "administrator", "sudo":
		return "elevation.request"
	case "gui", "gui.control", "gui.control.requiresapproval", "control.gui":
		return "gui.control"
	default:
		return value
	}
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

func stringMatrixValue(values map[string]any, key string) [][]string {
	value, ok := values[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case [][]string:
		result := make([][]string, 0, len(typed))
		for _, row := range typed {
			result = append(result, append([]string(nil), row...))
		}
		return result
	case []any:
		result := make([][]string, 0, len(typed))
		for _, item := range typed {
			switch row := item.(type) {
			case []string:
				result = append(result, append([]string(nil), row...))
			case []any:
				var values []string
				for _, cell := range row {
					if text, ok := cell.(string); ok && text != "" {
						values = append(values, text)
					}
				}
				if len(values) > 0 {
					result = append(result, values)
				}
			}
		}
		return result
	default:
		return nil
	}
}

func stringValue(values map[string]any, key, fallback string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return fallback
	}
	if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
		return text
	}
	return fallback
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

func workspaceLockDenial(err error) denialSpec {
	if errors.Is(err, workspace.ErrLocked) {
		return denialSpec{
			Code:      "workspace_locked",
			Summary:   "Workspace is already locked by another job.",
			Detail:    err.Error(),
			Retryable: true,
		}
	}
	return denialSpec{
		Code:      "workspace_invalid",
		Summary:   "Workspace lock could not be acquired.",
		Detail:    err.Error(),
		Retryable: true,
	}
}

func codexDenial(job model.Job, err error) (denialSpec, bool) {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "not allowlisted"):
		return denialSpec{
			Code:      "command_not_allowlisted",
			Summary:   "Codex verification command is not allowlisted.",
			Detail:    err.Error(),
			Adapter:   "codex",
			Retryable: true,
		}, true
	case strings.Contains(text, "escapes workspace root"):
		return denialSpec{
			Code:      "workspace_escape",
			Summary:   "Requested write scope escapes the workspace root.",
			Detail:    err.Error(),
			Adapter:   "codex",
			Retryable: true,
		}, true
	case strings.Contains(text, "prompt is required"):
		return denialSpec{
			Code:      "adapter_payload_invalid",
			Summary:   "Codex prompt is required.",
			Detail:    err.Error(),
			Adapter:   "codex",
			Retryable: true,
		}, true
	case strings.Contains(text, "path is required") || strings.Contains(text, "resolve path") || strings.Contains(text, "stat path") || strings.Contains(text, "path must be a directory"):
		return denialSpec{
			Code:      "workspace_invalid",
			Summary:   "Workspace root is invalid.",
			Detail:    err.Error(),
			Adapter:   "codex",
			Retryable: true,
		}, true
	default:
		return denialSpec{}, false
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

func powershellDenial(job model.Job, err error) (denialSpec, bool) {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "not allowlisted"):
		return denialSpec{
			Code:      "command_not_allowlisted",
			Summary:   "PowerShell executable is not allowlisted.",
			Detail:    err.Error(),
			Adapter:   "powershell",
			Retryable: true,
		}, true
	case strings.Contains(text, "escapes workspace root"):
		return denialSpec{
			Code:      "workspace_escape",
			Summary:   "Requested write scope escapes the workspace root.",
			Detail:    err.Error(),
			Adapter:   "powershell",
			Retryable: true,
		}, true
	case strings.Contains(text, "workspace root is required"):
		return denialSpec{
			Code:      "workspace_required",
			Summary:   "Workspace root is required for PowerShell execution.",
			Detail:    err.Error(),
			Adapter:   "powershell",
			Retryable: true,
		}, true
	case strings.Contains(text, "workspace root must"):
		return denialSpec{
			Code:      "workspace_invalid",
			Summary:   "Workspace root is invalid.",
			Detail:    err.Error(),
			Adapter:   "powershell",
			Retryable: true,
		}, true
	case strings.Contains(text, "powershell command is required") || strings.Contains(text, "powershell executable is required"):
		return denialSpec{
			Code:      "adapter_payload_invalid",
			Summary:   "PowerShell command payload is invalid.",
			Detail:    err.Error(),
			Adapter:   "powershell",
			Retryable: true,
		}, true
	default:
		return denialSpec{}, false
	}
}
