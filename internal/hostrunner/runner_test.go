package hostrunner

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostapproval"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostnonce"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
	"github.com/EitanWong/remote-dev-skillkit/pkg/adapterkit"
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

func TestRunDevJobRejectsTamperedApprovalToken(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey := hostrunnerTestKeyPair(t)
	gw := gateway.NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-dev", publicKey, privateKey)
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root":       ".",
		"capabilities":         []string{"shell.user"},
		"argv":                 []string{"go", "env", "GOOS"},
		"allow_commands":       []string{"go"},
		"approvals_required":   []string{"git.push"},
		"max_output_bytes":     4096,
		"max_duration_seconds": 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = gw.ApproveJob(job.ID, "git.push", "approved", "test approval")
	if err != nil {
		t.Fatal(err)
	}
	if job.Envelope == nil || len(job.Envelope.ApprovalTokens) != 1 {
		t.Fatalf("expected one approval token, got %#v", job.Envelope)
	}
	envelope := *job.Envelope
	envelope.ApprovalTokens[0].Operation = "deploy.run"
	envelope.Signature = ""
	envelope, err = envelope.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	job.Envelope = &envelope
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertDenial(t, result, err, "approval_token_signature_invalid")
}

func TestRunDevJobRejectsConsumedApprovalToken(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root":       ".",
		"capabilities":         []string{"shell.user"},
		"argv":                 []string{"go", "env", "GOOS"},
		"allow_commands":       []string{"go"},
		"approvals_required":   []string{"git.push"},
		"max_output_bytes":     4096,
		"max_duration_seconds": 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = gw.ApproveJob(job.ID, "git.push", "approved", "test approval")
	if err != nil {
		t.Fatal(err)
	}
	store := hostapproval.NewMemoryStore()
	opts := Options{ApprovalStore: store}
	if _, err := RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, opts); err != nil {
		t.Fatalf("expected first approved execution to pass: %v", err)
	}
	result, err := RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, opts)
	assertDenial(t, result, err, "approval_token_consumed")
}

func TestRunDevJobAcquiresAndReleasesWorkspaceLock(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := t.TempDir()
	lockStore := filepath.Join(t.TempDir(), "locks")
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": repo,
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, Options{
		WorkspaceLockStore: lockStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ArtifactContent, `"exit_code": 0`) {
		t.Fatalf("expected successful shell artifact, got %s", result.ArtifactContent)
	}
	status, err := workspace.NewFileLockStore(lockStore).Status(repo, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if status.Exists {
		t.Fatalf("expected workspace lock release after execution, got %#v", status)
	}
}

func TestRunDevJobCapturesShellRuntimeFixture(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := t.TempDir()
	lockStore := filepath.Join(t.TempDir(), "locks")
	job, err := gw.CreateJob(host.ID, "shell", "capture shell runtime fixture", map[string]any{
		"workspace_root":       repo,
		"capabilities":         []string{"shell.user"},
		"argv":                 []string{"go", "env", "GOOS"},
		"allow_commands":       []string{"go"},
		"max_duration_seconds": 30,
		"max_output_bytes":     4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, Options{
		WorkspaceLockStore:    lockStore,
		CaptureRuntimeFixture: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.shell-result.v1"`) {
		t.Fatalf("expected primary shell artifact, got %s", result.ArtifactContent)
	}
	if !strings.Contains(result.RuntimeFixtureContent, `"schema_version": "rdev.adapter-runtime-fixture.v1"`) {
		t.Fatalf("expected runtime fixture, got %s", result.RuntimeFixtureContent)
	}
	report := adapterkit.VerifyRuntimeFixtureJSON([]byte(result.RuntimeFixtureContent), adapterkit.RuntimeFixtureContract{
		Adapter:               "shell",
		RequireSuccessful:     true,
		RequireCleanup:        true,
		RequireResultArtifact: true,
	})
	if !report.OK {
		t.Fatalf("shell runtime fixture failed conformance: %#v\n%s", report, result.RuntimeFixtureContent)
	}
	status, err := workspace.NewFileLockStore(lockStore).Status(repo, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if status.Exists {
		t.Fatalf("expected runtime cleanup to release workspace lock, got %#v", status)
	}
}

func TestRunDevJobRejectsLockedWorkspace(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := t.TempDir()
	lockStore := filepath.Join(t.TempDir(), "locks")
	if _, err := workspace.NewFileLockStore(lockStore).Acquire(workspace.LockOptions{
		RepoRoot:     repo,
		HostID:       host.ID,
		JobID:        "job_existing",
		OwnerAdapter: "codex",
		TTL:          time.Hour,
	}, now); err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": repo,
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, Options{
		WorkspaceLockStore: lockStore,
	})
	assertDenial(t, result, err, "workspace_locked")
}

func TestRunDevJobDoesNotConsumeApprovalTokenWhenWorkspaceLocked(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := t.TempDir()
	lockStore := filepath.Join(t.TempDir(), "locks")
	store := workspace.NewFileLockStore(lockStore)
	if _, err := store.Acquire(workspace.LockOptions{
		RepoRoot:     repo,
		HostID:       host.ID,
		JobID:        "job_existing",
		OwnerAdapter: "codex",
		TTL:          time.Hour,
	}, now); err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root":       repo,
		"capabilities":         []string{"shell.user"},
		"argv":                 []string{"go", "env", "GOOS"},
		"allow_commands":       []string{"go"},
		"approvals_required":   []string{"git.push"},
		"max_output_bytes":     4096,
		"max_duration_seconds": 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = gw.ApproveJob(job.ID, "git.push", "approved", "test approval")
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{
		WorkspaceLockStore: lockStore,
		ApprovalStore:      hostapproval.NewMemoryStore(),
	}
	result, err := RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, opts)
	assertDenial(t, result, err, "workspace_locked")
	if _, _, err := store.Release(repo, "job_existing", false); err != nil {
		t.Fatal(err)
	}
	result, err = RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, opts)
	if err != nil {
		t.Fatalf("expected approval token to remain usable after workspace lock denial: %v", err)
	}
	if !strings.Contains(result.ArtifactContent, `"exit_code": 0`) {
		t.Fatalf("expected successful shell artifact, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobReleasesWorkspaceLockAfterAdapterDenial(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := t.TempDir()
	lockStore := filepath.Join(t.TempDir(), "locks")
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": repo,
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"git"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, Options{
		WorkspaceLockStore: lockStore,
	})
	assertDenial(t, result, err, "command_not_allowlisted")
	status, err := workspace.NewFileLockStore(lockStore).Status(repo, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if status.Exists {
		t.Fatalf("expected workspace lock release after adapter denial, got %#v", status)
	}
}

func TestRunDevJobExecutesCodexAdapterWithWorkspaceLock(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := initHostrunnerGitRepo(t)
	fakeCodex := writeHostrunnerFakeCodex(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	if err := os.WriteFile("README.md", []byte("# demo\n\nchanged by hostrunner codex\n"), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("fake codex wrote README")
}
`)
	lockStore := filepath.Join(t.TempDir(), "locks")
	job, err := gw.CreateJob(host.ID, "codex", "update README", map[string]any{
		"workspace_root":              repo,
		"capabilities":                []string{"codex.run", "git.diff"},
		"prompt":                      "update README",
		"codex_command":               "go",
		"codex_args":                  []string{"run", fakeCodex},
		"verification_commands":       [][]string{{"git", "status", "--short"}},
		"allow_verification_commands": []string{"git"},
		"max_duration_seconds":        30,
		"max_output_bytes":            64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, Options{
		WorkspaceLockStore: lockStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.codex-result.v1"`) {
		t.Fatalf("expected codex result artifact, got %s", result.ArtifactContent)
	}
	if !strings.Contains(result.ArtifactContent, "changed by hostrunner codex") {
		t.Fatalf("expected diff evidence in codex artifact, got %s", result.ArtifactContent)
	}
	status, err := workspace.NewFileLockStore(lockStore).Status(repo, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if status.Exists {
		t.Fatalf("expected workspace lock release after codex execution, got %#v", status)
	}
}

func TestRunDevJobCancelsCodexWhenContextCanceled(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := initHostrunnerGitRepo(t)
	fakeCodex := buildHostrunnerFakeCodexBinary(t, `package main

import "time"

func main() {
	time.Sleep(5 * time.Second)
}
`)
	job, err := gw.CreateJob(host.ID, "codex", "sleep until canceled", map[string]any{
		"workspace_root":       repo,
		"capabilities":         []string{"codex.run", "git.diff"},
		"prompt":               "sleep until canceled",
		"codex_command":        fakeCodex,
		"max_duration_seconds": 30,
		"max_output_bytes":     64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	result, err := RunDevJobWithOptionsContext(ctx, host.ID, gw.TrustBundle(), job, now, Options{})
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.codex-result.v1"`) {
		t.Fatalf("expected codex artifact, got %s", result.ArtifactContent)
	}
	if !strings.Contains(result.ArtifactContent, `"canceled": true`) {
		t.Fatalf("expected canceled evidence, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobCancelsShellWhenContextCanceled(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := t.TempDir()
	sleeper := buildHostrunnerFakeCodexBinary(t, `package main

import "time"

func main() {
	time.Sleep(5 * time.Second)
}
`)
	job, err := gw.CreateJob(host.ID, "shell", "sleep until canceled", map[string]any{
		"workspace_root":       repo,
		"capabilities":         []string{"shell.user"},
		"argv":                 []string{sleeper},
		"allow_commands":       []string{sleeper},
		"max_duration_seconds": 30,
		"max_output_bytes":     64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	result, err := RunDevJobWithOptionsContext(ctx, host.ID, gw.TrustBundle(), job, now, Options{})
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.shell-result.v1"`) {
		t.Fatalf("expected shell artifact, got %s", result.ArtifactContent)
	}
	if !strings.Contains(result.ArtifactContent, `"canceled": true`) {
		t.Fatalf("expected canceled evidence, got %s", result.ArtifactContent)
	}
	if strings.Contains(result.ArtifactContent, `"timed_out": true`) {
		t.Fatalf("canceled shell job should not be marked timed out, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobCapturesCanceledShellRuntimeFixture(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := t.TempDir()
	lockStore := filepath.Join(t.TempDir(), "locks")
	sleeper := buildHostrunnerFakeCodexBinary(t, `package main

import "time"

func main() {
	time.Sleep(5 * time.Second)
}
`)
	job, err := gw.CreateJob(host.ID, "shell", "sleep until canceled with runtime fixture", map[string]any{
		"workspace_root":       repo,
		"capabilities":         []string{"shell.user"},
		"argv":                 []string{sleeper},
		"allow_commands":       []string{sleeper},
		"max_duration_seconds": 30,
		"max_output_bytes":     64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	result, err := RunDevJobWithOptionsContext(ctx, host.ID, gw.TrustBundle(), job, now, Options{
		WorkspaceLockStore:    lockStore,
		CaptureRuntimeFixture: true,
	})
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	if !strings.Contains(result.ArtifactContent, `"canceled": true`) {
		t.Fatalf("expected primary canceled artifact, got %s", result.ArtifactContent)
	}
	report := adapterkit.VerifyRuntimeFixtureJSON([]byte(result.RuntimeFixtureContent), adapterkit.RuntimeFixtureContract{
		Adapter:               "shell",
		RequiredPhases:        []string{adapterkit.PhaseDetect, adapterkit.PhasePlan, adapterkit.PhasePrepare, adapterkit.PhaseRun, adapterkit.PhaseCleanup},
		RequireCleanup:        true,
		RequireResultArtifact: true,
		RequireCancellation:   true,
	})
	if !report.OK {
		t.Fatalf("canceled shell runtime fixture failed conformance: %#v\n%s", report, result.RuntimeFixtureContent)
	}
	status, err := workspace.NewFileLockStore(lockStore).Status(repo, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if status.Exists {
		t.Fatalf("expected runtime cleanup after cancel to release workspace lock, got %#v", status)
	}
}

func TestRunDevJobExecutesPowerShellAdapterWithWorkspaceLock(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := t.TempDir()
	fakePowerShell := buildHostrunnerFakeCodexBinary(t, `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	fmt.Println(strings.Join(os.Args[1:], "|"))
	if err := os.WriteFile("ran.txt", []byte("powershell adapter ran"), 0o644); err != nil {
		panic(err)
	}
}
`)
	lockStore := filepath.Join(t.TempDir(), "locks")
	job, err := gw.CreateJob(host.ID, "powershell", "run scoped PowerShell command", map[string]any{
		"workspace_root":       repo,
		"capabilities":         []string{"powershell.user"},
		"command":              `Write-Output "hello"`,
		"powershell_command":   fakePowerShell,
		"allow_commands":       []string{fakePowerShell},
		"max_duration_seconds": 30,
		"max_output_bytes":     64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, Options{
		WorkspaceLockStore: lockStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.powershell-result.v1"`) {
		t.Fatalf("expected PowerShell result artifact, got %s", result.ArtifactContent)
	}
	if !strings.Contains(result.ArtifactContent, `"-NoProfile"`) || !strings.Contains(result.ArtifactContent, `"-NonInteractive"`) {
		t.Fatalf("expected constrained PowerShell argv evidence, got %s", result.ArtifactContent)
	}
	if strings.Contains(result.ArtifactContent, "ExecutionPolicy") {
		t.Fatalf("PowerShell adapter must not weaken execution policy, got %s", result.ArtifactContent)
	}
	content, err := os.ReadFile(filepath.Join(repo, "ran.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "powershell adapter ran" {
		t.Fatalf("unexpected marker content %q", string(content))
	}
	status, err := workspace.NewFileLockStore(lockStore).Status(repo, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if status.Exists {
		t.Fatalf("expected workspace lock release after PowerShell execution, got %#v", status)
	}
}

func TestRunDevJobCapturesPowerShellRuntimeFixture(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := t.TempDir()
	fakePowerShell := buildHostrunnerFakeCodexBinary(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("fake powershell runtime")
	if err := os.WriteFile("powershell-runtime.txt", []byte("runtime fixture"), 0o644); err != nil {
		panic(err)
	}
}
`)
	job, err := gw.CreateJob(host.ID, "powershell", "capture PowerShell runtime fixture", map[string]any{
		"workspace_root":       repo,
		"capabilities":         []string{"powershell.user"},
		"command":              `Write-Output "runtime"`,
		"powershell_command":   fakePowerShell,
		"allow_commands":       []string{fakePowerShell},
		"max_duration_seconds": 30,
		"max_output_bytes":     64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, Options{
		WorkspaceLockStore:    filepath.Join(t.TempDir(), "locks"),
		CaptureRuntimeFixture: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.powershell-result.v1"`) {
		t.Fatalf("expected primary PowerShell artifact, got %s", result.ArtifactContent)
	}
	report := adapterkit.VerifyRuntimeFixtureJSON([]byte(result.RuntimeFixtureContent), adapterkit.RuntimeFixtureContract{
		Adapter:               "powershell",
		RequireSuccessful:     true,
		RequireCleanup:        true,
		RequireResultArtifact: true,
	})
	if !report.OK {
		t.Fatalf("PowerShell runtime fixture failed conformance: %#v\n%s", report, result.RuntimeFixtureContent)
	}
	if !strings.Contains(result.RuntimeFixtureContent, `"result_artifact_schema": "rdev.powershell-result.v1"`) {
		t.Fatalf("expected PowerShell result schema in runtime fixture, got %s", result.RuntimeFixtureContent)
	}
}

func TestRunDevJobRejectsPowerShellWithoutCapability(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "powershell", "demo", map[string]any{
		"workspace_root":       t.TempDir(),
		"capabilities":         []string{"shell.user"},
		"command":              "Get-ChildItem",
		"powershell_command":   "pwsh",
		"allow_commands":       []string{"pwsh"},
		"max_duration_seconds": 30,
		"max_output_bytes":     1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertDenial(t, result, err, "missing_capability")
	if !strings.Contains(result.ArtifactContent, `"capability": "powershell.user"`) {
		t.Fatalf("expected missing powershell.user capability, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobRequiresApprovalForPowerShellRiskyCommand(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := t.TempDir()
	marker := filepath.Join(repo, "ran.txt")
	fakePowerShell := buildHostrunnerFakeCodexBinary(t, `package main

import "os"

func main() {
	if err := os.WriteFile("ran.txt", []byte("ran without approval"), 0o644); err != nil {
		panic(err)
	}
}
`)
	job, err := gw.CreateJob(host.ID, "powershell", "install service", map[string]any{
		"workspace_root":       repo,
		"capabilities":         []string{"powershell.user"},
		"command":              `New-Service -Name rdev -BinaryPathName C:\rdev.exe`,
		"powershell_command":   fakePowerShell,
		"allow_commands":       []string{fakePowerShell},
		"max_duration_seconds": 30,
		"max_output_bytes":     1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertApprovalRequired(t, result, err, "service.manage")
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("PowerShell command executed before service.manage approval; marker stat err=%v", statErr)
	}
}

func TestRunDevJobRequiresApprovalForPowerShellExecutionPolicyChange(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "powershell", "change execution policy", map[string]any{
		"workspace_root":       t.TempDir(),
		"capabilities":         []string{"powershell.user"},
		"command":              "Set-ExecutionPolicy Bypass -Scope CurrentUser",
		"powershell_command":   "pwsh",
		"allow_commands":       []string{"pwsh"},
		"max_duration_seconds": 30,
		"max_output_bytes":     1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertApprovalRequired(t, result, err, "elevation.request")
	if strings.Contains(result.ArtifactContent, `"schema_version": "rdev.powershell-result.v1"`) {
		t.Fatalf("PowerShell execution-policy command must not run before approval, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobCancelsPowerShellWhenContextCanceled(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	fakePowerShell := buildHostrunnerFakeCodexBinary(t, `package main

import "time"

func main() {
	time.Sleep(5 * time.Second)
}
`)
	job, err := gw.CreateJob(host.ID, "powershell", "sleep until canceled", map[string]any{
		"workspace_root":       t.TempDir(),
		"capabilities":         []string{"powershell.user"},
		"command":              "Start-Sleep -Seconds 5",
		"powershell_command":   fakePowerShell,
		"allow_commands":       []string{fakePowerShell},
		"max_duration_seconds": 30,
		"max_output_bytes":     64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	result, err := RunDevJobWithOptionsContext(ctx, host.ID, gw.TrustBundle(), job, now, Options{})
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.powershell-result.v1"`) {
		t.Fatalf("expected PowerShell artifact, got %s", result.ArtifactContent)
	}
	if !strings.Contains(result.ArtifactContent, `"canceled": true`) {
		t.Fatalf("expected canceled evidence, got %s", result.ArtifactContent)
	}
	if strings.Contains(result.ArtifactContent, `"timed_out": true`) {
		t.Fatalf("canceled PowerShell job should not be marked timed out, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobRequiresApprovalForCodexGitPushIntent(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := initHostrunnerGitRepo(t)
	fakeCodex := writeHostrunnerFakeCodex(t, `package main

import "os"

func main() {
	if err := os.WriteFile("README.md", []byte("# demo\n\nthis must not run without approval\n"), 0o644); err != nil {
		panic(err)
	}
}
`)
	job, err := gw.CreateJob(host.ID, "codex", "update README and git push to origin", map[string]any{
		"workspace_root": repo,
		"capabilities":   []string{"codex.run", "git.diff"},
		"prompt":         "Update README, then run git push to origin.",
		"codex_command":  "go",
		"codex_args":     []string{"run", fakeCodex},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertApprovalRequired(t, result, err, "git.push")
	content, readErr := os.ReadFile(filepath.Join(repo, "README.md"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(content), "this must not run without approval") {
		t.Fatalf("codex adapter executed before git.push approval: %s", string(content))
	}
}

func TestRunDevJobCapturesCodexRuntimeFixture(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := initHostrunnerGitRepo(t)
	fakeCodex := writeHostrunnerFakeCodex(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	if err := os.WriteFile("README.md", []byte("# demo\n\nruntime fixture codex\n"), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("fake codex wrote README")
}
`)
	job, err := gw.CreateJob(host.ID, "codex", "capture codex runtime fixture", map[string]any{
		"workspace_root":              repo,
		"capabilities":                []string{"codex.run", "git.diff"},
		"prompt":                      "update README",
		"codex_command":               "go",
		"codex_args":                  []string{"run", fakeCodex},
		"verification_commands":       [][]string{{"git", "status", "--short"}},
		"allow_verification_commands": []string{"git"},
		"max_duration_seconds":        30,
		"max_output_bytes":            64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, Options{
		WorkspaceLockStore:    filepath.Join(t.TempDir(), "locks"),
		CaptureRuntimeFixture: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.codex-result.v1"`) {
		t.Fatalf("expected primary codex artifact, got %s", result.ArtifactContent)
	}
	report := adapterkit.VerifyRuntimeFixtureJSON([]byte(result.RuntimeFixtureContent), adapterkit.RuntimeFixtureContract{
		Adapter:               "codex",
		RequireSuccessful:     true,
		RequireCleanup:        true,
		RequireResultArtifact: true,
	})
	if !report.OK {
		t.Fatalf("codex runtime fixture failed conformance: %#v\n%s", report, result.RuntimeFixtureContent)
	}
	if !strings.Contains(result.RuntimeFixtureContent, `"result_artifact_schema": "rdev.codex-result.v1"`) {
		t.Fatalf("expected codex result schema in runtime fixture, got %s", result.RuntimeFixtureContent)
	}
}

func TestRunDevJobExecutesClaudeCodeAdapterWithWorkspaceLock(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := initHostrunnerGitRepo(t)
	fakeClaude := writeHostrunnerFakeCodex(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	if err := os.WriteFile("README.md", []byte("# demo\n\nchanged by hostrunner claude code\n"), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("fake claude code wrote README")
}
`)
	lockStore := filepath.Join(t.TempDir(), "locks")
	job, err := gw.CreateJob(host.ID, "claude-code", "update README", map[string]any{
		"workspace_root":              repo,
		"capabilities":                []string{"claude-code.run", "git.diff"},
		"prompt":                      "update README",
		"claude_code_command":         "go",
		"claude_code_args":            []string{"run", fakeClaude},
		"verification_commands":       [][]string{{"git", "status", "--short"}},
		"allow_verification_commands": []string{"git"},
		"max_duration_seconds":        30,
		"max_output_bytes":            64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, Options{
		WorkspaceLockStore: lockStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.claude-code-result.v1"`) {
		t.Fatalf("expected Claude Code result artifact, got %s", result.ArtifactContent)
	}
	if !strings.Contains(result.ArtifactContent, "changed by hostrunner claude code") {
		t.Fatalf("expected diff evidence in Claude Code artifact, got %s", result.ArtifactContent)
	}
	status, err := workspace.NewFileLockStore(lockStore).Status(repo, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if status.Exists {
		t.Fatalf("expected workspace lock release after Claude Code execution, got %#v", status)
	}
}

func TestRunDevJobExecutesAcpxAdapterWithWorkspaceLock(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := initHostrunnerGitRepo(t)
	fakeAcpx := writeHostrunnerFakeCodex(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	if err := os.WriteFile("README.md", []byte("# demo\n\nchanged by hostrunner acpx\n"), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("fake acpx wrote README")
}
`)
	lockStore := filepath.Join(t.TempDir(), "locks")
	job, err := gw.CreateJob(host.ID, "acpx", "update README", map[string]any{
		"workspace_root":              repo,
		"capabilities":                []string{"acpx.run", "git.diff"},
		"prompt":                      "update README",
		"acpx_command":                "go",
		"acpx_args":                   []string{"run", fakeAcpx},
		"verification_commands":       [][]string{{"git", "status", "--short"}},
		"allow_verification_commands": []string{"git"},
		"max_duration_seconds":        30,
		"max_output_bytes":            64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, Options{
		WorkspaceLockStore: lockStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.acpx-result.v1"`) {
		t.Fatalf("expected acpx result artifact, got %s", result.ArtifactContent)
	}
	if !strings.Contains(result.ArtifactContent, "changed by hostrunner acpx") {
		t.Fatalf("expected diff evidence in acpx artifact, got %s", result.ArtifactContent)
	}
	status, err := workspace.NewFileLockStore(lockStore).Status(repo, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if status.Exists {
		t.Fatalf("expected workspace lock release after acpx execution, got %#v", status)
	}
}

func TestRunDevJobRejectsAcpxWithoutCapability(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "acpx", "demo", map[string]any{
		"workspace_root": t.TempDir(),
		"capabilities":   []string{"git.diff"},
		"prompt":         "demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertDenial(t, result, err, "missing_capability")
	if !strings.Contains(result.ArtifactContent, `"capability": "acpx.run"`) {
		t.Fatalf("expected missing acpx.run capability, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobRequiresApprovalForAcpxGitPushIntent(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := initHostrunnerGitRepo(t)
	fakeAcpx := writeHostrunnerFakeCodex(t, `package main

import "os"

func main() {
	if err := os.WriteFile("README.md", []byte("# demo\n\nthis acpx must not run without approval\n"), 0o644); err != nil {
		panic(err)
	}
}
`)
	job, err := gw.CreateJob(host.ID, "acpx", "update README and git push to origin", map[string]any{
		"workspace_root": repo,
		"capabilities":   []string{"acpx.run", "git.diff"},
		"prompt":         "Update README, then run git push to origin.",
		"acpx_command":   "go",
		"acpx_args":      []string{"run", fakeAcpx},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertApprovalRequired(t, result, err, "git.push")
	content, readErr := os.ReadFile(filepath.Join(repo, "README.md"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(content), "this acpx must not run without approval") {
		t.Fatalf("acpx adapter executed before git.push approval: %s", string(content))
	}
}

func TestRunDevJobCapturesAcpxRuntimeFixture(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := initHostrunnerGitRepo(t)
	fakeAcpx := writeHostrunnerFakeCodex(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	if err := os.WriteFile("README.md", []byte("# demo\n\nruntime fixture acpx\n"), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("fake acpx wrote README")
}
`)
	job, err := gw.CreateJob(host.ID, "acpx", "capture acpx runtime fixture", map[string]any{
		"workspace_root":              repo,
		"capabilities":                []string{"acpx.run", "git.diff"},
		"prompt":                      "update README",
		"acpx_command":                "go",
		"acpx_args":                   []string{"run", fakeAcpx},
		"verification_commands":       [][]string{{"git", "status", "--short"}},
		"allow_verification_commands": []string{"git"},
		"max_duration_seconds":        30,
		"max_output_bytes":            64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, Options{
		WorkspaceLockStore:    filepath.Join(t.TempDir(), "locks"),
		CaptureRuntimeFixture: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.acpx-result.v1"`) {
		t.Fatalf("expected primary acpx artifact, got %s", result.ArtifactContent)
	}
	report := adapterkit.VerifyRuntimeFixtureJSON([]byte(result.RuntimeFixtureContent), adapterkit.RuntimeFixtureContract{
		Adapter:               "acpx",
		RequireSuccessful:     true,
		RequireCleanup:        true,
		RequireResultArtifact: true,
	})
	if !report.OK {
		t.Fatalf("acpx runtime fixture failed conformance: %#v\n%s", report, result.RuntimeFixtureContent)
	}
	if !strings.Contains(result.RuntimeFixtureContent, `"result_artifact_schema": "rdev.acpx-result.v1"`) {
		t.Fatalf("expected acpx result schema in runtime fixture, got %s", result.RuntimeFixtureContent)
	}
}

func TestRunDevJobCancelsAcpxWhenContextCanceled(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := initHostrunnerGitRepo(t)
	fakeAcpx := buildHostrunnerFakeCodexBinary(t, `package main

import "time"

func main() {
	time.Sleep(5 * time.Second)
}
`)
	job, err := gw.CreateJob(host.ID, "acpx", "sleep until canceled", map[string]any{
		"workspace_root":       repo,
		"capabilities":         []string{"acpx.run", "git.diff"},
		"prompt":               "sleep until canceled",
		"acpx_command":         fakeAcpx,
		"max_duration_seconds": 30,
		"max_output_bytes":     64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	result, err := RunDevJobWithOptionsContext(ctx, host.ID, gw.TrustBundle(), job, now, Options{})
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.acpx-result.v1"`) {
		t.Fatalf("expected acpx artifact, got %s", result.ArtifactContent)
	}
	if !strings.Contains(result.ArtifactContent, `"canceled": true`) {
		t.Fatalf("expected canceled evidence, got %s", result.ArtifactContent)
	}
	if strings.Contains(result.ArtifactContent, `"timed_out": true`) {
		t.Fatalf("canceled acpx job should not be marked timed out, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobRejectsClaudeCodeWithoutCapability(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "claude-code", "demo", map[string]any{
		"workspace_root": t.TempDir(),
		"capabilities":   []string{"git.diff"},
		"prompt":         "demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertDenial(t, result, err, "missing_capability")
	if !strings.Contains(result.ArtifactContent, `"capability": "claude-code.run"`) {
		t.Fatalf("expected missing claude-code.run capability, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobRequiresApprovalForClaudeCodeGitPushIntent(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := initHostrunnerGitRepo(t)
	fakeClaude := writeHostrunnerFakeCodex(t, `package main

import "os"

func main() {
	if err := os.WriteFile("README.md", []byte("# demo\n\nthis must not run without approval\n"), 0o644); err != nil {
		panic(err)
	}
}
`)
	job, err := gw.CreateJob(host.ID, "claude-code", "update README and git push to origin", map[string]any{
		"workspace_root":      repo,
		"capabilities":        []string{"claude-code.run", "git.diff"},
		"prompt":              "Update README, then run git push to origin.",
		"claude_code_command": "go",
		"claude_code_args":    []string{"run", fakeClaude},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertApprovalRequired(t, result, err, "git.push")
	content, readErr := os.ReadFile(filepath.Join(repo, "README.md"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(content), "this must not run without approval") {
		t.Fatalf("Claude Code adapter executed before git.push approval: %s", string(content))
	}
}

func TestRunDevJobCapturesClaudeCodeRuntimeFixture(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := initHostrunnerGitRepo(t)
	fakeClaude := writeHostrunnerFakeCodex(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	if err := os.WriteFile("README.md", []byte("# demo\n\nruntime fixture claude code\n"), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("fake claude code wrote README")
}
`)
	job, err := gw.CreateJob(host.ID, "claude-code", "capture Claude Code runtime fixture", map[string]any{
		"workspace_root":              repo,
		"capabilities":                []string{"claude-code.run", "git.diff"},
		"prompt":                      "update README",
		"claude_code_command":         "go",
		"claude_code_args":            []string{"run", fakeClaude},
		"verification_commands":       [][]string{{"git", "status", "--short"}},
		"allow_verification_commands": []string{"git"},
		"max_duration_seconds":        30,
		"max_output_bytes":            64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJobWithOptions(host.ID, gw.TrustBundle(), job, now, Options{
		WorkspaceLockStore:    filepath.Join(t.TempDir(), "locks"),
		CaptureRuntimeFixture: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.claude-code-result.v1"`) {
		t.Fatalf("expected primary Claude Code artifact, got %s", result.ArtifactContent)
	}
	report := adapterkit.VerifyRuntimeFixtureJSON([]byte(result.RuntimeFixtureContent), adapterkit.RuntimeFixtureContract{
		Adapter:               "claude-code",
		RequireSuccessful:     true,
		RequireCleanup:        true,
		RequireResultArtifact: true,
	})
	if !report.OK {
		t.Fatalf("Claude Code runtime fixture failed conformance: %#v\n%s", report, result.RuntimeFixtureContent)
	}
	if !strings.Contains(result.RuntimeFixtureContent, `"result_artifact_schema": "rdev.claude-code-result.v1"`) {
		t.Fatalf("expected Claude Code result schema in runtime fixture, got %s", result.RuntimeFixtureContent)
	}
}

func TestRunDevJobCancelsClaudeCodeWhenContextCanceled(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := initHostrunnerGitRepo(t)
	fakeClaude := buildHostrunnerFakeCodexBinary(t, `package main

import "time"

func main() {
	time.Sleep(5 * time.Second)
}
`)
	job, err := gw.CreateJob(host.ID, "claude-code", "sleep until canceled", map[string]any{
		"workspace_root":       repo,
		"capabilities":         []string{"claude-code.run", "git.diff"},
		"prompt":               "sleep until canceled",
		"claude_code_command":  fakeClaude,
		"max_duration_seconds": 30,
		"max_output_bytes":     64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	result, err := RunDevJobWithOptionsContext(ctx, host.ID, gw.TrustBundle(), job, now, Options{})
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.claude-code-result.v1"`) {
		t.Fatalf("expected Claude Code artifact, got %s", result.ArtifactContent)
	}
	if !strings.Contains(result.ArtifactContent, `"canceled": true`) {
		t.Fatalf("expected canceled evidence, got %s", result.ArtifactContent)
	}
	if strings.Contains(result.ArtifactContent, `"timed_out": true`) {
		t.Fatalf("canceled Claude Code job should not be marked timed out, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobExecutesCodexAfterImplicitApprovalSatisfied(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := initHostrunnerGitRepo(t)
	fakeCodex := writeHostrunnerFakeCodex(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	if err := os.WriteFile("README.md", []byte("# demo\n\napproved push intent\n"), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("approved codex run")
}
`)
	job, err := gw.CreateJob(host.ID, "codex", "update README and git push to origin", map[string]any{
		"workspace_root":              repo,
		"capabilities":                []string{"codex.run", "git.diff"},
		"prompt":                      "Update README, then run git push to origin.",
		"codex_command":               "go",
		"codex_args":                  []string{"run", fakeCodex},
		"verification_commands":       [][]string{{"git", "status", "--short"}},
		"allow_verification_commands": []string{"git"},
		"max_duration_seconds":        30,
		"max_output_bytes":            64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = gw.ApproveJob(job.ID, "git.push", "approved", "test approval")
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ArtifactContent, "approved push intent") {
		t.Fatalf("expected codex artifact after approval, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobRequiresApprovalForCodexExternalActionsPayload(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "codex", "ship release", map[string]any{
		"workspace_root":    t.TempDir(),
		"capabilities":      []string{"codex.run", "git.diff"},
		"prompt":            "Prepare release notes.",
		"external_actions":  []string{"deploy"},
		"dangerous_actions": []string{"publish"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertApprovalRequired(t, result, err, "deploy.run")
	if !strings.Contains(result.ArtifactContent, "publish.run") {
		t.Fatalf("expected publish.run to be included in approval artifact, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobRequiresApprovalForShellRiskyArgv(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		argv     []string
		approval string
	}{
		{name: "git push", argv: []string{"git", "push", "origin", "main"}, approval: "git.push"},
		{name: "git merge", argv: []string{"git", "merge", "main"}, approval: "git.merge"},
		{name: "package install", argv: []string{"brew", "install", "node"}, approval: "package.install"},
		{name: "elevation", argv: []string{"sudo", "true"}, approval: "elevation.request"},
		{name: "service", argv: []string{"launchctl", "kickstart", "gui/501/com.example"}, approval: "service.manage"},
		{name: "gui", argv: []string{"osascript", "-e", `tell application "System Events" to keystroke "a"`}, approval: "gui.control"},
		{name: "deploy", argv: []string{"vercel", "--prod"}, approval: "deploy.run"},
		{name: "publish", argv: []string{"npm", "publish"}, approval: "publish.run"},
		{name: "credentials", argv: []string{"gh", "auth", "login"}, approval: "credential.change"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
			host := activeHost(t, gw)
			job, err := gw.CreateJob(host.ID, "shell", "risk check", map[string]any{
				"workspace_root":   t.TempDir(),
				"capabilities":     []string{"shell.user"},
				"argv":             tc.argv,
				"allow_commands":   []string{tc.argv[0]},
				"max_output_bytes": 1024,
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
			assertApprovalRequired(t, result, err, tc.approval)
		})
	}
}

func TestRunDevJobDoesNotExecuteShellBeforeImplicitApproval(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := t.TempDir()
	marker := filepath.Join(repo, "ran.txt")
	writer := writeHostrunnerFakeCodex(t, `package main

import "os"

func main() {
	if err := os.WriteFile("ran.txt", []byte("ran without approval"), 0o644); err != nil {
		panic(err)
	}
}
`)
	job, err := gw.CreateJob(host.ID, "shell", "safe command with external push intent", map[string]any{
		"workspace_root":       repo,
		"capabilities":         []string{"shell.user"},
		"argv":                 []string{"go", "run", writer},
		"allow_commands":       []string{"go"},
		"external_actions":     []string{"git.push"},
		"max_duration_seconds": 30,
		"max_output_bytes":     1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertApprovalRequired(t, result, err, "git.push")
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("shell command executed before approval; marker stat err=%v", statErr)
	}
}

func TestRunDevJobExecutesShellAfterImplicitApprovalSatisfied(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := t.TempDir()
	marker := filepath.Join(repo, "ran.txt")
	writer := writeHostrunnerFakeCodex(t, `package main

import "os"

func main() {
	if err := os.WriteFile("ran.txt", []byte("approved"), 0o644); err != nil {
		panic(err)
	}
}
`)
	job, err := gw.CreateJob(host.ID, "shell", "safe command with external push intent", map[string]any{
		"workspace_root":       repo,
		"capabilities":         []string{"shell.user"},
		"argv":                 []string{"go", "run", writer},
		"allow_commands":       []string{"go"},
		"requested_approvals":  []string{"git.push"},
		"max_duration_seconds": 30,
		"max_output_bytes":     1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = gw.ApproveJob(job.ID, "git.push", "approved", "test approval")
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.shell-result.v1"`) {
		t.Fatalf("expected shell result artifact, got %s", result.ArtifactContent)
	}
	content, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "approved" {
		t.Fatalf("unexpected marker content %q", string(content))
	}
}

func TestRunDevJobRejectsCodexWithoutCapability(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	repo := t.TempDir()
	job, err := gw.CreateJob(host.ID, "codex", "demo", map[string]any{
		"workspace_root": repo,
		"capabilities":   []string{"git.diff"},
		"prompt":         "demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertDenial(t, result, err, "missing_capability")
	if !strings.Contains(result.ArtifactContent, `"capability": "codex.run"`) {
		t.Fatalf("expected missing codex.run capability, got %s", result.ArtifactContent)
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
	job, err := gw.CreateJob(host.ID, "python", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertDenial(t, result, err, "unsupported_adapter")
}

func TestRunDevJobAcceptsFileAdapterRead(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello from file adapter"), 0o644); err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "file", "read note", map[string]any{
		"workspace_root": root,
		"capabilities":   []string{"file.transfer.read", "fs.read"},
		"action":         "read",
		"path":           "note.txt",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ArtifactContent, `"schema_version": "rdev.file-result.v1"`) ||
		!strings.Contains(result.ArtifactContent, "hello from file adapter") {
		t.Fatalf("expected file adapter artifact, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobRequiresDesktopApprovalBeforeExecution(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "desktop", "take screenshot", map[string]any{
		"workspace_root":     ".",
		"capabilities":       []string{"screen.screenshot"},
		"action":             "screen.screenshot",
		"approvals_required": []string{"screen.screenshot"},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertApprovalRequired(t, result, err, "screen.screenshot")
	if strings.Contains(result.ArtifactContent, `"schema_version": "rdev.desktop-result.v1"`) {
		t.Fatalf("approval-required desktop job must not execute adapter, got %s", result.ArtifactContent)
	}
}

func TestRunDevJobRequiresFileDeleteApprovalBeforeExecution(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	root := t.TempDir()
	target := filepath.Join(root, "old.txt")
	if err := os.WriteFile(target, []byte("keep until approved"), 0o600); err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "file", "delete old file", map[string]any{
		"workspace_root":     root,
		"write_scope":        []string{"."},
		"capabilities":       []string{"file.transfer.write", "fs.write.scoped"},
		"action":             "delete",
		"path":               "old.txt",
		"approvals_required": []string{"file.delete"},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	assertApprovalRequired(t, result, err, "file.delete")
	if _, statErr := os.Stat(target); statErr != nil {
		t.Fatalf("approval-required delete must not remove file: %v", statErr)
	}
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
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	registration := model.HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "host",
		OS:                  "darwin",
		Arch:                "arm64",
		IdentityKeyID:       "host-test",
		IdentityPublicKey:   encodeHostIdentityPublicKey(publicKey),
		IdentityFingerprint: hostIdentityFingerprint(publicKey),
	}
	proof, err := model.SignHostRegistration(registration, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	registration.IdentityProof = &proof
	host, err := gw.RegisterHost(registration)
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

func initHostrunnerGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for hostrunner codex adapter tests")
	}
	repo := t.TempDir()
	runHostrunnerGit(t, repo, "init")
	runHostrunnerGit(t, repo, "config", "user.email", "test@example.com")
	runHostrunnerGit(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runHostrunnerGit(t, repo, "add", "README.md")
	runHostrunnerGit(t, repo, "commit", "-m", "initial")
	return repo
}

func runHostrunnerGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func writeHostrunnerFakeCodex(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fakecodex.go")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func buildHostrunnerFakeCodexBinary(t *testing.T, source string) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is required for hostrunner codex adapter tests")
	}
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "fakecodex.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	binaryName := "fakecodex"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(dir, binaryName)
	cmd := exec.Command("go", "build", "-o", binaryPath, sourcePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fake codex binary: %v\n%s", err, string(output))
	}
	return binaryPath
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
