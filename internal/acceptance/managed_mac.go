package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/codexadapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/evidence"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostapproval"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostidentity"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostnonce"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostrunner"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

const ManagedMacReportSchemaVersion = "rdev.acceptance.managed-mac.v1"

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
	GeneratedAt          time.Time                   `json:"generated_at"`
	Mode                 string                      `json:"mode"`
	FixtureRepo          bool                        `json:"fixture_repo"`
	RepoRoot             string                      `json:"repo_root"`
	OutDir               string                      `json:"out_dir"`
	WorkspaceLockStore   string                      `json:"workspace_lock_store"`
	Ticket               model.Ticket                `json:"ticket"`
	Host                 model.Host                  `json:"host"`
	Worktree             workspace.GitWorktreeResult `json:"worktree"`
	CodingJob            model.Job                   `json:"coding_job"`
	ApprovalJob          model.Job                   `json:"approval_job"`
	CodingArtifacts      []model.Artifact            `json:"coding_artifacts"`
	ApprovalArtifacts    []model.Artifact            `json:"approval_artifacts"`
	EvidenceDir          string                      `json:"evidence_dir"`
	ApprovalEvidenceDir  string                      `json:"approval_evidence_dir"`
	EvidenceManifest     evidence.Manifest           `json:"evidence_manifest"`
	ApprovalManifest     evidence.Manifest           `json:"approval_manifest"`
	Checks               []Check                     `json:"checks"`
	RecommendedNextSteps []string                    `json:"recommended_next_steps"`
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
	ticket, err := gw.CreateTicket(model.HostModeManaged, 7200, capabilities, "managed Mac acceptance")
	if err != nil {
		return ManagedMacReport{}, err
	}
	identity, err := hostidentity.Generate("acceptance-host")
	if err != nil {
		return ManagedMacReport{}, err
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "managed-mac-acceptance",
		OS:                  runtime.GOOS,
		Arch:                runtime.GOARCH,
		Capabilities:        capabilities,
		IdentityKeyID:       identity.KeyID,
		IdentityPublicKey:   identity.EncodedPublicKey(),
		IdentityFingerprint: identity.Fingerprint(),
	})
	if err != nil {
		return ManagedMacReport{}, err
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		return ManagedMacReport{}, err
	}

	prepareJobID := "job_acceptance_prepare"
	worktree, err := workspace.PrepareGitWorktree(ctx, workspace.GitWorktreeOptions{
		StoreDir:     workspaceLockStore,
		RepoRoot:     repoRoot,
		HostID:       host.ID,
		JobID:        prepareJobID,
		OwnerAdapter: "codex",
		BaseRef:      "HEAD",
		Branch:       "rdev/acceptance-managed-mac",
		WorktreeRoot: worktreeRoot,
		TTL:          workspace.DefaultLockTTL,
	}, now)
	if err != nil {
		return ManagedMacReport{}, err
	}
	_, _, _ = workspace.NewFileLockStore(workspaceLockStore).Release(repoRoot, prepareJobID, false)

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
	codingJob, err := gw.CreateJob(host.ID, "codex", "managed Mac acceptance coding job", codingPolicy)
	if err != nil {
		return ManagedMacReport{}, err
	}
	runnerOpts := hostrunner.Options{
		IdentityFingerprint: identity.Fingerprint(),
		NonceStore:          hostnonce.NewMemoryStore(),
		ApprovalStore:       hostapproval.NewMemoryStore(),
		WorkspaceLockStore:  workspaceLockStore,
	}
	codingResult, err := hostrunner.RunDevJobWithOptionsContext(ctx, host.ID, gw.TrustBundle(), codingJob, now, runnerOpts)
	if err != nil {
		_, _, _ = gw.FailJobForHostWithArtifact(host.ID, codingJob.ID, err.Error(), codingResult.ArtifactContent)
		return ManagedMacReport{}, err
	}
	codingJob, _, err = gw.CompleteJobForHost(host.ID, codingJob.ID, codingResult.ArtifactContent)
	if err != nil {
		return ManagedMacReport{}, err
	}

	approvalJob, approvalArtifacts, approvalManifest, err := runApprovalGateProbe(ctx, gw, host, identity, worktree.WorktreePath, workspaceLockStore, now, outDir)
	if err != nil {
		return ManagedMacReport{}, err
	}

	codingArtifacts := gw.Artifacts(codingJob.ID)
	evidenceDir := filepath.Join(outDir, "evidence")
	evidenceManifest, err := evidence.ExportDirectory(evidenceDir, evidence.Input{
		Job:         codingJob,
		Artifacts:   codingArtifacts,
		AuditEvents: gw.AuditEvents(),
		GeneratedAt: now,
	})
	if err != nil {
		return ManagedMacReport{}, err
	}

	status, err := workspace.NewFileLockStore(workspaceLockStore).Status(worktree.WorktreePath, now)
	if err != nil {
		return ManagedMacReport{}, err
	}
	report := ManagedMacReport{
		SchemaVersion:       ManagedMacReportSchemaVersion,
		GeneratedAt:         now.UTC(),
		Mode:                string(model.HostModeManaged),
		FixtureRepo:         fixture,
		RepoRoot:            repoRoot,
		OutDir:              outDir,
		WorkspaceLockStore:  workspaceLockStore,
		Ticket:              ticket,
		Host:                host,
		Worktree:            worktree,
		CodingJob:           codingJob,
		ApprovalJob:         approvalJob,
		CodingArtifacts:     codingArtifacts,
		ApprovalArtifacts:   approvalArtifacts,
		EvidenceDir:         evidenceDir,
		ApprovalEvidenceDir: filepath.Join(outDir, "approval-evidence"),
		EvidenceManifest:    evidenceManifest,
		ApprovalManifest:    approvalManifest,
		Checks: acceptanceChecks(acceptanceCheckInput{
			Host:              host,
			Worktree:          worktree,
			CodingJob:         codingJob,
			CodingArtifacts:   codingArtifacts,
			ApprovalJob:       approvalJob,
			ApprovalArtifacts: approvalArtifacts,
			LockStatus:        status,
			EvidenceManifest:  evidenceManifest,
			Fixture:           fixture,
		}),
		RecommendedNextSteps: []string{
			"Review evidence/manifest.json and artifacts before approving any external consequence.",
			"Use a separate approval token before push, merge, deploy, credential, GUI, package, elevation, or service actions.",
			"For a real managed Mac run, install the LaunchAgent explicitly and repeat this acceptance command with --repo.",
		},
	}
	if err := writeReport(filepath.Join(outDir, "report.json"), report); err != nil {
		return ManagedMacReport{}, err
	}
	return report, nil
}

func runApprovalGateProbe(ctx context.Context, gw *gateway.MemoryGateway, host model.Host, identity hostidentity.Identity, workspaceRoot, lockStore string, now time.Time, outDir string) (model.Job, []model.Artifact, evidence.Manifest, error) {
	approvalJob, err := gw.CreateJob(host.ID, "shell", "attempt git push should require approval", map[string]any{
		"workspace_root":       workspaceRoot,
		"capabilities":         []string{"shell.user"},
		"argv":                 []string{"git", "push", "origin", "main"},
		"allow_commands":       []string{"git"},
		"max_duration_seconds": 30,
		"max_output_bytes":     4096,
	})
	if err != nil {
		return model.Job{}, nil, evidence.Manifest{}, err
	}
	result, err := hostrunner.RunDevJobWithOptionsContext(ctx, host.ID, gw.TrustBundle(), approvalJob, now, hostrunner.Options{
		IdentityFingerprint: identity.Fingerprint(),
		NonceStore:          hostnonce.NewMemoryStore(),
		ApprovalStore:       hostapproval.NewMemoryStore(),
		WorkspaceLockStore:  lockStore,
	})
	var approvalErr hostrunner.ApprovalRequiredError
	if !errors.As(err, &approvalErr) {
		return model.Job{}, nil, evidence.Manifest{}, fmt.Errorf("expected approval-required probe, got %v", err)
	}
	approvalJob, _, err = gw.FailJobForHostWithArtifact(host.ID, approvalJob.ID, approvalErr.Error(), result.ArtifactContent)
	if err != nil {
		return model.Job{}, nil, evidence.Manifest{}, err
	}
	artifacts := gw.Artifacts(approvalJob.ID)
	manifest, err := evidence.ExportDirectory(filepath.Join(outDir, "approval-evidence"), evidence.Input{
		Job:         approvalJob,
		Artifacts:   artifacts,
		AuditEvents: gw.AuditEvents(),
		GeneratedAt: now,
	})
	return approvalJob, artifacts, manifest, err
}

type acceptanceCheckInput struct {
	Host              model.Host
	Worktree          workspace.GitWorktreeResult
	CodingJob         model.Job
	CodingArtifacts   []model.Artifact
	ApprovalJob       model.Job
	ApprovalArtifacts []model.Artifact
	LockStatus        workspace.LockStatus
	EvidenceManifest  evidence.Manifest
	Fixture           bool
}

func acceptanceChecks(input acceptanceCheckInput) []Check {
	artifactText := joinedArtifacts(input.CodingArtifacts)
	approvalText := joinedArtifacts(input.ApprovalArtifacts)
	return []Check{
		{Name: "host_mode_managed", Passed: input.Host.Mode == model.HostModeManaged, Detail: string(input.Host.Mode)},
		{Name: "host_active", Passed: input.Host.Status == model.HostStatusActive, Detail: string(input.Host.Status)},
		{Name: "worktree_created", Passed: input.Worktree.WorktreePath != "" && pathExists(input.Worktree.WorktreePath), Detail: input.Worktree.WorktreePath},
		{Name: "coding_job_succeeded", Passed: input.CodingJob.Status == model.JobStatusSucceeded, Detail: string(input.CodingJob.Status)},
		{Name: "codex_result_artifact", Passed: strings.Contains(artifactText, codexadapter.ResultSchemaVersion)},
		{Name: "diff_evidence_present", Passed: strings.Contains(artifactText, "git_diff") && strings.Contains(artifactText, "git_diff_stat")},
		{Name: "verification_evidence_present", Passed: strings.Contains(artifactText, "verification_results")},
		{Name: "test_report_present_when_fixture", Passed: !input.Fixture || strings.Contains(artifactText, codexadapter.TestReportSchemaVersion)},
		{Name: "approval_required_probe", Passed: strings.Contains(approvalText, hostrunner.ApprovalRequiredSchemaVersion) && strings.Contains(approvalText, "git.push")},
		{Name: "workspace_lock_released", Passed: !input.LockStatus.Exists, Detail: input.LockStatus.StorePath},
		{Name: "evidence_bundle_written", Passed: input.EvidenceManifest.SchemaVersion == evidence.BundleSchemaVersion && input.EvidenceManifest.JobID == input.CodingJob.ID},
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
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(outDir, 0o700)
}

func writeReport(path string, report ManagedMacReport) error {
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(path, content, 0o600)
}

func joinedArtifacts(artifacts []model.Artifact) string {
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
