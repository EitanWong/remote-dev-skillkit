package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
)

// TestSessionHostUpdateArtifactServedUnderEndpointLease pins the host-update
// artifact route: the configured Windows connector is served only to the
// enrolled target under its endpoint lease, with digest metadata and an audit
// record, and no other credential can fetch it.
func TestSessionHostUpdateArtifactServedUnderEndpointLease(t *testing.T) {
	asset, err := NewWindowsAMD64WebHandoffAsset("rdev-host.exe", []byte("MZ-host-update-fixture"))
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGateway()
	server, err := NewServerWithWebHandoff(gw, WebHandoffOptions{
		PublicBaseURL: "https://remote.example.test",
		WindowsAMD64:  asset,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	created := createHTTPSession(t, handler)
	joined := joinHTTPSession(t, handler, created.Session.JoinCode)
	path := "/v1/sessions/" + url.PathEscape(created.Session.ID) + "/artifacts/host-update"

	// Missing or mismatched lease is rejected before any bytes leave.
	unauthorized := []struct {
		name       string
		endpointID string
		secret     string
	}{
		{"no-credentials", joined.Endpoint.ID, ""},
		{"wrong-endpoint", "end_wrong", joined.Lease.Secret},
	}
	for _, probe := range unauthorized {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set(hostUpdateEndpointHeader, probe.endpointID)
		if probe.secret != "" {
			req.Header.Set("Authorization", "Bearer "+probe.secret)
		}
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
			t.Fatalf("%s: status = %d body=%s", probe.name, rec.Code, rec.Body.String())
		}
		if rec.Code == http.StatusOK {
			t.Fatalf("%s: unauthorized fetch must not serve the artifact", probe.name)
		}
	}

	// The enrolled target under its lease receives the exact artifact.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+joined.Lease.Secret)
	req.Header.Set(hostUpdateEndpointHeader, joined.Endpoint.ID)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authorized fetch status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "MZ-host-update-fixture" {
		t.Fatalf("artifact body = %q", rec.Body.String())
	}
	if rec.Header().Get("X-Rdev-Sha256") != asset.SHA256 {
		t.Fatalf("digest header = %q want %q", rec.Header().Get("X-Rdev-Sha256"), asset.SHA256)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("artifact response must not be cached: %#v", rec.Header())
	}

	audited := false
	for _, event := range gw.AuditEvents() {
		if event.Action == "session.host-update.fetch" && event.TargetID == joined.Endpoint.ID {
			audited = true
		}
	}
	if !audited {
		t.Fatalf("host-update artifact fetch must be audited: %#v", gw.AuditEvents())
	}
}
