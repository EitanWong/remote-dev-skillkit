package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	WorkspaceScopeSnapshotSchemaVersion = "rdev.workspace-scope-snapshot.v1"
	WorkspaceScopeEvidenceSchemaVersion = "rdev.workspace-scope-evidence.v1"
	maxWorkspaceSnapshotFiles           = 20_000
)

// WorkspaceScopeSnapshot is a metadata-only digest for non-Git task
// workspaces. It never reads file bodies and does not follow symlinks.
type WorkspaceScopeSnapshot struct {
	SchemaVersion string   `json:"schema_version"`
	Root          string   `json:"root"`
	Scopes        []string `json:"scopes"`
	FileCount     int      `json:"file_count"`
	Truncated     bool     `json:"truncated"`
	TreeSHA256    string   `json:"tree_sha256"`

	entries map[string]string
}

// WorkspaceScopeEvidence compares metadata-only snapshots before and after a
// non-Git task runs under the regular workspace lock.
type WorkspaceScopeEvidence struct {
	SchemaVersion      string                 `json:"schema_version"`
	Before             WorkspaceScopeSnapshot `json:"before"`
	After              WorkspaceScopeSnapshot `json:"after"`
	Changed            bool                   `json:"changed"`
	ChangedFiles       []string               `json:"changed_files,omitempty"`
	ChangesTruncated   bool                   `json:"changes_truncated"`
	DeclaredWriteScope []string               `json:"declared_write_scope,omitempty"`
}

func CompareWorkspaceScope(before, after WorkspaceScopeSnapshot) WorkspaceScopeEvidence {
	changedFiles, truncated := changedWorkspaceSnapshotFiles(before.entries, after.entries)
	changed := before.TreeSHA256 != after.TreeSHA256 ||
		before.FileCount != after.FileCount ||
		before.Truncated != after.Truncated
	return WorkspaceScopeEvidence{
		SchemaVersion:    WorkspaceScopeEvidenceSchemaVersion,
		Before:           before,
		After:            after,
		Changed:          changed,
		ChangedFiles:     changedFiles,
		ChangesTruncated: truncated,
	}
}

func changedWorkspaceSnapshotFiles(before, after map[string]string) ([]string, bool) {
	if before == nil || after == nil {
		return nil, false
	}
	const maxChangedWorkspaceFiles = 200
	all := make(map[string]struct{}, len(before)+len(after))
	for path := range before {
		all[path] = struct{}{}
	}
	for path := range after {
		all[path] = struct{}{}
	}
	paths := make([]string, 0, len(all))
	for path := range all {
		if before[path] != after[path] {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	if len(paths) <= maxChangedWorkspaceFiles {
		return paths, false
	}
	return paths[:maxChangedWorkspaceFiles], true
}

func SnapshotWorkspaceScope(root string, scopes []string) (WorkspaceScopeSnapshot, error) {
	canonicalRoot, err := CanonicalDir(root)
	if err != nil {
		return WorkspaceScopeSnapshot{}, err
	}
	resolvedScopes, normalizedScopes, err := resolveWorkspaceSnapshotScopes(canonicalRoot, scopes)
	if err != nil {
		return WorkspaceScopeSnapshot{}, err
	}
	records := make([]string, 0)
	entries := make(map[string]string)
	seen := make(map[string]struct{})
	truncated := false
	for _, scope := range resolvedScopes {
		if err := filepath.WalkDir(scope, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(canonicalRoot, path)
			if err != nil {
				return err
			}
			if relative == ".git" && entry.IsDir() {
				return filepath.SkipDir
			}
			if entry.IsDir() {
				return nil
			}
			if len(records) >= maxWorkspaceSnapshotFiles {
				truncated = true
				return errWorkspaceSnapshotTruncated
			}
			relative = filepath.ToSlash(relative)
			if _, ok := seen[relative]; ok {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			seen[relative] = struct{}{}
			record := fmt.Sprintf("%s\x00%s\x00%d\x00%d", relative, info.Mode().String(), info.Size(), info.ModTime().UTC().UnixNano())
			entries[relative] = record
			records = append(records, record)
			return nil
		}); err != nil && err != errWorkspaceSnapshotTruncated {
			return WorkspaceScopeSnapshot{}, err
		}
		if truncated {
			break
		}
	}
	sort.Strings(records)
	hash := sha256.New()
	for _, record := range records {
		_, _ = hash.Write([]byte(record))
		_, _ = hash.Write([]byte{'\n'})
	}
	return WorkspaceScopeSnapshot{
		SchemaVersion: WorkspaceScopeSnapshotSchemaVersion,
		Root:          canonicalRoot,
		Scopes:        normalizedScopes,
		FileCount:     len(records),
		Truncated:     truncated,
		TreeSHA256:    "sha256:" + hex.EncodeToString(hash.Sum(nil)),
		entries:       entries,
	}, nil
}

var errWorkspaceSnapshotTruncated = fmt.Errorf("workspace snapshot truncated")

func resolveWorkspaceSnapshotScopes(root string, scopes []string) ([]string, []string, error) {
	if len(scopes) == 0 {
		scopes = []string{"."}
	}
	resolved := make([]string, 0, len(scopes))
	normalized := make([]string, 0, len(scopes))
	seen := make(map[string]struct{})
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		candidate := scope
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(root, candidate)
		}
		candidate = filepath.Clean(candidate)
		relative, err := filepath.Rel(root, candidate)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, nil, fmt.Errorf("workspace snapshot scope %q escapes workspace root", scope)
		}
		canonical, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve workspace snapshot scope %q: %w", scope, err)
		}
		canonical = filepath.Clean(canonical)
		relativeCanonical, err := filepath.Rel(root, canonical)
		if err != nil || relativeCanonical == ".." || strings.HasPrefix(relativeCanonical, ".."+string(filepath.Separator)) {
			return nil, nil, fmt.Errorf("workspace snapshot scope %q resolves outside workspace root", scope)
		}
		if _, err := os.Lstat(canonical); err != nil {
			return nil, nil, err
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		resolved = append(resolved, canonical)
		if relativeCanonical == "." {
			normalized = append(normalized, ".")
		} else {
			normalized = append(normalized, filepath.ToSlash(relativeCanonical))
		}
	}
	if len(resolved) == 0 {
		return nil, nil, fmt.Errorf("workspace snapshot requires at least one non-empty scope")
	}
	sort.Strings(normalized)
	return resolved, normalized, nil
}
