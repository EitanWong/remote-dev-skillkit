package httpapi

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
		if strings.Contains(strings.ToLower(marker), "key") {
			t.Fatalf("gateway instance marker must not expose key material: %q", marker)
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

func TestTrustBundleEndpointRenewsExpiredBundle(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	gw := gateway.NewMemoryGatewayWithClock(clock)
	server := NewServer(gw)
	initial := gw.SignedTrustBundle()
	previousHash, err := initial.Hash()
	if err != nil {
		t.Fatal(err)
	}

	now = initial.NotAfter.Add(time.Second)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/trust-bundle", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("renewal status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		TrustBundle model.SignedTrustBundle `json:"trust_bundle"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TrustBundle.Sequence != initial.Sequence+1 {
		t.Fatalf("renewed sequence = %d, want %d", payload.TrustBundle.Sequence, initial.Sequence+1)
	}
	if payload.TrustBundle.PreviousBundleHash != previousHash {
		t.Fatalf("renewed previous hash = %q, want %q", payload.TrustBundle.PreviousBundleHash, previousHash)
	}
	if !payload.TrustBundle.NotAfter.After(now) {
		t.Fatalf("renewed bundle expires at %s, which is not after %s", payload.TrustBundle.NotAfter, now)
	}
	root, err := payload.TrustBundle.ActiveTrustBundle(payload.TrustBundle.SigningKeyID, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := payload.TrustBundle.Verify(root, now); err != nil {
		t.Fatalf("renewed bundle should verify: %v", err)
	}
	audit := gw.AuditEvents()
	if len(audit) != 1 || audit[0].Action != "trust_bundle.renew" {
		t.Fatalf("audit = %#v, want one trust_bundle.renew event", audit)
	}
}

func TestTrustBundleEndpointRenewsExpiredPersistedBundle(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	publicKey, privateKey := httpTestKeyPair(t)
	gw := gateway.NewMemoryGatewayWithSigningKey(clock, "persistent-gateway", publicKey, privateKey)
	initial := gw.SignedTrustBundle()
	statePath := filepath.Join(t.TempDir(), "gateway-state.json")
	store, err := gateway.NewFileStateStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveFrom(gw); err != nil {
		t.Fatal(err)
	}

	now = initial.NotAfter.Add(time.Second)
	restarted := gateway.NewMemoryGatewayWithSigningKey(clock, "persistent-gateway", publicKey, privateKey)
	if _, ok, err := store.LoadInto(restarted); err != nil || !ok {
		t.Fatalf("load expired state = ok:%t err:%v", ok, err)
	}
	server := NewServerWithStateStore(restarted, store)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/trust-bundle", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("renewal status = %d, body=%s", recorder.Code, recorder.Body.String())
	}

	verified := gateway.NewMemoryGatewayWithSigningKey(clock, "persistent-gateway", publicKey, privateKey)
	if _, ok, err := store.LoadInto(verified); err != nil || !ok {
		t.Fatalf("load persisted renewal = ok:%t err:%v", ok, err)
	}
	if got := verified.SignedTrustBundle().Sequence; got != initial.Sequence+1 {
		t.Fatalf("persisted renewal sequence = %d, want %d", got, initial.Sequence+1)
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
