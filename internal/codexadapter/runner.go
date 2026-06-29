package codexadapter

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

	"github.com/EitanWong/remote-dev-skillkit/internal/shelladapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

const ResultSchemaVersion = "rdev.codex-result.v1"

type Spec struct {
	WorkspaceRoot             string
	WriteScope                []string
	Prompt                    string
	CodexCommand              string
	CodexArgs                 []string
	VerificationCommands      [][]string
	AllowVerificationCommands []string
	MaxDurationSeconds        int
	MaxOutputBytes            int
}

type CommandResult struct {
	Argv            []string `json:"argv"`
	Dir             string   `json:"dir"`
	ExitCode        int      `json:"exit_code"`
	Stdout          string   `json:"stdout,omitempty"`
	Stderr          string   `json:"stderr,omitempty"`
	TimedOut        bool     `json:"timed_out"`
	OutputTruncated bool     `json:"output_truncated"`
}

type Result struct {
	Adapter             string          `json:"adapter"`
	WorkspaceRoot       string          `json:"workspace_root"`
	Prompt              string          `json:"prompt"`
	CodexCommand        CommandResult   `json:"codex_command"`
	GitStatus           CommandResult   `json:"git_status"`
	GitDiffStat         CommandResult   `json:"git_diff_stat"`
	GitDiff             CommandResult   `json:"git_diff"`
	VerificationResults []CommandResult `json:"verification_results,omitempty"`
	StartedAt           string          `json:"started_at"`
	EndedAt             string          `json:"ended_at"`
	DurationMillis      int64           `json:"duration_millis"`
}

type ResultArtifact struct {
	SchemaVersion       string          `json:"schema_version"`
	Adapter             string          `json:"adapter"`
	WorkspaceRoot       string          `json:"workspace_root"`
	Prompt              string          `json:"prompt"`
	CodexCommand        CommandResult   `json:"codex_command"`
	GitStatus           CommandResult   `json:"git_status"`
	GitDiffStat         CommandResult   `json:"git_diff_stat"`
	GitDiff             CommandResult   `json:"git_diff"`
	VerificationResults []CommandResult `json:"verification_results,omitempty"`
	StartedAt           string          `json:"started_at"`
	EndedAt             string          `json:"ended_at"`
	DurationMillis      int64           `json:"duration_millis"`
	Redacted            bool            `json:"redacted"`
	RedactionRules      []string        `json:"redaction_rules"`
	RedactionCounts     map[string]int  `json:"redaction_counts,omitempty"`
}

func Execute(spec Spec) (Result, error) {
	workspaceRoot, err := workspace.CanonicalDir(spec.WorkspaceRoot)
	if err != nil {
		return Result{}, err
	}
	if err := verifyWriteScope(workspaceRoot, spec.WriteScope); err != nil {
		return Result{}, err
	}
	prompt := strings.TrimSpace(spec.Prompt)
	if prompt == "" {
		return Result{}, fmt.Errorf("codex prompt is required")
	}
	maxDuration := spec.MaxDurationSeconds
	if maxDuration <= 0 {
		maxDuration = 1800
	}
	maxOutputBytes := spec.MaxOutputBytes
	if maxOutputBytes <= 0 {
		maxOutputBytes = 1024 * 1024
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(maxDuration)*time.Second)
	defer cancel()

	started := time.Now().UTC()
	argv := codexArgv(workspaceRoot, prompt, spec)
	codexResult := runCommand(ctx, workspaceRoot, argv, maxOutputBytes)
	result := Result{
		Adapter:       "codex",
		WorkspaceRoot: workspaceRoot,
		Prompt:        prompt,
		CodexCommand:  codexResult,
		StartedAt:     started.Format(time.RFC3339Nano),
	}
	result.GitStatus = runCommand(ctx, workspaceRoot, []string{"git", "status", "--short"}, maxOutputBytes)
	result.GitDiffStat = runCommand(ctx, workspaceRoot, []string{"git", "diff", "--stat"}, maxOutputBytes)
	result.GitDiff = runCommand(ctx, workspaceRoot, []string{"git", "diff", "--"}, maxOutputBytes)
	for _, command := range spec.VerificationCommands {
		if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
			continue
		}
		if !allowedCommand(command[0], spec.AllowVerificationCommands) {
			ended := time.Now().UTC()
			result.EndedAt = ended.Format(time.RFC3339Nano)
			result.DurationMillis = ended.Sub(started).Milliseconds()
			result.VerificationResults = append(result.VerificationResults, CommandResult{
				Argv:     append([]string(nil), command...),
				Dir:      workspaceRoot,
				ExitCode: -1,
				Stderr:   fmt.Sprintf("verification command %q is not allowlisted", commandName(command[0])),
			})
			return result, fmt.Errorf("verification command %q is not allowlisted", commandName(command[0]))
		}
		result.VerificationResults = append(result.VerificationResults, runCommand(ctx, workspaceRoot, command, maxOutputBytes))
	}
	ended := time.Now().UTC()
	result.EndedAt = ended.Format(time.RFC3339Nano)
	result.DurationMillis = ended.Sub(started).Milliseconds()
	if codexResult.TimedOut {
		return result, fmt.Errorf("codex command timed out after %ds", maxDuration)
	}
	if codexResult.ExitCode != 0 {
		return result, fmt.Errorf("codex command exited with status %d", codexResult.ExitCode)
	}
	for _, verification := range result.VerificationResults {
		if verification.ExitCode != 0 {
			return result, fmt.Errorf("verification command exited with status %d", verification.ExitCode)
		}
	}
	return result, nil
}

func codexArgv(workspaceRoot, prompt string, spec Spec) []string {
	command := strings.TrimSpace(spec.CodexCommand)
	if command == "" {
		command = "codex"
	}
	args := append([]string(nil), spec.CodexArgs...)
	if len(args) == 0 {
		args = []string{"exec", "-C", workspaceRoot, "--sandbox", "workspace-write", "--json", prompt}
	}
	return append([]string{command}, args...)
}

func (r Result) ArtifactContent() string {
	if r.Adapter == "" {
		return ""
	}
	content, err := json.MarshalIndent(r.Artifact(), "", "  ")
	if err != nil {
		return ""
	}
	return string(content)
}

func (r Result) Artifact() ResultArtifact {
	redactor := shelladapter.NewArtifactRedactor()
	return ResultArtifact{
		SchemaVersion:       ResultSchemaVersion,
		Adapter:             r.Adapter,
		WorkspaceRoot:       r.WorkspaceRoot,
		Prompt:              redactor.Redact(r.Prompt),
		CodexCommand:        redactCommand(redactor, r.CodexCommand),
		GitStatus:           redactCommand(redactor, r.GitStatus),
		GitDiffStat:         redactCommand(redactor, r.GitDiffStat),
		GitDiff:             redactCommand(redactor, r.GitDiff),
		VerificationResults: redactCommands(redactor, r.VerificationResults),
		StartedAt:           r.StartedAt,
		EndedAt:             r.EndedAt,
		DurationMillis:      r.DurationMillis,
		Redacted:            redactor.Redacted(),
		RedactionRules:      shelladapter.RedactionRuleNames(),
		RedactionCounts:     redactor.Counts(),
	}
}

func redactCommands(redactor *shelladapter.ArtifactRedactor, commands []CommandResult) []CommandResult {
	if len(commands) == 0 {
		return nil
	}
	result := make([]CommandResult, 0, len(commands))
	for _, command := range commands {
		result = append(result, redactCommand(redactor, command))
	}
	return result
}

func redactCommand(redactor *shelladapter.ArtifactRedactor, command CommandResult) CommandResult {
	argv := make([]string, 0, len(command.Argv))
	for _, arg := range command.Argv {
		argv = append(argv, redactor.Redact(arg))
	}
	command.Argv = argv
	command.Stdout = redactor.Redact(command.Stdout)
	command.Stderr = redactor.Redact(command.Stderr)
	return command
}

func runCommand(ctx context.Context, dir string, argv []string, maxOutputBytes int) CommandResult {
	if len(argv) == 0 {
		return CommandResult{Dir: dir, ExitCode: -1, Stderr: "argv is required"}
	}
	limiter := newOutputLimiter(maxOutputBytes)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdout = limiter.stdoutWriter()
	cmd.Stderr = limiter.stderrWriter()
	err := cmd.Run()
	stderr := limiter.stderr()
	if err != nil && strings.TrimSpace(stderr) == "" {
		stderr = err.Error()
	}
	return CommandResult{
		Argv:            append([]string(nil), argv...),
		Dir:             dir,
		ExitCode:        exitCode(err),
		Stdout:          limiter.stdout(),
		Stderr:          stderr,
		TimedOut:        ctx.Err() == context.DeadlineExceeded,
		OutputTruncated: limiter.truncated(),
	}
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
		path = root + string(filepath.Separator) + path
	}
	resolved, err := resolveExistingPrefix(path)
	if err != nil {
		return "", fmt.Errorf("resolve write scope: %w", err)
	}
	return resolved, nil
}

func resolveExistingPrefix(path string) (string, error) {
	volume := filepath.VolumeName(path)
	rest := strings.TrimPrefix(path, volume)
	current := volume + string(filepath.Separator)
	parts := strings.FieldsFunc(rest, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			current = filepath.Dir(current)
			continue
		}
		next := filepath.Join(current, part)
		resolved, err := filepath.EvalSymlinks(next)
		if err == nil {
			current = resolved
			continue
		}
		if os.IsNotExist(err) {
			current = next
			continue
		}
		return "", err
	}
	return filepath.Clean(current), nil
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
	if maxBytes <= 0 {
		maxBytes = 1024 * 1024
	}
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
	write := len(p)
	if write > w.limiter.remaining {
		write = w.limiter.remaining
		w.markTruncated()
	}
	if write > 0 {
		if w.stream == "stdout" {
			w.limiter.stdoutBuilder.Write(p[:write])
		} else {
			w.limiter.stderrBuilder.Write(p[:write])
		}
		w.limiter.remaining -= write
	}
	if write < len(p) {
		w.markTruncated()
	}
	return len(p), nil
}

func (w *limitedWriter) markTruncated() {
	if w.stream == "stdout" {
		w.limiter.stdoutTruncated = true
		return
	}
	w.limiter.stderrTruncated = true
}
