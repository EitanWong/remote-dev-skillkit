package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestJSONLStoreAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit", "events.jsonl")
	store := NewJSONLStore(path)
	event := model.AuditEvent{
		Sequence: 1,
		Actor:    "operator",
		Action:   "ticket.create",
		TargetID: "tkt_123",
		Message:  "created",
		At:       time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC),
	}

	if err := store.Append(event); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded model.AuditEvent
	if err := json.Unmarshal(content[:len(content)-1], &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Action != event.Action {
		t.Fatalf("expected action %q, got %q", event.Action, decoded.Action)
	}
}
