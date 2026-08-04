package hostrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/buildinfo"
)

const (
	hostUpdateResultSchema = "rdev.host-update-result.v1"
	hostUpdateBinaryName   = "rdev-host.exe"
	hostUpdateMaxBytes     = 128 << 20
)

// executeHostUpdate implements the host-update adapter. It fetches the host
// connector artifact the session gateway currently serves (authorized by the
// endpoint lease the host already uses for task fetch), verifies its SHA-256,
// stages it in a digest-bound release directory, and applies it through the
// detached `service update` updater. The updater replaces the service
// atomically (staged path + SCM switch, never overwriting the running image)
// and rolls back automatically if the replacement fails to boot; the task
// result is posted before the service restarts, and the operator verifies the
// outcome from the reconnected endpoint's host_version plus the
// UPDATE_RESULT.json marker.
func executeHostUpdate(ctx context.Context, envelope taskEnvelope) (string, error) {
	if envelope.GatewayURL == "" || envelope.SessionID == "" || envelope.LeaseSecret == "" {
		return "", fmt.Errorf("host update task is missing gateway/session/lease transport context")
	}
	expected := strings.TrimSpace(stringValue(envelope.Payload, "expected_sha256", ""))
	currentDigest, err := executableSHA256()
	if err != nil {
		return "", fmt.Errorf("hash current host executable: %w", err)
	}

	artifactURL := strings.TrimRight(envelope.GatewayURL, "/") + "/v1/sessions/" + envelope.SessionID + "/artifacts/host-update"
	download, err := fetchHostUpdateArtifact(ctx, artifactURL, envelope.EndpointID, envelope.LeaseSecret)
	if err != nil {
		return "", err
	}
	if expected != "" && !strings.EqualFold(expected, download.SHA256) {
		return "", fmt.Errorf("gateway host artifact digest %s does not match requested %s; cut the gateway over to the target release first", download.SHA256, expected)
	}
	if strings.EqualFold(download.SHA256, currentDigest) {
		return hostUpdateResult(download.SHA256, "up-to-date", "")
	}
	releaseDir, err := stageHostUpdateRelease(download)
	if err != nil {
		return "", err
	}
	if err := launchDetachedHostUpdater(releaseDir); err != nil {
		return "", err
	}
	return hostUpdateResult(download.SHA256, "applied, service restarting", releaseDir)
}

type hostUpdateArtifact struct {
	Content []byte
	SHA256  string
}

func fetchHostUpdateArtifact(ctx context.Context, url, endpointID, leaseSecret string) (hostUpdateArtifact, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return hostUpdateArtifact{}, fmt.Errorf("build host update artifact request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+leaseSecret)
	request.Header.Set(hostUpdateEndpointIDHeader, endpointID)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return hostUpdateArtifact{}, fmt.Errorf("fetch host update artifact: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return hostUpdateArtifact{}, fmt.Errorf("fetch host update artifact: gateway responded %s", response.Status)
	}
	declared := strings.ToLower(strings.TrimSpace(response.Header.Get("X-Rdev-Sha256")))
	content, err := io.ReadAll(io.LimitReader(response.Body, hostUpdateMaxBytes))
	if err != nil {
		return hostUpdateArtifact{}, fmt.Errorf("read host update artifact: %w", err)
	}
	if len(content) == 0 {
		return hostUpdateArtifact{}, fmt.Errorf("host update artifact is empty")
	}
	sum := sha256.Sum256(content)
	actual := hex.EncodeToString(sum[:])
	if declared != "" && !strings.EqualFold(declared, actual) {
		return hostUpdateArtifact{}, fmt.Errorf("host update artifact digest mismatch: got %s want %s", actual, declared)
	}
	return hostUpdateArtifact{Content: content, SHA256: actual}, nil
}

// stageHostUpdateRelease writes the verified artifact into a fresh digest-keyed
// release directory with the digest sidecar the detached updater re-verifies
// before activation.
func stageHostUpdateRelease(artifact hostUpdateArtifact) (string, error) {
	releaseDir, err := os.MkdirTemp("", "rdev-host-update-*")
	if err != nil {
		return "", fmt.Errorf("create host update staging directory: %w", err)
	}
	binaryPath := filepath.Join(releaseDir, hostUpdateBinaryName)
	if err := os.WriteFile(binaryPath, artifact.Content, 0o700); err != nil {
		_ = os.RemoveAll(releaseDir)
		return "", fmt.Errorf("stage host update binary: %w", err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, hostUpdateBinaryName+".sha256"), []byte(artifact.SHA256+"\n"), 0o600); err != nil {
		_ = os.RemoveAll(releaseDir)
		return "", fmt.Errorf("stage host update digest: %w", err)
	}
	return releaseDir, nil
}

func executableSHA256() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(executable)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}

func hostUpdateResult(digest, status, releaseDir string) (string, error) {
	result := map[string]any{
		"schema_version": hostUpdateResultSchema,
		"status":         status,
		"sha256":         digest,
		"host_version":   buildinfo.Version,
		"host_commit":    buildinfo.Commit,
		"at":             time.Now().UTC().Format(time.RFC3339),
	}
	if releaseDir != "" {
		result["release_dir"] = releaseDir
	}
	content, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	return string(content), nil
}
