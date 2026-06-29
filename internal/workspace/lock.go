package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	LockSchemaVersion = "rdev.workspace-lock.v1"
	DefaultLockTTL    = 2 * time.Hour
)

var (
	ErrLocked        = errors.New("workspace locked")
	ErrNotLocked     = errors.New("workspace lock not found")
	ErrOwnerMismatch = errors.New("workspace lock owner mismatch")
)

type Lock struct {
	SchemaVersion string    `json:"schema_version"`
	LockID        string    `json:"lock_id"`
	HostID        string    `json:"host_id"`
	JobID         string    `json:"job_id"`
	RepoRoot      string    `json:"repo_root"`
	WorktreePath  string    `json:"worktree_path,omitempty"`
	BaseRef       string    `json:"base_ref,omitempty"`
	Branch        string    `json:"branch,omitempty"`
	OwnerAdapter  string    `json:"owner_adapter,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type LockOptions struct {
	StoreDir     string
	RepoRoot     string
	HostID       string
	JobID        string
	WorktreePath string
	BaseRef      string
	Branch       string
	OwnerAdapter string
	TTL          time.Duration
}

type LockStatus struct {
	StorePath string `json:"store_path"`
	Exists    bool   `json:"exists"`
	Expired   bool   `json:"expired"`
	Lock      *Lock  `json:"lock,omitempty"`
}

type FileLockStore struct {
	Dir string
}

func NewFileLockStore(dir string) FileLockStore {
	return FileLockStore{Dir: dir}
}

func DefaultStoreDir(repoRoot string) (string, error) {
	root, err := CanonicalDir(repoRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ".rdev", "workspace-locks"), nil
}

func (s FileLockStore) Acquire(opts LockOptions, now time.Time) (Lock, error) {
	if strings.TrimSpace(opts.JobID) == "" {
		return Lock{}, fmt.Errorf("job id is required")
	}
	if strings.TrimSpace(opts.HostID) == "" {
		return Lock{}, fmt.Errorf("host id is required")
	}
	root, err := CanonicalDir(opts.RepoRoot)
	if err != nil {
		return Lock{}, err
	}
	storeDir := firstNonEmpty(opts.StoreDir, s.Dir)
	if storeDir == "" {
		storeDir, err = DefaultStoreDir(root)
		if err != nil {
			return Lock{}, err
		}
	}
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		return Lock{}, err
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultLockTTL
	}
	now = now.UTC()
	lock := Lock{
		SchemaVersion: LockSchemaVersion,
		LockID:        newLockID(root, opts.JobID, now),
		HostID:        opts.HostID,
		JobID:         opts.JobID,
		RepoRoot:      root,
		WorktreePath:  cleanOptionalPath(opts.WorktreePath),
		BaseRef:       strings.TrimSpace(opts.BaseRef),
		Branch:        strings.TrimSpace(opts.Branch),
		OwnerAdapter:  strings.TrimSpace(opts.OwnerAdapter),
		CreatedAt:     now,
		ExpiresAt:     now.Add(ttl).UTC(),
	}
	path := lockPath(storeDir, root)
	content, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return Lock{}, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if _, err := file.Write(append(content, '\n')); err != nil {
				_ = file.Close()
				_ = os.Remove(path)
				return Lock{}, err
			}
			if err := file.Close(); err != nil {
				_ = os.Remove(path)
				return Lock{}, err
			}
			return lock, os.Chmod(path, 0o600)
		}
		if !os.IsExist(err) {
			return Lock{}, err
		}
		existing, readErr := readLock(path)
		if readErr != nil {
			return Lock{}, readErr
		}
		if now.Before(existing.ExpiresAt) {
			return Lock{}, fmt.Errorf("%w: repo %q is held by job %q until %s", ErrLocked, root, existing.JobID, existing.ExpiresAt.Format(time.RFC3339))
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return Lock{}, err
		}
	}
	return Lock{}, fmt.Errorf("%w: repo %q changed while acquiring lock", ErrLocked, root)
}

func (s FileLockStore) Status(repoRoot string, now time.Time) (LockStatus, error) {
	root, err := CanonicalDir(repoRoot)
	if err != nil {
		return LockStatus{}, err
	}
	storeDir := s.Dir
	if storeDir == "" {
		storeDir, err = DefaultStoreDir(root)
		if err != nil {
			return LockStatus{}, err
		}
	}
	path := lockPath(storeDir, root)
	lock, err := readLock(path)
	if os.IsNotExist(err) {
		return LockStatus{StorePath: path, Exists: false}, nil
	}
	if err != nil {
		return LockStatus{}, err
	}
	return LockStatus{
		StorePath: path,
		Exists:    true,
		Expired:   !now.UTC().Before(lock.ExpiresAt),
		Lock:      &lock,
	}, nil
}

func (s FileLockStore) Release(repoRoot, jobID string, force bool) (Lock, bool, error) {
	root, err := CanonicalDir(repoRoot)
	if err != nil {
		return Lock{}, false, err
	}
	storeDir := s.Dir
	if storeDir == "" {
		storeDir, err = DefaultStoreDir(root)
		if err != nil {
			return Lock{}, false, err
		}
	}
	path := lockPath(storeDir, root)
	lock, err := readLock(path)
	if os.IsNotExist(err) {
		return Lock{}, false, nil
	}
	if err != nil {
		return Lock{}, false, err
	}
	if !force && strings.TrimSpace(jobID) != "" && lock.JobID != jobID {
		return Lock{}, false, fmt.Errorf("%w: lock belongs to job %q", ErrOwnerMismatch, lock.JobID)
	}
	if !force && strings.TrimSpace(jobID) == "" {
		return Lock{}, false, fmt.Errorf("%w: job id is required unless force is set", ErrOwnerMismatch)
	}
	if err := os.Remove(path); err != nil {
		return Lock{}, false, err
	}
	return lock, true, nil
}

func CanonicalDir(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path must be a directory")
	}
	return filepath.Clean(canonical), nil
}

func readLock(path string) (Lock, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Lock{}, err
	}
	var lock Lock
	if err := json.Unmarshal(content, &lock); err != nil {
		return Lock{}, fmt.Errorf("decode workspace lock: %w", err)
	}
	if lock.SchemaVersion != LockSchemaVersion {
		return Lock{}, fmt.Errorf("unsupported workspace lock schema %q", lock.SchemaVersion)
	}
	return lock, nil
}

func lockPath(storeDir, repoRoot string) string {
	sum := sha256.Sum256([]byte(repoRoot))
	return filepath.Join(storeDir, hex.EncodeToString(sum[:])+".json")
}

func newLockID(repoRoot, jobID string, now time.Time) string {
	sum := sha256.Sum256([]byte(repoRoot + "\x00" + jobID + "\x00" + now.Format(time.RFC3339Nano)))
	return "wlk_" + hex.EncodeToString(sum[:8])
}

func cleanOptionalPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return filepath.Clean(path)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
