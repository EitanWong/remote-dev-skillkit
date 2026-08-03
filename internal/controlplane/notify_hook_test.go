package controlplane

import (
	"strings"
	"testing"
	"time"
)

func TestEventHookFiresOnAppend(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	store := NewMemoryStore(func() time.Time { return now })

	var got []Event
	var gotURLs []string
	var gotSecrets []string
	store.SetEventHook(func(sessionID string, event Event, notifyURL, notifySecret string) {
		got = append(got, event)
		gotURLs = append(gotURLs, notifyURL)
		gotSecrets = append(gotSecrets, notifySecret)
	})

	session, err := store.CreateSession(SessionSpec{Reason: "hook test", NotifyURL: "https://hooks.example.test/session"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.JoinSession(session.ID, EndpointSpec{Role: EndpointRoleTarget, IdentityFingerprint: "fp-hook"}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != EventTypeHello {
		t.Fatalf("expected hello hook, got %#v", got)
	}
	if len(gotURLs) != 1 || gotURLs[0] != "https://hooks.example.test/session" {
		t.Fatalf("hook notify URL wrong: %#v", gotURLs)
	}
	if len(gotSecrets) != 1 || gotSecrets[0] != "" {
		t.Fatalf("hook secret wrong: %#v", gotSecrets)
	}
}

func TestEventHookSkipsIdempotentReplay(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	store := NewMemoryStore(func() time.Time { return now })

	var count int
	store.SetEventHook(func(string, Event, string, string) { count++ })

	session, err := store.CreateSession(SessionSpec{Reason: "replay test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.JoinSession(session.ID, EndpointSpec{Role: EndpointRoleTarget, IdentityFingerprint: "fp-replay"}); err != nil {
		t.Fatal(err)
	}
	event := Event{Type: EventTypeStatus, FromEndpointID: "end_x", IdempotencyKey: "same-key", Payload: map[string]any{"state": "online"}}
	if _, err := store.AppendEvent(session.ID, event); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(session.ID, event); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected hook for join + first append only, got %d", count)
	}
}

func TestSetSessionNotifyURL(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	store := NewMemoryStore(func() time.Time { return now })
	session, err := store.CreateSession(SessionSpec{Reason: "notify test"})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.SetSessionNotifyURL(session.ID, "  https://hooks.example.test/agent  ", "topsecret")
	if err != nil {
		t.Fatal(err)
	}
	if updated.NotifyURL != "https://hooks.example.test/agent" {
		t.Fatalf("notify url not trimmed: %q", updated.NotifyURL)
	}
	if updated.NotifySecret != "topsecret" {
		t.Fatalf("notify secret not stored: %q", updated.NotifySecret)
	}
	if snapshot := updated.Snapshot(); snapshot.Session.NotifySecret != "" {
		t.Fatalf("snapshot leaks notify secret: %q", snapshot.Session.NotifySecret)
	}
	cleared, err := store.SetSessionNotifyURL(session.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.NotifyURL != "" || cleared.NotifySecret != "" {
		t.Fatalf("notify not cleared: %q/%q", cleared.NotifyURL, cleared.NotifySecret)
	}
	if _, err := store.SetSessionNotifyURL("ses_unknown", "https://x.example.test", ""); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown session should fail, got %v", err)
	}
}
