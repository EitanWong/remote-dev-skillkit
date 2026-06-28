package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

	failReq := httptest.NewRequest(http.MethodPost, "/v1/jobs/"+jobPayload.Job.ID+"/fail", bytes.NewBufferString(`{"host_id":"`+host.ID+`","reason":"signature rejected"}`))
	failRec := httptest.NewRecorder()
	handler.ServeHTTP(failRec, failReq)
	if failRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", failRec.Code, failRec.Body.String())
	}
	var failPayload struct {
		Job model.Job `json:"job"`
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
