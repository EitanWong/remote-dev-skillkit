package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
)

func TestWebHandoffLinkClaimsBootstrapAndDeliversVerifiedHostBinary(t *testing.T) {
	asset, err := NewWindowsAMD64WebHandoffAsset("rdev-host.exe", []byte("MZ-web-handoff-fixture"))
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGateway()
	session, err := gw.CreateSession(controlplane.SessionSpec{
		Profile:            "managed",
		Reason:             "web handoff fixture",
		JoinPolicy:         "single-target",
		SelectedGatewayURL: "https://remote.example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServerWithWebHandoff(gw, WebHandoffOptions{
		PublicBaseURL: "https://remote.example.test",
		WindowsAMD64:  asset,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()

	created := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(session.ID)+"/host-handoffs", `{"platform":"windows-amd64","expires_in_ms":3600000}`, "")
	if created.Code != http.StatusCreated {
		t.Fatalf("create web handoff status = %d body=%s", created.Code, created.Body.String())
	}
	var handoff struct {
		Handoff struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"handoff"`
	}
	decodeHTTP(t, created, &handoff)
	link, err := url.Parse(handoff.Handoff.URL)
	if err != nil {
		t.Fatal(err)
	}
	if link.Scheme != "https" || link.Host != "remote.example.test" || link.Fragment == "" || link.RawQuery != "" {
		t.Fatalf("unexpected handoff link %q", handoff.Handoff.URL)
	}

	pageReq := httptest.NewRequest(http.MethodGet, link.Path, nil)
	pageRec := httptest.NewRecorder()
	handler.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("handoff page status = %d body=%s", pageRec.Code, pageRec.Body.String())
	}
	if strings.Contains(pageRec.Body.String(), session.JoinCode) || strings.Contains(pageRec.Body.String(), link.Fragment) {
		t.Fatal("handoff page must not expose the join code or fragment proof")
	}
	if pageRec.Header().Get("Content-Security-Policy") == "" || pageRec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("handoff page missing security headers: %#v", pageRec.Header())
	}

	claim := postJSON(t, handler, link.Path+"/claim", `{"proof":"`+link.Fragment+`"}`, "")
	if claim.Code != http.StatusOK {
		t.Fatalf("claim status = %d body=%s", claim.Code, claim.Body.String())
	}
	var claimed struct {
		Bootstrap string `json:"bootstrap"`
	}
	decodeHTTP(t, claim, &claimed)
	if !strings.Contains(claimed.Bootstrap, session.JoinCode) || !strings.Contains(claimed.Bootstrap, "X-Rdev-Handoff-Ticket") {
		t.Fatalf("bootstrap did not contain scoped connection material: %s", claimed.Bootstrap)
	}
	if strings.Contains(claimed.Bootstrap, "operator-secret") {
		t.Fatal("bootstrap leaked an operator credential")
	}
	ticketMatch := regexp.MustCompile(`(?m)^\$artifactTicket = '([^']+)'$`).FindStringSubmatch(claimed.Bootstrap)
	if len(ticketMatch) != 2 {
		t.Fatalf("bootstrap did not contain an artifact ticket assignment: %s", claimed.Bootstrap)
	}

	artifactReq := httptest.NewRequest(http.MethodGet, link.Path+"/rdev-host.exe", nil)
	artifactReq.Header.Set("X-Rdev-Handoff-Ticket", ticketMatch[1])
	artifactRec := httptest.NewRecorder()
	handler.ServeHTTP(artifactRec, artifactReq)
	if artifactRec.Code != http.StatusOK || artifactRec.Body.String() != "MZ-web-handoff-fixture" {
		t.Fatalf("artifact delivery status=%d body=%q", artifactRec.Code, artifactRec.Body.String())
	}
	if artifactRec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("artifact response missing no-store: %#v", artifactRec.Header())
	}

	replayed := postJSON(t, handler, link.Path+"/claim", `{"proof":"`+link.Fragment+`"}`, "")
	if replayed.Code != http.StatusGone {
		t.Fatalf("second claim status = %d, want 410: %s", replayed.Code, replayed.Body.String())
	}
}

func TestWebHandoffRequiresConfiguredAssetAndMatchingGateway(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	session, err := gw.CreateSession(controlplane.SessionSpec{SelectedGatewayURL: "https://another.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServerWithWebHandoff(gw, WebHandoffOptions{PublicBaseURL: "https://remote.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	response := postJSON(t, server.Handler(), "/v1/sessions/"+url.PathEscape(session.ID)+"/host-handoffs", `{"platform":"windows-amd64"}`, "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured asset status = %d, want 503: %s", response.Code, response.Body.String())
	}

	asset, err := NewWindowsAMD64WebHandoffAsset("rdev-host.exe", []byte("MZ"))
	if err != nil {
		t.Fatal(err)
	}
	server, err = NewServerWithWebHandoff(gw, WebHandoffOptions{PublicBaseURL: "https://remote.example.test", WindowsAMD64: asset})
	if err != nil {
		t.Fatal(err)
	}
	response = postJSON(t, server.Handler(), "/v1/sessions/"+url.PathEscape(session.ID)+"/host-handoffs", `{"platform":"windows-amd64","expires_in_ms":60000}`, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("mismatched gateway status = %d, want 400: %s", response.Code, response.Body.String())
	}
}

func TestWebHandoffClaimRejectsExpiredProof(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	asset, err := NewWindowsAMD64WebHandoffAsset("rdev-host.exe", []byte("MZ"))
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	session, err := gw.CreateSession(controlplane.SessionSpec{SelectedGatewayURL: "https://remote.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServerWithWebHandoff(gw, WebHandoffOptions{PublicBaseURL: "https://remote.example.test", WindowsAMD64: asset})
	if err != nil {
		t.Fatal(err)
	}
	created := postJSON(t, server.Handler(), "/v1/sessions/"+url.PathEscape(session.ID)+"/host-handoffs", `{"platform":"windows-amd64","expires_in_ms":60000}`, "")
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", created.Code, created.Body.String())
	}
	var response struct {
		Handoff struct {
			URL string `json:"url"`
		} `json:"handoff"`
	}
	if err := json.NewDecoder(created.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	link, err := url.Parse(response.Handoff.URL)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(61 * time.Second)
	claim := postJSON(t, server.Handler(), link.Path+"/claim", `{"proof":"`+link.Fragment+`"}`, "")
	if claim.Code != http.StatusGone {
		t.Fatalf("expired claim status = %d, want 410: %s", claim.Code, claim.Body.String())
	}
}

func TestWebHandoffClaimRevalidatesSessionBeforeIssuingBootstrap(t *testing.T) {
	asset, err := NewWindowsAMD64WebHandoffAsset("rdev-host.exe", []byte("MZ"))
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGateway()
	session, err := gw.CreateSession(controlplane.SessionSpec{
		Profile:            "managed",
		Reason:             "claim revalidation fixture",
		JoinPolicy:         "single-target",
		SelectedGatewayURL: "https://remote.example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServerWithWebHandoff(gw, WebHandoffOptions{
		PublicBaseURL: "https://remote.example.test",
		WindowsAMD64:  asset,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	created := postJSON(t, handler, "/v1/sessions/"+url.PathEscape(session.ID)+"/host-handoffs", `{"platform":"windows-amd64"}`, "")
	if created.Code != http.StatusCreated {
		t.Fatalf("create web handoff status = %d body=%s", created.Code, created.Body.String())
	}
	var response struct {
		Handoff struct {
			URL string `json:"url"`
		} `json:"handoff"`
	}
	decodeHTTP(t, created, &response)
	link, err := url.Parse(response.Handoff.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := gw.JoinSessionByCode(session.JoinCode, controlplane.EndpointSpec{
		Role: controlplane.EndpointRoleTarget,
	}); err != nil {
		t.Fatal(err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		claim := postJSON(t, handler, link.Path+"/claim", `{"proof":"`+link.Fragment+`"}`, "")
		if claim.Code != http.StatusConflict {
			t.Fatalf("claim attempt %d status = %d, want 409: %s", attempt, claim.Code, claim.Body.String())
		}
		if strings.Contains(claim.Body.String(), "bootstrap") {
			t.Fatalf("claim attempt %d issued a bootstrap: %s", attempt, claim.Body.String())
		}
	}
}
