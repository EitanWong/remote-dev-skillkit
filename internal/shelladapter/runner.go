package shelladapter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Spec struct {
	WorkspaceRoot      string
	WriteScope         []string
	Argv               []string
	AllowCommands      []string
	MaxDurationSeconds int
	MaxOutputBytes     int
}

type Result struct {
	Adapter         string   `json:"adapter"`
	Argv            []string `json:"argv"`
	WorkspaceRoot   string   `json:"workspace_root"`
	ExitCode        int      `json:"exit_code"`
	Stdout          string   `json:"stdout,omitempty"`
	Stderr          string   `json:"stderr,omitempty"`
	TimedOut        bool     `json:"timed_out"`
	OutputTruncated bool     `json:"output_truncated"`
	StartedAt       string   `json:"started_at"`
	EndedAt         string   `json:"ended_at"`
	DurationMillis  int64    `json:"duration_millis"`
}

func Execute(spec Spec) (Result, error) {
	workspaceRoot, err := canonicalWorkspace(spec.WorkspaceRoot)
	if err != nil {
		return Result{}, err
	}
	if err := verifyWriteScope(workspaceRoot, spec.WriteScope); err != nil {
		return Result{}, err
	}
	if len(spec.Argv) == 0 || strings.TrimSpace(spec.Argv[0]) == "" {
		return Result{}, fmt.Errorf("argv is required")
	}
	if !allowedCommand(spec.Argv[0], spec.AllowCommands) {
		return Result{}, fmt.Errorf("command %q is not allowlisted", commandName(spec.Argv[0]))
	}
	maxDuration := spec.MaxDurationSeconds
	if maxDuration <= 0 {
		maxDuration = 60
	}
	maxOutputBytes := spec.MaxOutputBytes
	if maxOutputBytes <= 0 {
		maxOutputBytes = 1024 * 1024
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(maxDuration)*time.Second)
	defer cancel()

	started := time.Now().UTC()
	limiter := newOutputLimiter(maxOutputBytes)
	cmd := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = workspaceRoot
	cmd.Stdout = limiter.stdoutWriter()
	cmd.Stderr = limiter.stderrWriter()
	err = cmd.Run()
	ended := time.Now().UTC()

	result := Result{
		Adapter:         "shell",
		Argv:            append([]string(nil), spec.Argv...),
		WorkspaceRoot:   workspaceRoot,
		ExitCode:        exitCode(err),
		Stdout:          limiter.stdout(),
		Stderr:          limiter.stderr(),
		TimedOut:        ctx.Err() == context.DeadlineExceeded,
		OutputTruncated: limiter.truncated(),
		StartedAt:       started.Format(time.RFC3339Nano),
		EndedAt:         ended.Format(time.RFC3339Nano),
		DurationMillis:  ended.Sub(started).Milliseconds(),
	}
	if result.TimedOut {
		result.ExitCode = -1
		return result, fmt.Errorf("command timed out after %ds", maxDuration)
	}
	if err != nil {
		return result, fmt.Errorf("command exited with status %d", result.ExitCode)
	}
	return result, nil
}

func (r Result) ArtifactContent() string {
	if r.Adapter == "" {
		return ""
	}
	content, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return ""
	}
	return string(content)
}

func canonicalWorkspace(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("workspace root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("workspace root must exist: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("stat workspace root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace root must be a directory")
	}
	return canonical, nil
}

func verifyWriteScope(root string, scopes []string) error {
	for _, scope := range scopes {
		if strings.TrimSpace(scope) == "" {
			continue
		}
		resolved, err := resolveScope(root, scope)
		if err != nil {
			return err
		}
		if !pathWithin(root, resolved) {
			return fmt.Errorf("write scope %q escapes workspace root", scope)
		}
	}
	return nil
}

func resolveScope(root, scope string) (string, error) {
	path := scope
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve write scope: %w", err)
	}
	if canonical, err := filepath.EvalSymlinks(abs); err == nil {
		return canonical, nil
	}
	return filepath.Clean(abs), nil
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

func allowedCommand(command string, allowlist []string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	name := commandName(command)
	for _, allowed := range allowlist {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		if hasPathSeparator(command) || hasPathSeparator(allowed) {
			if command == allowed {
				return true
			}
			continue
		}
		if name == commandName(allowed) {
			return true
		}
	}
	return false
}

func commandName(command string) string {
	command = strings.TrimSpace(command)
	command = strings.ReplaceAll(command, "\\", "/")
	return filepath.Base(command)
}

func hasPathSeparator(command string) bool {
	return strings.Contains(command, "/") || strings.Contains(command, "\\")
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}

type outputLimiter struct {
	mu              sync.Mutex
	remaining       int
	stdoutBuilder   strings.Builder
	stderrBuilder   strings.Builder
	stdoutTruncated bool
	stderrTruncated bool
}

func newOutputLimiter(maxBytes int) *outputLimiter {
	return &outputLimiter{remaining: maxBytes}
}

func (l *outputLimiter) stdoutWriter() *limitedWriter {
	return &limitedWriter{limiter: l, stream: "stdout"}
}

func (l *outputLimiter) stderrWriter() *limitedWriter {
	return &limitedWriter{limiter: l, stream: "stderr"}
}

func (l *outputLimiter) stdout() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stdoutBuilder.String()
}

func (l *outputLimiter) stderr() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stderrBuilder.String()
}

func (l *outputLimiter) truncated() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stdoutTruncated || l.stderrTruncated
}

type limitedWriter struct {
	limiter *outputLimiter
	stream  string
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	w.limiter.mu.Lock()
	defer w.limiter.mu.Unlock()

	if w.limiter.remaining <= 0 {
		w.markTruncated()
		return len(p), nil
	}
	toWrite := len(p)
	if toWrite > w.limiter.remaining {
		toWrite = w.limiter.remaining
		w.markTruncated()
	}
	if w.stream == "stderr" {
		_, _ = w.limiter.stderrBuilder.Write(p[:toWrite])
	} else {
		_, _ = w.limiter.stdoutBuilder.Write(p[:toWrite])
	}
	w.limiter.remaining -= toWrite
	return len(p), nil
}

func (w *limitedWriter) markTruncated() {
	if w.stream == "stderr" {
		w.limiter.stderrTruncated = true
		return
	}
	w.limiter.stdoutTruncated = true
}
