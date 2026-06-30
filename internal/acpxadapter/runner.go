package acpxadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/shelladapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

const ResultSchemaVersion = "rdev.acpx-result.v1"
const TestReportSchemaVersion = "rdev.test-report.v1"

type Spec struct {
	WorkspaceRoot             string
	WriteScope                []string
	Prompt                    string
	AcpxCommand               string
	AcpxAgent                 string
	AcpxArgs                  []string
	VerificationCommands      [][]string
	AllowVerificationCommands []string
	MaxDurationSeconds        int
	MaxOutputBytes            int
}

type CommandResult struct {
	Argv            []string    `json:"argv"`
	Dir             string      `json:"dir"`
	ExitCode        int         `json:"exit_code"`
	Stdout          string      `json:"stdout,omitempty"`
	Stderr          string      `json:"stderr,omitempty"`
	Canceled        bool        `json:"canceled"`
	TimedOut        bool        `json:"timed_out"`
	OutputTruncated bool        `json:"output_truncated"`
	TestReport      *TestReport `json:"test_report,omitempty"`
}

type TestReport struct {
	SchemaVersion string        `json:"schema_version"`
	Tool          string        `json:"tool"`
	Total         int           `json:"total"`
	Passed        int           `json:"passed"`
	Failed        int           `json:"failed"`
	Skipped       int           `json:"skipped"`
	Packages      []TestPackage `json:"packages,omitempty"`
	Tests         []TestCase    `json:"tests,omitempty"`
	ParseErrors   []string      `json:"parse_errors,omitempty"`
	Incomplete    bool          `json:"incomplete"`
}

type TestPackage struct {
	Name    string  `json:"name"`
	Action  string  `json:"action"`
	Elapsed float64 `json:"elapsed,omitempty"`
}

type TestCase struct {
	Package string  `json:"package"`
	Name    string  `json:"name"`
	Action  string  `json:"action"`
	Elapsed float64 `json:"elapsed,omitempty"`
}

type Result struct {
	Adapter             string          `json:"adapter"`
	WorkspaceRoot       string          `json:"workspace_root"`
	Prompt              string          `json:"prompt"`
	AcpxCommand         CommandResult   `json:"acpx_command"`
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
	AcpxCommand         CommandResult   `json:"acpx_command"`
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
	return ExecuteContext(context.Background(), spec)
}

func ExecuteContext(parent context.Context, spec Spec) (Result, error) {
	if parent == nil {
		parent = context.Background()
	}
	workspaceRoot, err := workspace.CanonicalDir(spec.WorkspaceRoot)
	if err != nil {
		return Result{}, err
	}
	if err := verifyWriteScope(workspaceRoot, spec.WriteScope); err != nil {
		return Result{}, err
	}
	prompt := strings.TrimSpace(spec.Prompt)
	if prompt == "" {
		return Result{}, fmt.Errorf("acpx prompt is required")
	}
	maxDuration := spec.MaxDurationSeconds
	if maxDuration <= 0 {
		maxDuration = 1800
	}
	maxOutputBytes := spec.MaxOutputBytes
	if maxOutputBytes <= 0 {
		maxOutputBytes = 1024 * 1024
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(maxDuration)*time.Second)
	defer cancel()

	started := time.Now().UTC()
	argv := acpxArgv(prompt, workspaceRoot, spec)
	acpxResult := runCommand(ctx, workspaceRoot, argv, maxOutputBytes)
	result := Result{
		Adapter:       "acpx",
		WorkspaceRoot: workspaceRoot,
		Prompt:        prompt,
		AcpxCommand:   acpxResult,
		StartedAt:     started.Format(time.RFC3339Nano),
	}
	evidenceCtx, evidenceCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer evidenceCancel()
	result.GitStatus = runCommand(evidenceCtx, workspaceRoot, []string{"git", "status", "--short"}, maxOutputBytes)
	result.GitDiffStat = runCommand(evidenceCtx, workspaceRoot, []string{"git", "diff", "--stat"}, maxOutputBytes)
	result.GitDiff = runCommand(evidenceCtx, workspaceRoot, []string{"git", "diff", "--"}, maxOutputBytes)
	if !acpxResult.Canceled && !acpxResult.TimedOut {
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
			result.VerificationResults = append(result.VerificationResults, runVerificationCommand(ctx, workspaceRoot, command, maxOutputBytes))
		}
	}
	ended := time.Now().UTC()
	result.EndedAt = ended.Format(time.RFC3339Nano)
	result.DurationMillis = ended.Sub(started).Milliseconds()
	if acpxResult.Canceled {
		return result, fmt.Errorf("acpx command canceled")
	}
	if acpxResult.TimedOut {
		return result, fmt.Errorf("acpx command timed out after %ds", maxDuration)
	}
	if acpxResult.ExitCode != 0 {
		return result, fmt.Errorf("acpx command exited with status %d", acpxResult.ExitCode)
	}
	for _, verification := range result.VerificationResults {
		if verification.Canceled {
			return result, fmt.Errorf("verification command canceled")
		}
		if verification.TimedOut {
			return result, fmt.Errorf("verification command timed out")
		}
		if verification.ExitCode != 0 {
			return result, fmt.Errorf("verification command exited with status %d", verification.ExitCode)
		}
	}
	return result, nil
}

func acpxArgv(prompt, workspaceRoot string, spec Spec) []string {
	command := strings.TrimSpace(spec.AcpxCommand)
	if command == "" {
		command = "acpx"
	}
	args := append([]string(nil), spec.AcpxArgs...)
	if len(args) == 0 {
		agent := strings.TrimSpace(spec.AcpxAgent)
		if agent == "" {
			agent = "codex"
		}
		args = []string{"--cwd", workspaceRoot, agent, "exec", prompt}
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
		AcpxCommand:         redactCommand(redactor, r.AcpxCommand),
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

func runVerificationCommand(ctx context.Context, dir string, argv []string, maxOutputBytes int) CommandResult {
	result := runCommand(ctx, dir, argv, maxOutputBytes)
	if report := parseTestReport(argv, result.Stdout, result.OutputTruncated); report != nil {
		result.TestReport = report
	}
	return result
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
	ctxErr := ctx.Err()
	return CommandResult{
		Argv:            append([]string(nil), argv...),
		Dir:             dir,
		ExitCode:        exitCode(err),
		Stdout:          limiter.stdout(),
		Stderr:          stderr,
		Canceled:        ctxErr == context.Canceled,
		TimedOut:        ctxErr == context.DeadlineExceeded,
		OutputTruncated: limiter.truncated(),
	}
}

func parseTestReport(argv []string, stdout string, outputTruncated bool) *TestReport {
	if !isGoTestJSONCommand(argv) {
		return nil
	}
	return parseGoTestJSON(stdout, outputTruncated)
}

func isGoTestJSONCommand(argv []string) bool {
	if len(argv) < 3 || commandName(argv[0]) != "go" || argv[1] != "test" {
		return false
	}
	for _, arg := range argv[2:] {
		if arg == "-json" || arg == "--json" || arg == "-json=true" || arg == "--json=true" {
			return true
		}
	}
	return false
}

func parseGoTestJSON(stdout string, outputTruncated bool) *TestReport {
	report := &TestReport{
		SchemaVersion: TestReportSchemaVersion,
		Tool:          "go test",
		Incomplete:    outputTruncated,
	}
	decoder := json.NewDecoder(strings.NewReader(stdout))
	packages := map[string]TestPackage{}
	tests := map[string]TestCase{}
	for {
		var event struct {
			Action  string  `json:"Action"`
			Package string  `json:"Package"`
			Test    string  `json:"Test"`
			Elapsed float64 `json:"Elapsed"`
		}
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			report.ParseErrors = append(report.ParseErrors, err.Error())
			break
		}
		action := strings.TrimSpace(event.Action)
		if action != "pass" && action != "fail" && action != "skip" {
			continue
		}
		pkg := strings.TrimSpace(event.Package)
		test := strings.TrimSpace(event.Test)
		if test == "" {
			if pkg != "" {
				packages[pkg] = TestPackage{Name: pkg, Action: action, Elapsed: event.Elapsed}
			}
			continue
		}
		key := pkg + "\x00" + test
		tests[key] = TestCase{
			Package: pkg,
			Name:    test,
			Action:  action,
			Elapsed: event.Elapsed,
		}
	}
	for _, pkg := range packages {
		report.Packages = append(report.Packages, pkg)
	}
	for _, test := range tests {
		report.Tests = append(report.Tests, test)
		switch test.Action {
		case "pass":
			report.Passed++
		case "fail":
			report.Failed++
		case "skip":
			report.Skipped++
		}
	}
	sort.Slice(report.Packages, func(i, j int) bool {
		return report.Packages[i].Name < report.Packages[j].Name
	})
	sort.Slice(report.Tests, func(i, j int) bool {
		if report.Tests[i].Package == report.Tests[j].Package {
			return report.Tests[i].Name < report.Tests[j].Name
		}
		return report.Tests[i].Package < report.Tests[j].Package
	})
	report.Total = report.Passed + report.Failed + report.Skipped
	if report.Total == 0 && len(report.Packages) == 0 && len(report.ParseErrors) == 0 {
		return nil
	}
	return report
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
