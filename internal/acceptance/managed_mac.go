package acceptance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/bootstrapcmd/windowsentry"
	"github.com/EitanWong/remote-dev-skillkit/internal/codexadapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostidentity"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostrunner"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

const ManagedMacReportSchemaVersion = "rdev.acceptance.managed-mac.v1"
const SessionEvidenceSchemaVersion = "rdev.session-evidence.v1"

type ManagedMacOptions struct {
	RepoRoot                  string
	OutDir                    string
	WorktreeRoot              string
	WorkspaceLockStore        string
	CodexCommand              string
	CodexArgs                 []string
	Prompt                    string
	VerificationCommands      [][]string
	AllowVerificationCommands []string
	MaxDurationSeconds        int
	MaxOutputBytes            int
	Now                       time.Time
}

type ManagedMacReport struct {
	SchemaVersion        string                      `json:"schema_version"`
	Protocol             string                      `json:"protocol"`
	GeneratedAt          time.Time                   `json:"generated_at"`
	Mode                 string                      `json:"mode"`
	FixtureRepo          bool                        `json:"fixture_repo"`
	RepoRoot             string                      `json:"repo_root"`
	OutDir               string                      `json:"out_dir"`
	WorkspaceLockStore   string                      `json:"workspace_lock_store"`
	SessionID            string                      `json:"session_id"`
	SessionStatus        controlplane.SessionStatus  `json:"session_status"`
	TargetEndpoint       controlplane.Endpoint       `json:"target_endpoint"`
	Worktree             workspace.GitWorktreeResult `json:"worktree"`
	CodingTask           controlplane.Task           `json:"coding_task"`
	SideEffectProbeTask  controlplane.Task           `json:"side_effect_probe_task"`
	CodingArtifacts      []SessionArtifact           `json:"coding_artifacts"`
	ProbeArtifacts       []SessionArtifact           `json:"side_effect_probe_artifacts"`
	EvidenceDir          string                      `json:"evidence_dir"`
	ProbeEvidenceDir     string                      `json:"side_effect_probe_evidence_dir"`
	EvidenceManifest     SessionEvidenceManifest     `json:"evidence_manifest"`
	ProbeManifest        SessionEvidenceManifest     `json:"side_effect_probe_manifest"`
	Checks               []Check                     `json:"checks"`
	RecommendedNextSteps []string                    `json:"recommended_next_steps"`
}

type SessionArtifact struct {
	Ref     controlplane.ArtifactRef `json:"ref"`
	Content string                   `json:"content"`
}

type SessionEvidenceManifest struct {
	SchemaVersion string                     `json:"schema_version"`
	GeneratedAt   time.Time                  `json:"generated_at"`
	SessionID     string                     `json:"session_id"`
	TaskID        string                     `json:"task_id"`
	TaskStatus    controlplane.TaskStatus    `json:"task_status"`
	Artifacts     []controlplane.ArtifactRef `json:"artifacts"`
	Files         []SessionEvidenceFile      `json:"files"`
}

type SessionEvidenceFile struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

func RunManagedMac(ctx context.Context, opts ManagedMacOptions) (ManagedMacReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return ManagedMacReport{}, fmt.Errorf("out directory is required")
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return ManagedMacReport{}, err
	}
	if err := prepareAcceptanceOut(outDir); err != nil {
		return ManagedMacReport{}, err
	}

	repoRoot := opts.RepoRoot
	fixture := strings.TrimSpace(repoRoot) == ""
	if fixture {
		repoRoot = filepath.Join(outDir, "fixture-repo")
		if err := createFixtureRepo(ctx, repoRoot); err != nil {
			return ManagedMacReport{}, err
		}
	}
	repoRoot, err = workspace.CanonicalDir(repoRoot)
	if err != nil {
		return ManagedMacReport{}, err
	}

	workspaceLockStore := opts.WorkspaceLockStore
	if strings.TrimSpace(workspaceLockStore) == "" {
		workspaceLockStore = filepath.Join(outDir, "workspace-locks")
	}
	worktreeRoot := opts.WorktreeRoot
	if strings.TrimSpace(worktreeRoot) == "" {
		worktreeRoot = filepath.Join(outDir, "worktrees")
	}

	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	capabilities := []string{"shell.user", "codex.run", "git.diff"}
	identity, err := hostidentity.Generate("acceptance-host")
	if err != nil {
		return ManagedMacReport{}, err
	}
	session, err := gw.CreateSession(controlplane.SessionSpec{
		Profile:            "managed",
		Reason:             "managed Mac acceptance",
		Capabilities:       capabilities,
		JoinPolicy:         "single-target",
		SelectedGatewayURL: "local://acceptance",
		AuthorityID:        "acceptance-gateway",
		ExpiresAt:          now.UTC().Add(2 * time.Hour),
	})
	if err != nil {
		return ManagedMacReport{}, err
	}
	_, targetEndpoint, _, err := gw.JoinSession(session.ID, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Name:                "managed-mac-acceptance",
		Platform:            runtime.GOOS + "/" + runtime.GOARCH,
		IdentityFingerprint: identity.Fingerprint(),
		Capabilities:        capabilities,
		Transport:           controlplane.TransportLocal,
	})
	if err != nil {
		return ManagedMacReport{}, err
	}

	prepareTaskID := "task_acceptance_prepare"
	worktree, err := workspace.PrepareGitWorktree(ctx, workspace.GitWorktreeOptions{
		StoreDir:     workspaceLockStore,
		RepoRoot:     repoRoot,
		HostID:       targetEndpoint.ID,
		TaskID:       prepareTaskID,
		OwnerAdapter: "codex",
		BaseRef:      "HEAD",
		Branch:       "rdev/acceptance-managed-mac",
		WorktreeRoot: worktreeRoot,
		TTL:          workspace.DefaultLockTTL,
	}, now)
	if err != nil {
		return ManagedMacReport{}, err
	}
	_, _, _ = workspace.NewFileLockStore(workspaceLockStore).Release(repoRoot, prepareTaskID, false)

	verificationCommands, allowVerificationCommands := managedMacVerificationCommands(opts, fixture)
	prompt := strings.TrimSpace(opts.Prompt)
	if prompt == "" {
		prompt = "Make a small scoped change in this repository, then leave reviewable diff and test evidence."
	}
	codingPolicy := map[string]any{
		"workspace_root":              worktree.WorktreePath,
		"write_scope":                 []string{worktree.WorktreePath},
		"branch":                      worktree.Branch,
		"capabilities":                []string{"codex.run", "git.diff"},
		"prompt":                      prompt,
		"verification_commands":       verificationCommands,
		"allow_verification_commands": allowVerificationCommands,
		"max_duration_seconds":        positiveOrDefault(opts.MaxDurationSeconds, 300),
		"max_output_bytes":            positiveOrDefault(opts.MaxOutputBytes, 1024*1024),
	}
	if strings.TrimSpace(opts.CodexCommand) != "" {
		codingPolicy["codex_command"] = opts.CodexCommand
	}
	if len(opts.CodexArgs) > 0 {
		codingPolicy["codex_args"] = opts.CodexArgs
	}
	runnerOpts := hostrunner.Options{
		IdentityFingerprint: identity.Fingerprint(),
		WorkspaceLockStore:  workspaceLockStore,
	}
	codingTask, codingArtifact, err := runManagedMacSessionTask(ctx, gw, session.ID, targetEndpoint, identity, managedMacSessionTaskRequest{
		Adapter:        "codex",
		Intent:         "managed Mac acceptance coding task",
		Capabilities:   []string{"codex.run", "git.diff"},
		Payload:        codingPolicy,
		IdempotencyKey: "managed-mac-coding",
		ArtifactName:   "coding-result.json",
	}, now, runnerOpts)
	if err != nil {
		return ManagedMacReport{}, err
	}

	probeTask, probeArtifact, err := runManagedMacSessionTask(ctx, gw, session.ID, targetEndpoint, identity, managedMacSessionTaskRequest{
		Adapter:      "shell",
		Intent:       "attempt git push side effect should fail safely",
		Capabilities: nil,
		Payload: map[string]any{
			"workspace_root":       worktree.WorktreePath,
			"write_scope":          []string{worktree.WorktreePath},
			"branch":               worktree.Branch,
			"argv":                 []string{"git", "push", "origin", "main"},
			"allow_commands":       []string{"git"},
			"max_duration_seconds": 30,
			"max_output_bytes":     4096,
		},
		IdempotencyKey: "managed-mac-side-effect-probe",
		ArtifactName:   "side-effect-probe-result.json",
	}, now, runnerOpts)
	if err == nil {
		return ManagedMacReport{}, fmt.Errorf("expected side-effect probe to fail before external consequence")
	}
	var denialErr hostrunner.DenialError
	if !errors.As(err, &denialErr) {
		return ManagedMacReport{}, fmt.Errorf("expected session task denial for side-effect probe, got %v", err)
	}
	codingArtifacts := []SessionArtifact{codingArtifact}
	probeArtifacts := []SessionArtifact{probeArtifact}
	evidenceDir := filepath.Join(outDir, "evidence")
	evidenceManifest, err := writeSessionEvidenceDirectory(evidenceDir, session.ID, codingTask, codingArtifacts, now)
	if err != nil {
		return ManagedMacReport{}, err
	}
	probeEvidenceDir := filepath.Join(outDir, "side-effect-probe-evidence")
	probeManifest, err := writeSessionEvidenceDirectory(probeEvidenceDir, session.ID, probeTask, probeArtifacts, now)
	if err != nil {
		return ManagedMacReport{}, err
	}

	status, err := workspace.NewFileLockStore(workspaceLockStore).Status(worktree.WorktreePath, now)
	if err != nil {
		return ManagedMacReport{}, err
	}
	session, err = gw.Session(session.ID)
	if err != nil {
		return ManagedMacReport{}, err
	}
	report := ManagedMacReport{
		SchemaVersion:       ManagedMacReportSchemaVersion,
		Protocol:            controlplane.SessionSchemaVersion,
		GeneratedAt:         now.UTC(),
		Mode:                "managed",
		FixtureRepo:         fixture,
		RepoRoot:            repoRoot,
		OutDir:              outDir,
		WorkspaceLockStore:  workspaceLockStore,
		SessionID:           session.ID,
		SessionStatus:       session.Status,
		TargetEndpoint:      targetEndpoint,
		Worktree:            worktree,
		CodingTask:          codingTask,
		SideEffectProbeTask: probeTask,
		CodingArtifacts:     codingArtifacts,
		ProbeArtifacts:      probeArtifacts,
		EvidenceDir:         evidenceDir,
		ProbeEvidenceDir:    probeEvidenceDir,
		EvidenceManifest:    evidenceManifest,
		ProbeManifest:       probeManifest,
		Checks: acceptanceChecks(acceptanceCheckInput{
			Session:               session,
			TargetEndpoint:        targetEndpoint,
			Worktree:              worktree,
			CodingTask:            codingTask,
			CodingArtifacts:       codingArtifacts,
			SideEffectProbeTask:   probeTask,
			SideEffectProbeOutput: probeArtifact.Content,
			LockStatus:            status,
			EvidenceManifest:      evidenceManifest,
			Fixture:               fixture,
		}),
		RecommendedNextSteps: []string{
			"Review evidence/manifest.json, side-effect-probe-evidence/manifest.json, and session task artifacts before any external consequence.",
			"Use a fresh explicit session task for push, merge, deploy, credential, GUI, package, elevation, or service actions.",
			"For a real managed Mac run, install the LaunchAgent explicitly and repeat this acceptance command with --repo.",
		},
	}
	if err := writeReport(filepath.Join(outDir, "report.json"), report); err != nil {
		return ManagedMacReport{}, err
	}
	return report, nil
}

type managedMacSessionTaskRequest struct {
	Adapter        string
	Intent         string
	Capabilities   []string
	Payload        map[string]any
	IdempotencyKey string
	ArtifactName   string
}

func runManagedMacSessionTask(ctx context.Context, gw *gateway.MemoryGateway, sessionID string, endpoint controlplane.Endpoint, identity hostidentity.Identity, req managedMacSessionTaskRequest, now time.Time, opts hostrunner.Options) (controlplane.Task, SessionArtifact, error) {
	task, _, err := gw.SubmitSessionTask(sessionID, controlplane.TaskSpec{
		TargetEndpointID: endpoint.ID,
		Adapter:          req.Adapter,
		Intent:           req.Intent,
		Capabilities:     append([]string(nil), req.Capabilities...),
		Payload:          cloneStringAnyMap(req.Payload),
		Limits: map[string]any{
			"max_duration_seconds": intValueFromAny(req.Payload["max_duration_seconds"]),
			"max_output_bytes":     intValueFromAny(req.Payload["max_output_bytes"]),
			"network":              stringValueFromAny(req.Payload["network"]),
		},
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return controlplane.Task{}, SessionArtifact{}, err
	}
	if _, err := gw.MarkSessionTaskRunning(sessionID, task.ID); err != nil {
		return controlplane.Task{}, SessionArtifact{}, err
	}

	result, runErr := hostrunner.RunSessionTaskWithOptionsContext(ctx, managedMacHostTaskSpec(task, endpoint.ID, identity.Fingerprint()), now, opts)
	status := string(controlplane.TaskStatusSucceeded)
	resultPayload := map[string]any{
		"status":           status,
		"attempt_id":       task.AttemptID,
		"idempotency_key":  req.IdempotencyKey + "-result",
		"artifact_content": result.ArtifactContent,
	}
	if runErr != nil {
		status = string(controlplane.TaskStatusFailed)
		resultPayload["status"] = status
		resultPayload["reason"] = runErr.Error()
	}
	if result.RuntimeFixtureContent != "" {
		resultPayload["runtime_fixture_content"] = result.RuntimeFixtureContent
	}
	completed, _, completeErr := gw.CompleteSessionTask(sessionID, task.ID, resultPayload)
	if completeErr != nil {
		if runErr != nil {
			return completed, SessionArtifact{}, fmt.Errorf("%v; additionally failed to complete session task: %w", runErr, completeErr)
		}
		return completed, SessionArtifact{}, completeErr
	}
	artifactContent := result.ArtifactContent
	if strings.TrimSpace(artifactContent) == "" && runErr != nil {
		artifactContent = fmt.Sprintf(`{"schema_version":"rdev.task-error.v1","task_id":%q,"error":%q}`, completed.ID, runErr.Error())
	}
	artifact, _, artifactErr := gw.UpsertSessionArtifact(sessionID, sessionArtifactRef(completed.ID, req.ArtifactName, artifactContent))
	sessionArtifact := SessionArtifact{Ref: artifact, Content: artifactContent}
	if artifactErr != nil {
		if runErr != nil {
			return completed, sessionArtifact, fmt.Errorf("%v; additionally failed to upsert session artifact: %w", runErr, artifactErr)
		}
		return completed, sessionArtifact, artifactErr
	}
	return completed, sessionArtifact, runErr
}

func managedMacHostTaskSpec(task controlplane.Task, endpointID, identityFingerprint string) hostrunner.SessionTaskSpec {
	payload := cloneStringAnyMap(task.Payload)
	writeScope := stringSliceFromAny(payload["write_scope"])
	if len(writeScope) == 0 {
		writeScope = []string{stringValueFromAny(payload["workspace_root"])}
	}
	return hostrunner.SessionTaskSpec{
		TaskID:              task.ID,
		EndpointID:          endpointID,
		IdentityFingerprint: identityFingerprint,
		Adapter:             task.Adapter,
		Intent:              task.Intent,
		Workspace: model.TaskWorkspace{
			Root:       stringValueFromAny(payload["workspace_root"]),
			WriteScope: writeScope,
			Branch:     stringValueFromAny(payload["branch"]),
		},
		Capabilities: append([]string(nil), task.Capabilities...),
		Limits: model.TaskLimits{
			MaxDurationSeconds: intValueFromAny(firstPresent(payload["max_duration_seconds"], task.Limits["max_duration_seconds"])),
			MaxOutputBytes:     intValueFromAny(firstPresent(payload["max_output_bytes"], task.Limits["max_output_bytes"])),
			Network:            stringValueFromAny(firstPresent(payload["network"], task.Limits["network"])),
		},
		Payload: payload,
	}
}

func sessionArtifactRef(taskID, name, content string) controlplane.ArtifactRef {
	sum := sha256.Sum256([]byte(content))
	return controlplane.ArtifactRef{
		TaskID:       taskID,
		Kind:         "json",
		Name:         name,
		SizeBytes:    int64(len(content)),
		SHA256:       fmt.Sprintf("%x", sum),
		ContentType:  "application/json",
		UploadOffset: int64(len(content)),
		Complete:     true,
	}
}

func writeSessionEvidenceDirectory(dir, sessionID string, task controlplane.Task, artifacts []SessionArtifact, now time.Time) (SessionEvidenceManifest, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return SessionEvidenceManifest{}, err
	}
	manifest := SessionEvidenceManifest{
		SchemaVersion: SessionEvidenceSchemaVersion,
		GeneratedAt:   now.UTC(),
		SessionID:     sessionID,
		TaskID:        task.ID,
		TaskStatus:    task.Status,
	}
	var checksums strings.Builder
	for i, artifact := range artifacts {
		name := safeEvidenceFileName(artifact.Ref.Name, i)
		content := []byte(artifact.Content)
		sum := sha256.Sum256(content)
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			return SessionEvidenceManifest{}, err
		}
		manifest.Artifacts = append(manifest.Artifacts, artifact.Ref)
		manifest.Files = append(manifest.Files, SessionEvidenceFile{
			Path:      name,
			Kind:      artifact.Ref.Kind,
			SizeBytes: int64(len(content)),
			SHA256:    fmt.Sprintf("%x", sum),
		})
		checksums.WriteString(fmt.Sprintf("%x  %s\n", sum, name))
	}
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return SessionEvidenceManifest{}, err
	}
	content = append(content, '\n')
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), content, 0o600); err != nil {
		return SessionEvidenceManifest{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(checksums.String()), 0o600); err != nil {
		return SessionEvidenceManifest{}, err
	}
	return manifest, nil
}

func safeEvidenceFileName(name string, index int) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = fmt.Sprintf("artifact-%d.json", index+1)
	}
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "/", "_")
	if !strings.HasSuffix(name, ".json") {
		name += ".json"
	}
	return name
}

type acceptanceCheckInput struct {
	Session               controlplane.Session
	TargetEndpoint        controlplane.Endpoint
	Worktree              workspace.GitWorktreeResult
	CodingTask            controlplane.Task
	CodingArtifacts       []SessionArtifact
	SideEffectProbeTask   controlplane.Task
	SideEffectProbeOutput string
	LockStatus            workspace.LockStatus
	EvidenceManifest      SessionEvidenceManifest
	Fixture               bool
}

func acceptanceChecks(input acceptanceCheckInput) []Check {
	artifactText := joinedArtifacts(input.CodingArtifacts)
	return []Check{
		{Name: "session_protocol", Passed: input.Session.SchemaVersion == controlplane.SessionSchemaVersion && input.Session.ID != "", Detail: input.Session.ID},
		{Name: "target_endpoint_online", Passed: input.TargetEndpoint.State == controlplane.EndpointStateOnline, Detail: string(input.TargetEndpoint.State)},
		{Name: "worktree_created", Passed: input.Worktree.WorktreePath != "" && pathExists(input.Worktree.WorktreePath), Detail: input.Worktree.WorktreePath},
		{Name: "coding_task_succeeded", Passed: input.CodingTask.Status == controlplane.TaskStatusSucceeded, Detail: string(input.CodingTask.Status)},
		{Name: "codex_result_artifact", Passed: strings.Contains(artifactText, codexadapter.ResultSchemaVersion)},
		{Name: "diff_evidence_present", Passed: strings.Contains(artifactText, "git_diff") && strings.Contains(artifactText, "git_diff_stat")},
		{Name: "verification_evidence_present", Passed: strings.Contains(artifactText, "verification_results")},
		{Name: "test_report_present_when_fixture", Passed: !input.Fixture || strings.Contains(artifactText, codexadapter.TestReportSchemaVersion)},
		{Name: "side_effect_probe_failed", Passed: input.SideEffectProbeTask.Status == controlplane.TaskStatusFailed && strings.Contains(input.SideEffectProbeOutput, hostrunner.DenialSchemaVersion), Detail: string(input.SideEffectProbeTask.Status)},
		{Name: "workspace_lock_released", Passed: !input.LockStatus.Exists, Detail: input.LockStatus.StorePath},
		{Name: "session_evidence_written", Passed: input.EvidenceManifest.SchemaVersion == SessionEvidenceSchemaVersion && input.EvidenceManifest.TaskID == input.CodingTask.ID},
	}
}

func managedMacVerificationCommands(opts ManagedMacOptions, fixture bool) ([][]string, []string) {
	commands := cloneMatrix(opts.VerificationCommands)
	allow := append([]string(nil), opts.AllowVerificationCommands...)
	if len(commands) == 0 {
		commands = [][]string{{"git", "status", "--short"}}
		allow = append(allow, "git")
		if fixture {
			commands = append(commands, []string{"go", "test", "-json", "./..."})
			allow = append(allow, "go")
		}
	}
	if len(allow) == 0 {
		seen := map[string]bool{}
		for _, command := range commands {
			if len(command) > 0 && strings.TrimSpace(command[0]) != "" && !seen[command[0]] {
				allow = append(allow, command[0])
				seen[command[0]] = true
			}
		}
	}
	return commands, allow
}

func createFixtureRepo(ctx context.Context, repoRoot string) error {
	if err := os.MkdirAll(repoRoot, 0o700); err != nil {
		return err
	}
	files := map[string]string{
		"go.mod":        "module example.com/rdev-acceptance\n\ngo 1.22\n",
		"hello.go":      "package acceptancefixture\n\nfunc Message() string { return \"hello\" }\n",
		"hello_test.go": "package acceptancefixture\n\nimport \"testing\"\n\nfunc TestMessage(t *testing.T) {\n\tif Message() != \"hello\" {\n\t\tt.Fatal(\"unexpected message\")\n\t}\n}\n",
		"README.md":     "# rdev acceptance fixture\n\nInitial content.\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(repoRoot, name), []byte(content), 0o600); err != nil {
			return err
		}
	}
	if _, err := runCommand(ctx, repoRoot, "git", "init"); err != nil {
		return err
	}
	if _, err := runCommand(ctx, repoRoot, "git", "add", "."); err != nil {
		return err
	}
	_, err := runCommand(ctx, repoRoot, "git", "-c", "user.name=rdev", "-c", "user.email=rdev@example.invalid", "commit", "-m", "init acceptance fixture")
	return err
}

func runCommand(ctx context.Context, dir string, argv ...string) (string, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%s failed: %s", strings.Join(argv, " "), strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func prepareAcceptanceOut(outDir string) error {
	if entries, err := os.ReadDir(outDir); err == nil {
		if len(entries) > 0 {
			return fmt.Errorf("out directory must be empty: %s", outDir)
		}
		return windowsentry.ProtectPrivatePath(outDir, true)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return err
	}
	return windowsentry.ProtectPrivatePath(outDir, true)
}

func writeReport(path string, report ManagedMacReport) error {
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(path, content, 0o600)
}

func joinedArtifacts(artifacts []SessionArtifact) string {
	var builder strings.Builder
	for _, artifact := range artifacts {
		builder.WriteString(artifact.Content)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func cloneMatrix(values [][]string) [][]string {
	result := make([][]string, 0, len(values))
	for _, row := range values {
		result = append(result, append([]string(nil), row...))
	}
	return result
}

func positiveOrDefault(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func cloneStringAnyMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func firstPresent(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func stringValueFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func intValueFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	default:
		return 0
	}
}
