package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
)

func TestSetSessionNotifyURLValidatesHTTPS(t *testing.T) {
	gw := NewMemoryGateway()
	session, err := gw.CreateSession(controlplane.SessionSpec{Reason: "notify"})
	if err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{"http://hooks.example.test/x", "not a url", "https://", "ftp://x.example.test/y"} {
		if err := gw.SetSessionNotifyURL(session.ID, bad); err == nil {
			t.Fatalf("notify url %q accepted", bad)
		}
	}
	if err := gw.SetSessionNotifyURL(session.ID, "https://hooks.example.test/agent"); err != nil {
		t.Fatal(err)
	}
	if err := gw.SetSessionNotifyURL(session.ID, ""); err != nil {
		t.Fatalf("clear failed: %v", err)
	}
	if err := gw.SetSessionNotifyURL("ses_unknown", "https://hooks.example.test/x"); err == nil {
		t.Fatal("unknown session accepted")
	}
}

// TestNotifyDeliversEventsOverHTTPS proves the full push path: appended
// session events reach the registered webhook with the notification contract.
func TestNotifyDeliversEventsOverHTTPS(t *testing.T) {
	var mu sync.Mutex
	var received []map[string]any
	var wg sync.WaitGroup
	webhook := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("webhook decode: %v", err)
			return
		}
		received = append(received, payload)
		wg.Done()
	}))
	defer webhook.Close()
	// The delivery client uses the default transport; trust the test cert.
	previous := http.DefaultTransport
	http.DefaultTransport = webhook.Client().Transport
	defer func() { http.DefaultTransport = previous }()

	gw := NewMemoryGateway()
	session, err := gw.CreateSession(controlplane.SessionSpec{Reason: "notify https"})
	if err != nil {
		t.Fatal(err)
	}
	if err := gw.SetSessionNotifyURL(session.ID, webhook.URL); err != nil {
		t.Fatalf("register notify url: %v", err)
	}

	// Join = hello event -> one push.
	wg.Add(1)
	if _, _, _, err := gw.JoinSession(session.ID, controlplane.EndpointSpec{Role: controlplane.EndpointRoleTarget, IdentityFingerprint: "fp-notify"}); err != nil {
		t.Fatal(err)
	}
	// Append a status event -> second push.
	wg.Add(1)
	if _, err := gw.AppendSessionEvent(session.ID, controlplane.Event{
		Type:           controlplane.EventTypeStatus,
		FromEndpointID: "end_x",
		Payload:        map[string]any{"state": "offline"},
	}); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("webhook received %d notifications, want 2", len(received))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(received))
	}
	var hello, status map[string]any
	for _, notification := range received {
		switch notification["type"] {
		case "hello":
			hello = notification
		case "status":
			status = notification
		}
	}
	if hello == nil || hello["schema_version"] != "rdev.notification.v1" || hello["session_id"] != session.ID {
		t.Fatalf("unexpected hello notification: %#v", hello)
	}
	if status == nil || status["seq"].(float64) != 2 {
		t.Fatalf("unexpected status notification: %#v", status)
	}
	if _, hasPayload := status["payload"].(map[string]any); !hasPayload {
		t.Fatalf("notification missing payload: %#v", status)
	}
}
