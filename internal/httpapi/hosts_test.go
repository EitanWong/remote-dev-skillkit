package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
)

func TestHTTPHostsListAndRename(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	session, err := gw.CreateSession(controlplane.SessionSpec{Reason: "http hosts fixture", JoinPolicy: "single-target"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := gw.JoinSessionByCode(session.JoinCode, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Name:                "dev-win",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-win",
		Transport:           controlplane.TransportLongPoll,
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewServerWithOperatorAuth(gw, "", httpTestOperatorAuth(t)).Handler()

	listRec := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/v1/hosts", nil)
	listReq.Header.Set("Authorization", "Bearer operator-secret")
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list hosts status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listPayload struct {
		Hosts []controlplane.Host `json:"hosts"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Hosts) != 1 || listPayload.Hosts[0].IdentityFingerprint != "fp-win" {
		t.Fatalf("list hosts = %#v", listPayload.Hosts)
	}

	hostID := listPayload.Hosts[0].HostID
	renameRec := httptest.NewRecorder()
	renameReq := httptest.NewRequest(http.MethodPost, "/v1/hosts/rename", bytes.NewBufferString(`{"host_id":"`+hostID+`","display_name":"Unity Dev Machine"}`))
	renameReq.Header.Set("Authorization", "Bearer operator-secret")
	handler.ServeHTTP(renameRec, renameReq)
	if renameRec.Code != http.StatusOK {
		t.Fatalf("rename host status = %d body=%s", renameRec.Code, renameRec.Body.String())
	}
	var renamePayload struct {
		Host controlplane.Host `json:"host"`
	}
	if err := json.Unmarshal(renameRec.Body.Bytes(), &renamePayload); err != nil {
		t.Fatal(err)
	}
	if renamePayload.Host.DisplayName != "Unity Dev Machine" {
		t.Fatalf("renamed host = %#v", renamePayload.Host)
	}
}

func TestHTTPHostsRequireOperatorAuth(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	handler := NewServerWithOperatorAuth(gw, "", httpTestOperatorAuth(t)).Handler()

	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/v1/hosts", nil))
	if listRec.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated list status = %d, want 403", listRec.Code)
	}

	renameRec := httptest.NewRecorder()
	handler.ServeHTTP(renameRec, httptest.NewRequest(http.MethodPost, "/v1/hosts/rename", bytes.NewBufferString(`{"host_id":"hst_x","display_name":"n"}`)))
	if renameRec.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated rename status = %d, want 403", renameRec.Code)
	}
}

func TestHTTPHostsRenameUnknownHost(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	handler := NewServerWithOperatorAuth(gw, "", httpTestOperatorAuth(t)).Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/hosts/rename", bytes.NewBufferString(`{"host_id":"hst_missing","display_name":"name"}`))
	req.Header.Set("Authorization", "Bearer operator-secret")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("rename unknown host status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}
