package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestExportAndVerifyChain(t *testing.T) {
	generatedAt := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	events := auditEventsForTest(generatedAt)
	chain, err := ExportChain(events, generatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if chain.SchemaVersion != ChainSchemaVersion {
		t.Fatalf("unexpected schema %q", chain.SchemaVersion)
	}
	if chain.EventCount != len(events) {
		t.Fatalf("expected %d events, got %d", len(events), chain.EventCount)
	}
	if chain.RootHash == "" {
		t.Fatal("root hash is required")
	}
	if err := VerifyChain(chain); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyChainRejectsTamperedEvent(t *testing.T) {
	generatedAt := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	chain, err := ExportChain(auditEventsForTest(generatedAt), generatedAt)
	if err != nil {
		t.Fatal(err)
	}
	chain.Entries[0].Event.Message = "tampered"
	if err := VerifyChain(chain); err == nil {
		t.Fatal("expected tampered event to fail verification")
	}
}

func TestExportChainFromJSONL(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "events.jsonl")
	chainPath := filepath.Join(dir, "audit-chain.json")
	store := NewJSONLStore(jsonlPath)
	generatedAt := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	for _, event := range auditEventsForTest(generatedAt) {
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	chain, err := ExportChainFromJSONL(jsonlPath, chainPath, generatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyChain(chain); err != nil {
		t.Fatal(err)
	}
	loaded, err := ReadChain(chainPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyChain(loaded); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(chainPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected chain file 0600 permissions, got %#o", got)
	}
}

func auditEventsForTest(at time.Time) []model.AuditEvent {
	return []model.AuditEvent{
		{
			Sequence: 1,
			Actor:    "operator",
			Action:   "ticket.create",
			TargetID: "tkt_1",
			Message:  "created",
			At:       at,
		},
		{
			Sequence: 2,
			Actor:    "host",
			Action:   "host.register",
			TargetID: "hst_1",
			Message:  "registered",
			At:       at.Add(time.Second),
		},
	}
}
