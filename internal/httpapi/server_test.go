package httpapi

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestCreateTicketAndAudit(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	handler := server.Handler()

	body := bytes.NewBufferString(`{"mode":"attended-temporary","ttl_seconds":600,"reason":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tickets", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["joinUrl"].(string); !ok {
		t.Fatalf("expected joinUrl, got %#v", payload)
	}

	auditReq := httptest.NewRequest(http.MethodGet, "/v1/audit", nil)
	auditRec := httptest.NewRecorder()
	handler.ServeHTTP(auditRec, auditReq)
	if auditRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", auditRec.Code)
	}
	if !bytes.Contains(auditRec.Body.Bytes(), []byte("ticket.create")) {
		t.Fatalf("expected audit response to include ticket.create, got %s", auditRec.Body.String())
	}
}

func TestTrustEndpointVerifiesJobEnvelope(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := NewServer(gw)
	handler := server.Handler()
	host := registerAndApproveHost(t, handler)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	trustReq := httptest.NewRequest(http.MethodGet, "/v1/trust", nil)
	trustRec := httptest.NewRecorder()
	handler.ServeHTTP(trustRec, trustReq)
	if trustRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", trustRec.Code, trustRec.Body.String())
	}
	var payload struct {
		Trust model.TrustBundle `json:"trust"`
	}
	if err := json.Unmarshal(trustRec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	publicKey, err := payload.Trust.Ed25519PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if job.Envelope == nil {
		t.Fatal("job envelope must be present")
	}
	if err := job.Envelope.VerifyForHost(publicKey, host.ID, job.CreatedAt); err != nil {
		t.Fatalf("expected trust bundle to verify envelope: %v", err)
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

func TestHostTrustBundleUpdateEndpointReportsCurrentAndAvailableUpdate(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey := httpTestKeyPair(t)
	gw := gateway.NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-dev", publicKey, privateKey)
	server := NewServer(gw)
	handler := server.Handler()
	host := registerAndApproveHost(t, handler)

	current := gw.SignedTrustBundle()
	currentHash, err := current.Hash()
	if err != nil {
		t.Fatal(err)
	}
	currentURL := "/v1/hosts/" + host.ID + "/trust-bundle/update?current_sequence=1&current_hash=" + url.QueryEscape(currentHash)
	currentReq := httptest.NewRequest(http.MethodGet, currentURL, nil)
	currentRec := httptest.NewRecorder()
	handler.ServeHTTP(currentRec, currentReq)
	if currentRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", currentRec.Code, currentRec.Body.String())
	}
	var currentPayload struct {
		TrustBundleUpdate model.TrustBundleUpdate `json:"trust_bundle_update"`
	}
	if err := json.Unmarshal(currentRec.Body.Bytes(), &currentPayload); err != nil {
		t.Fatal(err)
	}
	if currentPayload.TrustBundleUpdate.Status != model.TrustBundleUpdateStatusCurrent {
		t.Fatalf("expected current status, got %#v", currentPayload.TrustBundleUpdate)
	}
	if currentPayload.TrustBundleUpdate.TrustBundle != nil {
		t.Fatal("current response should not include a bundle")
	}

	next, err := model.NewSignedTrustBundle(model.SignedTrustBundleSpec{
		BundleID:           current.BundleID,
		Sequence:           2,
		NotBefore:          now,
		NotAfter:           now.Add(time.Hour),
		PreviousBundleHash: currentHash,
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
	if _, err := gw.UpdateSignedTrustBundle(next); err != nil {
		t.Fatal(err)
	}

	updateReq := httptest.NewRequest(http.MethodGet, currentURL, nil)
	updateRec := httptest.NewRecorder()
	handler.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", updateRec.Code, updateRec.Body.String())
	}
	var updatePayload struct {
		TrustBundleUpdate model.TrustBundleUpdate `json:"trust_bundle_update"`
	}
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updatePayload); err != nil {
		t.Fatal(err)
	}
	if updatePayload.TrustBundleUpdate.Status != model.TrustBundleUpdateStatusAvailable {
		t.Fatalf("expected update_available status, got %#v", updatePayload.TrustBundleUpdate)
	}
	if updatePayload.TrustBundleUpdate.TrustBundle == nil || updatePayload.TrustBundleUpdate.TrustBundle.Sequence != 2 {
		t.Fatalf("expected sequence 2 bundle, got %#v", updatePayload.TrustBundleUpdate.TrustBundle)
	}
}

func TestTicketManifestEndpointSignsJoinManifest(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := NewServer(gw)
	handler := server.Handler()
	ticket := createTicket(t, handler)

	req := httptest.NewRequest(http.MethodGet, "/v1/tickets/"+ticket.Code+"/manifest", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Manifest model.JoinManifest `json:"manifest"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Manifest.TicketCode != ticket.Code {
		t.Fatalf("expected ticket code %q, got %q", ticket.Code, payload.Manifest.TicketCode)
	}
	if payload.Manifest.TrustFingerprint == "" {
		t.Fatal("manifest should include trust fingerprint")
	}
	if err := payload.Manifest.Verify(ticket.CreatedAt); err != nil {
		t.Fatalf("expected manifest to verify: %v", err)
	}
}

func TestRegisterAndApproveHost(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	handler := server.Handler()

	ticket := createTicket(t, handler)
	registerBody := bytes.NewBufferString(`{"ticket_code":"` + ticket.Code + `","name":"win-temp","os":"windows","arch":"amd64","capabilities":["shell.user"]}`)
	registerReq := httptest.NewRequest(http.MethodPost, "/v1/hosts/register", registerBody)
	registerRec := httptest.NewRecorder()
	handler.ServeHTTP(registerRec, registerReq)
	if registerRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", registerRec.Code, registerRec.Body.String())
	}
	var registerPayload struct {
		Host model.Host `json:"host"`
	}
	if err := json.Unmarshal(registerRec.Body.Bytes(), &registerPayload); err != nil {
		t.Fatal(err)
	}
	if registerPayload.Host.Status != model.HostStatusPending {
		t.Fatalf("expected pending host, got %s", registerPayload.Host.Status)
	}

	approveReq := httptest.NewRequest(http.MethodPost, "/v1/hosts/"+registerPayload.Host.ID+"/approve", bytes.NewBufferString(`{"capabilities":["shell.user","fs.read"]}`))
	approveRec := httptest.NewRecorder()
	handler.ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", approveRec.Code, approveRec.Body.String())
	}
	var approvePayload struct {
		Host model.Host `json:"host"`
	}
	if err := json.Unmarshal(approveRec.Body.Bytes(), &approvePayload); err != nil {
		t.Fatal(err)
	}
	if approvePayload.Host.Status != model.HostStatusActive {
		t.Fatalf("expected active host, got %s", approvePayload.Host.Status)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/hosts/"+approvePayload.Host.ID, nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}
}

func TestRegisterHostPreservesIdentityFields(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	handler := server.Handler()
	ticket := createTicket(t, handler)
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := httpHostIdentityFingerprint(publicKey)
	body, err := json.Marshal(map[string]any{
		"ticket_code":          ticket.Code,
		"name":                 "win-temp",
		"os":                   "windows",
		"arch":                 "amd64",
		"capabilities":         []string{"shell.user"},
		"identity_key_id":      "host-test",
		"identity_public_key":  base64.RawURLEncoding.EncodeToString(publicKey),
		"identity_fingerprint": fingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/hosts/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Host model.Host `json:"host"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Host.IdentityKeyID != "host-test" {
		t.Fatalf("expected identity key id, got %q", payload.Host.IdentityKeyID)
	}
	if payload.Host.IdentityFingerprint != fingerprint {
		t.Fatalf("expected identity fingerprint %q, got %q", fingerprint, payload.Host.IdentityFingerprint)
	}
}

func TestRevokeHostCancelsQueuedJobs(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := NewServer(gw)
	handler := server.Handler()
	host := registerAndApproveHost(t, handler)

	jobBody := bytes.NewBufferString(`{"host_id":"` + host.ID + `","adapter":"shell","intent":"local demo","policy":{"workspace_root":".","capabilities":["shell.user"]}}`)
	jobReq := httptest.NewRequest(http.MethodPost, "/v1/jobs", jobBody)
	jobRec := httptest.NewRecorder()
	handler.ServeHTTP(jobRec, jobReq)
	if jobRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", jobRec.Code, jobRec.Body.String())
	}
	var jobPayload struct {
		Job model.Job `json:"job"`
	}
	if err := json.Unmarshal(jobRec.Body.Bytes(), &jobPayload); err != nil {
		t.Fatal(err)
	}

	revokeReq := httptest.NewRequest(http.MethodPost, "/v1/hosts/"+host.ID+"/revoke", bytes.NewBufferString(`{"reason":"done"}`))
	revokeRec := httptest.NewRecorder()
	handler.ServeHTTP(revokeRec, revokeReq)
	if revokeRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", revokeRec.Code, revokeRec.Body.String())
	}
	var revokePayload struct {
		Host model.Host `json:"host"`
	}
	if err := json.Unmarshal(revokeRec.Body.Bytes(), &revokePayload); err != nil {
		t.Fatal(err)
	}
	if revokePayload.Host.Status != model.HostStatusRevoked {
		t.Fatalf("expected revoked host, got %s", revokePayload.Host.Status)
	}
	job, err := gw.Job(jobPayload.Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != model.JobStatusCanceled {
		t.Fatalf("expected canceled job, got %s", job.Status)
	}
	auditReq := httptest.NewRequest(http.MethodGet, "/v1/audit", nil)
	auditRec := httptest.NewRecorder()
	handler.ServeHTTP(auditRec, auditReq)
	if !bytes.Contains(auditRec.Body.Bytes(), []byte("job.cancel")) {
		t.Fatalf("expected audit response to include job.cancel, got %s", auditRec.Body.String())
	}
}

func TestJobCreateClaimAndComplete(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	handler := server.Handler()
	host := registerAndApproveHost(t, handler)

	jobBody := bytes.NewBufferString(`{"host_id":"` + host.ID + `","adapter":"shell","intent":"local demo","policy":{"workspace_root":".","capabilities":["shell.user"]}}`)
	jobReq := httptest.NewRequest(http.MethodPost, "/v1/jobs", jobBody)
	jobRec := httptest.NewRecorder()
	handler.ServeHTTP(jobRec, jobReq)
	if jobRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", jobRec.Code, jobRec.Body.String())
	}
	var jobPayload struct {
		Job model.Job `json:"job"`
	}
	if err := json.Unmarshal(jobRec.Body.Bytes(), &jobPayload); err != nil {
		t.Fatal(err)
	}
	if jobPayload.Job.Envelope == nil || jobPayload.Job.Envelope.Signature == "" {
		t.Fatal("created job should include signed envelope")
	}

	nextReq := httptest.NewRequest(http.MethodGet, "/v1/hosts/"+host.ID+"/jobs/next", nil)
	nextRec := httptest.NewRecorder()
	handler.ServeHTTP(nextRec, nextReq)
	if nextRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", nextRec.Code, nextRec.Body.String())
	}
	var nextPayload struct {
		Job model.Job `json:"job"`
	}
	if err := json.Unmarshal(nextRec.Body.Bytes(), &nextPayload); err != nil {
		t.Fatal(err)
	}
	if nextPayload.Job.Status != model.JobStatusRunning {
		t.Fatalf("expected running job after claim, got %s", nextPayload.Job.Status)
	}

	completeReq := httptest.NewRequest(http.MethodPost, "/v1/jobs/"+jobPayload.Job.ID+"/complete", bytes.NewBufferString(`{"host_id":"`+host.ID+`","artifact_content":"done"}`))
	completeRec := httptest.NewRecorder()
	handler.ServeHTTP(completeRec, completeReq)
	if completeRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", completeRec.Code, completeRec.Body.String())
	}
	var completePayload struct {
		Job      model.Job      `json:"job"`
		Artifact model.Artifact `json:"artifact"`
	}
	if err := json.Unmarshal(completeRec.Body.Bytes(), &completePayload); err != nil {
		t.Fatal(err)
	}
	if completePayload.Job.Status != model.JobStatusSucceeded {
		t.Fatalf("expected succeeded job, got %s", completePayload.Job.Status)
	}
	if completePayload.Artifact.Content != "done" {
		t.Fatalf("expected artifact content, got %q", completePayload.Artifact.Content)
	}

	artifactsReq := httptest.NewRequest(http.MethodGet, "/v1/jobs/"+jobPayload.Job.ID+"/artifacts", nil)
	artifactsRec := httptest.NewRecorder()
	handler.ServeHTTP(artifactsRec, artifactsReq)
	if artifactsRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", artifactsRec.Code, artifactsRec.Body.String())
	}
	var artifactsPayload struct {
		Artifacts []model.Artifact `json:"artifacts"`
	}
	if err := json.Unmarshal(artifactsRec.Body.Bytes(), &artifactsPayload); err != nil {
		t.Fatal(err)
	}
	if len(artifactsPayload.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifactsPayload.Artifacts))
	}

	artifactReq := httptest.NewRequest(http.MethodGet, "/v1/artifacts/"+completePayload.Artifact.ID, nil)
	artifactRec := httptest.NewRecorder()
	handler.ServeHTTP(artifactRec, artifactReq)
	if artifactRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", artifactRec.Code, artifactRec.Body.String())
	}
	var artifactPayload struct {
		Artifact model.Artifact `json:"artifact"`
	}
	if err := json.Unmarshal(artifactRec.Body.Bytes(), &artifactPayload); err != nil {
		t.Fatal(err)
	}
	if artifactPayload.Artifact.Content != "done" {
		t.Fatalf("expected artifact content, got %q", artifactPayload.Artifact.Content)
	}
}

func TestServerStateSnapshotPersistsGatewayMutations(t *testing.T) {
	now := time.Date(2026, 6, 30, 18, 30, 0, 0, time.UTC)
	publicKey, privateKey := httpTestKeyPair(t)
	statePath := filepath.Join(t.TempDir(), "gateway", "state.json")
	gw := gateway.NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-dev", publicKey, privateKey)
	server := NewServerWithState(gw, statePath)
	handler := server.Handler()
	host := registerAndApproveHost(t, handler)

	jobBody := bytes.NewBufferString(`{"host_id":"` + host.ID + `","adapter":"shell","intent":"persistent demo","policy":{"workspace_root":".","capabilities":["shell.user"]}}`)
	jobReq := httptest.NewRequest(http.MethodPost, "/v1/jobs", jobBody)
	jobRec := httptest.NewRecorder()
	handler.ServeHTTP(jobRec, jobReq)
	if jobRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", jobRec.Code, jobRec.Body.String())
	}
	var jobPayload struct {
		Job model.Job `json:"job"`
	}
	if err := json.Unmarshal(jobRec.Body.Bytes(), &jobPayload); err != nil {
		t.Fatal(err)
	}

	nextReq := httptest.NewRequest(http.MethodGet, "/v1/hosts/"+host.ID+"/jobs/next", nil)
	nextRec := httptest.NewRecorder()
	handler.ServeHTTP(nextRec, nextReq)
	if nextRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", nextRec.Code, nextRec.Body.String())
	}

	completeReq := httptest.NewRequest(http.MethodPost, "/v1/jobs/"+jobPayload.Job.ID+"/complete", bytes.NewBufferString(`{"host_id":"`+host.ID+`","artifact_content":"durable result"}`))
	completeRec := httptest.NewRecorder()
	handler.ServeHTTP(completeRec, completeReq)
	if completeRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", completeRec.Code, completeRec.Body.String())
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected state snapshot: %v", err)
	}

	restartedGateway := gateway.NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-dev", publicKey, privateKey)
	snapshot, err := restartedGateway.LoadSnapshot(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SchemaVersion != gateway.SnapshotSchemaVersion {
		t.Fatalf("unexpected snapshot schema %q", snapshot.SchemaVersion)
	}
	restartedJob, err := restartedGateway.Job(jobPayload.Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restartedJob.Status != model.JobStatusSucceeded {
		t.Fatalf("expected persisted succeeded job, got %s", restartedJob.Status)
	}
	if restartedJob.Envelope == nil {
		t.Fatal("expected persisted signed envelope")
	}
	if err := restartedJob.Envelope.VerifyForHost(publicKey, host.ID, now); err != nil {
		t.Fatalf("expected persisted envelope to verify: %v", err)
	}
	if artifacts := restartedGateway.Artifacts(jobPayload.Job.ID); len(artifacts) != 1 || artifacts[0].Content != "durable result" {
		t.Fatalf("expected persisted artifact, got %#v", artifacts)
	}
	if events := restartedGateway.AuditEvents(); len(events) == 0 || events[len(events)-1].Action != "job.complete" {
		t.Fatalf("expected persisted job.complete audit event, got %#v", events)
	}
}

func TestHostJobsNextLongPollWaitsForJob(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := NewServer(gw)
	handler := server.Handler()
	host := registerAndApproveHost(t, handler)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	result := make(chan struct {
		job model.Job
		err error
	}, 1)

	go func() {
		resp, err := http.Get(httpServer.URL + "/v1/hosts/" + host.ID + "/jobs/next?wait_ms=1000")
		if err != nil {
			result <- struct {
				job model.Job
				err error
			}{err: err}
			return
		}
		defer resp.Body.Close()
		var payload struct {
			Job *model.Job `json:"job"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			result <- struct {
				job model.Job
				err error
			}{err: err}
			return
		}
		if payload.Job == nil {
			result <- struct {
				job model.Job
				err error
			}{err: nil}
			return
		}
		result <- struct {
			job model.Job
			err error
		}{job: *payload.Job}
	}()

	time.Sleep(50 * time.Millisecond)
	created, err := gw.CreateJob(host.ID, "shell", "long-poll demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-result:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.job.ID != created.ID {
			t.Fatalf("expected job %q, got %q", created.ID, got.job.ID)
		}
		if got.job.Status != model.JobStatusRunning {
			t.Fatalf("expected long-polled job to be running, got %s", got.job.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for long-poll result")
	}
}

func TestHostJobsNextLongPollTimeoutReturnsNoJob(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := NewServer(gw)
	handler := server.Handler()
	host := registerAndApproveHost(t, handler)

	req := httptest.NewRequest(http.MethodGet, "/v1/hosts/"+host.ID+"/jobs/next?wait_ms=1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"job":null`)) {
		t.Fatalf("expected null job after timeout, got %s", rec.Body.String())
	}
}

func TestJobEvidenceBundleEndpointExportsBundleFromJobID(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	handler := server.Handler()
	host := registerAndApproveHost(t, handler)

	jobBody := bytes.NewBufferString(`{"host_id":"` + host.ID + `","adapter":"shell","intent":"local demo","policy":{"workspace_root":".","capabilities":["shell.user"]}}`)
	jobReq := httptest.NewRequest(http.MethodPost, "/v1/jobs", jobBody)
	jobRec := httptest.NewRecorder()
	handler.ServeHTTP(jobRec, jobReq)
	if jobRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", jobRec.Code, jobRec.Body.String())
	}
	var jobPayload struct {
		Job model.Job `json:"job"`
	}
	if err := json.Unmarshal(jobRec.Body.Bytes(), &jobPayload); err != nil {
		t.Fatal(err)
	}
	nextReq := httptest.NewRequest(http.MethodGet, "/v1/hosts/"+host.ID+"/jobs/next", nil)
	nextRec := httptest.NewRecorder()
	handler.ServeHTTP(nextRec, nextReq)
	if nextRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", nextRec.Code, nextRec.Body.String())
	}
	completeReq := httptest.NewRequest(http.MethodPost, "/v1/jobs/"+jobPayload.Job.ID+"/complete", bytes.NewBufferString(`{"host_id":"`+host.ID+`","artifact_content":"done"}`))
	completeRec := httptest.NewRecorder()
	handler.ServeHTTP(completeRec, completeReq)
	if completeRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", completeRec.Code, completeRec.Body.String())
	}

	out := filepath.Join(t.TempDir(), "bundle")
	exportReq := httptest.NewRequest(http.MethodGet, "/v1/jobs/"+jobPayload.Job.ID+"/evidence-bundle?out="+url.QueryEscape(out), nil)
	exportRec := httptest.NewRecorder()
	handler.ServeHTTP(exportRec, exportReq)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", exportRec.Code, exportRec.Body.String())
	}
	var exportPayload struct {
		OK       bool   `json:"ok"`
		JobID    string `json:"job_id"`
		Manifest struct {
			SchemaVersion   string `json:"schema_version"`
			AuditEventCount int    `json:"audit_event_count"`
		} `json:"manifest"`
	}
	if err := json.Unmarshal(exportRec.Body.Bytes(), &exportPayload); err != nil {
		t.Fatal(err)
	}
	if !exportPayload.OK || exportPayload.JobID != jobPayload.Job.ID {
		t.Fatalf("unexpected export payload: %s", exportRec.Body.String())
	}
	if exportPayload.Manifest.SchemaVersion != "rdev.evidence-bundle.v1" {
		t.Fatalf("unexpected manifest schema %q", exportPayload.Manifest.SchemaVersion)
	}
	if exportPayload.Manifest.AuditEventCount == 0 {
		t.Fatal("expected audit slice in exported bundle")
	}
	for _, path := range []string{"manifest.json", "job.json", "artifacts.json", "audit-chain.json", "checksums.txt"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected evidence bundle file %s: %v", path, err)
		}
	}
}

func TestJobFail(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	handler := server.Handler()
	host := registerAndApproveHost(t, handler)

	jobBody := bytes.NewBufferString(`{"host_id":"` + host.ID + `","adapter":"shell","intent":"local demo","policy":{"workspace_root":".","capabilities":["shell.user"]}}`)
	jobReq := httptest.NewRequest(http.MethodPost, "/v1/jobs", jobBody)
	jobRec := httptest.NewRecorder()
	handler.ServeHTTP(jobRec, jobReq)
	if jobRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", jobRec.Code, jobRec.Body.String())
	}
	var jobPayload struct {
		Job model.Job `json:"job"`
	}
	if err := json.Unmarshal(jobRec.Body.Bytes(), &jobPayload); err != nil {
		t.Fatal(err)
	}
	nextReq := httptest.NewRequest(http.MethodGet, "/v1/hosts/"+host.ID+"/jobs/next", nil)
	nextRec := httptest.NewRecorder()
	handler.ServeHTTP(nextRec, nextReq)
	if nextRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", nextRec.Code, nextRec.Body.String())
	}

	failReq := httptest.NewRequest(http.MethodPost, "/v1/jobs/"+jobPayload.Job.ID+"/fail", bytes.NewBufferString(`{"host_id":"`+host.ID+`","reason":"signature rejected","artifact_content":"failure evidence"}`))
	failRec := httptest.NewRecorder()
	handler.ServeHTTP(failRec, failReq)
	if failRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", failRec.Code, failRec.Body.String())
	}
	var failPayload struct {
		Job      model.Job      `json:"job"`
		Artifact model.Artifact `json:"artifact"`
	}
	if err := json.Unmarshal(failRec.Body.Bytes(), &failPayload); err != nil {
		t.Fatal(err)
	}
	if failPayload.Job.Status != model.JobStatusFailed {
		t.Fatalf("expected failed job, got %s", failPayload.Job.Status)
	}
	if failPayload.Job.FailureReason != "signature rejected" {
		t.Fatalf("unexpected failure reason %q", failPayload.Job.FailureReason)
	}
	if failPayload.Artifact.Content != "failure evidence" {
		t.Fatalf("expected failure artifact content, got %q", failPayload.Artifact.Content)
	}
}

func TestJobCanceledArtifact(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := NewServer(gw)
	handler := server.Handler()
	host := registerAndApproveHost(t, handler)

	jobBody := bytes.NewBufferString(`{"host_id":"` + host.ID + `","adapter":"codex","intent":"local demo","policy":{"workspace_root":".","capabilities":["codex.run","git.diff"]}}`)
	jobReq := httptest.NewRequest(http.MethodPost, "/v1/jobs", jobBody)
	jobRec := httptest.NewRecorder()
	handler.ServeHTTP(jobRec, jobReq)
	if jobRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", jobRec.Code, jobRec.Body.String())
	}
	var jobPayload struct {
		Job model.Job `json:"job"`
	}
	if err := json.Unmarshal(jobRec.Body.Bytes(), &jobPayload); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CancelJob(jobPayload.Job.ID, "operator cancel"); err != nil {
		t.Fatal(err)
	}

	artifactReq := httptest.NewRequest(http.MethodPost, "/v1/jobs/"+jobPayload.Job.ID+"/artifact", bytes.NewBufferString(`{"host_id":"`+host.ID+`","artifact_content":"cancellation evidence"}`))
	artifactRec := httptest.NewRecorder()
	handler.ServeHTTP(artifactRec, artifactReq)
	if artifactRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", artifactRec.Code, artifactRec.Body.String())
	}
	var artifactPayload struct {
		Job      model.Job      `json:"job"`
		Artifact model.Artifact `json:"artifact"`
	}
	if err := json.Unmarshal(artifactRec.Body.Bytes(), &artifactPayload); err != nil {
		t.Fatal(err)
	}
	if artifactPayload.Job.Status != model.JobStatusCanceled {
		t.Fatalf("expected canceled job, got %s", artifactPayload.Job.Status)
	}
	if artifactPayload.Artifact.Content != "cancellation evidence" {
		t.Fatalf("expected cancellation evidence, got %q", artifactPayload.Artifact.Content)
	}
	if artifacts := gw.Artifacts(jobPayload.Job.ID); len(artifacts) != 1 {
		t.Fatalf("expected one artifact, got %d", len(artifacts))
	}
}

func createTicket(t *testing.T, handler http.Handler) model.Ticket {
	t.Helper()
	body := bytes.NewBufferString(`{"mode":"attended-temporary","ttl_seconds":600,"reason":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tickets", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Ticket model.Ticket `json:"ticket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Ticket
}

func registerAndApproveHost(t *testing.T, handler http.Handler) model.Host {
	t.Helper()
	ticket := createTicket(t, handler)
	registerBody := bytes.NewBufferString(`{"ticket_code":"` + ticket.Code + `","name":"win-temp","os":"windows","arch":"amd64","capabilities":["shell.user"]}`)
	registerReq := httptest.NewRequest(http.MethodPost, "/v1/hosts/register", registerBody)
	registerRec := httptest.NewRecorder()
	handler.ServeHTTP(registerRec, registerReq)
	if registerRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", registerRec.Code, registerRec.Body.String())
	}
	var registerPayload struct {
		Host model.Host `json:"host"`
	}
	if err := json.Unmarshal(registerRec.Body.Bytes(), &registerPayload); err != nil {
		t.Fatal(err)
	}
	approveReq := httptest.NewRequest(http.MethodPost, "/v1/hosts/"+registerPayload.Host.ID+"/approve", bytes.NewBufferString(`{"capabilities":["shell.user","fs.read"]}`))
	approveRec := httptest.NewRecorder()
	handler.ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", approveRec.Code, approveRec.Body.String())
	}
	var approvePayload struct {
		Host model.Host `json:"host"`
	}
	if err := json.Unmarshal(approveRec.Body.Bytes(), &approvePayload); err != nil {
		t.Fatal(err)
	}
	return approvePayload.Host
}

func httpTestKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return publicKey, privateKey
}

func httpHostIdentityFingerprint(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}
