package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
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
