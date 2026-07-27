package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
)

func TestLegacyProtocolRoutesAreNotRegistered(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	for _, request := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/v1/tickets"},
		{method: http.MethodGet, path: "/v1/tickets/legacy/manifest"},
		{method: http.MethodGet, path: "/join/legacy"},
		{method: http.MethodGet, path: "/join/legacy/bootstrap.sh"},
		{method: http.MethodGet, path: "/v1/support-session/bootstrap-probe.ps1"},
		{method: http.MethodPost, path: "/v1/support-session/preconnect"},
		{method: http.MethodGet, path: "/v1/support-session/status"},
		{method: http.MethodGet, path: "/v1/enrollment/revocations"},
		{method: http.MethodPost, path: "/v1/enrollment/certificates"},
		{method: http.MethodPost, path: "/v1/enrollment/certificates/renew"},
	} {
		t.Run(request.method+" "+request.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(request.method, request.path, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("legacy route %s %s returned %d, want 404", request.method, request.path, rec.Code)
			}
		})
	}
}
