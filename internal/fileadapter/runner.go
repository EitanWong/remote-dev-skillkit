package fileadapter

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

const ResultSchemaVersion = "rdev.file-result.v1"

type Spec struct {
	WorkspaceRoot      string
	WriteScope         []string
	Action             string
	Path               string
	Content            string
	Encoding           string
	ExpectedBytes      int
	ExpectedSHA256     string
	MaxBytes           int
	MaxDurationSeconds int
	MaxOutputBytes     int
}

type Entry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Type      string `json:"type"`
	SizeBytes int64  `json:"size_bytes"`
	Modified  string `json:"modified,omitempty"`
}

type ResultArtifact struct {
	SchemaVersion   string  `json:"schema_version"`
	Adapter         string  `json:"adapter"`
	Action          string  `json:"action"`
	WorkspaceRoot   string  `json:"workspace_root"`
	Path            string  `json:"path,omitempty"`
	ResolvedPath    string  `json:"resolved_path,omitempty"`
	Entries         []Entry `json:"entries,omitempty"`
	ContentBase64   string  `json:"content_base64,omitempty"`
	ContentText     string  `json:"content_text,omitempty"`
	Encoding        string  `json:"encoding,omitempty"`
	Bytes           int     `json:"bytes,omitempty"`
	SHA256          string  `json:"sha256,omitempty"`
	ExpectedBytes   int     `json:"expected_bytes,omitempty"`
	ExpectedSHA256  string  `json:"expected_sha256,omitempty"`
	ByteCompare     string  `json:"byte_compare,omitempty"`
	Deleted         bool    `json:"deleted,omitempty"`
	OutputTruncated bool    `json:"output_truncated"`
	StartedAt       string  `json:"started_at"`
	EndedAt         string  `json:"ended_at"`
	DurationMillis  int64   `json:"duration_millis"`
}

func Execute(spec Spec) (ResultArtifact, error) {
	return ExecuteContext(context.Background(), spec)
}

func ExecuteContext(ctx context.Context, spec Spec) (ResultArtifact, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	maxDuration := spec.MaxDurationSeconds
	if maxDuration <= 0 {
		maxDuration = 60
	}
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, time.Duration(maxDuration)*time.Second)
	defer cancel()

	started := time.Now().UTC()
	root, err := canonicalWorkspace(spec.WorkspaceRoot)
	if err != nil {
		return ResultArtifact{}, err
	}
	action := normalizeAction(spec.Action)
	if action == "" {
		return ResultArtifact{}, fmt.Errorf("file action is required")
	}
	artifact := ResultArtifact{
		SchemaVersion: ResultSchemaVersion,
		Adapter:       "file",
		Action:        action,
		WorkspaceRoot: root,
		Path:          spec.Path,
		StartedAt:     started.Format(time.RFC3339Nano),
	}
	finish := func() ResultArtifact {
		ended := time.Now().UTC()
		artifact.EndedAt = ended.Format(time.RFC3339Nano)
		artifact.DurationMillis = ended.Sub(started).Milliseconds()
		return artifact
	}
	select {
	case <-ctx.Done():
		return finish(), ctx.Err()
	default:
	}

	switch action {
	case "list":
		resolved, err := resolveExistingPath(root, spec.Path)
		if err != nil {
			return finish(), err
		}
		artifact.ResolvedPath = resolved
		entries, err := listEntries(root, resolved)
		if err != nil {
			return finish(), err
		}
		artifact.Entries = entries
	case "read", "download":
		resolved, err := resolveExistingPath(root, spec.Path)
		if err != nil {
			return finish(), err
		}
		artifact.ResolvedPath = resolved
		if err := readFileContent(resolved, spec.MaxBytes, &artifact); err != nil {
			return finish(), err
		}
		applyExpectedTransferEvidence(&artifact, spec.ExpectedBytes, spec.ExpectedSHA256)
	case "write", "upload":
		resolved, err := resolveWritablePath(root, spec.Path, spec.WriteScope)
		if err != nil {
			return finish(), err
		}
		artifact.ResolvedPath = resolved
		content, encoding, err := decodeContent(spec.Content, spec.Encoding)
		if err != nil {
			return finish(), err
		}
		if err := os.MkdirAll(filepath.Dir(resolved), 0o700); err != nil {
			return finish(), err
		}
		if err := os.WriteFile(resolved, content, 0o600); err != nil {
			return finish(), err
		}
		sum := sha256.Sum256(content)
		artifact.Encoding = encoding
		artifact.Bytes = len(content)
		artifact.SHA256 = "sha256:" + hex.EncodeToString(sum[:])
		applyExpectedTransferEvidence(&artifact, spec.ExpectedBytes, spec.ExpectedSHA256)
	case "delete":
		resolved, err := resolveDeletablePath(root, spec.Path, spec.WriteScope)
		if err != nil {
			return finish(), err
		}
		info, err := os.Lstat(resolved)
		if err != nil {
			return finish(), err
		}
		if err := os.Remove(resolved); err != nil {
			return finish(), err
		}
		artifact.ResolvedPath = resolved
		artifact.Bytes = int(info.Size())
		artifact.Deleted = true
	default:
		return finish(), fmt.Errorf("unsupported file action %q", action)
	}
	return finish(), nil
}

func (r ResultArtifact) ArtifactContent() string {
	content, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return ""
	}
	return string(content)
}

func normalizeAction(action string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	action = strings.ReplaceAll(action, "_", ".")
	return action
}

func canonicalWorkspace(root string) (string, error) {
	canonical, err := workspace.CanonicalDir(root)
	if err != nil {
		return "", fmt.Errorf("workspace root must exist: %w", err)
	}
	return canonical, nil
}

func resolveExistingPath(root, raw string) (string, error) {
	target, err := cleanTarget(root, raw)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("target path must exist: %w", err)
	}
	if !pathWithin(root, canonical) {
		return "", fmt.Errorf("target path escapes workspace root")
	}
	return canonical, nil
}

func resolveWritablePath(root, raw string, writeScope []string) (string, error) {
	if len(writeScope) == 0 {
		return "", fmt.Errorf("write_scope is required")
	}
	target, err := cleanTarget(root, raw)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(target)
	parentCanonical, err := filepath.EvalSymlinks(parent)
	if err != nil {
		ancestor, ancestorErr := existingAncestor(parent)
		if ancestorErr != nil {
			return "", ancestorErr
		}
		parentCanonical, err = filepath.EvalSymlinks(ancestor)
		if err != nil {
			return "", fmt.Errorf("target parent ancestor must resolve: %w", err)
		}
	}
	if !pathWithin(root, parentCanonical) {
		return "", fmt.Errorf("target parent escapes workspace root")
	}
	for _, scope := range writeScope {
		scopeTarget, err := cleanTarget(root, scope)
		if err != nil {
			continue
		}
		scopeCanonical := scopeTarget
		if existing, err := filepath.EvalSymlinks(scopeTarget); err == nil {
			scopeCanonical = existing
		}
		if pathWithin(scopeCanonical, target) || pathWithin(scopeCanonical, parentCanonical) {
			return target, nil
		}
	}
	return "", fmt.Errorf("target path is outside declared write_scope")
}

func resolveDeletablePath(root, raw string, writeScope []string) (string, error) {
	target, err := resolveWritablePath(root, raw, writeScope)
	if err != nil {
		return "", err
	}
	if filepath.Clean(target) == filepath.Clean(root) {
		return "", fmt.Errorf("refusing to delete workspace root")
	}
	if _, err := os.Lstat(target); err != nil {
		return "", fmt.Errorf("delete target must exist: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("delete target must resolve inside workspace: %w", err)
	}
	if !pathWithin(root, canonical) {
		return "", fmt.Errorf("delete target escapes workspace root")
	}
	return target, nil
}

func existingAncestor(path string) (string, error) {
	path = filepath.Clean(path)
	for {
		if info, err := os.Stat(path); err == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("target parent ancestor is not a directory")
			}
			return path, nil
		}
		next := filepath.Dir(path)
		if next == path {
			return "", fmt.Errorf("target parent must have an existing ancestor")
		}
		path = next
	}
}

func cleanTarget(root, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		raw = "."
	}
	target := raw
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target = filepath.Clean(target)
	if !pathWithin(root, target) {
		return "", fmt.Errorf("path escapes workspace root")
	}
	return target, nil
}

func pathWithin(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if root == target {
		return true
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != "." && rel != "" && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func listEntries(root, target string) ([]Entry, error) {
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []Entry{entryFor(root, target, info)}, nil
	}
	items, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		info, err := item.Info()
		if err != nil {
			continue
		}
		entries = append(entries, entryFor(root, filepath.Join(target, item.Name()), info))
	}
	return entries, nil
}

func entryFor(root, path string, info os.FileInfo) Entry {
	kind := "file"
	if info.IsDir() {
		kind = "directory"
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	return Entry{
		Name:      info.Name(),
		Path:      filepath.ToSlash(rel),
		Type:      kind,
		SizeBytes: info.Size(),
		Modified:  info.ModTime().UTC().Format(time.RFC3339Nano),
	}
}

func readFileContent(path string, maxBytes int, artifact *ResultArtifact) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("cannot read directory as file")
	}
	if maxBytes <= 0 {
		maxBytes = 1024 * 1024
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	limited := io.LimitReader(f, int64(maxBytes)+1)
	content, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if len(content) > maxBytes {
		content = content[:maxBytes]
		artifact.OutputTruncated = true
	}
	sum := sha256.Sum256(content)
	artifact.Bytes = len(content)
	artifact.SHA256 = "sha256:" + hex.EncodeToString(sum[:])
	artifact.ContentBase64 = base64.StdEncoding.EncodeToString(content)
	if utf8.Valid(content) {
		artifact.ContentText = string(content)
	}
	return nil
}

func applyExpectedTransferEvidence(artifact *ResultArtifact, expectedBytes int, expectedSHA256 string) {
	expectedSHA256 = normalizeSHA256(expectedSHA256)
	if expectedBytes <= 0 && expectedSHA256 == "" {
		return
	}
	if expectedBytes > 0 {
		artifact.ExpectedBytes = expectedBytes
	}
	if expectedSHA256 != "" {
		artifact.ExpectedSHA256 = expectedSHA256
	}
	match := true
	if expectedBytes > 0 && artifact.Bytes != expectedBytes {
		match = false
	}
	if expectedSHA256 != "" && normalizeSHA256(artifact.SHA256) != expectedSHA256 {
		match = false
	}
	if match {
		artifact.ByteCompare = "match"
		return
	}
	artifact.ByteCompare = "mismatch"
}

func normalizeSHA256(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	value = strings.TrimPrefix(value, "sha256:")
	if value == "" {
		return ""
	}
	return "sha256:" + value
}

func decodeContent(content, encoding string) ([]byte, string, error) {
	encoding = strings.ToLower(strings.TrimSpace(encoding))
	if encoding == "" {
		encoding = "utf-8"
	}
	switch encoding {
	case "utf-8", "text":
		return []byte(content), "utf-8", nil
	case "base64":
		data, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return nil, "", fmt.Errorf("decode base64 content: %w", err)
		}
		return data, "base64", nil
	default:
		return nil, "", fmt.Errorf("unsupported content encoding %q", encoding)
	}
}
