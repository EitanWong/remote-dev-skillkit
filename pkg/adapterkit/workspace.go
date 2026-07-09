package adapterkit

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WorkspaceSessionSchemaVersion is the schema version for WorkspaceSession.
const WorkspaceSessionSchemaVersion = "rdev.adapter-workspace-session.v1"

// WorkspaceSession captures the preparation phase output for a third-party
// adapter. It records which workspace root was used, what locks were acquired,
// which boundaries are enforced, and a session ID that ties the prepare/run/
// collect/cleanup phases together.
type WorkspaceSession struct {
	SchemaVersion   string   `json:"schema_version"`
	SessionID       string   `json:"session_id"`
	Adapter         string   `json:"adapter"`
	TaskID          string   `json:"task_id,omitempty"`
	WorkspaceRoot   string   `json:"workspace_root"`
	LockPath        string   `json:"lock_path,omitempty"`
	WriteBoundaries []string `json:"write_boundaries,omitempty"`
	PreparedAt      string   `json:"prepared_at"`
	CleanedUp       bool     `json:"cleaned_up"`
	CleanedUpAt     string   `json:"cleaned_up_at,omitempty"`
}

// WorkspaceSessionOptions configures workspace session preparation.
type WorkspaceSessionOptions struct {
	// LockDir overrides where the workspace lock file is written.
	// Defaults to WorkspaceRoot/.rdev/workspace-locks.
	LockDir string
	// WriteBoundaries restricts which sub-paths under WorkspaceRoot the adapter
	// is allowed to mutate. An empty slice means unrestricted within the root.
	WriteBoundaries []string
}

// PrepareWorkspaceSession creates a WorkspaceSession for the given adapter and
// request. It validates that the workspace root exists and is a directory,
// resolves and canonicalises the root, and returns an initialised session.
// The caller is responsible for acquiring a workspace lock separately via the
// workspace package if required.
func PrepareWorkspaceSession(adapter, taskID, workspaceRoot string, opts WorkspaceSessionOptions, now time.Time) (WorkspaceSession, error) {
	adapter = strings.TrimSpace(adapter)
	if adapter == "" {
		return WorkspaceSession{}, fmt.Errorf("workspace session adapter is required")
	}
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return WorkspaceSession{}, fmt.Errorf("workspace root is required")
	}
	canonical, err := canonicalWorkspaceRoot(workspaceRoot)
	if err != nil {
		return WorkspaceSession{}, fmt.Errorf("workspace session: %w", err)
	}
	sessionID, err := newSessionID()
	if err != nil {
		return WorkspaceSession{}, fmt.Errorf("workspace session id: %w", err)
	}
	lockPath := ""
	if opts.LockDir != "" {
		lockPath = filepath.Join(opts.LockDir, sessionID+".lock")
	}
	boundaries, err := validateWriteBoundaries(canonical, opts.WriteBoundaries)
	if err != nil {
		return WorkspaceSession{}, fmt.Errorf("workspace session: %w", err)
	}
	return WorkspaceSession{
		SchemaVersion:   WorkspaceSessionSchemaVersion,
		SessionID:       sessionID,
		Adapter:         adapter,
		TaskID:          strings.TrimSpace(taskID),
		WorkspaceRoot:   canonical,
		LockPath:        lockPath,
		WriteBoundaries: boundaries,
		PreparedAt:      now.UTC().Format(time.RFC3339Nano),
	}, nil
}

// MarkCleaned returns a copy of the session with CleanedUp set to true.
func (s WorkspaceSession) MarkCleaned(now time.Time) WorkspaceSession {
	s.CleanedUp = true
	s.CleanedUpAt = now.UTC().Format(time.RFC3339Nano)
	return s
}

// WorkspaceSessionContract defines the checks to apply when verifying a
// WorkspaceSession evidence record.
type WorkspaceSessionContract struct {
	Adapter           string
	RequireLock       bool
	RequireBoundaries bool
	RequireCleaned    bool
}

// WorkspaceSessionReport is the result of verifying a WorkspaceSession record.
type WorkspaceSessionReport struct {
	SchemaVersion string  `json:"schema_version"`
	Adapter       string  `json:"adapter"`
	OK            bool    `json:"ok"`
	Checks        []Check `json:"checks"`
}

// VerifyWorkspaceSessionJSON verifies a serialised WorkspaceSession against the
// supplied contract. It is intended to be called from adapter lifecycle tests
// and from evidence reviewers.
func VerifyWorkspaceSessionJSON(content []byte, contract WorkspaceSessionContract) WorkspaceSessionReport {
	report := WorkspaceSessionReport{
		SchemaVersion: ConformanceReportSchemaVersion,
		Adapter:       contract.Adapter,
	}
	add := func(name string, passed bool, detail string) {
		report.Checks = append(report.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	var session map[string]any
	if err := json.Unmarshal(content, &session); err != nil {
		add("json_valid", false, err.Error())
		report.OK = allChecksPassed(report.Checks)
		return report
	}
	add("json_valid", true, "")
	add("schema_version", stringField(session, "schema_version") == WorkspaceSessionSchemaVersion, stringField(session, "schema_version"))
	add("adapter", strings.TrimSpace(contract.Adapter) == "" || stringField(session, "adapter") == contract.Adapter, stringField(session, "adapter"))
	add("session_id_present", strings.TrimSpace(stringField(session, "session_id")) != "", stringField(session, "session_id"))
	add("workspace_root_present", strings.TrimSpace(stringField(session, "workspace_root")) != "", stringField(session, "workspace_root"))
	add("prepared_at_valid", validRFC3339(stringField(session, "prepared_at")), stringField(session, "prepared_at"))

	lockPath := strings.TrimSpace(stringField(session, "lock_path"))
	add("lock_path_present", lockPath != "" || !contract.RequireLock,
		lockPath)

	boundaries := stringArrayField(session, "write_boundaries")
	add("write_boundaries_declared", len(boundaries) > 0 || !contract.RequireBoundaries,
		fmt.Sprintf("%d declared", len(boundaries)))

	if contract.RequireCleaned {
		add("cleaned_up", boolFieldEquals(session, "cleaned_up", true), "")
		add("cleaned_up_at_valid", validRFC3339(stringField(session, "cleaned_up_at")), stringField(session, "cleaned_up_at"))
	}

	report.OK = allChecksPassed(report.Checks)
	return report
}

// --- internal helpers ---

func canonicalWorkspaceRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("stat workspace root %q: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace root is not a directory: %q", abs)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("eval symlinks workspace root: %w", err)
	}
	return resolved, nil
}

func validateWriteBoundaries(root string, boundaries []string) ([]string, error) {
	if len(boundaries) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(boundaries))
	for _, b := range boundaries {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		var resolved string
		if filepath.IsAbs(b) {
			// Resolve symlinks so absolute boundaries can be compared with
			// the already-resolved canonical root (e.g. /private/var on macOS).
			if evaled, err := filepath.EvalSymlinks(b); err == nil {
				resolved = evaled
			} else {
				resolved = b
			}
		} else {
			resolved = filepath.Join(root, b)
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
			return nil, fmt.Errorf("write boundary %q escapes workspace root", b)
		}
		out = append(out, resolved)
	}
	return out, nil
}

func newSessionID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("ws-%x", b), nil
}
