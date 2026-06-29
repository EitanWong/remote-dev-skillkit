package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/audit"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestMemoryGatewayDemoFlow(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })

	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "win-temp-01",
		OS:         "windows",
		Arch:       "amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if host.Status != model.HostStatusPending {
		t.Fatalf("host should start pending, got %s", host.Status)
	}
	host, err = gw.ApproveHost(host.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host.Status != model.HostStatusActive {
		t.Fatalf("host should be active, got %s", host.Status)
	}
	job, err := gw.CreateJob(host.ID, "powershell", "diagnose node", map[string]any{"cwd": "%USERPROFILE%"})
	if err != nil {
		t.Fatal(err)
	}
	job, artifact, err := gw.CompleteJob(job.ID, "demo complete")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != model.JobStatusSucceeded {
		t.Fatalf("job should succeed, got %s", job.Status)
	}
	if job.Envelope == nil {
		t.Fatal("job should include a signed envelope")
	}
	if artifact.Content != "demo complete" {
		t.Fatalf("unexpected artifact content %q", artifact.Content)
	}
	if got := len(gw.AuditEvents()); got != 5 {
		t.Fatalf("expected 5 audit events, got %d", got)
	}
}

func TestMemoryGatewaySignsJobEnvelope(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })

	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "codex", "fix tests", map[string]any{
		"operator_id":          "eitan",
		"workspace_root":       "/repo",
		"write_scope":          []any{"/repo"},
		"branch":               "rdev/job",
		"capabilities":         []any{"fs.read", "fs.write.scoped", "dev.codex"},
		"approvals_required":   []any{"git.push"},
		"max_duration_seconds": 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Envelope == nil {
		t.Fatal("job envelope must be present")
	}
	if err := gw.VerifyJobEnvelope(*job.Envelope, host.ID); err != nil {
		t.Fatalf("expected gateway-signed envelope to verify: %v", err)
	}
	if job.Envelope.OperatorID != "eitan" {
		t.Fatalf("expected operator_id from policy, got %q", job.Envelope.OperatorID)
	}
	if job.Envelope.Workspace.Root != "/repo" {
		t.Fatalf("unexpected workspace root %q", job.Envelope.Workspace.Root)
	}
	if got := job.Envelope.Limits.MaxDurationSeconds; got != 300 {
		t.Fatalf("expected max duration 300, got %d", got)
	}
}

func TestMemoryGatewayUsesProvidedSigningKey(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "provided-key", publicKey, privateKey)

	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Envelope == nil {
		t.Fatal("job envelope must be present")
	}
	if job.Envelope.SigningKeyID != "provided-key" {
		t.Fatalf("expected provided signing key id, got %q", job.Envelope.SigningKeyID)
	}
	if err := job.Envelope.VerifyForHost(publicKey, host.ID, now); err != nil {
		t.Fatalf("expected envelope to verify with provided public key: %v", err)
	}
}

func TestMemoryGatewayApproveJobSignsApprovalToken(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root":     ".",
		"capabilities":       []string{"shell.user"},
		"argv":               []string{"go", "env", "GOOS"},
		"allow_commands":     []string{"go"},
		"approvals_required": []string{"git.push"},
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := gw.ApproveJob(job.ID, "git.push", "approved", "operator approved push")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Envelope == nil {
		t.Fatal("approved job envelope must be present")
	}
	if len(approved.Envelope.ApprovalTokens) != 1 {
		t.Fatalf("expected one approval token, got %#v", approved.Envelope.ApprovalTokens)
	}
	token := approved.Envelope.ApprovalTokens[0]
	if token.Operation != "git.push" || token.ApprovalID != "git.push" {
		t.Fatalf("unexpected approval token scope: %#v", token)
	}
	if token.Signature == "" {
		t.Fatal("approval token signature must be set")
	}
	if err := token.Verify(gw.TrustBundle(), approved.ID, host.ID, "git.push", now); err != nil {
		t.Fatalf("expected approval token to verify: %v", err)
	}
	if err := gw.VerifyJobEnvelope(*approved.Envelope, host.ID); err != nil {
		t.Fatalf("expected re-signed approved envelope to verify: %v", err)
	}
}

func TestMemoryGatewayCreatesSignedJoinManifest(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := gw.JoinManifest(ticket.Code, "http://127.0.0.1:8787", "http://127.0.0.1:8787/join/"+ticket.Code)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.TicketCode != ticket.Code {
		t.Fatalf("expected ticket code %q, got %q", ticket.Code, manifest.TicketCode)
	}
	if err := manifest.Verify(now); err != nil {
		t.Fatalf("expected manifest to verify: %v", err)
	}
}

func TestMemoryGatewayPreservesHostIdentityAndBindsEnvelope(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := hostIdentityFingerprint(publicKey)
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "win-temp-01",
		OS:                  "windows",
		Arch:                "amd64",
		IdentityKeyID:       "host-test",
		IdentityPublicKey:   encodeHostIdentityPublicKey(publicKey),
		IdentityFingerprint: fingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	if host.IdentityFingerprint != fingerprint {
		t.Fatalf("expected host identity fingerprint %q, got %q", fingerprint, host.IdentityFingerprint)
	}
	host, err = gw.ApproveHost(host.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Envelope == nil {
		t.Fatal("job envelope must be present")
	}
	if job.Envelope.HostIdentityFingerprint != fingerprint {
		t.Fatalf("expected envelope fingerprint %q, got %q", fingerprint, job.Envelope.HostIdentityFingerprint)
	}
}

func TestMemoryGatewayRejectsHostIdentityFingerprintMismatch(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	_, err = gw.RegisterHost(model.HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "win-temp-01",
		OS:                  "windows",
		Arch:                "amd64",
		IdentityKeyID:       "host-test",
		IdentityPublicKey:   encodeHostIdentityPublicKey(publicKey),
		IdentityFingerprint: "sha256:wrong",
	})
	if err == nil {
		t.Fatal("expected identity fingerprint mismatch")
	}
}

func TestMemoryGatewayCreatesJoinManifestWithSeparateRoot(t *testing.T) {
	gatewayPublicKey, gatewayPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifestPublicKey, manifestPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-jobs", gatewayPublicKey, gatewayPrivateKey).
		WithManifestSigningKey("manifest-root", manifestPublicKey, manifestPrivateKey)
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := gw.JoinManifest(ticket.Code, "http://127.0.0.1:8787", "http://127.0.0.1:8787/join/"+ticket.Code)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SigningKeyID != "manifest-root" {
		t.Fatalf("expected manifest signing root, got %q", manifest.SigningKeyID)
	}
	if manifest.Trust.SigningKeyID != "gateway-jobs" {
		t.Fatalf("expected embedded gateway job trust, got %q", manifest.Trust.SigningKeyID)
	}
	if err := manifest.VerifyWithRoot(model.NewTrustBundle("manifest-root", manifestPublicKey), now); err != nil {
		t.Fatalf("expected manifest to verify with separate root: %v", err)
	}
	if err := manifest.Verify(now); !errors.Is(err, model.ErrJoinManifestInvalid) {
		t.Fatalf("expected dev self-trust verify to reject separate root, got %v", err)
	}
}

func TestMemoryGatewayRejectsJobAfterTicketExpiry(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	current := now
	gw := NewMemoryGatewayWithClock(func() time.Time { return current })

	host := activeHost(t, gw)
	current = now.Add(601 * time.Second)
	_, err := gw.CreateJob(host.ID, "powershell", "diagnose", nil)
	if !errors.Is(err, ErrTicketExpired) {
		t.Fatalf("expected ticket expired error, got %v", err)
	}
}

func TestMemoryGatewayFailsJobForHost(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })

	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = gw.FailJobForHost(host.ID, job.ID, "signature rejected")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != model.JobStatusFailed {
		t.Fatalf("expected failed job, got %s", job.Status)
	}
	if job.FailureReason != "signature rejected" {
		t.Fatalf("unexpected failure reason %q", job.FailureReason)
	}
	events := gw.AuditEvents()
	if events[len(events)-1].Action != "job.fail" {
		t.Fatalf("expected job.fail audit event, got %s", events[len(events)-1].Action)
	}
}

func TestMemoryGatewayRejectsFailJobForWrongHost(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })

	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.FailJobForHost("hst_other", job.ID, "nope"); err == nil {
		t.Fatal("expected wrong host failure report to fail")
	}
}

func TestMemoryGatewayRejectsJobForPendingHost(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })

	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "win-temp-01",
		OS:         "windows",
		Arch:       "amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateJob(host.ID, "powershell", "diagnose node", nil); err == nil {
		t.Fatal("expected pending host job creation to fail")
	}
}

func TestMemoryGatewayRevokeHostCancelsQueuedAndRunningJobs(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	queued, err := gw.CreateJob(host.ID, "shell", "queued", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	running, err := gw.CreateJob(host.ID, "shell", "running", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := gw.NextJobForHost(host.ID); err != nil || !ok {
		t.Fatalf("expected running job claim, ok=%v err=%v", ok, err)
	}
	if _, err := gw.RevokeHost(host.ID, "operator stop"); err != nil {
		t.Fatal(err)
	}
	queued, err = gw.Job(queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != model.JobStatusCanceled {
		t.Fatalf("expected queued job canceled, got %s", queued.Status)
	}
	running, err = gw.Job(running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != model.JobStatusCanceled {
		t.Fatalf("expected running job canceled, got %s", running.Status)
	}
	if _, ok, err := gw.NextJobForHost(host.ID); err == nil || ok {
		t.Fatalf("expected revoked host to be unable to claim jobs, ok=%v err=%v", ok, err)
	}
	events := gw.AuditEvents()
	if !hasAuditAction(events, "host.revoke") {
		t.Fatalf("expected host.revoke audit event: %#v", events)
	}
	if got := countAuditAction(events, "job.cancel"); got != 2 {
		t.Fatalf("expected 2 job.cancel audit events, got %d: %#v", got, events)
	}
}

func TestMemoryGatewayRevokeTicketPreventsRegistration(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })

	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.RevokeTicket(ticket.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.RegisterHost(model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "win-temp-01",
		OS:         "windows",
		Arch:       "amd64",
	}); err == nil {
		t.Fatal("expected revoked ticket registration to fail")
	}
}

func TestMemoryGatewayWritesAuditSink(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	store := audit.NewJSONLStore(path)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now }).WithAuditSink(&store)

	if _, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) == 0 {
		t.Fatal("expected audit file to contain an event")
	}
}

func countAuditAction(events []model.AuditEvent, action string) int {
	count := 0
	for _, event := range events {
		if event.Action == action {
			count++
		}
	}
	return count
}

func hasAuditAction(events []model.AuditEvent, action string) bool {
	return countAuditAction(events, action) > 0
}

func encodeHostIdentityPublicKey(publicKey ed25519.PublicKey) string {
	return base64.RawURLEncoding.EncodeToString(publicKey)
}

func hostIdentityFingerprint(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func activeHost(t *testing.T, gw *MemoryGateway) model.Host {
	t.Helper()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "win-temp-01",
		OS:         "windows",
		Arch:       "amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	return host
}
