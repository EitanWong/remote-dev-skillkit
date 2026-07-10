package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestRecoverSupportSessionPublicationRestoresEveryUncommittedPhase(t *testing.T) {
	for _, phase := range []string{"journal-written", "first-artifact", "all-artifacts", "active-persisted"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			readyFile := filepath.Join(root, "ready.json")
			handoffFile := filepath.Join(root, "handoff.txt")
			journalPath := filepath.Join(root, "journal.json")
			if err := os.WriteFile(readyFile, []byte("previous-ready\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(handoffFile, []byte("previous-handoff\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			gw := gateway.NewMemoryGateway()
			store := &recordingStateStore{}
			ticket, err := gw.CreateProbingTicketWithMetadata(model.HostModeAttendedTemporary, 60, nil, "crash recovery", nil)
			if err != nil {
				t.Fatal(err)
			}
			artifacts := []*stagedSupportSessionArtifact{
				{path: readyFile, label: "ready file", data: []byte("replacement-ready\n")},
				{path: handoffFile, label: "handoff file", data: []byte("replacement-handoff\n")},
			}
			if err := stageSupportSessionArtifacts(artifacts); err != nil {
				t.Fatal(err)
			}
			if err := prepareSupportSessionArtifactBackups(artifacts); err != nil {
				t.Fatal(err)
			}
			if err := writeSupportSessionPublicationJournal(journalPath, publicationJournalFromArtifacts(ticket.ID, "publishing", artifacts)); err != nil {
				t.Fatal(err)
			}
			switch phase {
			case "first-artifact":
				if err := os.Rename(artifacts[0].path, artifacts[0].backupPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(artifacts[0].tempPath, artifacts[0].path); err != nil {
					t.Fatal(err)
				}
				artifacts[0].tempPath = ""
			case "all-artifacts", "active-persisted":
				if err := commitSupportSessionArtifacts(artifacts); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "active-persisted" {
				if _, err := gw.PublishTicket(ticket.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := store.SaveFrom(gw); err != nil {
					t.Fatal(err)
				}
			}
			if err := recoverSupportSessionPublication(gw, store, journalPath, []string{readyFile, handoffFile}); err != nil {
				t.Fatal(err)
			}
			readyContent, readyErr := os.ReadFile(readyFile)
			handoffContent, handoffErr := os.ReadFile(handoffFile)
			if readyErr != nil || string(readyContent) != "previous-ready\n" || handoffErr != nil || string(handoffContent) != "previous-handoff\n" {
				t.Fatalf("mixed artifacts survived recovery: ready=%q/%v handoff=%q/%v", readyContent, readyErr, handoffContent, handoffErr)
			}
			rolledBack, ok := gw.TicketForCode(ticket.Code)
			if !ok || rolledBack.Status != model.TicketStatusRevoked {
				t.Fatalf("recovery left active ticket: %#v, found=%v", rolledBack, ok)
			}
			if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
				t.Fatalf("recovery journal remains: %v", err)
			}
		})
	}
}

func TestRecoverSupportSessionPublicationRejectsTamperedArtifactPaths(t *testing.T) {
	root := t.TempDir()
	readyFile := filepath.Join(root, "ready.json")
	handoffFile := filepath.Join(root, "handoff.txt")
	journalPath := filepath.Join(root, "journal.json")
	gw := gateway.NewMemoryGateway()
	store := &recordingStateStore{}
	for _, testCase := range []struct {
		name      string
		artifacts []supportSessionPublicationJournalArtifact
	}{
		{name: "unexpected final path", artifacts: []supportSessionPublicationJournalArtifact{{Path: filepath.Join(root, "victim")}, {Path: handoffFile}}},
		{name: "unsafe temp prefix", artifacts: []supportSessionPublicationJournalArtifact{{Path: readyFile, TempPath: filepath.Join(root, "victim")}, {Path: handoffFile}}},
		{name: "duplicate final", artifacts: []supportSessionPublicationJournalArtifact{{Path: readyFile}, {Path: readyFile}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			journal := supportSessionPublicationJournal{SchemaVersion: supportSessionPublicationJournalSchema, TicketID: "tkt_tampered", Phase: "publishing", Artifacts: testCase.artifacts}
			if err := writeSupportSessionPublicationJournal(journalPath, journal); err != nil {
				t.Fatal(err)
			}
			if err := recoverSupportSessionPublication(gw, store, journalPath, []string{readyFile, handoffFile}); err == nil {
				t.Fatal("tampered journal accepted")
			}
		})
	}
}

func TestRecoverCommittedSupportSessionPublicationKeepsPublishedState(t *testing.T) {
	root := t.TempDir()
	readyFile := filepath.Join(root, "ready.json")
	handoffFile := filepath.Join(root, "handoff.txt")
	journalPath := filepath.Join(root, "journal.json")
	if err := os.WriteFile(readyFile, []byte("old-ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(handoffFile, []byte("old-handoff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGateway()
	store := &recordingStateStore{}
	ticket, err := gw.CreateProbingTicketWithMetadata(model.HostModeAttendedTemporary, 60, nil, "committed recovery", nil)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := []*stagedSupportSessionArtifact{
		{path: readyFile, label: "ready file", data: []byte("new-ready\n")},
		{path: handoffFile, label: "handoff file", data: []byte("new-handoff\n")},
	}
	if err := stageSupportSessionArtifacts(artifacts); err != nil {
		t.Fatal(err)
	}
	if err := prepareSupportSessionArtifactBackups(artifacts); err != nil {
		t.Fatal(err)
	}
	if err := commitSupportSessionArtifacts(artifacts); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.PublishTicket(ticket.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveFrom(gw); err != nil {
		t.Fatal(err)
	}
	if err := writeSupportSessionPublicationJournal(journalPath, publicationJournalFromArtifacts(ticket.ID, "committed", artifacts)); err != nil {
		t.Fatal(err)
	}
	if err := recoverSupportSessionPublication(gw, store, journalPath, []string{readyFile, handoffFile}); err != nil {
		t.Fatal(err)
	}
	ready, _ := os.ReadFile(readyFile)
	handoff, _ := os.ReadFile(handoffFile)
	if string(ready) != "new-ready\n" || string(handoff) != "new-handoff\n" {
		t.Fatalf("committed recovery restored old artifacts: ready=%q handoff=%q", ready, handoff)
	}
	current, _ := gw.TicketForCode(ticket.Code)
	if current.Status != model.TicketStatusActive {
		t.Fatalf("committed recovery revoked published ticket: %#v", current)
	}
}

func TestRecoverCommittedSupportSessionPublicationRestoresOldFilesForRevokedTicket(t *testing.T) {
	root := t.TempDir()
	readyFile := filepath.Join(root, "ready.json")
	handoffFile := filepath.Join(root, "handoff.txt")
	journalPath := filepath.Join(root, "journal.json")
	if err := os.WriteFile(readyFile, []byte("old-ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(handoffFile, []byte("old-handoff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGateway()
	store := &recordingStateStore{}
	ticket, err := gw.CreateProbingTicketWithMetadata(model.HostModeAttendedTemporary, 60, nil, "revoked committed recovery", nil)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := []*stagedSupportSessionArtifact{
		{path: readyFile, label: "ready file", data: []byte("new-ready\n")},
		{path: handoffFile, label: "handoff file", data: []byte("new-handoff\n")},
	}
	if err := stageSupportSessionArtifacts(artifacts); err != nil {
		t.Fatal(err)
	}
	if err := prepareSupportSessionArtifactBackups(artifacts); err != nil {
		t.Fatal(err)
	}
	if err := commitSupportSessionArtifacts(artifacts); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.PublishTicket(ticket.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := gw.RollbackTicket(ticket.ID, "commit journal sync failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveFrom(gw); err != nil {
		t.Fatal(err)
	}
	if err := writeSupportSessionPublicationJournal(journalPath, publicationJournalFromArtifacts(ticket.ID, "committed", artifacts)); err != nil {
		t.Fatal(err)
	}
	if err := recoverSupportSessionPublication(gw, store, journalPath, []string{readyFile, handoffFile}); err != nil {
		t.Fatal(err)
	}
	ready, _ := os.ReadFile(readyFile)
	handoff, _ := os.ReadFile(handoffFile)
	if string(ready) != "old-ready\n" || string(handoff) != "old-handoff\n" {
		t.Fatalf("revoked committed recovery retained new artifacts: ready=%q handoff=%q", ready, handoff)
	}
}
