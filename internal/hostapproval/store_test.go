package hostapproval

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestMemoryStoreRejectsConsumedToken(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	token := approvalTokenForTest(t, now)
	store := NewMemoryStore()
	if err := store.Consume(token, now); err != nil {
		t.Fatal(err)
	}
	if err := store.Consume(token, now); !errors.Is(err, model.ErrApprovalTokenConsumed) {
		t.Fatalf("expected consumed token rejection, got %v", err)
	}
}

func TestMemoryStorePrunesExpiredTokens(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	token := approvalTokenForTest(t, now)
	store := NewMemoryStore()
	if err := store.Consume(token, now); err != nil {
		t.Fatal(err)
	}
	if err := store.Consume(token, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("expected expired approval token consumption entry to be pruned, got %v", err)
	}
}

func TestFileStorePersistsAndRejectsConsumedToken(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	token := approvalTokenForTest(t, now)
	path := filepath.Join(t.TempDir(), "approval", "store.json")
	store := FileStore{Path: path}
	if err := store.Consume(token, now); err != nil {
		t.Fatal(err)
	}
	if err := store.Consume(token, now); !errors.Is(err, model.ErrApprovalTokenConsumed) {
		t.Fatalf("expected consumed token rejection, got %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected 0600 permissions, got %#o", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("expected 0700 directory permissions, got %#o", got)
	}
}

func approvalTokenForTest(t *testing.T, now time.Time) model.ApprovalToken {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	token, err := model.NewApprovalToken(model.ApprovalTokenSpec{
		JobID:        "job_1",
		HostID:       "hst_1",
		ApprovalID:   "git.push",
		Operation:    "git.push",
		SigningKeyID: "gateway-dev",
		ExpiresAt:    now.Add(time.Minute),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	token, err = token.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return token
}
