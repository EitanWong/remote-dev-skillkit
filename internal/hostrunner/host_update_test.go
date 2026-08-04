package hostrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

// TestHostUpdateAdapterRequiresCapabilityButNotWorkspace pins the two
// preflight behaviors that make host-update usable as a control-plane
// operation: it demands the host.update capability, and it is exempt from the
// workspace_root requirement that every coding adapter enforces.
func TestHostUpdateAdapterRequiresCapabilityButNotWorkspace(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

	noCapability := SessionTaskSpec{
		TaskID:       "task-host-update",
		EndpointID:   "endpoint_target",
		Adapter:      "host-update",
		Intent:       "host update smoke",
		SessionID:    "ses_update",
		LeaseSecret:  "lease_update",
		GatewayURL:   "https://gateway.example.test",
		Capabilities: []string{},
		Limits:       model.TaskLimits{MaxDurationSeconds: 60, MaxOutputBytes: 4096},
		Payload:      map[string]any{},
	}
	_, err := RunSessionTaskWithOptionsContext(context.Background(), noCapability, now, Options{})
	var denial DenialError
	if !errors.As(err, &denial) {
		t.Fatalf("expected DenialError for missing capability, got %T %v", err, err)
	}
	if denial.Explanation.Code != "missing_capability" || denial.Explanation.Capability != "host.update" {
		t.Fatalf("expected host.update capability denial: %#v", denial.Explanation)
	}

	// With the capability and no workspace_root the preflight must pass; the
	// platform launcher gate then rejects the actual update on this host.
	withCapability := noCapability
	withCapability.Capabilities = []string{"host.update"}
	withCapability.GatewayURL = artifactServer(t, "MZ-different-host-binary").URL
	_, err = RunSessionTaskWithOptionsContext(context.Background(), withCapability, now, Options{})
	if err == nil || !strings.Contains(err.Error(), "host updater is only supported on Windows") {
		t.Fatalf("expected platform launcher gate error, got %v", err)
	}
}

// TestExecuteHostUpdateFlow covers the adapter's decision branches: transport
// context validation, expected-digest pinning, idempotent up-to-date short
// circuit, and the apply path that stages then launches the updater.
func TestExecuteHostUpdateFlow(t *testing.T) {
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	currentBytes, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	currentSum := sha256.Sum256(currentBytes)
	currentDigest := hex.EncodeToString(currentSum[:])

	envelope := taskEnvelope{
		TaskID:      "task-host-update",
		EndpointID:  "endpoint_target",
		SessionID:   "ses_update",
		LeaseSecret: "lease_update",
		GatewayURL:  "https://gateway.example.test",
		Payload:     map[string]any{},
	}

	// Missing transport context fails closed.
	noContext := envelope
	noContext.GatewayURL = ""
	noContext.SessionID = ""
	noContext.LeaseSecret = ""
	if _, err := executeHostUpdate(context.Background(), noContext); err == nil || !strings.Contains(err.Error(), "transport context") {
		t.Fatalf("expected transport context error, got %v", err)
	}

	// The served artifact is the running connector: up-to-date, no updater.
	envelope.GatewayURL = artifactServer(t, string(currentBytes)).URL
	result, err := executeHostUpdate(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, `"status": "up-to-date"`) || !strings.Contains(result, currentDigest) {
		t.Fatalf("up-to-date result = %s", result)
	}

	// Pinning an expected digest the gateway does not serve aborts the apply.
	pinned := envelope
	pinned.Payload = map[string]any{"expected_sha256": strings.Repeat("ab", 32)}
	if _, err := executeHostUpdate(context.Background(), pinned); err == nil || !strings.Contains(err.Error(), "does not match requested") {
		t.Fatalf("expected digest pin mismatch error, got %v", err)
	}

	// A different artifact passes verification and reaches the launcher gate.
	envelope.GatewayURL = artifactServer(t, "MZ-different-host-binary").URL
	if _, err := executeHostUpdate(context.Background(), envelope); err == nil || !strings.Contains(err.Error(), "host updater is only supported on Windows") {
		t.Fatalf("expected launcher gate error after staging, got %v", err)
	}
}

// artifactServer serves the given bytes as the host connector under the
// endpoint lease header the adapter sends.
func artifactServer(t *testing.T, content string) *httptest.Server {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer lease_update" || r.Header.Get(hostUpdateEndpointIDHeader) != "endpoint_target" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-Rdev-Sha256", hex.EncodeToString(sum[:]))
		_, _ = w.Write([]byte(content))
	}))
	t.Cleanup(server.Close)
	return server
}

// TestFetchHostUpdateArtifactVerifiesDigest ensures the downloaded connector
// is checked against the gateway-declared digest before staging, and that a
// mismatched declaration aborts the update.
func TestFetchHostUpdateArtifactVerifiesDigest(t *testing.T) {
	content := []byte("MZ-rdev-host-update")
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer lease_secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get(hostUpdateEndpointIDHeader) != "endpoint_target" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("X-Rdev-Sha256", digest)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	artifact, err := fetchHostUpdateArtifact(context.Background(), server.URL, "endpoint_target", "lease_secret")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.SHA256 != digest || string(artifact.Content) != string(content) {
		t.Fatalf("artifact = %s %d bytes", artifact.SHA256, len(artifact.Content))
	}

	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Rdev-Sha256", strings.Repeat("0", 64))
		_, _ = w.Write(content)
	}))
	defer badServer.Close()
	if _, err := fetchHostUpdateArtifact(context.Background(), badServer.URL, "endpoint_target", "lease_secret"); err == nil {
		t.Fatal("digest mismatch must abort the fetch")
	}
}

// TestStageHostUpdateReleaseWritesDigestSidecar ensures the detached updater
// can re-verify the staged binary before activation.
func TestStageHostUpdateReleaseWritesDigestSidecar(t *testing.T) {
	content := []byte("MZ-staged-release")
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	releaseDir, err := stageHostUpdateRelease(hostUpdateArtifact{Content: content, SHA256: digest})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(releaseDir)
	staged, err := os.ReadFile(filepath.Join(releaseDir, hostUpdateBinaryName))
	if err != nil {
		t.Fatal(err)
	}
	if string(staged) != string(content) {
		t.Fatalf("staged binary = %q", staged)
	}
	sidecar, err := os.ReadFile(filepath.Join(releaseDir, hostUpdateBinaryName+".sha256"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(sidecar)) != digest {
		t.Fatalf("digest sidecar = %q want %q", sidecar, digest)
	}
}

// TestHostUpdateResultArtifactSchema ensures the posted task result carries
// the fields an operator needs to verify the update from the control plane.
func TestHostUpdateResultArtifactSchema(t *testing.T) {
	content, err := hostUpdateResult("abc123", "applied, service restarting", "C:\\staging\\release")
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(content), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["schema_version"] != hostUpdateResultSchema || decoded["status"] != "applied, service restarting" || decoded["sha256"] != "abc123" {
		t.Fatalf("unexpected result artifact: %s", content)
	}
}
