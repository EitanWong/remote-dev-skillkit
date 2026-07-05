package adapterkit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var wsNow = time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)

func tmpWorkspaceRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

func TestPrepareWorkspaceSessionRequiresAdapter(t *testing.T) {
	root := tmpWorkspaceRoot(t)
	_, err := PrepareWorkspaceSession("", "", root, WorkspaceSessionOptions{}, wsNow)
	if err == nil {
		t.Fatal("expected error for missing adapter")
	}
}

func TestPrepareWorkspaceSessionRequiresRoot(t *testing.T) {
	_, err := PrepareWorkspaceSession("my-adapter", "", "", WorkspaceSessionOptions{}, wsNow)
	if err == nil {
		t.Fatal("expected error for missing workspace root")
	}
}

func TestPrepareWorkspaceSessionRejectsNonExistentRoot(t *testing.T) {
	_, err := PrepareWorkspaceSession("my-adapter", "", "/nonexistent/path/xyz", WorkspaceSessionOptions{}, wsNow)
	if err == nil {
		t.Fatal("expected error for nonexistent workspace root")
	}
}

func TestPrepareWorkspaceSessionRejectsFileRoot(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir.txt")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := PrepareWorkspaceSession("my-adapter", "", file, WorkspaceSessionOptions{}, wsNow)
	if err == nil {
		t.Fatal("expected error for file root")
	}
}

func TestPrepareWorkspaceSessionSetsSessionID(t *testing.T) {
	root := tmpWorkspaceRoot(t)
	session, err := PrepareWorkspaceSession("my-adapter", "job_1", root, WorkspaceSessionOptions{}, wsNow)
	if err != nil {
		t.Fatalf("PrepareWorkspaceSession: %v", err)
	}
	if session.SchemaVersion != WorkspaceSessionSchemaVersion {
		t.Fatalf("schema = %q", session.SchemaVersion)
	}
	if session.SessionID == "" {
		t.Fatal("session id must be set")
	}
	if session.WorkspaceRoot == "" {
		t.Fatal("workspace root must be set")
	}
	if session.PreparedAt == "" {
		t.Fatal("prepared_at must be set")
	}
	if session.CleanedUp {
		t.Fatal("cleaned_up must be false initially")
	}
}

func TestPrepareWorkspaceSessionSetsLockPath(t *testing.T) {
	root := tmpWorkspaceRoot(t)
	lockDir := t.TempDir()
	session, err := PrepareWorkspaceSession("my-adapter", "", root, WorkspaceSessionOptions{LockDir: lockDir}, wsNow)
	if err != nil {
		t.Fatal(err)
	}
	if session.LockPath == "" {
		t.Fatal("lock path should be set when lock dir is configured")
	}
}

func TestPrepareWorkspaceSessionRejectsEscapingBoundary(t *testing.T) {
	root := tmpWorkspaceRoot(t)
	_, err := PrepareWorkspaceSession("my-adapter", "", root, WorkspaceSessionOptions{
		WriteBoundaries: []string{"../outside"},
	}, wsNow)
	if err == nil {
		t.Fatal("expected error for escaping write boundary")
	}
}

func TestPrepareWorkspaceSessionAcceptsRelativeBoundary(t *testing.T) {
	root := tmpWorkspaceRoot(t)
	sub := filepath.Join(root, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	session, err := PrepareWorkspaceSession("my-adapter", "", root, WorkspaceSessionOptions{
		WriteBoundaries: []string{"src"},
	}, wsNow)
	if err != nil {
		t.Fatalf("PrepareWorkspaceSession: %v", err)
	}
	if len(session.WriteBoundaries) != 1 {
		t.Fatalf("expected 1 boundary, got %d", len(session.WriteBoundaries))
	}
}

func TestMarkCleanedSetsFlag(t *testing.T) {
	root := tmpWorkspaceRoot(t)
	session, err := PrepareWorkspaceSession("my-adapter", "", root, WorkspaceSessionOptions{}, wsNow)
	if err != nil {
		t.Fatal(err)
	}
	cleaned := session.MarkCleaned(wsNow)
	if !cleaned.CleanedUp {
		t.Fatal("cleaned_up should be true after MarkCleaned")
	}
	if cleaned.CleanedUpAt == "" {
		t.Fatal("cleaned_up_at must be set")
	}
}

func TestVerifyWorkspaceSessionJSONAcceptsValidSession(t *testing.T) {
	root := tmpWorkspaceRoot(t)
	session, err := PrepareWorkspaceSession("my-adapter", "job_1", root, WorkspaceSessionOptions{
		LockDir:         t.TempDir(),
		WriteBoundaries: []string{root},
	}, wsNow)
	if err != nil {
		t.Fatal(err)
	}
	cleaned := session.MarkCleaned(wsNow)
	content, err := json.Marshal(cleaned)
	if err != nil {
		t.Fatal(err)
	}
	report := VerifyWorkspaceSessionJSON(content, WorkspaceSessionContract{
		Adapter:           "my-adapter",
		RequireLock:       true,
		RequireBoundaries: true,
		RequireCleaned:    true,
	})
	if !report.OK {
		t.Fatalf("expected OK, checks: %#v", report.Checks)
	}
}

func TestVerifyWorkspaceSessionJSONRejectsWrongAdapter(t *testing.T) {
	root := tmpWorkspaceRoot(t)
	session, err := PrepareWorkspaceSession("my-adapter", "", root, WorkspaceSessionOptions{}, wsNow)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	report := VerifyWorkspaceSessionJSON(content, WorkspaceSessionContract{Adapter: "other-adapter"})
	if report.OK {
		t.Fatal("expected failure for wrong adapter")
	}
}

func TestVerifyWorkspaceSessionJSONFailsWhenCleanupMissing(t *testing.T) {
	root := tmpWorkspaceRoot(t)
	session, err := PrepareWorkspaceSession("my-adapter", "", root, WorkspaceSessionOptions{}, wsNow)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	report := VerifyWorkspaceSessionJSON(content, WorkspaceSessionContract{
		Adapter:        "my-adapter",
		RequireCleaned: true,
	})
	if report.OK {
		t.Fatal("expected failure when cleanup required but not performed")
	}
}
