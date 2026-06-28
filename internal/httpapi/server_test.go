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
