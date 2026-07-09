package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileLockStoreAcquireStatusAndRelease(t *testing.T) {
	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	repo := t.TempDir()
	store := NewFileLockStore(filepath.Join(t.TempDir(), "locks"))

	lock, err := store.Acquire(LockOptions{
		RepoRoot:     repo,
		HostID:       "hst_1",
		TaskID:       "task_1",
		OwnerAdapter: "codex",
		TTL:          time.Hour,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if lock.SchemaVersion != LockSchemaVersion {
		t.Fatalf("unexpected schema %q", lock.SchemaVersion)
	}
	if lock.RepoRoot != canonicalForTest(t, repo) {
		t.Fatalf("expected canonical repo root, got %q", lock.RepoRoot)
	}

	status, err := store.Status(repo, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !status.Exists || status.Expired || status.Lock == nil {
		t.Fatalf("unexpected status %#v", status)
	}
	info, err := os.Stat(status.StorePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected lock file permissions 0600, got %#o", got)
	}

	released, removed, err := store.Release(repo, "task_1", false)
	if err != nil {
		t.Fatal(err)
	}
	if !removed || released.TaskID != "task_1" {
		t.Fatalf("unexpected release removed=%v lock=%#v", removed, released)
	}
	status, err = store.Status(repo, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if status.Exists {
		t.Fatalf("expected no lock after release, got %#v", status)
	}
}

func TestFileLockStoreRejectsConcurrentWriter(t *testing.T) {
	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	repo := t.TempDir()
	store := NewFileLockStore(filepath.Join(t.TempDir(), "locks"))
	if _, err := store.Acquire(LockOptions{
		RepoRoot: repo,
		HostID:   "hst_1",
		TaskID:   "task_1",
		TTL:      time.Hour,
	}, now); err != nil {
		t.Fatal(err)
	}
	_, err := store.Acquire(LockOptions{
		RepoRoot: repo,
		HostID:   "hst_1",
		TaskID:   "task_2",
		TTL:      time.Hour,
	}, now.Add(time.Minute))
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("expected ErrLocked, got %v", err)
	}
}

func TestFileLockStoreAllowsExpiredLockReplacement(t *testing.T) {
	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	repo := t.TempDir()
	store := NewFileLockStore(filepath.Join(t.TempDir(), "locks"))
	if _, err := store.Acquire(LockOptions{
		RepoRoot: repo,
		HostID:   "hst_1",
		TaskID:   "task_old",
		TTL:      time.Minute,
	}, now); err != nil {
		t.Fatal(err)
	}
	lock, err := store.Acquire(LockOptions{
		RepoRoot: repo,
		HostID:   "hst_1",
		TaskID:   "task_new",
		TTL:      time.Hour,
	}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if lock.TaskID != "task_new" {
		t.Fatalf("expected replacement lock, got %#v", lock)
	}
}

func TestFileLockStoreReleaseRequiresOwnerUnlessForced(t *testing.T) {
	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	repo := t.TempDir()
	store := NewFileLockStore(filepath.Join(t.TempDir(), "locks"))
	if _, err := store.Acquire(LockOptions{
		RepoRoot: repo,
		HostID:   "hst_1",
		TaskID:   "task_1",
		TTL:      time.Hour,
	}, now); err != nil {
		t.Fatal(err)
	}
	_, _, err := store.Release(repo, "task_2", false)
	if !errors.Is(err, ErrOwnerMismatch) {
		t.Fatalf("expected owner mismatch, got %v", err)
	}
	_, removed, err := store.Release(repo, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected forced removal")
	}
}

func TestCanonicalDirRejectsFilesAndMissingPaths(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CanonicalDir(file); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("expected file rejection, got %v", err)
	}
	if _, err := CanonicalDir(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("expected missing path rejection")
	}
}

func canonicalForTest(t *testing.T, path string) string {
	t.Helper()
	canonical, err := CanonicalDir(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}
