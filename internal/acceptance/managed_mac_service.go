package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/service"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

const ManagedMacServicePlanSchemaVersion = "rdev.acceptance.managed-mac-service-plan.v1"

type ManagedMacServiceOptions struct {
	RepoRoot                 string
	OutDir                   string
	BinaryPath               string
	GatewayURL               string
	TicketCode               string
	ManifestURL              string
	Label                    string
	PlistOut                 string
	IdentityStore            string
	TrustStore               string
	NonceStore               string
	ApprovalStore            string
	WorkspaceLockStore       string
	LogDir                   string
	ReleaseBundle            string
	ReleaseRootPublicKey     string
	ReleaseRequiredArtifacts []string
	Transport                string
	LongPollTimeout          string
	Force                    bool
	Now                      time.Time
}

type ManagedMacServicePlan struct {
	SchemaVersion      string                    `json:"schema_version"`
	GeneratedAt        time.Time                 `json:"generated_at"`
	Platform           string                    `json:"platform"`
	RepoRoot           string                    `json:"repo_root,omitempty"`
	OutDir             string                    `json:"out_dir"`
	PlistPath          string                    `json:"plist_path"`
	LaunchAgent        service.LaunchAgent       `json:"launch_agent"`
	Status             service.LaunchAgentStatus `json:"status"`
	Checks             []Check                   `json:"checks"`
	Commands           []ServiceCommand          `json:"commands"`
	RequiredEvidence   []string                  `json:"required_evidence"`
	RecommendedActions []string                  `json:"recommended_actions"`
}

type ServiceCommand struct {
	Name    string   `json:"name"`
	Purpose string   `json:"purpose"`
	Shell   string   `json:"shell"`
	Argv    []string `json:"argv,omitempty"`
	Manual  bool     `json:"manual"`
}

func RunManagedMacServicePlan(ctx context.Context, opts ManagedMacServiceOptions) (ManagedMacServicePlan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return ManagedMacServicePlan{}, fmt.Errorf("out directory is required")
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return ManagedMacServicePlan{}, err
	}
	if err := prepareAcceptanceOut(outDir); err != nil {
		return ManagedMacServicePlan{}, err
	}
	repoRoot := ""
	if strings.TrimSpace(opts.RepoRoot) != "" {
		repoRoot, err = workspace.CanonicalDir(opts.RepoRoot)
		if err != nil {
			return ManagedMacServicePlan{}, err
		}
	}
	defaults, err := managedMacServiceDefaults(outDir, opts)
	if err != nil {
		return ManagedMacServicePlan{}, err
	}
	agent, err := service.NewMacOSLaunchAgent(service.LaunchAgentOptions{
		Label:                    defaults.Label,
		BinaryPath:               defaults.BinaryPath,
		GatewayURL:               defaults.GatewayURL,
		TicketCode:               defaults.TicketCode,
		ManifestURL:              defaults.ManifestURL,
		IdentityStorePath:        defaults.IdentityStore,
		TrustStorePath:           defaults.TrustStore,
		NonceStorePath:           defaults.NonceStore,
		ApprovalStorePath:        defaults.ApprovalStore,
		WorkspaceLockStorePath:   defaults.WorkspaceLockStore,
		ReleaseBundlePath:        defaults.ReleaseBundle,
		ReleaseRootPublicKey:     defaults.ReleaseRootPublicKey,
		ReleaseRequiredArtifacts: defaults.ReleaseRequiredArtifacts,
		LogDir:                   defaults.LogDir,
		Transport:                defaults.Transport,
		LongPollTimeout:          defaults.LongPollTimeout,
	})
	if err != nil {
		return ManagedMacServicePlan{}, err
	}
	content, err := service.RenderMacOSLaunchAgent(agent)
	if err != nil {
		return ManagedMacServicePlan{}, err
	}
	if err := writeAcceptanceFile(defaults.PlistOut, content, opts.Force); err != nil {
		return ManagedMacServicePlan{}, err
	}
	status, err := service.InspectMacOSLaunchAgent(defaults.PlistOut)
	if err != nil {
		return ManagedMacServicePlan{}, err
	}
	plan := ManagedMacServicePlan{
		SchemaVersion: ManagedMacServicePlanSchemaVersion,
		GeneratedAt:   now.UTC(),
		Platform:      "macos",
		RepoRoot:      repoRoot,
		OutDir:        outDir,
		PlistPath:     defaults.PlistOut,
		LaunchAgent:   agent,
		Status:        status,
		Commands:      managedMacServiceCommands(defaults, repoRoot),
		RequiredEvidence: []string{
			"Generated service-plan.json, written LaunchAgent plist, and verifier output.",
			"plutil -lint review transcript for the LaunchAgent plist.",
			"rdev host service-control --platform macos --action start --execute transcript proving the LaunchAgent was loaded.",
			"rdev host service-control --platform macos --action inspect --execute transcript or launchctl print output.",
			"rdev host startup output proving the release-bundle gate passed before registration.",
			"Gateway host registration, approval, trust refresh, and managed host audit events.",
			"Reconnect evidence after logout, login, or reboot.",
			"Managed Mac acceptance report and evidence bundle with diff, tests, audit chain, and approval-required artifacts.",
			"rdev acceptance verify output with ok=true for the service-backed managed Mac report.",
			"rdev host service-control --platform macos --action stop --execute transcript.",
			"rdev host uninstall-service --platform macos transcript confirming only the reviewed plist was removed.",
		},
		RecommendedActions: []string{
			"Review the generated plist before running launchctl.",
			"Start the LaunchAgent manually, then confirm reconnect through the gateway host registry.",
			"Run managed Mac coding acceptance against the service-backed host and verify the evidence.",
			"Stop and uninstall the LaunchAgent after acceptance unless this is a deliberate managed enrollment.",
		},
	}
	plan.Checks = managedMacServiceChecks(plan, defaults)
	if err := writeManagedMacServicePlan(filepath.Join(outDir, "service-plan.json"), plan); err != nil {
		return ManagedMacServicePlan{}, err
	}
	return plan, nil
}

type managedMacServiceResolvedOptions struct {
	BinaryPath               string
	GatewayURL               string
	TicketCode               string
	ManifestURL              string
	Label                    string
	PlistOut                 string
	IdentityStore            string
	TrustStore               string
	NonceStore               string
	ApprovalStore            string
	WorkspaceLockStore       string
	LogDir                   string
	ReleaseBundle            string
	ReleaseRootPublicKey     string
	ReleaseRequiredArtifacts []string
	Transport                string
	LongPollTimeout          string
}

func managedMacServiceDefaults(outDir string, opts ManagedMacServiceOptions) (managedMacServiceResolvedOptions, error) {
	binaryPath := strings.TrimSpace(opts.BinaryPath)
	if binaryPath == "" {
		current, err := os.Executable()
		if err != nil {
			return managedMacServiceResolvedOptions{}, err
		}
		binaryPath = current
	}
	if !filepath.IsAbs(binaryPath) {
		abs, err := filepath.Abs(binaryPath)
		if err != nil {
			return managedMacServiceResolvedOptions{}, err
		}
		binaryPath = abs
	}
	label := firstNonEmptyString(opts.Label, service.DefaultMacOSLaunchAgentLabel)
	plistOut := opts.PlistOut
	if strings.TrimSpace(plistOut) == "" {
		plistOut = filepath.Join(outDir, label+".plist")
	}
	if !filepath.IsAbs(plistOut) {
		plistOut = filepath.Join(outDir, plistOut)
	}
	hostDir := filepath.Join(outDir, "host-state")
	releaseRequired := append([]string(nil), opts.ReleaseRequiredArtifacts...)
	if strings.TrimSpace(opts.ReleaseBundle) != "" && len(releaseRequired) == 0 {
		releaseRequired = []string{"rdev", "rdev-host", "rdev-verify"}
	}
	return managedMacServiceResolvedOptions{
		BinaryPath:               binaryPath,
		GatewayURL:               opts.GatewayURL,
		TicketCode:               opts.TicketCode,
		ManifestURL:              opts.ManifestURL,
		Label:                    label,
		PlistOut:                 filepath.Clean(plistOut),
		IdentityStore:            firstNonEmptyString(opts.IdentityStore, filepath.Join(hostDir, "identity.json")),
		TrustStore:               firstNonEmptyString(opts.TrustStore, filepath.Join(hostDir, "trust-bundle.json")),
		NonceStore:               firstNonEmptyString(opts.NonceStore, filepath.Join(hostDir, "nonces.json")),
		ApprovalStore:            firstNonEmptyString(opts.ApprovalStore, filepath.Join(hostDir, "approvals.json")),
		WorkspaceLockStore:       firstNonEmptyString(opts.WorkspaceLockStore, filepath.Join(outDir, "workspace-locks")),
		LogDir:                   firstNonEmptyString(opts.LogDir, filepath.Join(outDir, "logs")),
		ReleaseBundle:            strings.TrimSpace(opts.ReleaseBundle),
		ReleaseRootPublicKey:     strings.TrimSpace(opts.ReleaseRootPublicKey),
		ReleaseRequiredArtifacts: releaseRequired,
		Transport:                firstNonEmptyString(opts.Transport, "long-poll"),
		LongPollTimeout:          firstNonEmptyString(opts.LongPollTimeout, "25s"),
	}, nil
}

func managedMacServiceChecks(plan ManagedMacServicePlan, opts managedMacServiceResolvedOptions) []Check {
	args := strings.Join(plan.LaunchAgent.ProgramArguments, "\x00")
	return []Check{
		{Name: "plist_written", Passed: plan.Status.Exists, Detail: plan.PlistPath},
		{Name: "plist_mode_0600", Passed: plan.Status.Mode == "0600", Detail: plan.Status.Mode},
		{Name: "label_matches", Passed: plan.Status.Label == opts.Label, Detail: plan.Status.Label},
		{Name: "run_at_load", Passed: plan.Status.RunAtLoad},
		{Name: "keep_alive", Passed: plan.Status.KeepAlive},
		{Name: "managed_mode_arg", Passed: strings.Contains(args, "--mode\x00managed")},
		{Name: "once_false_arg", Passed: strings.Contains(args, "--once=false")},
		{Name: "transport_arg", Passed: strings.Contains(args, "--transport\x00"+opts.Transport), Detail: opts.Transport},
		{Name: "workspace_lock_store_arg", Passed: strings.Contains(args, "--workspace-lock-store\x00"+opts.WorkspaceLockStore), Detail: opts.WorkspaceLockStore},
		{Name: "identity_store_arg", Passed: strings.Contains(args, "--identity-store\x00"+opts.IdentityStore), Detail: opts.IdentityStore},
		{Name: "trust_store_arg", Passed: strings.Contains(args, "--trust-store\x00"+opts.TrustStore), Detail: opts.TrustStore},
		{Name: "nonce_store_arg", Passed: strings.Contains(args, "--nonce-store\x00"+opts.NonceStore), Detail: opts.NonceStore},
		{Name: "approval_store_arg", Passed: strings.Contains(args, "--approval-store\x00"+opts.ApprovalStore), Detail: opts.ApprovalStore},
		{Name: "enrollment_arg", Passed: enrollmentConfigured(args), Detail: "ticket-code or manifest-url"},
		{Name: "release_bundle_arg", Passed: opts.ReleaseBundle != "" && strings.Contains(args, "--release-bundle\x00"+opts.ReleaseBundle), Detail: opts.ReleaseBundle},
		{Name: "release_root_arg", Passed: opts.ReleaseRootPublicKey != "" && strings.Contains(args, "--release-root-public-key\x00"+opts.ReleaseRootPublicKey)},
		{Name: "release_required_artifacts_arg", Passed: len(opts.ReleaseRequiredArtifacts) > 0 && strings.Contains(args, "--release-require-artifacts\x00"+strings.Join(opts.ReleaseRequiredArtifacts, ",")), Detail: strings.Join(opts.ReleaseRequiredArtifacts, ",")},
		{Name: "required_evidence_complete", Passed: managedMacServiceRequiredEvidenceComplete(plan.RequiredEvidence), Detail: missingManagedMacServiceEvidence(plan.RequiredEvidence)},
	}
}

func managedMacServiceRequiredEvidenceComplete(evidence []string) bool {
	return missingManagedMacServiceEvidence(evidence) == ""
}

func missingManagedMacServiceEvidence(evidence []string) string {
	joined := strings.ToLower(strings.Join(evidence, "\n"))
	required := []string{
		"plutil -lint",
		"service-control --platform macos --action start",
		"service-control --platform macos --action inspect",
		"release-bundle gate",
		"reconnect",
		"evidence bundle",
		"approval-required",
		"acceptance verify",
		"service-control --platform macos --action stop",
		"uninstall-service --platform macos",
	}
	var missing []string
	for _, value := range required {
		if !strings.Contains(joined, value) {
			missing = append(missing, value)
		}
	}
	return strings.Join(missing, ",")
}

func enrollmentConfigured(args string) bool {
	return strings.Contains(args, "--ticket-code\x00") || strings.Contains(args, "--manifest-url\x00")
}

func managedMacServiceCommands(opts managedMacServiceResolvedOptions, repoRoot string) []ServiceCommand {
	acceptanceOut := filepath.Join(filepath.Dir(opts.PlistOut), "managed-mac-run")
	acceptanceRun := "rdev acceptance managed-mac --out " + shellQuote(acceptanceOut)
	if repoRoot != "" {
		acceptanceRun += " --repo " + shellQuote(repoRoot)
	}
	return []ServiceCommand{
		{
			Name:    "review_plist",
			Purpose: "Inspect the managed host LaunchAgent before loading it.",
			Shell:   "plutil -lint " + shellQuote(opts.PlistOut) + " && cat " + shellQuote(opts.PlistOut),
			Argv:    []string{"plutil", "-lint", opts.PlistOut},
			Manual:  true,
		},
		{
			Name:    "start",
			Purpose: "Load the LaunchAgent for the current GUI user through rdev's explicit service-control command.",
			Shell:   "rdev host service-control --platform macos --action start --label " + shellQuote(opts.Label) + " --plist " + shellQuote(opts.PlistOut) + " --execute",
			Manual:  true,
		},
		{
			Name:    "inspect",
			Purpose: "Confirm launchd knows about the managed host service through rdev's service-control command.",
			Shell:   "rdev host service-control --platform macos --action inspect --label " + shellQuote(opts.Label) + " --plist " + shellQuote(opts.PlistOut) + " --execute",
			Manual:  true,
		},
		{
			Name:    "run_acceptance",
			Purpose: "Run the managed Mac coding acceptance against the enrolled service-backed host.",
			Shell:   acceptanceRun,
			Manual:  true,
		},
		{
			Name:    "verify_acceptance",
			Purpose: "Verify evidence, checksums, approval gate, audit chain, and workspace lock release.",
			Shell:   "rdev acceptance verify --report " + shellQuote(filepath.Join(acceptanceOut, "report.json")),
			Manual:  true,
		},
		{
			Name:    "stop",
			Purpose: "Stop the LaunchAgent after the service-backed acceptance run through rdev's service-control command.",
			Shell:   "rdev host service-control --platform macos --action stop --label " + shellQuote(opts.Label) + " --plist " + shellQuote(opts.PlistOut) + " --execute",
			Manual:  true,
		},
		{
			Name:    "uninstall",
			Purpose: "Remove the plist safely after stopping the service.",
			Shell:   "rdev host uninstall-service --platform macos --label " + shellQuote(opts.Label) + " --plist " + shellQuote(opts.PlistOut),
			Manual:  true,
		},
	}
}

func writeAcceptanceFile(path string, content []byte, force bool) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	flags := os.O_CREATE | os.O_WRONLY
	if force {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	file, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func writeManagedMacServicePlan(path string, plan ManagedMacServicePlan) error {
	content, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(path, content, 0o600)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
