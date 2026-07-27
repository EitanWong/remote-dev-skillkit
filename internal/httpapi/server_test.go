package httpapi

import (
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"

	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"

	"os"
	"path/filepath"

	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/operatorauth"
)

func TestHealthzIncludesGatewayInstanceMarker(t *testing.T) {
	first := NewServer(gateway.NewMemoryGateway())
	second := NewServer(gateway.NewMemoryGateway())

	readMarker := func(t *testing.T, server Server) string {
		t.Helper()
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		marker := rec.Header().Get("X-Rdev-Gateway-Instance")
		decoded, err := hex.DecodeString(marker)
		if err != nil || len(decoded) != 16 {
			t.Fatalf("expected a 128-bit hex gateway instance marker, got %q", marker)
		}
		if strings.Contains(strings.ToLower(marker), "ticket") || strings.Contains(strings.ToLower(marker), "key") {
			t.Fatalf("gateway instance marker must not expose ticket or key material: %q", marker)
		}
		return marker
	}

	firstMarker := readMarker(t, first)
	if repeated := readMarker(t, first); repeated != firstMarker {
		t.Fatalf("expected stable marker %q, got %q", firstMarker, repeated)
	}
	if first.GatewayInstance() != firstMarker {
		t.Fatalf("GatewayInstance returned %q, health returned %q", first.GatewayInstance(), firstMarker)
	}
	if secondMarker := readMarker(t, second); secondMarker == firstMarker {
		t.Fatalf("expected distinct per-server markers, both were %q", firstMarker)
	}
}

func TestUnexpectedControlPlaneErrorIsRecoverableAndDoesNotLeakDetails(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeControlPlaneError(recorder, errors.New("database password secret-detail"))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	var payload struct {
		Error controlplane.ProtocolError `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Error.Recoverable {
		t.Fatal("unexpected server error must remain retryable")
	}
	if strings.Contains(recorder.Body.String(), "secret-detail") {
		t.Fatal("unexpected server error leaked internal details")
	}
}

func TestJoinAssetsServeConfiguredBinaryAndHash(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "rdev-bootstrap-windows-amd64.exe")
	if err := os.WriteFile(binaryPath, []byte("fake bootstrap binary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(gateway.NewMemoryGateway())
	server.Assets.RdevBootstrapWindowsAMD64Path = binaryPath
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodGet, "/assets/rdev-bootstrap-windows-amd64.exe.sha256", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	sum := sha256.Sum256([]byte("fake bootstrap binary\n"))
	if strings.TrimSpace(rec.Body.String()) != hex.EncodeToString(sum[:]) {
		t.Fatalf("unexpected sha body: %q", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/assets/rdev-bootstrap-windows-amd64.exe", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "fake bootstrap binary") {
		t.Fatalf("expected configured binary, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRdevHostWindowsAMD64AssetServesExactBinaryAndHashOnly(t *testing.T) {
	dir := t.TempDir()
	binaryContent := []byte("fake Windows host core runtime\n")
	binaryPath := filepath.Join(dir, "rdev-host-windows-amd64.exe")
	if err := os.WriteFile(binaryPath, binaryContent, 0o600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(gateway.NewMemoryGateway())
	server.Assets.RdevHostWindowsAMD64Path = binaryPath
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodGet, "/assets/rdev-host-windows-amd64.exe", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), binaryContent) {
		t.Fatalf("expected configured Windows host runtime, got %d: %q", rec.Code, rec.Body.Bytes())
	}

	req = httptest.NewRequest(http.MethodGet, "/assets/rdev-host-windows-amd64.exe.sha256", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	sum := sha256.Sum256(binaryContent)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != hex.EncodeToString(sum[:]) {
		t.Fatalf("expected Windows host runtime checksum, got %d: %q", rec.Code, rec.Body.String())
	}

	for _, requestPath := range []string{
		"/assets/rdev-host-windows-arm64.exe",
		"/assets/rdev-host-windows-amd64.exe.extra",
		"/assets/nested/rdev-host-windows-amd64.exe",
		"/assets/../rdev-host-windows-amd64.exe",
		"/assets/rdev-host-windows-amd64.exe.gz",
		"/assets/rdev-host-windows-amd64.exe.gz.sha256",
		"/assets/rdev-host-windows-amd64.exe/",
		"/assets/rdev-host-windows-amd64.exe.sha256/",
		"/assets/rdev-host-windows-amd64.exe?download=1",
		"/assets/rdev-host-windows-amd64.exe.sha256?download=1",
		"/assets/%2Frdev-host-windows-amd64.exe",
		"/assets/%2Frdev-host-windows-amd64.exe.sha256",
	} {
		req = httptest.NewRequest(http.MethodGet, requestPath, nil)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK || bytes.Contains(rec.Body.Bytes(), binaryContent) {
			t.Fatalf("unexpected Windows host asset exposure for %q: %d %q", requestPath, rec.Code, rec.Body.Bytes())
		}
	}
}

func TestAssetErrorsDoNotExposeConfiguredFilesystemPath(t *testing.T) {
	privatePath := filepath.Join(t.TempDir(), "private", "rdev-host-windows-amd64.exe")
	rec := httptest.NewRecorder()
	NewServer(gateway.NewMemoryGateway()).serveGzipAsset(rec, privatePath)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected asset read failure, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), privatePath) || strings.Contains(rec.Body.String(), filepath.Dir(privatePath)) {
		t.Fatalf("asset error exposed configured filesystem path: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error":"asset is unavailable"`) {
		t.Fatalf("expected generic asset error, got %s", rec.Body.String())
	}
}

func TestJoinAssetsServeGzipBinary(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "rdev-bootstrap-windows-amd64.exe")
	content := bytes.Repeat([]byte("fake bootstrap binary\n"), 1024)
	if err := os.WriteFile(binaryPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(gateway.NewMemoryGateway())
	server.Assets.RdevBootstrapWindowsAMD64Path = binaryPath
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodGet, "/assets/rdev-bootstrap-windows-amd64.exe.gz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/gzip" {
		t.Fatalf("expected application/gzip, got %q", got)
	}
	reader, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("gzip bootstrap body did not round trip")
	}
}

func TestTrustBundleEndpointUpdatesSignedBundle(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey := httpTestKeyPair(t)
	gw := gateway.NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-dev", publicKey, privateKey)
	server := NewServer(gw)
	handler := server.Handler()

	getReq := httptest.NewRequest(http.MethodGet, "/v1/trust-bundle", nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}
	var getPayload struct {
		TrustBundle model.SignedTrustBundle `json:"trust_bundle"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &getPayload); err != nil {
		t.Fatal(err)
	}
	if getPayload.TrustBundle.Sequence != 1 {
		t.Fatalf("expected initial sequence 1, got %d", getPayload.TrustBundle.Sequence)
	}
	previousHash, err := getPayload.TrustBundle.Hash()
	if err != nil {
		t.Fatal(err)
	}
	next, err := model.NewSignedTrustBundle(model.SignedTrustBundleSpec{
		BundleID:           getPayload.TrustBundle.BundleID,
		Sequence:           2,
		NotBefore:          now,
		NotAfter:           now.Add(time.Hour),
		PreviousBundleHash: previousHash,
		SigningKeyID:       "gateway-dev",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway-dev", publicKey, model.TrustKeyStatusActive, now),
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	next, err = next.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"trust_bundle": next})
	if err != nil {
		t.Fatal(err)
	}
	updateReq := httptest.NewRequest(http.MethodPost, "/v1/trust-bundle", bytes.NewReader(body))
	updateRec := httptest.NewRecorder()
	handler.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", updateRec.Code, updateRec.Body.String())
	}
	var updatePayload struct {
		TrustBundle model.SignedTrustBundle `json:"trust_bundle"`
	}
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updatePayload); err != nil {
		t.Fatal(err)
	}
	if updatePayload.TrustBundle.Sequence != 2 {
		t.Fatalf("expected updated sequence 2, got %d", updatePayload.TrustBundle.Sequence)
	}
	if err := updatePayload.TrustBundle.Verify(model.NewTrustBundle("gateway-dev", publicKey), now); err != nil {
		t.Fatalf("updated trust bundle should verify: %v", err)
	}
	auditReq := httptest.NewRequest(http.MethodGet, "/v1/audit", nil)
	auditRec := httptest.NewRecorder()
	handler.ServeHTTP(auditRec, auditReq)
	if !bytes.Contains(auditRec.Body.Bytes(), []byte("trust_bundle.update")) {
		t.Fatalf("expected audit response to include trust_bundle.update, got %s", auditRec.Body.String())
	}
}

func TestTrustBundleEndpointRejectsRollback(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey := httpTestKeyPair(t)
	gw := gateway.NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-dev", publicKey, privateKey)
	server := NewServer(gw)
	handler := server.Handler()

	current := gw.SignedTrustBundle()
	hash, err := current.Hash()
	if err != nil {
		t.Fatal(err)
	}
	rollback, err := model.NewSignedTrustBundle(model.SignedTrustBundleSpec{
		BundleID:           current.BundleID,
		Sequence:           1,
		NotBefore:          now,
		NotAfter:           now.Add(time.Hour),
		PreviousBundleHash: hash,
		SigningKeyID:       "gateway-dev",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway-dev", publicKey, model.TrustKeyStatusActive, now),
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	rollback, err = rollback.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"trust_bundle": rollback})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/trust-bundle", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func httpTestKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return publicKey, privateKey
}

func httpTestOperatorAuth(t *testing.T) *operatorauth.Authorizer {
	t.Helper()
	auth, err := operatorauth.New([]operatorauth.Principal{
		{ID: "operator", Roles: []string{operatorauth.RoleOperator}, TokenHash: operatorauth.HashToken("operator-secret")},
		{ID: "issuer", Roles: []string{operatorauth.RoleIssuer}, TokenHash: operatorauth.HashToken("issuer-secret")},
		{ID: "auditor", Roles: []string{operatorauth.RoleAuditor}, TokenHash: operatorauth.HashToken("auditor-secret")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return auth
}

func httpHostIdentityFingerprint(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}
