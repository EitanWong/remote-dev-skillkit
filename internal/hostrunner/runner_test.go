package hostrunner

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
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
	if _, err := RunDevJob("hst_other", gw.TrustBundle(), job, now); err == nil {
		t.Fatal("expected wrong host to fail")
	}
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
	if _, err := RunDevJob(host.ID, gw.TrustBundle(), job, now); err == nil {
		t.Fatal("expected missing workspace to fail")
	}
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
	if _, err := RunDevJob(host.ID, gw.TrustBundle(), job, now); err == nil {
		t.Fatal("expected non-allowlisted command to fail")
	}
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
	if _, err := RunDevJob(host.ID, gw.TrustBundle(), job, now); err == nil {
		t.Fatal("expected tampered envelope to fail")
	}
}

func activeHost(t *testing.T, gw *gateway.MemoryGateway) model.Host {
	t.Helper()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "host",
		OS:         "darwin",
		Arch:       "arm64",
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
