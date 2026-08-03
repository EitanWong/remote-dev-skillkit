package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
)

func TestHTTPSessionNotifySetAndClear(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	created := createHTTPSession(t, handler)
	base := "/v1/sessions/" + url.PathEscape(created.Session.ID) + "/notify"

	rec := postJSON(t, handler, base, `{"notify_url":"https://hooks.example.test/agent"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("set notify status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Session struct {
			Session struct {
				NotifyURL string `json:"notify_url"`
			} `json:"session"`
		} `json:"session"`
	}
	decodeHTTP(t, rec, &payload)
	if payload.Session.Session.NotifyURL != "https://hooks.example.test/agent" {
		t.Fatalf("notify url not persisted: %#v", payload)
	}

	// Empty notify_url clears the subscription. Use a fresh target: a
	// cleared field is absent from the JSON response (omitempty), so
	// unmarshalling into a reused struct would keep the old value.
	rec = postJSON(t, handler, base, `{"notify_url":""}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("clear notify status = %d body=%s", rec.Code, rec.Body.String())
	}
	var cleared struct {
		Session struct {
			Session struct {
				NotifyURL string `json:"notify_url"`
			} `json:"session"`
		} `json:"session"`
	}
	decodeHTTP(t, rec, &cleared)
	if cleared.Session.Session.NotifyURL != "" {
		t.Fatalf("notify url not cleared: %#v", cleared)
	}
}

func TestHTTPSessionNotifyRejectsNonHTTPS(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	created := createHTTPSession(t, handler)
	base := "/v1/sessions/" + url.PathEscape(created.Session.ID) + "/notify"

	rec := postJSON(t, handler, base, `{"notify_url":"http://hooks.example.test/agent"}`, "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "https") {
		t.Fatalf("non-https notify status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTPSessionNotifyRequiresOperatorRole(t *testing.T) {
	handler := NewServerWithOperatorAuth(gateway.NewMemoryGateway(), "", httpTestOperatorAuth(t)).Handler()
	created := createHTTPSessionWithBearer(t, handler, "operator-secret")
	base := "/v1/sessions/" + url.PathEscape(created.Session.ID) + "/notify"

	rec := postJSON(t, handler, base, `{"notify_url":"https://hooks.example.test/agent"}`, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("notify without operator status = %d body=%s, want 403", rec.Code, rec.Body.String())
	}
	rec = postJSON(t, handler, base, `{"notify_url":"https://hooks.example.test/agent"}`, "operator-secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("notify with operator status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
}

func TestHTTPSessionNotifyRejectsUnknownSession(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()

	rec := postJSON(t, handler, "/v1/sessions/ses_unknown/notify", `{"notify_url":"https://hooks.example.test/agent"}`, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session notify status = %d body=%s, want 404", rec.Code, rec.Body.String())
	}
}
