package wsproto

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPToWebSocketURL(t *testing.T) {
	got, err := HTTPToWebSocketURL("https://api.example.com/v1", "/v1/ws/hosts/hst_123")
	if err != nil {
		t.Fatal(err)
	}
	if got != "wss://api.example.com/v1/v1/ws/hosts/hst_123" {
		t.Fatalf("unexpected websocket URL %q", got)
	}
}

func TestUpgradeAndDialExchangeJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := Upgrade(w, r)
		if err != nil {
			t.Errorf("upgrade failed: %v", err)
			return
		}
		defer conn.Close()
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			t.Errorf("read failed: %v", err)
			return
		}
		if msg.Type != MessageReady {
			t.Errorf("unexpected message %#v", msg)
			return
		}
		_ = conn.WriteJSON(Message{Type: MessageNoop, HostID: msg.HostID})
	}))
	defer server.Close()
	wsURL, err := HTTPToWebSocketURL(server.URL, "/")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(Message{Type: MessageReady, HostID: "hst_123"}); err != nil {
		t.Fatal(err)
	}
	var msg Message
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != MessageNoop || msg.HostID != "hst_123" {
		t.Fatalf("unexpected response %#v", msg)
	}
}
