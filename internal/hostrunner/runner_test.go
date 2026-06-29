package hostrunner

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostnonce"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestRunDevJobAcceptsScopedShellJob(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.ArtifactContent == "" {
		t.Fatal("artifact content must be set")
	}
	if !strings.Contains(result.ArtifactContent, `"exit_code": 0`) {
		t.Fatalf("expected successful command evidence, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobWithTrustBundleUsesActiveSigningKey(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey := hostrunnerTestKeyPair(t)
	gw := gateway.NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-dev", publicKey, privateKey)
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJobWithTrustBundle(host.ID, gw.SignedTrustBundle(), job, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ArtifactContent, `"exit_code": 0`) {
		t.Fatalf("expected successful command evidence, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobWithTrustBundleRejectsRevokedSigningKey(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey := hostrunnerTestKeyPair(t)
	gw := gateway.NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-dev", publicKey, privateKey)
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	revokedAt := now.Add(time.Minute)
	revokedKey := model.NewTrustKey("gateway-dev", publicKey, model.TrustKeyStatusRevoked, now)
	revokedKey.RevokedAt = &revokedAt
	revokedBundle := signedHostrunnerTrustBundle(t, model.SignedTrustBundleSpec{
		BundleID:     "dev-gateway",
		Sequence:     1,
		NotBefore:    now,
		NotAfter:     now.Add(time.Hour),
		SigningKeyID: "gateway-dev",
		Keys:         []model.TrustKey{revokedKey},
	}, privateKey, now)
	if _, err := RunDevJobWithTrustBundle(host.ID, revokedBundle, job, now.Add(2*time.Minute)); !errors.Is(err, model.ErrTrustKeyRevoked) {
		t.Fatalf("expected revoked key error, got %v", err)
	}
}

func TestRunDevJobRedactsShellArtifact(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	workspace := t.TempDir()
	secret := "sk-" + "testsecret12345678901234567890"
	source := `package main

import "fmt"

func main() {
	fmt.Println("token=` + secret + `")
}
`
	if err := os.WriteFile(filepath.Join(workspace, "printsecret.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "redaction demo", map[string]any{
		"workspace_root":        workspace,
		"capabilities":          []string{"shell.user"},
		"argv":                  []string{"go", "run", "./printsecret.go"},
		"allow_commands":        []string{"go"},
		"max_duration_seconds":  30,
		"max_output_bytes":      4096,
		"approvals_required":    []string{},
		"network_access_policy": "default-deny",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.ArtifactContent, secret) {
		t.Fatalf("artifact leaked secret: %s", result.ArtifactContent)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.shell-result.v1"`) {
		t.Fatalf("expected shell result schema, got %s", result.ArtifactContent)
	}
	if !strings.Contains(result.ArtifactContent, "[REDACTED:openai_api_key]") {
		t.Fatalf("expected redaction marker, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobRequiresApprovalBeforeExecution(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root":       ".",
		"capabilities":         []string{"shell.user"},
		"argv":                 []string{"go", "env", "GOOS"},
		"allow_commands":       []string{"go"},
		"approvals_required":   []string{"pkg.install"},
		"max_output_bytes":     4096,
		"max_duration_seconds": 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertApprovalRequired(t, result, err, "pkg.install")
	if strings.Contains(result.ArtifactContent, `"schema_version": "rdev.shell-result.v1"`) {
		t.Fatalf("approval-required job must not execute shell adapter, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobExecutesAfterApprovalSatisfied(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root":       ".",
		"capabilities":         []string{"shell.user"},
		"argv":                 []string{"go", "env", "GOOS"},
		"allow_commands":       []string{"go"},
		"approvals_required":   []string{"pkg.install"},
		"max_output_bytes":     4096,
		"max_duration_seconds": 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = gw.ApproveJob(job.ID, "pkg.install", "approved", "test approval")
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.shell-result.v1"`) {
		t.Fatalf("expected shell result after approval satisfied, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobRejectsWrongHost(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob("hst_other", gw.TrustBundle(), job, now)
	assertDenial(t, result, err, "wrong_host")
}

func TestRunDevJobRejectsWrongHostIdentityFingerprint(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJobForIdentity(host.ID, "sha256:wrong", gw.TrustBundle(), job, now)
	assertDenial(t, result, err, "host_identity_mismatch")
}

func TestRunDevJobAcceptsMatchingHostIdentityFingerprint(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Envelope == nil || job.Envelope.HostIdentityFingerprint == "" {
		t.Fatal("expected envelope to include host identity fingerprint")
	}
	if _, err := RunDevJobForIdentity(host.ID, host.IdentityFingerprint, gw.TrustBundle(), job, now); err != nil {
		t.Fatalf("expected matching identity to execute: %v", err)
	}
}

func TestRunDevJobRejectsReplayedNonce(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := hostnonce.NewMemoryStore()
	opts := Options{
		IdentityFingerprint: host.IdentityFingerprint,
		NonceStore:          store,
	}
	if _, err := RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, opts); err != nil {
		t.Fatalf("expected first execution to pass: %v", err)
	}
	result, err := RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, opts)
	assertDenial(t, result, err, "nonce_replay")
}

func TestRunDevJobRejectsMissingWorkspace(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"capabilities": []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertDenial(t, result, err, "workspace_required")
}

func TestRunDevJobRejectsCommandNotAllowlisted(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"git"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertDenial(t, result, err, "command_not_allowlisted")
}

func TestRunDevJobRejectsSymlinkWriteScopeEscape(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	workspace := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(workspace, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation is not available: %v", err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": workspace,
		"write_scope":    []string{filepath.Join(link, "missing-child")},
		"capabilities":   []string{"shell.user", "fs.write.scoped"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertDenial(t, result, err, "workspace_escape")
}

func TestRunDevJobRejectsTamperedEnvelope(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	job.Envelope.Intent = "tampered"
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertDenial(t, result, err, "envelope_signature_invalid")
}

func TestRunDevJobRejectsUnsupportedAdapterWithDenial(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "codex", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertDenial(t, result, err, "unsupported_adapter")
}

func TestRunDevJobRejectsMissingCapabilityWithDenial(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"fs.read.scoped"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertDenial(t, result, err, "missing_capability")
}

func TestRunDevJobRejectsExpiredEnvelopeWithDenial(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now.Add(2*time.Hour))
	assertDenial(t, result, err, "envelope_expired")
}

func TestRunDevJobRequiresEnvelopeWithDenial(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	result, err := RunDevJob("hst_1", model.TrustBundle{}, model.Job{ID: "job_1", HostID: "hst_1"}, now)
	assertDenial(t, result, err, "job_envelope_required")
}

func activeHost(t *testing.T, gw *gateway.MemoryGateway) model.Host {
	t.Helper()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "host",
		OS:                  "darwin",
		Arch:                "arm64",
		IdentityKeyID:       "host-test",
		IdentityPublicKey:   encodeHostIdentityPublicKey(publicKey),
		IdentityFingerprint: hostIdentityFingerprint(publicKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, []string{"shell.user"})
	if err != nil {
		t.Fatal(err)
	}
	return host
}

func encodeHostIdentityPublicKey(publicKey ed25519.PublicKey) string {
	return base64.RawURLEncoding.EncodeToString(publicKey)
}

func hostIdentityFingerprint(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func signedHostrunnerTrustBundle(t *testing.T, spec model.SignedTrustBundleSpec, privateKey ed25519.PrivateKey, now time.Time) model.SignedTrustBundle {
	t.Helper()
	bundle, err := model.NewSignedTrustBundle(spec, now)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err = bundle.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func hostrunnerTestKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return publicKey, privateKey
}

func assertDenial(t *testing.T, result Result, err error, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected denial %q, got nil error", wantCode)
	}
	var denial DenialError
	if !errors.As(err, &denial) {
		t.Fatalf("expected DenialError, got %T %v", err, err)
	}
	if denial.Explanation.SchemaVersion != DenialSchemaVersion {
		t.Fatalf("expected denial schema %q, got %q", DenialSchemaVersion, denial.Explanation.SchemaVersion)
	}
	if denial.Explanation.Code != wantCode {
		t.Fatalf("expected denial code %q, got %q", wantCode, denial.Explanation.Code)
	}
	if denial.Explanation.Summary == "" {
		t.Fatal("denial summary must be set")
	}
	if result.ArtifactContent == "" {
		t.Fatal("denial artifact content must be set")
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "`+DenialSchemaVersion+`"`) {
		t.Fatalf("expected denial artifact schema, got %s", result.ArtifactContent)
	}
	if !strings.Contains(result.ArtifactContent, `"code": "`+wantCode+`"`) {
		t.Fatalf("expected denial artifact code %q, got %s", wantCode, result.ArtifactContent)
	}
}

func assertApprovalRequired(t *testing.T, result Result, err error, wantApproval string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected approval requirement %q, got nil error", wantApproval)
	}
	var approval ApprovalRequiredError
	if !errors.As(err, &approval) {
		t.Fatalf("expected ApprovalRequiredError, got %T %v", err, err)
	}
	if approval.Explanation.SchemaVersion != ApprovalRequiredSchemaVersion {
		t.Fatalf("expected approval schema %q, got %q", ApprovalRequiredSchemaVersion, approval.Explanation.SchemaVersion)
	}
	if approval.Explanation.Code != "approval_required" {
		t.Fatalf("expected approval_required code, got %q", approval.Explanation.Code)
	}
	if !containsString(approval.Explanation.RequiredApprovals, wantApproval) {
		t.Fatalf("expected approval %q, got %#v", wantApproval, approval.Explanation.RequiredApprovals)
	}
	if result.ArtifactContent == "" {
		t.Fatal("approval-required artifact content must be set")
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "`+ApprovalRequiredSchemaVersion+`"`) {
		t.Fatalf("expected approval artifact schema, got %s", result.ArtifactContent)
	}
	if !strings.Contains(result.ArtifactContent, wantApproval) {
		t.Fatalf("expected approval artifact to mention %q, got %s", wantApproval, result.ArtifactContent)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
