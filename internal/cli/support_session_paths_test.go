package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

func TestSupportSessionArtifactCommitRestoresPreviousFilesAfterHandoffCommitFailure(t *testing.T) {
	root := t.TempDir()
	readyFile := filepath.Join(root, "ready.json")
	handoffFile := filepath.Join(root, "handoff.txt")
	if err := os.WriteFile(readyFile, []byte("previous-ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(handoffFile, []byte("previous-handoff\n"), 0o600); err != nil {
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
	if err := os.Remove(artifacts[1].tempPath); err != nil {
		t.Fatal(err)
	}
	if err := commitSupportSessionArtifacts(artifacts); err == nil || !strings.Contains(err.Error(), "publish handoff file") {
		t.Fatalf("expected handoff commit failure, got %v", err)
	}
	if err := restoreSupportSessionArtifacts(artifacts); err != nil {
		t.Fatal(err)
	}
	readyContent, readyErr := os.ReadFile(readyFile)
	handoffContent, handoffErr := os.ReadFile(handoffFile)
	if readyErr != nil || string(readyContent) != "previous-ready\n" {
		t.Fatalf("previous ready file was not restored: content=%q err=%v", readyContent, readyErr)
	}
	if handoffErr != nil || string(handoffContent) != "previous-handoff\n" {
		t.Fatalf("previous handoff file was not restored: content=%q err=%v", handoffContent, handoffErr)
	}
}

func TestPrepareAndValidateSupportSessionPublicationPathsRejectsCollisionsAndUnsafePaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows path protection is covered by ACL-specific tunnel tests")
	}
	t.Run("same path", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "same")
		if err := prepareAndValidateSupportSessionPublicationPaths([]string{path, path}); err == nil {
			t.Fatal("same ready and handoff path accepted")
		}
	})
	t.Run("state collision", func(t *testing.T) {
		root := t.TempDir()
		state := filepath.Join(root, "state.json")
		if err := prepareAndValidateSupportSessionPublicationPaths([]string{filepath.Join(root, "ready.json"), state, state}); err == nil {
			t.Fatal("handoff and state collision accepted")
		}
	})
	t.Run("shared directory", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "shared")
		if err := os.Mkdir(root, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(root, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := prepareAndValidateSupportSessionPublicationPaths([]string{filepath.Join(root, "ready.json")}); err == nil {
			t.Fatal("shared writable publication directory accepted")
		}
	})
	t.Run("symlink directory", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if err := prepareAndValidateSupportSessionPublicationPaths([]string{filepath.Join(link, "ready.json")}); err == nil {
			t.Fatal("symlink publication directory accepted")
		}
	})
	t.Run("existing loose file", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "ready.json")
		if err := os.WriteFile(path, []byte("sentinel"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := prepareAndValidateSupportSessionPublicationPaths([]string{path}); err == nil {
			t.Fatal("0644 existing publication file accepted")
		}
	})
}

func supportSessionAvailabilityForTests(providerIDs ...string) tunnel.AvailabilitySet {
	set := tunnel.AvailabilitySet{SchemaVersion: tunnel.AvailabilitySchemaVersion, Region: tunnel.RegionGlobal}
	for _, providerID := range providerIDs {
		set.Candidates = append(set.Candidates, tunnel.Candidate{ProviderID: providerID, URL: "https://" + providerID + ".example.test"})
		set.Attempts = append(set.Attempts, tunnel.Attempt{ProviderID: providerID, Status: tunnel.AttemptHealthy})
	}
	return set
}

func recordedSnapshotHasRevokedTicket(snapshots []gateway.Snapshot) bool {
	for _, snapshot := range snapshots {
		for _, ticket := range snapshot.Tickets {
			if ticket.Status == model.TicketStatusRevoked {
				return true
			}
		}
	}
	return false
}

func validStartedPayloadForPublicationTest() map[string]any {
	return map[string]any{
		"schema_version":          "rdev.support-session-started.v1",
		"target_command":          "safe-placeholder",
		"target_handoff_envelope": map[string]any{"full_text": "safe placeholder handoff"},
	}
}
