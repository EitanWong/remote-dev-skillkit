package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/acceptance"
	"github.com/EitanWong/remote-dev-skillkit/internal/audit"
	"github.com/EitanWong/remote-dev-skillkit/internal/buildinfo"
	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/evidence"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostapproval"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostcap"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostidentity"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostnonce"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostrunner"
	"github.com/EitanWong/remote-dev-skillkit/internal/hosttrust"
	"github.com/EitanWong/remote-dev-skillkit/internal/httpapi"
	"github.com/EitanWong/remote-dev-skillkit/internal/mcpstdio"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
	"github.com/EitanWong/remote-dev-skillkit/internal/release"
	"github.com/EitanWong/remote-dev-skillkit/internal/service"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
	"github.com/EitanWong/remote-dev-skillkit/internal/skillkit"
	"github.com/EitanWong/remote-dev-skillkit/internal/trustref"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

type App struct {
	Stdout io.Writer
	Stderr io.Writer
}

func NewApp(stdout, stderr io.Writer) App {
	return App{Stdout: stdout, Stderr: stderr}
}

func (a App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		a.printUsage()
		return nil
	}

	switch args[0] {
	case "version":
		return a.version()
	case "doctor":
		return a.doctor(ctx)
	case "mcp":
		return a.mcp(args[1:])
	case "host":
		return a.host(ctx, args[1:])
	case "ticket":
		return a.ticket(args[1:])
	case "policy":
		return a.policy(args[1:])
	case "demo":
		return a.demo(args[1:])
	case "gateway":
		return a.gateway(args[1:])
	case "release":
		return a.release(args[1:])
	case "audit":
		return a.audit(args[1:])
	case "evidence":
		return a.evidence(ctx, args[1:])
	case "skillkit":
		return a.skillkit(args[1:])
	case "workspace":
		return a.workspace(ctx, args[1:])
	case "acceptance":
		return a.acceptance(ctx, args[1:])
	case "help", "-h", "--help":
		a.printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (a App) acceptance(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing acceptance subcommand")
	}
	switch args[0] {
	case "managed-mac":
		fs := flag.NewFlagSet("acceptance managed-mac", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		repo := fs.String("repo", "", "git repository root; defaults to a generated fixture repo inside --out")
		out := fs.String("out", "", "empty output directory for the acceptance report and evidence bundles")
		worktreeRoot := fs.String("worktree-root", "", "directory for generated worktrees; defaults to <out>/worktrees")
		workspaceLockStore := fs.String("workspace-lock-store", "", "workspace lock store directory; defaults to <out>/workspace-locks")
		codexCommand := fs.String("codex-command", "", "codex command override; defaults to real codex")
		codexArgsJSON := fs.String("codex-args-json", "", "JSON array of codex command args")
		prompt := fs.String("prompt", "", "prompt for the Codex job")
		verificationJSON := fs.String("verification-commands-json", "", "JSON matrix of verification commands")
		allowVerification := fs.String("allow-verification-commands", "", "comma-separated allowlist for verification commands")
		maxDuration := fs.Int("max-duration-seconds", 300, "maximum adapter duration")
		maxOutput := fs.Int("max-output-bytes", 1024*1024, "maximum captured output bytes")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		codexArgs, err := parseJSONStringArray(*codexArgsJSON, "codex-args-json")
		if err != nil {
			return err
		}
		verificationCommands, err := parseJSONStringMatrix(*verificationJSON, "verification-commands-json")
		if err != nil {
			return err
		}
		return a.acceptanceManagedMac(ctx, acceptance.ManagedMacOptions{
			RepoRoot:                  *repo,
			OutDir:                    *out,
			WorktreeRoot:              *worktreeRoot,
			WorkspaceLockStore:        *workspaceLockStore,
			CodexCommand:              *codexCommand,
			CodexArgs:                 codexArgs,
			Prompt:                    *prompt,
			VerificationCommands:      verificationCommands,
			AllowVerificationCommands: splitCapabilities(*allowVerification),
			MaxDurationSeconds:        *maxDuration,
			MaxOutputBytes:            *maxOutput,
		})
	case "managed-mac-service":
		fs := flag.NewFlagSet("acceptance managed-mac-service", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		repo := fs.String("repo", "", "git repository root to include in the follow-up managed-mac acceptance command")
		out := fs.String("out", "", "empty output directory for the service plan and generated plist")
		binaryPath := fs.String("binary", "", "absolute path to rdev binary; defaults to current executable")
		gatewayURL := fs.String("gateway", "", "gateway URL for managed ticket enrollment")
		ticketCode := fs.String("ticket-code", "", "managed enrollment ticket code")
		manifestURL := fs.String("manifest-url", "", "signed managed enrollment manifest URL")
		label := fs.String("label", service.DefaultMacOSLaunchAgentLabel, "managed host service label")
		plistOut := fs.String("plist-out", "", "LaunchAgent plist output path; defaults to <out>/<label>.plist")
		identityStore := fs.String("identity-store", "", "managed host identity store path")
		trustStore := fs.String("trust-store", "", "managed host trust bundle store path")
		nonceStore := fs.String("nonce-store", "", "managed host nonce store path")
		approvalStore := fs.String("approval-store", "", "managed host approval store path")
		workspaceLockStore := fs.String("workspace-lock-store", "", "managed host workspace lock store directory")
		logDir := fs.String("log-dir", "", "managed host log directory")
		force := fs.Bool("force", false, "overwrite an existing plist output path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceManagedMacService(ctx, acceptance.ManagedMacServiceOptions{
			RepoRoot:           *repo,
			OutDir:             *out,
			BinaryPath:         *binaryPath,
			GatewayURL:         *gatewayURL,
			TicketCode:         *ticketCode,
			ManifestURL:        *manifestURL,
			Label:              *label,
			PlistOut:           *plistOut,
			IdentityStore:      *identityStore,
			TrustStore:         *trustStore,
			NonceStore:         *nonceStore,
			ApprovalStore:      *approvalStore,
			WorkspaceLockStore: *workspaceLockStore,
			LogDir:             *logDir,
			Force:              *force,
		})
	case "windows-temporary":
		fs := flag.NewFlagSet("acceptance windows-temporary", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		out := fs.String("out", "", "empty output directory for the Windows temporary acceptance plan")
		gatewayURL := fs.String("gateway", "", "gateway URL for attended temporary enrollment")
		ticketCode := fs.String("ticket-code", "", "attended temporary ticket code")
		downloadURL := fs.String("download-url", "", "rdev-host.exe download URL")
		expectedSHA256 := fs.String("expected-sha256", "", "expected SHA-256 for rdev-host.exe")
		bootstrapScript := fs.String("bootstrap-script", "", "local windows-temporary.ps1 path; defaults to scripts/bootstrap/windows-temporary.ps1")
		bootstrapScriptURL := fs.String("bootstrap-script-url", "", "optional URL for downloading windows-temporary.ps1 on the target host")
		bootstrapScriptSHA256 := fs.String("bootstrap-script-sha256", "", "expected SHA-256 for windows-temporary.ps1; defaults to local script hash when available")
		manifestURL := fs.String("manifest-url", "", "signed join manifest URL")
		manifestRootPublicKey := fs.String("manifest-root-public-key", "", "pinned manifest root public key")
		releaseManifestURL := fs.String("release-manifest-url", "", "signed rdev-host release manifest URL")
		releaseBundleURL := fs.String("release-bundle-url", "", "signed release bundle index URL")
		releaseBundleRequiredArtifacts := fs.String("release-bundle-required-artifacts", "rdev-host.exe,rdev-verify.exe", "comma-separated artifact ids required in the release bundle")
		releaseRootPublicKey := fs.String("release-root-public-key", "", "pinned release root public key")
		verifierDownloadURL := fs.String("verifier-download-url", "", "rdev-verify.exe download URL")
		verifierSHA256 := fs.String("verifier-sha256", "", "expected SHA-256 for rdev-verify.exe")
		trustPin := fs.String("trust-pin", "", "optional gateway trust pin for development acceptance")
		hostName := fs.String("host-name", "", "optional host display name override")
		force := fs.Bool("force", false, "overwrite generated launcher if it already exists")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceWindowsTemporary(acceptance.WindowsTemporaryOptions{
			OutDir:                         *out,
			GatewayURL:                     *gatewayURL,
			TicketCode:                     *ticketCode,
			DownloadURL:                    *downloadURL,
			ExpectedSHA256:                 *expectedSHA256,
			BootstrapScriptPath:            *bootstrapScript,
			BootstrapScriptURL:             *bootstrapScriptURL,
			BootstrapScriptExpectedSHA256:  *bootstrapScriptSHA256,
			ManifestURL:                    *manifestURL,
			ManifestRootPublicKey:          *manifestRootPublicKey,
			ReleaseManifestURL:             *releaseManifestURL,
			ReleaseBundleURL:               *releaseBundleURL,
			ReleaseBundleRequiredArtifacts: *releaseBundleRequiredArtifacts,
			ReleaseRootPublicKey:           *releaseRootPublicKey,
			VerifierDownloadURL:            *verifierDownloadURL,
			VerifierExpectedSHA256:         *verifierSHA256,
			TrustPin:                       *trustPin,
			HostName:                       *hostName,
			Force:                          *force,
		})
	case "verify":
		fs := flag.NewFlagSet("acceptance verify", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		report := fs.String("report", "", "acceptance report path, for example <out>/report.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceVerify(*report)
	case "verify-windows-temporary":
		fs := flag.NewFlagSet("acceptance verify-windows-temporary", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		plan := fs.String("plan", "", "Windows temporary acceptance plan path, for example <out>/windows-temporary-plan.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceVerifyWindowsTemporary(*plan)
	case "package-windows-temporary":
		fs := flag.NewFlagSet("acceptance package-windows-temporary", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		plan := fs.String("plan", "", "Windows temporary acceptance plan path")
		out := fs.String("out", "", "empty output directory for the packaged Windows acceptance evidence")
		transcript := fs.String("transcript", "", "PowerShell transcript from the Windows temporary run")
		releaseVerification := fs.String("release-verification", "", "rdev-verify release manifest or bundle verification output")
		auditPath := fs.String("audit", "", "audit export or transcript for host registration, jobs, approvals, revocation, and cancellation")
		noPersistenceDir := fs.String("no-persistence-dir", "", "directory containing one evidence file per no-persistence check")
		approvalProbesDir := fs.String("approval-probes-dir", "", "directory containing one evidence file per approval probe")
		notes := fs.String("notes", "", "optional operator notes file")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptancePackageWindowsTemporary(acceptance.WindowsTemporaryPackageOptions{
			PlanPath:                *plan,
			OutDir:                  *out,
			TranscriptPath:          *transcript,
			ReleaseVerificationPath: *releaseVerification,
			AuditPath:               *auditPath,
			NoPersistenceDir:        *noPersistenceDir,
			ApprovalProbesDir:       *approvalProbesDir,
			NotesPath:               *notes,
		})
	default:
		return fmt.Errorf("unknown acceptance subcommand %q", args[0])
	}
}

func (a App) acceptanceManagedMac(ctx context.Context, opts acceptance.ManagedMacOptions) error {
	report, err := acceptance.RunManagedMac(ctx, opts)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                     allAcceptanceChecksPassed(report.Checks),
		"schema":                 report.SchemaVersion,
		"out":                    report.OutDir,
		"report":                 filepath.Join(report.OutDir, "report.json"),
		"evidence":               report.EvidenceDir,
		"approval_evidence":      report.ApprovalEvidenceDir,
		"repo":                   report.RepoRoot,
		"worktree":               report.Worktree.WorktreePath,
		"host_id":                report.Host.ID,
		"coding_job_id":          report.CodingJob.ID,
		"approval_probe_job_id":  report.ApprovalJob.ID,
		"checks":                 report.Checks,
		"recommended_next_steps": report.RecommendedNextSteps,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) acceptanceManagedMacService(ctx context.Context, opts acceptance.ManagedMacServiceOptions) error {
	plan, err := acceptance.RunManagedMacServicePlan(ctx, opts)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                  allAcceptanceChecksPassed(plan.Checks),
		"schema":              plan.SchemaVersion,
		"out":                 plan.OutDir,
		"plan":                filepath.Join(plan.OutDir, "service-plan.json"),
		"plist":               plan.PlistPath,
		"label":               plan.LaunchAgent.Label,
		"program_arguments":   plan.LaunchAgent.ProgramArguments,
		"checks":              plan.Checks,
		"commands":            plan.Commands,
		"recommended_actions": plan.RecommendedActions,
		"note":                "plist and plan written only; launchctl was not executed by this command",
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) acceptanceWindowsTemporary(opts acceptance.WindowsTemporaryOptions) error {
	plan, err := acceptance.RunWindowsTemporaryPlan(opts)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                    allAcceptanceChecksPassed(plan.Checks),
		"schema":                plan.SchemaVersion,
		"out":                   plan.OutDir,
		"plan":                  filepath.Join(plan.OutDir, "windows-temporary-plan.json"),
		"launcher":              plan.LauncherPath,
		"bootstrap_script_hash": plan.BootstrapScriptSHA256,
		"checks":                plan.Checks,
		"commands":              plan.Commands,
		"no_persistence_checks": plan.NoPersistenceChecks,
		"approval_probes":       plan.ApprovalProbes,
		"required_evidence":     plan.RequiredEvidence,
		"recommended_actions":   plan.RecommendedActions,
		"note":                  "plan and launcher written only; no PowerShell command was executed by this command",
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) acceptanceVerify(reportPath string) error {
	verification, err := acceptance.VerifyManagedMacReport(reportPath)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                  verification.OK(),
		"schema":              verification.SchemaVersion,
		"report":              verification.ReportPath,
		"checks":              verification.Checks,
		"evidence_checks":     verification.Evidence.Checks,
		"approval_checks":     verification.ApprovalEvidence.Checks,
		"recommended_actions": verification.RecommendedActions,
		"evidence_manifest":   verification.Evidence.Manifest,
		"approval_manifest":   verification.ApprovalEvidence.Manifest,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !verification.OK() {
		return fmt.Errorf("acceptance verification failed")
	}
	return nil
}

func (a App) acceptanceVerifyWindowsTemporary(planPath string) error {
	verification, err := acceptance.VerifyWindowsTemporaryPlan(planPath)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                  verification.OK(),
		"schema":              verification.SchemaVersion,
		"plan":                verification.PlanPath,
		"plan_schema":         verification.PlanSchema,
		"checks":              verification.Checks,
		"recommended_actions": verification.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !verification.OK() {
		return fmt.Errorf("windows temporary acceptance plan verification failed")
	}
	return nil
}

func (a App) acceptancePackageWindowsTemporary(opts acceptance.WindowsTemporaryPackageOptions) error {
	pkg, err := acceptance.PackageWindowsTemporaryEvidence(opts)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                    pkg.OK(),
		"schema":                pkg.SchemaVersion,
		"out":                   pkg.OutDir,
		"package":               filepath.Join(pkg.OutDir, "package.json"),
		"checksums":             filepath.Join(pkg.OutDir, "checksums.txt"),
		"checks":                pkg.Checks,
		"files":                 pkg.Files,
		"redaction_rule_counts": pkg.RedactionRuleCounts,
		"recommended_actions":   pkg.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !pkg.OK() {
		return fmt.Errorf("windows temporary acceptance package verification failed")
	}
	return nil
}

func (a App) version() error {
	_, err := fmt.Fprintf(a.Stdout, "%s %s\n", buildinfo.Name, buildinfo.Version)
	return err
}

func (a App) doctor(ctx context.Context) error {
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(hostcap.Detect(ctx))
}

func (a App) mcp(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing mcp subcommand")
	}

	switch args[0] {
	case "tools":
		payload := map[string]any{
			"version": "0.0.1",
			"tools":   contracts.Tools(),
		}
		enc := json.NewEncoder(a.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	case "serve":
		server := mcpstdio.NewServer(gateway.NewMemoryGateway())
		return server.Serve(context.Background(), os.Stdin, a.Stdout)
	default:
		return fmt.Errorf("unknown mcp subcommand %q", args[0])
	}
}

func (a App) host(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing host subcommand")
	}

	switch args[0] {
	case "serve":
		fs := flag.NewFlagSet("host serve", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		mode := fs.String("mode", "temporary", "host mode: temporary, managed, or break-glass")
		gateway := fs.String("gateway", "https://agent.lunflux.com", "gateway URL")
		ticketCode := fs.String("ticket-code", "", "one-time ticket code for local dev registration")
		manifestURL := fs.String("manifest-url", "", "signed join manifest URL")
		name := fs.String("name", "", "host display name; defaults to detected hostname")
		once := fs.Bool("once", true, "register once and exit after printing status")
		transport := fs.String("transport", "poll", "host job transport: poll or long-poll")
		pollInterval := fs.Duration("poll-interval", time.Second, "job polling interval when --once=false")
		longPollTimeout := fs.Duration("long-poll-timeout", 25*time.Second, "long-poll wait duration when --transport=long-poll")
		maxJobs := fs.Int("max-jobs", 1, "maximum jobs to process when --once=false")
		approvalTimeout := fs.Duration("approval-timeout", 30*time.Second, "maximum time to wait for host approval when --once=false")
		trustPin := fs.String("trust-pin", "", "optional gateway signing public key pin, formatted sha256:<hex>")
		trustStore := fs.String("trust-store", "", "optional local signed trust bundle store path for managed hosts")
		identityStore := fs.String("identity-store", "", "optional local host identity key store path")
		identityKeyID := fs.String("identity-key-id", hostidentity.DefaultKeyID, "host identity key id")
		nonceStore := fs.String("nonce-store", "", "optional local host nonce replay cache path")
		approvalStore := fs.String("approval-store", "", "optional local host approval token consumption store path")
		workspaceLockStore := fs.String("workspace-lock-store", "", "optional local workspace lock store directory")
		manifestRootPublicKey := fs.String("manifest-root-public-key", "", "optional join manifest trust root, formatted key_id:base64url_public_key")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.hostServe(ctx, hostServeOptions{
			Mode:                  *mode,
			GatewayURL:            *gateway,
			TicketCode:            *ticketCode,
			ManifestURL:           *manifestURL,
			Name:                  *name,
			Once:                  *once,
			Transport:             *transport,
			PollInterval:          *pollInterval,
			LongPollTimeout:       *longPollTimeout,
			MaxJobs:               *maxJobs,
			ApprovalTimeout:       *approvalTimeout,
			TrustPin:              *trustPin,
			TrustStorePath:        *trustStore,
			IdentityStorePath:     *identityStore,
			IdentityKeyID:         *identityKeyID,
			NonceStorePath:        *nonceStore,
			ApprovalStorePath:     *approvalStore,
			WorkspaceLockStore:    *workspaceLockStore,
			ManifestRootPublicKey: *manifestRootPublicKey,
		})
	case "install-service":
		fs := flag.NewFlagSet("host install-service", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		platform := fs.String("platform", "macos", "service platform: macos")
		label := fs.String("label", service.DefaultMacOSLaunchAgentLabel, "managed host service label")
		binaryPath := fs.String("binary", "", "absolute path to rdev binary; defaults to current executable")
		gatewayURL := fs.String("gateway", "", "gateway URL")
		ticketCode := fs.String("ticket-code", "", "managed enrollment ticket code")
		manifestURL := fs.String("manifest-url", "", "signed managed enrollment manifest URL")
		identityStore := fs.String("identity-store", "", "managed host identity store path")
		trustStore := fs.String("trust-store", "", "managed host trust bundle store path")
		nonceStore := fs.String("nonce-store", "", "managed host nonce store path")
		approvalStore := fs.String("approval-store", "", "managed host approval store path")
		workspaceLockStore := fs.String("workspace-lock-store", "", "managed host workspace lock store directory")
		logDir := fs.String("log-dir", "", "managed host log directory")
		plistOut := fs.String("plist-out", "", "LaunchAgent plist output path; defaults to ~/Library/LaunchAgents/<label>.plist on macOS")
		force := fs.Bool("force", false, "overwrite an existing plist output path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.hostInstallService(hostInstallServiceOptions{
			Platform:           *platform,
			Label:              *label,
			BinaryPath:         *binaryPath,
			GatewayURL:         *gatewayURL,
			TicketCode:         *ticketCode,
			ManifestURL:        *manifestURL,
			IdentityStorePath:  *identityStore,
			TrustStorePath:     *trustStore,
			NonceStorePath:     *nonceStore,
			ApprovalStorePath:  *approvalStore,
			WorkspaceLockStore: *workspaceLockStore,
			LogDir:             *logDir,
			PlistOut:           *plistOut,
			Force:              *force,
		})
	case "service-status":
		fs := flag.NewFlagSet("host service-status", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		platform := fs.String("platform", "macos", "service platform: macos")
		label := fs.String("label", service.DefaultMacOSLaunchAgentLabel, "managed host service label")
		plistPath := fs.String("plist", "", "LaunchAgent plist path; defaults to ~/Library/LaunchAgents/<label>.plist on macOS")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.hostServiceStatus(hostServiceOptions{
			Platform: *platform,
			Label:    *label,
			Plist:    *plistPath,
		})
	case "service-control":
		fs := flag.NewFlagSet("host service-control", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		platform := fs.String("platform", "macos", "service platform: macos")
		action := fs.String("action", "", "service action: start, stop, or inspect")
		label := fs.String("label", service.DefaultMacOSLaunchAgentLabel, "managed host service label")
		plistPath := fs.String("plist", "", "LaunchAgent plist path; defaults to ~/Library/LaunchAgents/<label>.plist on macOS")
		domain := fs.String("domain", "gui/$(id -u)", "launchctl domain; default is resolved for --execute")
		execute := fs.Bool("execute", false, "execute launchctl instead of only printing the planned command")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.hostServiceControl(ctx, hostServiceControlOptions{
			Platform: *platform,
			Action:   *action,
			Label:    *label,
			Plist:    *plistPath,
			Domain:   *domain,
			Execute:  *execute,
		})
	case "uninstall-service":
		fs := flag.NewFlagSet("host uninstall-service", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		platform := fs.String("platform", "macos", "service platform: macos")
		label := fs.String("label", service.DefaultMacOSLaunchAgentLabel, "managed host service label")
		plistPath := fs.String("plist", "", "LaunchAgent plist path; defaults to ~/Library/LaunchAgents/<label>.plist on macOS")
		force := fs.Bool("force", false, "remove plist even if the embedded label does not match --label")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.hostUninstallService(hostServiceOptions{
			Platform: *platform,
			Label:    *label,
			Plist:    *plistPath,
			Force:    *force,
		})
	default:
		return fmt.Errorf("unknown host subcommand %q", args[0])
	}
}

type hostServeOptions struct {
	Mode                  string
	GatewayURL            string
	TicketCode            string
	ManifestURL           string
	Name                  string
	Once                  bool
	Transport             string
	PollInterval          time.Duration
	LongPollTimeout       time.Duration
	MaxJobs               int
	ApprovalTimeout       time.Duration
	TrustPin              string
	TrustStorePath        string
	IdentityStorePath     string
	IdentityKeyID         string
	NonceStorePath        string
	ApprovalStorePath     string
	WorkspaceLockStore    string
	ManifestRootPublicKey string
}

type hostInstallServiceOptions struct {
	Platform           string
	Label              string
	BinaryPath         string
	GatewayURL         string
	TicketCode         string
	ManifestURL        string
	IdentityStorePath  string
	TrustStorePath     string
	NonceStorePath     string
	ApprovalStorePath  string
	WorkspaceLockStore string
	LogDir             string
	PlistOut           string
	Force              bool
}

type hostServiceOptions struct {
	Platform string
	Label    string
	Plist    string
	Force    bool
}

type hostServiceControlOptions struct {
	Platform string
	Action   string
	Label    string
	Plist    string
	Domain   string
	Execute  bool
}

func (a App) hostServe(ctx context.Context, opts hostServeOptions) error {
	switch opts.Mode {
	case "temporary", "managed", "break-glass":
	default:
		return fmt.Errorf("unsupported host mode %q", opts.Mode)
	}
	if opts.Transport == "" {
		opts.Transport = "poll"
	}
	switch opts.Transport {
	case "poll", "long-poll":
	default:
		return fmt.Errorf("unsupported host transport %q", opts.Transport)
	}
	if opts.ManifestURL != "" {
		manifest, err := fetchJoinManifest(ctx, opts.ManifestURL, opts.TrustPin, opts.ManifestRootPublicKey)
		if err != nil {
			return err
		}
		opts.GatewayURL = manifest.GatewayURL
		opts.TicketCode = manifest.TicketCode
		opts.TrustPin = manifest.TrustFingerprint
	}
	if opts.TicketCode == "" {
		_, err := fmt.Fprintf(
			a.Stdout,
			"rdev-host foreground placeholder\nmode=%s\ngateway=%s\nstatus=not-connected\nnote=provide --ticket-code to register with a local dev gateway; production transport is not implemented yet\n",
			opts.Mode,
			opts.GatewayURL,
		)
		return err
	}
	if !strings.HasPrefix(opts.GatewayURL, "http://127.0.0.1:") && !strings.HasPrefix(opts.GatewayURL, "http://localhost:") {
		return fmt.Errorf("host registration currently supports local dev gateways only")
	}
	identity, identityCreated, err := hostidentity.LoadOrCreate(opts.IdentityStorePath, opts.IdentityKeyID)
	if err != nil {
		return err
	}
	inventory := hostcap.Detect(ctx)
	if opts.Name != "" {
		inventory.Name = opts.Name
	}
	host, err := registerHost(ctx, opts.GatewayURL, model.HostRegistration{
		TicketCode:          opts.TicketCode,
		Name:                inventory.Name,
		OS:                  inventory.OS,
		Arch:                inventory.Arch,
		Capabilities:        inventory.TemporaryCapabilities,
		IdentityKeyID:       identity.KeyID,
		IdentityPublicKey:   identity.EncodedPublicKey(),
		IdentityFingerprint: identity.Fingerprint(),
	})
	if err != nil {
		return err
	}

	payload := map[string]any{
		"mode":      opts.Mode,
		"gateway":   opts.GatewayURL,
		"host":      host,
		"inventory": inventory,
		"identity": map[string]any{
			"key_id":      identity.KeyID,
			"fingerprint": identity.Fingerprint(),
			"created":     identityCreated,
			"stored":      opts.IdentityStorePath != "",
		},
		"status": "registered-pending-approval",
		"note":   "local development registration only; WSS transport is not implemented yet",
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if opts.Once {
		return enc.Encode(payload)
	}
	if _, err := waitForHostActive(ctx, opts.GatewayURL, host.ID, opts.ApprovalTimeout, opts.PollInterval); err != nil {
		return err
	}
	processed, err := a.pollAndRunDevJobs(ctx, opts, host.ID, identity.Fingerprint())
	if err != nil {
		return err
	}
	payload["processed_jobs"] = processed
	payload["status"] = "polling-complete"
	return enc.Encode(payload)
}

func (a App) hostInstallService(opts hostInstallServiceOptions) error {
	if opts.Platform == "" {
		opts.Platform = "macos"
	}
	if opts.Platform != "macos" {
		return fmt.Errorf("host install-service currently supports macos only")
	}
	binaryPath := opts.BinaryPath
	if binaryPath == "" {
		current, err := os.Executable()
		if err != nil {
			return err
		}
		binaryPath = current
	}
	if !filepath.IsAbs(binaryPath) {
		return fmt.Errorf("binary path must be absolute")
	}
	plistOut := opts.PlistOut
	if plistOut == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		label := opts.Label
		if label == "" {
			label = service.DefaultMacOSLaunchAgentLabel
		}
		plistOut = service.DefaultMacOSLaunchAgentPath(home, label)
	}
	agent, err := service.NewMacOSLaunchAgent(service.LaunchAgentOptions{
		Label:                  opts.Label,
		BinaryPath:             binaryPath,
		GatewayURL:             opts.GatewayURL,
		TicketCode:             opts.TicketCode,
		ManifestURL:            opts.ManifestURL,
		IdentityStorePath:      opts.IdentityStorePath,
		TrustStorePath:         opts.TrustStorePath,
		NonceStorePath:         opts.NonceStorePath,
		ApprovalStorePath:      opts.ApprovalStorePath,
		WorkspaceLockStorePath: opts.WorkspaceLockStore,
		LogDir:                 opts.LogDir,
		Transport:              "long-poll",
		LongPollTimeout:        "25s",
	})
	if err != nil {
		return err
	}
	content, err := service.RenderMacOSLaunchAgent(agent)
	if err != nil {
		return err
	}
	if err := writeServiceFile(plistOut, content, opts.Force); err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                true,
		"platform":          opts.Platform,
		"label":             agent.Label,
		"plist":             plistOut,
		"program_arguments": agent.ProgramArguments,
		"stdout":            agent.StdoutPath,
		"stderr":            agent.StderrPath,
		"next": map[string]string{
			"start":   "launchctl bootstrap gui/$(id -u) " + plistOut,
			"stop":    "launchctl bootout gui/$(id -u) " + plistOut,
			"inspect": "launchctl print gui/$(id -u)/" + agent.Label,
		},
		"note": "plist written only; launchctl was not executed by this command",
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func writeServiceFile(path string, content []byte, force bool) error {
	if path == "" {
		return fmt.Errorf("service output path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	flag := os.O_CREATE | os.O_WRONLY
	if force {
		flag |= os.O_TRUNC
	} else {
		flag |= os.O_EXCL
	}
	file, err := os.OpenFile(path, flag, 0o600)
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

func (a App) hostServiceStatus(opts hostServiceOptions) error {
	if opts.Platform == "" {
		opts.Platform = "macos"
	}
	if opts.Platform != "macos" {
		return fmt.Errorf("host service-status currently supports macos only")
	}
	if opts.Label == "" {
		opts.Label = service.DefaultMacOSLaunchAgentLabel
	}
	plistPath, err := servicePlistPath(opts)
	if err != nil {
		return err
	}
	status, err := service.InspectMacOSLaunchAgent(plistPath)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":       true,
		"platform": opts.Platform,
		"label":    opts.Label,
		"plist":    plistPath,
		"status":   status,
		"next":     macOSLaunchAgentNextSteps(opts.Label, plistPath),
		"note":     "status reads the plist only; launchctl was not executed by this command",
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

type launchctlRunResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

func (a App) hostServiceControl(ctx context.Context, opts hostServiceControlOptions) error {
	if opts.Platform == "" {
		opts.Platform = "macos"
	}
	if opts.Platform != "macos" {
		return fmt.Errorf("host service-control currently supports macos only")
	}
	if opts.Label == "" {
		opts.Label = service.DefaultMacOSLaunchAgentLabel
	}
	if strings.TrimSpace(opts.Action) == "" {
		return fmt.Errorf("service action is required")
	}
	plistPath, err := servicePlistPath(hostServiceOptions{
		Platform: opts.Platform,
		Label:    opts.Label,
		Plist:    opts.Plist,
	})
	if err != nil {
		return err
	}
	status, err := service.InspectMacOSLaunchAgent(plistPath)
	if err != nil {
		return err
	}
	if opts.Action == "start" || opts.Action == "stop" {
		if !status.Exists {
			return fmt.Errorf("plist does not exist: %s", plistPath)
		}
	}
	if status.Exists && status.Label != opts.Label {
		return fmt.Errorf("refusing service-control for plist label %q; expected %q", status.Label, opts.Label)
	}
	domain := opts.Domain
	if domain == "" {
		domain = "gui/$(id -u)"
	}
	if opts.Execute {
		domain, err = resolveLaunchctlDomain(ctx, domain)
		if err != nil {
			return err
		}
	}
	plan, err := service.NewMacOSLaunchAgentControlPlan(service.LaunchAgentControlOptions{
		Action:    opts.Action,
		Label:     opts.Label,
		PlistPath: plistPath,
		Domain:    domain,
	})
	if err != nil {
		return err
	}
	var result *launchctlRunResult
	if opts.Execute {
		runResult, err := runLaunchctl(ctx, plan.Argv)
		result = &runResult
		payload := map[string]any{
			"ok":       err == nil,
			"platform": opts.Platform,
			"label":    opts.Label,
			"plist":    plistPath,
			"execute":  true,
			"status":   status,
			"command":  plan,
			"result":   result,
			"note":     "launchctl was executed because --execute was set",
		}
		enc := json.NewEncoder(a.Stdout)
		enc.SetIndent("", "  ")
		if encodeErr := enc.Encode(payload); encodeErr != nil {
			return encodeErr
		}
		return err
	}
	payload := map[string]any{
		"ok":       true,
		"platform": opts.Platform,
		"label":    opts.Label,
		"plist":    plistPath,
		"execute":  false,
		"status":   status,
		"command":  plan,
		"note":     "dry-run only; add --execute to run launchctl",
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func resolveLaunchctlDomain(ctx context.Context, domain string) (string, error) {
	if domain != "gui/$(id -u)" {
		return domain, nil
	}
	cmd := exec.CommandContext(ctx, "id", "-u")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("resolve launchctl domain uid: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	uid := strings.TrimSpace(stdout.String())
	if uid == "" {
		return "", fmt.Errorf("resolve launchctl domain uid: empty uid")
	}
	return "gui/" + uid, nil
}

func runLaunchctl(ctx context.Context, argv []string) (launchctlRunResult, error) {
	if len(argv) == 0 {
		return launchctlRunResult{ExitCode: -1}, fmt.Errorf("launchctl argv is required")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := launchctlRunResult{
		ExitCode: processExitCode(err),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}
	return result, err
}

func processExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}

func (a App) hostUninstallService(opts hostServiceOptions) error {
	if opts.Platform == "" {
		opts.Platform = "macos"
	}
	if opts.Platform != "macos" {
		return fmt.Errorf("host uninstall-service currently supports macos only")
	}
	if opts.Label == "" {
		opts.Label = service.DefaultMacOSLaunchAgentLabel
	}
	plistPath, err := servicePlistPath(opts)
	if err != nil {
		return err
	}
	status, err := service.InspectMacOSLaunchAgent(plistPath)
	if err != nil {
		return err
	}
	if status.Exists && status.Label != opts.Label && !opts.Force {
		return fmt.Errorf("refusing to remove plist for label %q; expected %q", status.Label, opts.Label)
	}
	removed := false
	if status.Exists {
		if err := os.Remove(plistPath); err != nil {
			return err
		}
		removed = true
	}
	payload := map[string]any{
		"ok":       true,
		"platform": opts.Platform,
		"label":    opts.Label,
		"plist":    plistPath,
		"removed":  removed,
		"previous": status,
		"next": map[string]string{
			"ensure_stopped": "launchctl bootout gui/$(id -u) " + plistPath,
		},
		"note": "plist removal only; launchctl was not executed by this command",
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func servicePlistPath(opts hostServiceOptions) (string, error) {
	if opts.Label == "" {
		opts.Label = service.DefaultMacOSLaunchAgentLabel
	}
	if opts.Plist != "" {
		return opts.Plist, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return service.DefaultMacOSLaunchAgentPath(home, opts.Label), nil
}

func macOSLaunchAgentNextSteps(label, plistPath string) map[string]string {
	if label == "" {
		label = service.DefaultMacOSLaunchAgentLabel
	}
	return map[string]string{
		"start":     "launchctl bootstrap gui/$(id -u) " + plistPath,
		"stop":      "launchctl bootout gui/$(id -u) " + plistPath,
		"inspect":   "launchctl print gui/$(id -u)/" + label,
		"uninstall": "rdev host uninstall-service --platform macos --label " + label + " --plist " + plistPath,
	}
}

func (a App) ticket(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing ticket subcommand")
	}

	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("ticket create", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		mode := fs.String("mode", string(model.HostModeAttendedTemporary), "ticket mode: attended-temporary, managed, or break-glass")
		ttl := fs.Int("ttl-seconds", 7200, "ticket TTL in seconds")
		reason := fs.String("reason", "remote support", "ticket reason")
		capList := fs.String("capabilities", "", "comma-separated capabilities; defaults to temporary-mode capabilities")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.ticketCreate(model.HostMode(*mode), *ttl, *reason, *capList)
	default:
		return fmt.Errorf("unknown ticket subcommand %q", args[0])
	}
}

func (a App) ticketCreate(mode model.HostMode, ttlSeconds int, reason, capList string) error {
	if !mode.Valid() {
		return fmt.Errorf("unsupported ticket mode %q", mode)
	}
	if ttlSeconds < 60 || ttlSeconds > 86400 {
		return fmt.Errorf("ttl-seconds must be between 60 and 86400")
	}

	capabilities := splitCapabilities(capList)
	if len(capabilities) == 0 {
		capabilities = capabilitiesToStrings(policy.TemporaryDefaults())
	}
	ticket, err := model.NewTicket(mode, ttlSeconds, capabilities, reason, time.Now())
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ticket":  ticket,
		"joinUrl": "https://agent.lunflux.com/join/" + ticket.Code,
		"note":    "local preview only; gateway persistence is not implemented yet",
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) policy(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing policy subcommand")
	}

	switch args[0] {
	case "explain":
		fs := flag.NewFlagSet("policy explain", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		mode := fs.String("mode", string(model.HostModeAttendedTemporary), "host mode")
		capability := fs.String("capability", "", "capability name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *capability == "" {
			return fmt.Errorf("capability is required")
		}
		explanation := policy.Explain(model.HostMode(*mode), policy.Capability(*capability))
		enc := json.NewEncoder(a.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(explanation)
	case "explain-shell":
		fs := flag.NewFlagSet("policy explain-shell", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		mode := fs.String("mode", string(model.HostModeAttendedTemporary), "host mode")
		policyJSON := fs.String("policy-json", "", "shell job policy JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *policyJSON == "" {
			return fmt.Errorf("policy-json is required")
		}
		var jobPolicy map[string]any
		if err := json.Unmarshal([]byte(*policyJSON), &jobPolicy); err != nil {
			return fmt.Errorf("invalid policy-json: %w", err)
		}
		explanation := policy.ExplainShellJob(model.HostMode(*mode), jobPolicy)
		enc := json.NewEncoder(a.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(explanation)
	default:
		return fmt.Errorf("unknown policy subcommand %q", args[0])
	}
}

func (a App) demo(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing demo subcommand")
	}

	switch args[0] {
	case "local":
		return a.demoLocal()
	default:
		return fmt.Errorf("unknown demo subcommand %q", args[0])
	}
}

func (a App) demoLocal() error {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "local demo")
	if err != nil {
		return err
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "local-demo-host",
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		Capabilities: capabilities,
	})
	if err != nil {
		return err
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		return err
	}
	job, err := gw.CreateJob(host.ID, "shell", "local demo diagnostic", map[string]any{
		"cwd":            ".",
		"allow_commands": []string{"echo"},
	})
	if err != nil {
		return err
	}
	job, artifact, err := gw.CompleteJob(job.ID, "local demo completed without remote transport")
	if err != nil {
		return err
	}

	payload := map[string]any{
		"ticket":    ticket,
		"joinUrl":   "https://agent.lunflux.com/join/" + ticket.Code,
		"host":      host,
		"job":       job,
		"artifact":  artifact,
		"audit":     gw.AuditEvents(),
		"transport": "in-memory demo only",
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) gateway(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing gateway subcommand")
	}
	switch args[0] {
	case "serve":
		fs := flag.NewFlagSet("gateway serve", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		dev := fs.Bool("dev", false, "run local development gateway")
		addr := fs.String("addr", "127.0.0.1:8787", "listen address")
		auditLog := fs.String("audit-log", "", "optional JSONL audit log path")
		signingKey := fs.String("signing-key", "", "optional persistent Ed25519 signing key file")
		signingKeyID := fs.String("signing-key-id", signing.DefaultKeyID, "signing key id for new or existing signing key file")
		manifestSigningKey := fs.String("manifest-signing-key", "", "optional Ed25519 key file for signing join manifests")
		manifestSigningKeyID := fs.String("manifest-signing-key-id", "manifest-dev", "signing key id for join manifests")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if !*dev {
			return fmt.Errorf("gateway serve currently requires --dev")
		}
		return a.gatewayServeDev(*addr, *auditLog, *signingKey, *signingKeyID, *manifestSigningKey, *manifestSigningKeyID)
	default:
		return fmt.Errorf("unknown gateway subcommand %q", args[0])
	}
}

func (a App) audit(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing audit subcommand")
	}
	switch args[0] {
	case "export":
		fs := flag.NewFlagSet("audit export", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		input := fs.String("input", "", "input audit JSONL path")
		out := fs.String("out", "", "output audit chain JSON path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.auditExport(*input, *out)
	case "verify":
		fs := flag.NewFlagSet("audit verify", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		input := fs.String("input", "", "input audit chain JSON path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.auditVerify(*input)
	default:
		return fmt.Errorf("unknown audit subcommand %q", args[0])
	}
}

func (a App) evidence(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing evidence subcommand")
	}
	switch args[0] {
	case "export":
		fs := flag.NewFlagSet("evidence export", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		jobPath := fs.String("job-json", "", "job JSON path")
		artifactsPath := fs.String("artifacts-json", "", "artifacts JSON path")
		auditPath := fs.String("audit-jsonl", "", "audit JSONL path")
		gatewayURL := fs.String("gateway", "", "gateway API URL for job-id export")
		jobID := fs.String("job-id", "", "job id to export from gateway API")
		out := fs.String("out", "", "output evidence bundle directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.evidenceExport(ctx, *jobPath, *artifactsPath, *auditPath, *gatewayURL, *jobID, *out)
	default:
		return fmt.Errorf("unknown evidence subcommand %q", args[0])
	}
}

func (a App) skillkit(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing skillkit subcommand")
	}
	switch args[0] {
	case "export":
		fs := flag.NewFlagSet("skillkit export", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		sourceRoot := fs.String("source-root", ".", "repository source root containing skills/ and mcp/tools.json")
		out := fs.String("out", "", "output skillkit bundle directory")
		gatewayURL := fs.String("gateway-url", "", "default gateway URL to include in install docs")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.skillkitExport(*sourceRoot, *out, *gatewayURL)
	case "verify":
		fs := flag.NewFlagSet("skillkit verify", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		bundle := fs.String("bundle", "", "skillkit bundle directory containing manifest.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.skillkitVerify(*bundle)
	default:
		return fmt.Errorf("unknown skillkit subcommand %q", args[0])
	}
}

func (a App) workspace(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing workspace subcommand")
	}
	switch args[0] {
	case "lock":
		fs := flag.NewFlagSet("workspace lock", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		repo := fs.String("repo", "", "repository root to lock")
		store := fs.String("store", "", "workspace lock store directory; defaults to <repo>/.rdev/workspace-locks")
		hostID := fs.String("host-id", "", "host id acquiring the lock")
		jobID := fs.String("job-id", "", "job id acquiring the lock")
		adapter := fs.String("adapter", "", "adapter that owns the lock")
		worktreePath := fs.String("worktree-path", "", "planned worktree path")
		baseRef := fs.String("base-ref", "", "planned base ref")
		branch := fs.String("branch", "", "planned branch")
		ttl := fs.Duration("ttl", workspace.DefaultLockTTL, "lock TTL")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.workspaceLock(workspace.LockOptions{
			StoreDir:     *store,
			RepoRoot:     *repo,
			HostID:       *hostID,
			JobID:        *jobID,
			OwnerAdapter: *adapter,
			WorktreePath: *worktreePath,
			BaseRef:      *baseRef,
			Branch:       *branch,
			TTL:          *ttl,
		})
	case "status":
		fs := flag.NewFlagSet("workspace status", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		repo := fs.String("repo", "", "repository root to inspect")
		store := fs.String("store", "", "workspace lock store directory; defaults to <repo>/.rdev/workspace-locks")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.workspaceStatus(*repo, *store)
	case "unlock":
		fs := flag.NewFlagSet("workspace unlock", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		repo := fs.String("repo", "", "repository root to unlock")
		store := fs.String("store", "", "workspace lock store directory; defaults to <repo>/.rdev/workspace-locks")
		jobID := fs.String("job-id", "", "job id that owns the lock")
		force := fs.Bool("force", false, "remove the lock even if job id does not match")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.workspaceUnlock(*repo, *store, *jobID, *force)
	case "prepare-worktree":
		fs := flag.NewFlagSet("workspace prepare-worktree", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		repo := fs.String("repo", "", "git repository root")
		store := fs.String("store", "", "workspace lock store directory; defaults to <repo>/.rdev/workspace-locks")
		hostID := fs.String("host-id", "", "host id preparing the worktree")
		jobID := fs.String("job-id", "", "job id preparing the worktree")
		adapter := fs.String("adapter", "codex", "adapter that will own the worktree")
		baseRef := fs.String("base-ref", "HEAD", "git base ref")
		branch := fs.String("branch", "", "git branch to create; defaults to rdev/job_<job-id>")
		worktreeRoot := fs.String("worktree-root", "", "directory for generated worktrees; defaults to <repo>/.rdev/worktrees")
		worktreePath := fs.String("worktree-path", "", "explicit worktree path")
		ttl := fs.Duration("ttl", workspace.DefaultLockTTL, "lock TTL")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.workspacePrepareWorktree(ctx, workspace.GitWorktreeOptions{
			StoreDir:     *store,
			RepoRoot:     *repo,
			HostID:       *hostID,
			JobID:        *jobID,
			OwnerAdapter: *adapter,
			BaseRef:      *baseRef,
			Branch:       *branch,
			WorktreeRoot: *worktreeRoot,
			WorktreePath: *worktreePath,
			TTL:          *ttl,
		})
	default:
		return fmt.Errorf("unknown workspace subcommand %q", args[0])
	}
}

func (a App) release(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing release subcommand")
	}
	switch args[0] {
	case "sign":
		fs := flag.NewFlagSet("release sign", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		artifact := fs.String("artifact", "", "artifact path to sign")
		keyPath := fs.String("key", "", "Ed25519 release signing key file")
		keyID := fs.String("key-id", "release-root", "release signing key id")
		out := fs.String("out", "", "output manifest path; defaults to <artifact>.rdev-release.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.releaseSign(*artifact, *keyPath, *keyID, *out)
	case "verify":
		fs := flag.NewFlagSet("release verify", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		artifact := fs.String("artifact", "", "artifact path to verify")
		manifestPath := fs.String("manifest", "", "release manifest path")
		root := fs.String("root-public-key", "", "release trust root, formatted key_id:base64url_public_key")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.releaseVerify(*artifact, *manifestPath, *root)
	case "create-bundle":
		fs := flag.NewFlagSet("release create-bundle", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		dir := fs.String("dir", "", "release directory containing artifacts and .rdev-release.json manifests")
		artifacts := fs.String("artifacts", "", "comma-separated artifact paths relative to --dir")
		requiredArtifacts := fs.String("require-artifacts", "", "comma-separated artifact ids that must be present in the bundle")
		keyPath := fs.String("key", "", "Ed25519 release signing key file")
		keyID := fs.String("key-id", "release-root", "release signing key id")
		out := fs.String("out", "", "output bundle index path; defaults to <dir>/release-bundle.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.releaseCreateBundle(*dir, splitCapabilities(*artifacts), splitCapabilities(*requiredArtifacts), *keyPath, *keyID, *out)
	case "verify-bundle":
		fs := flag.NewFlagSet("release verify-bundle", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		bundlePath := fs.String("bundle", "", "release bundle index path")
		root := fs.String("root-public-key", "", "release trust root, formatted key_id:base64url_public_key")
		requiredArtifacts := fs.String("require-artifacts", "", "comma-separated artifact ids that must be present in the bundle")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.releaseVerifyBundle(*bundlePath, *root, splitCapabilities(*requiredArtifacts))
	default:
		return fmt.Errorf("unknown release subcommand %q", args[0])
	}
}

func (a App) gatewayServeDev(addr, auditLog, signingKeyPath, signingKeyID, manifestSigningKeyPath, manifestSigningKeyID string) error {
	key, created, err := signing.LoadOrCreate(signingKeyPath, signingKeyID)
	if err != nil {
		return err
	}
	gw := gateway.NewMemoryGatewayWithSigningKey(time.Now, key.ID, key.PublicKey, key.PrivateKey)
	if manifestSigningKeyPath != "" {
		manifestKey, manifestCreated, err := signing.LoadOrCreate(manifestSigningKeyPath, manifestSigningKeyID)
		if err != nil {
			return err
		}
		gw.WithManifestSigningKey(manifestKey.ID, manifestKey.PublicKey, manifestKey.PrivateKey)
		action := "loaded"
		if manifestCreated {
			action = "created"
		}
		_, _ = fmt.Fprintf(a.Stderr, "rdev gateway manifest signing key %s at %s\n", action, manifestSigningKeyPath)
		_, _ = fmt.Fprintf(a.Stderr, "rdev gateway manifest root id=%s public_key=%s\n", manifestKey.ID, encodeRootPublicKey(manifestKey.ID, manifestKey.PublicKey))
	}
	if auditLog != "" {
		store := audit.NewJSONLStore(auditLog)
		gw.WithAuditSink(&store)
	}
	server := httpapi.NewServer(gw)
	if signingKeyPath != "" {
		action := "loaded"
		if created {
			action = "created"
		}
		_, _ = fmt.Fprintf(a.Stderr, "rdev gateway signing key %s at %s\n", action, signingKeyPath)
	}
	_, _ = fmt.Fprintf(a.Stderr, "rdev gateway signing key id=%s fingerprint=%s\n", key.ID, signing.Fingerprint(key.PublicKey))
	_, _ = fmt.Fprintf(a.Stderr, "rdev gateway dev listening on http://%s\n", addr)
	return http.ListenAndServe(addr, server.Handler())
}

func (a App) releaseSign(artifactPath, keyPath, keyID, outPath string) error {
	if artifactPath == "" {
		return fmt.Errorf("artifact is required")
	}
	if keyPath == "" {
		return fmt.Errorf("key is required")
	}
	key, _, err := signing.LoadOrCreate(keyPath, keyID)
	if err != nil {
		return err
	}
	manifest, err := release.SignArtifact(artifactPath, key, time.Now())
	if err != nil {
		return err
	}
	if outPath == "" {
		outPath = artifactPath + ".rdev-release.json"
	}
	if err := release.WriteManifest(outPath, manifest); err != nil {
		return err
	}
	payload := map[string]any{
		"manifest":        manifest,
		"manifest_path":   outPath,
		"root_public_key": encodeRootPublicKey(key.ID, key.PublicKey),
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) releaseVerify(artifactPath, manifestPath, rootPublicKey string) error {
	if artifactPath == "" {
		return fmt.Errorf("artifact is required")
	}
	if manifestPath == "" {
		return fmt.Errorf("manifest is required")
	}
	root, err := parseRootPublicKey(rootPublicKey)
	if err != nil {
		return err
	}
	manifest, err := release.ReadManifest(manifestPath)
	if err != nil {
		return err
	}
	if err := manifest.VerifyArtifact(artifactPath, root); err != nil {
		return err
	}
	payload := map[string]any{
		"ok":       true,
		"artifact": artifactPath,
		"manifest": manifestPath,
		"sha256":   manifest.ArtifactSHA256,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) releaseCreateBundle(dir string, artifactPaths, requiredArtifacts []string, keyPath, keyID, outPath string) error {
	if dir == "" {
		return fmt.Errorf("dir is required")
	}
	if len(artifactPaths) == 0 {
		return fmt.Errorf("artifacts are required")
	}
	if keyPath == "" {
		return fmt.Errorf("key is required")
	}
	key, _, err := signing.LoadOrCreate(keyPath, keyID)
	if err != nil {
		return err
	}
	bundle, err := release.CreateBundle(release.BundleOptions{
		Dir:               dir,
		ArtifactPaths:     artifactPaths,
		RequiredArtifacts: requiredArtifacts,
		Key:               key,
		Now:               time.Now(),
	})
	if err != nil {
		return err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if outPath == "" {
		outPath = filepath.Join(absDir, "release-bundle.json")
	} else if !filepath.IsAbs(outPath) {
		outPath = filepath.Join(absDir, outPath)
	}
	outDir, err := filepath.Abs(filepath.Dir(outPath))
	if err != nil {
		return err
	}
	if outDir != absDir {
		return fmt.Errorf("bundle output must be inside release directory %s", absDir)
	}
	if err := release.WriteBundle(outPath, bundle); err != nil {
		return err
	}
	payload := map[string]any{
		"ok":              true,
		"schema":          bundle.SchemaVersion,
		"bundle":          outPath,
		"artifacts":       bundle.Artifacts,
		"root_public_key": encodeRootPublicKey(key.ID, key.PublicKey),
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) releaseVerifyBundle(bundlePath, rootPublicKey string, requiredArtifacts []string) error {
	if bundlePath == "" {
		return fmt.Errorf("bundle is required")
	}
	root, err := parseRootPublicKey(rootPublicKey)
	if err != nil {
		return err
	}
	verification, err := release.VerifyBundle(bundlePath, root, requiredArtifacts)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                  verification.OK(),
		"schema":              verification.SchemaVersion,
		"bundle":              verification.BundlePath,
		"root_key_id":         verification.RootKeyID,
		"checks":              verification.Checks,
		"artifacts":           verification.Artifacts,
		"recommended_actions": verification.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !verification.OK() {
		return fmt.Errorf("release bundle verification failed")
	}
	return nil
}

func (a App) auditExport(inputPath, outputPath string) error {
	if inputPath == "" {
		return fmt.Errorf("input is required")
	}
	if outputPath == "" {
		return fmt.Errorf("out is required")
	}
	chain, err := audit.ExportChainFromJSONL(inputPath, outputPath, time.Now())
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":          true,
		"input":       inputPath,
		"out":         outputPath,
		"event_count": chain.EventCount,
		"root_hash":   chain.RootHash,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) auditVerify(inputPath string) error {
	if inputPath == "" {
		return fmt.Errorf("input is required")
	}
	chain, err := audit.ReadChain(inputPath)
	if err != nil {
		return err
	}
	if err := audit.VerifyChain(chain); err != nil {
		return err
	}
	payload := map[string]any{
		"ok":          true,
		"input":       inputPath,
		"event_count": chain.EventCount,
		"root_hash":   chain.RootHash,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) evidenceExport(ctx context.Context, jobPath, artifactsPath, auditPath, gatewayURL, jobID, outPath string) error {
	if outPath == "" {
		return fmt.Errorf("out is required")
	}
	input := evidence.Input{GeneratedAt: time.Now()}
	source := "files"
	if gatewayURL != "" || jobID != "" {
		if gatewayURL == "" {
			return fmt.Errorf("gateway is required when job-id is set")
		}
		if jobID == "" {
			return fmt.Errorf("job-id is required when gateway is set")
		}
		if jobPath != "" || artifactsPath != "" || auditPath != "" {
			return fmt.Errorf("gateway/job-id export cannot be combined with job-json, artifacts-json, or audit-jsonl")
		}
		gatewayInput, err := fetchEvidenceInput(ctx, gatewayURL, jobID)
		if err != nil {
			return err
		}
		input = gatewayInput
		input.GeneratedAt = time.Now()
		source = "gateway"
	} else {
		if jobPath == "" {
			return fmt.Errorf("job-json is required")
		}
		job, err := readJobJSON(jobPath)
		if err != nil {
			return err
		}
		artifacts, err := readArtifactsJSON(artifactsPath)
		if err != nil {
			return err
		}
		var events []model.AuditEvent
		if auditPath != "" {
			events, err = audit.ReadJSONL(auditPath)
			if err != nil {
				return err
			}
		}
		input.Job = job
		input.Artifacts = artifacts
		input.AuditEvents = events
	}
	manifest, err := evidence.ExportDirectory(outPath, input)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                true,
		"source":            source,
		"out":               outPath,
		"job_id":            manifest.JobID,
		"file_count":        len(manifest.Files) + 1,
		"audit_event_count": manifest.AuditEventCount,
		"audit_root_hash":   manifest.AuditRootHash,
		"manifest":          filepath.Join(outPath, "manifest.json"),
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) skillkitExport(sourceRoot, outPath, gatewayURL string) error {
	if outPath == "" {
		return fmt.Errorf("out is required")
	}
	manifest, err := skillkit.Export(skillkit.ExportOptions{
		SourceRoot: sourceRoot,
		OutDir:     outPath,
		GatewayURL: gatewayURL,
	})
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":          true,
		"out":         outPath,
		"schema":      manifest.SchemaVersion,
		"skill_count": len(manifest.Skills),
		"file_count":  len(manifest.Files),
		"frameworks":  manifest.Frameworks,
		"manifest":    filepath.Join(outPath, "manifest.json"),
		"gateway_url": manifest.GatewayURL,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) skillkitVerify(bundleDir string) error {
	if bundleDir == "" {
		return fmt.Errorf("bundle is required")
	}
	report, err := skillkit.Verify(skillkit.VerifyOptions{BundleDir: bundleDir})
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                  report.OK(),
		"schema":              report.SchemaVersion,
		"bundle":              report.BundleDir,
		"manifest":            report.ManifestPath,
		"manifest_schema":     report.ManifestSchema,
		"checks":              report.Checks,
		"files_verified":      report.FilesVerified,
		"skills_verified":     report.SkillsVerified,
		"frameworks_verified": report.FrameworksVerified,
		"recommended_actions": report.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !report.OK() {
		return fmt.Errorf("skillkit bundle verification failed")
	}
	return nil
}

func (a App) workspaceLock(opts workspace.LockOptions) error {
	lock, err := workspace.NewFileLockStore(opts.StoreDir).Acquire(opts, time.Now())
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":   true,
		"lock": lock,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) workspaceStatus(repoRoot, storeDir string) error {
	status, err := workspace.NewFileLockStore(storeDir).Status(repoRoot, time.Now())
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":     true,
		"status": status,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) workspaceUnlock(repoRoot, storeDir, jobID string, force bool) error {
	lock, removed, err := workspace.NewFileLockStore(storeDir).Release(repoRoot, jobID, force)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":      true,
		"removed": removed,
		"lock":    lock,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) workspacePrepareWorktree(ctx context.Context, opts workspace.GitWorktreeOptions) error {
	result, err := workspace.PrepareGitWorktree(ctx, opts, time.Now())
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":       true,
		"worktree": result,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) printUsage() {
	_, _ = fmt.Fprintln(a.Stdout, strings.TrimSpace(`rdev - remote development skillkit

Usage:
  rdev version
  rdev doctor
  rdev ticket create --mode attended-temporary --ttl-seconds 7200
  rdev policy explain --mode attended-temporary --capability shell.user
  rdev policy explain-shell --policy-json '{"workspace_root":".","capabilities":["shell.user"],"argv":["go","env","GOOS"],"allow_commands":["go"]}'
  rdev demo local
  rdev mcp tools
  rdev mcp serve
  rdev gateway serve --dev --addr 127.0.0.1:8787
  rdev audit export --input .rdev/audit/events.jsonl --out .rdev/audit/chain.json
  rdev audit verify --input .rdev/audit/chain.json
  rdev evidence export --job-json job.json --artifacts-json artifacts.json --audit-jsonl events.jsonl --out job_evidence
  rdev evidence export --gateway http://127.0.0.1:8787 --job-id job_... --out job_evidence
  rdev skillkit export --source-root . --out dist/remote-dev-skillkit --gateway-url https://api.example.com/v1
  rdev skillkit verify --bundle dist/remote-dev-skillkit
  rdev workspace lock --repo . --host-id hst_... --job-id job_... --adapter codex
  rdev workspace prepare-worktree --repo . --host-id hst_... --job-id job_... --adapter codex
  rdev acceptance managed-mac --out acceptance-run --repo .
  rdev acceptance managed-mac-service --out service-plan --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --repo .
  rdev acceptance windows-temporary --out windows-plan --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --download-url https://agent.example/rdev-host.exe --expected-sha256 <sha256> --release-bundle-url https://agent.example/release-bundle.json --release-root-public-key release-root:... --verifier-download-url https://agent.example/rdev-verify.exe --verifier-sha256 <sha256>
  rdev acceptance verify --report acceptance-run/report.json
  rdev acceptance verify-windows-temporary --plan windows-plan/windows-temporary-plan.json
  rdev acceptance package-windows-temporary --plan windows-plan/windows-temporary-plan.json --out windows-evidence --transcript transcript.txt --release-verification rdev-verify.json --audit audit.jsonl --no-persistence-dir no-persistence --approval-probes-dir approval-probes
  rdev release sign --artifact ./rdev-host.exe --key .rdev/keys/release-root.json
  rdev release verify --artifact ./rdev-host.exe --manifest ./rdev-host.exe.rdev-release.json --root-public-key release-root:...
  rdev release create-bundle --dir dist --artifacts rdev,rdev-host.exe,rdev-verify.exe --key .rdev/keys/release-root.json
  rdev release verify-bundle --bundle dist/release-bundle.json --root-public-key release-root:...
  rdev host serve --mode temporary --gateway http://127.0.0.1:8787 --ticket-code ABCD-1234
  rdev host install-service --platform macos --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --plist-out ./com.remote-dev-skillkit.host.plist
  rdev host service-status --platform macos --plist ./com.remote-dev-skillkit.host.plist
  rdev host service-control --platform macos --action start --plist ./com.remote-dev-skillkit.host.plist
  rdev host uninstall-service --platform macos --plist ./com.remote-dev-skillkit.host.plist
`))
}

func readJobJSON(path string) (model.Job, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return model.Job{}, err
	}
	var job model.Job
	if err := json.Unmarshal(content, &job); err != nil {
		return model.Job{}, fmt.Errorf("decode job-json: %w", err)
	}
	return job, nil
}

func readArtifactsJSON(path string) ([]model.Artifact, error) {
	if path == "" {
		return nil, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var artifacts []model.Artifact
	if err := json.Unmarshal(content, &artifacts); err == nil {
		return artifacts, nil
	}
	var wrapped struct {
		Artifacts []model.Artifact `json:"artifacts"`
	}
	if err := json.Unmarshal(content, &wrapped); err != nil {
		return nil, fmt.Errorf("decode artifacts-json: %w", err)
	}
	return wrapped.Artifacts, nil
}

func registerHost(ctx context.Context, gatewayURL string, registration model.HostRegistration) (model.Host, error) {
	body, err := json.Marshal(registration)
	if err != nil {
		return model.Host{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(gatewayURL, "/")+"/v1/hosts/register", bytes.NewReader(body))
	if err != nil {
		return model.Host{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return model.Host{}, err
	}
	defer resp.Body.Close()
	var payload struct {
		Host  model.Host `json:"host"`
		Error string     `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return model.Host{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return model.Host{}, fmt.Errorf("register host failed: %s", payload.Error)
	}
	return payload.Host, nil
}

func (a App) pollAndRunDevJobs(ctx context.Context, opts hostServeOptions, hostID, identityFingerprint string) (int, error) {
	maxJobs := opts.MaxJobs
	if maxJobs <= 0 {
		maxJobs = 1
	}
	interval := opts.PollInterval
	if interval <= 0 {
		interval = time.Second
	}
	transport := opts.Transport
	if transport == "" {
		transport = "poll"
	}
	switch transport {
	case "poll", "long-poll":
	default:
		return 0, fmt.Errorf("unsupported host transport %q", transport)
	}
	longPollTimeout := opts.LongPollTimeout
	if longPollTimeout <= 0 {
		longPollTimeout = 25 * time.Second
	}
	if longPollTimeout > 60*time.Second {
		longPollTimeout = 60 * time.Second
	}
	trust, err := fetchHostTrust(ctx, opts.GatewayURL, opts.TrustPin, opts.TrustStorePath)
	if err != nil {
		return 0, err
	}
	trust.NonceStore = hostNonceStore(opts.NonceStorePath)
	trust.ApprovalStore = hostApprovalStore(opts.ApprovalStorePath)
	trust.WorkspaceLockStore = opts.WorkspaceLockStore
	processed := 0
	for processed < maxJobs {
		wait := time.Duration(0)
		if transport == "long-poll" {
			wait = longPollTimeout
		}
		if opts.TrustStorePath != "" {
			trust, err = refreshHostTrustUpdate(ctx, opts.GatewayURL, hostID, opts.TrustStorePath, trust)
			if err != nil {
				return processed, err
			}
		}
		job, found, err := fetchNextJob(ctx, opts.GatewayURL, hostID, wait)
		if err != nil {
			return processed, err
		}
		if !found {
			if transport == "long-poll" {
				continue
			}
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return processed, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		jobCtx, cancelJob := context.WithCancel(ctx)
		monitorCtx, stopMonitor := context.WithCancel(ctx)
		var canceledByGateway atomic.Bool
		monitorDone := make(chan struct{})
		go func() {
			defer close(monitorDone)
			monitorJobCancellation(monitorCtx, opts.GatewayURL, job.ID, cancellationPollInterval(interval), func() {
				canceledByGateway.Store(true)
				cancelJob()
			})
		}()
		result, err := trust.RunDevJob(jobCtx, hostID, identityFingerprint, job, time.Now())
		cancelJob()
		stopMonitor()
		<-monitorDone
		if err != nil {
			if canceledByGateway.Load() {
				if result.ArtifactContent != "" {
					if _, appendErr := appendJobArtifact(ctx, opts.GatewayURL, hostID, job.ID, result.ArtifactContent); appendErr != nil {
						return processed, appendErr
					}
				}
				processed++
				continue
			}
			if ctx.Err() != nil {
				return processed, ctx.Err()
			}
			if _, failErr := failJob(ctx, opts.GatewayURL, hostID, job.ID, err.Error(), result.ArtifactContent); failErr != nil {
				return processed, fmt.Errorf("%v; additionally failed to report job failure: %w", err, failErr)
			}
			return processed, err
		}
		if _, err := completeJob(ctx, opts.GatewayURL, hostID, job.ID, result.ArtifactContent); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func cancellationPollInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return time.Second
	}
	if interval < 50*time.Millisecond {
		return 50 * time.Millisecond
	}
	if interval > time.Second {
		return time.Second
	}
	return interval
}

func monitorJobCancellation(ctx context.Context, gatewayURL, jobID string, interval time.Duration, cancel func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			job, err := fetchJob(ctx, gatewayURL, jobID)
			if err != nil {
				continue
			}
			if job.Status == model.JobStatusCanceled {
				cancel()
				return
			}
		}
	}
}

func fetchJoinManifest(ctx context.Context, manifestURL, trustPin, manifestRootPublicKey string) (model.JoinManifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return model.JoinManifest{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return model.JoinManifest{}, err
	}
	defer resp.Body.Close()
	var payload struct {
		Manifest model.JoinManifest `json:"manifest"`
		Error    string             `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return model.JoinManifest{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return model.JoinManifest{}, fmt.Errorf("fetch join manifest failed: %s", payload.Error)
	}
	if manifestRootPublicKey != "" {
		root, err := parseRootPublicKey(manifestRootPublicKey)
		if err != nil {
			return model.JoinManifest{}, err
		}
		if err := payload.Manifest.VerifyWithRoot(root, time.Now()); err != nil {
			return model.JoinManifest{}, err
		}
	} else if err := payload.Manifest.Verify(time.Now()); err != nil {
		return model.JoinManifest{}, err
	}
	if err := payload.Manifest.Trust.VerifyPin(trustPin); err != nil {
		return model.JoinManifest{}, err
	}
	return payload.Manifest, nil
}

type hostTrust struct {
	Legacy             *model.TrustBundle
	SignedBundle       *model.SignedTrustBundle
	NonceStore         hostnonce.Store
	ApprovalStore      hostapproval.Store
	WorkspaceLockStore string
}

func (t hostTrust) RunDevJob(ctx context.Context, hostID, identityFingerprint string, job model.Job, now time.Time) (hostrunner.Result, error) {
	opts := hostrunner.Options{
		IdentityFingerprint: identityFingerprint,
		NonceStore:          t.NonceStore,
		ApprovalStore:       t.ApprovalStore,
		WorkspaceLockStore:  t.WorkspaceLockStore,
	}
	if t.SignedBundle != nil {
		return hostrunner.RunDevJobWithTrustBundleOptionsContext(ctx, hostID, *t.SignedBundle, job, now, opts)
	}
	if t.Legacy != nil {
		return hostrunner.RunDevJobWithOptionsContext(ctx, hostID, *t.Legacy, job, now, opts)
	}
	return hostrunner.Result{}, fmt.Errorf("host trust is not configured")
}

func fetchHostTrust(ctx context.Context, gatewayURL, trustPin, trustStorePath string) (hostTrust, error) {
	store := hosttrust.FileStore{Path: trustStorePath}
	signed, err := fetchSignedTrustBundle(ctx, gatewayURL, trustPin)
	if err == nil {
		if trustStorePath != "" {
			root, rootErr := activeSigningRoot(signed)
			if rootErr != nil {
				return hostTrust{}, rootErr
			}
			if storeErr := store.VerifyAndSaveUpdate(signed, root, time.Now()); storeErr != nil {
				return hostTrust{}, storeErr
			}
		}
		return hostTrust{SignedBundle: &signed}, nil
	}
	if stored, ok, storeErr := store.Load(); storeErr != nil {
		return hostTrust{}, storeErr
	} else if ok {
		return hostTrust{SignedBundle: &stored}, nil
	}
	legacy, legacyErr := fetchTrustBundle(ctx, gatewayURL, trustPin)
	if legacyErr != nil {
		return hostTrust{}, fmt.Errorf("fetch signed trust bundle failed: %v; fallback legacy trust failed: %w", err, legacyErr)
	}
	return hostTrust{Legacy: &legacy}, nil
}

func refreshHostTrustUpdate(ctx context.Context, gatewayURL, hostID, trustStorePath string, current hostTrust) (hostTrust, error) {
	store := hosttrust.FileStore{Path: trustStorePath}
	stored, ok, err := store.Load()
	if err != nil {
		return hostTrust{}, err
	}
	if !ok {
		return current, nil
	}
	hash, err := stored.Hash()
	if err != nil {
		return hostTrust{}, err
	}
	update, err := fetchTrustBundleUpdate(ctx, gatewayURL, hostID, stored.Sequence, hash)
	if err != nil {
		return hostTrust{}, err
	}
	if update.Status == model.TrustBundleUpdateStatusCurrent {
		return current, nil
	}
	if update.Status != model.TrustBundleUpdateStatusAvailable {
		return hostTrust{}, fmt.Errorf("unsupported trust bundle update status %q", update.Status)
	}
	if update.TrustBundle == nil {
		return hostTrust{}, fmt.Errorf("trust bundle update missing bundle")
	}
	if err := store.VerifyAndSaveUpdate(*update.TrustBundle, model.TrustBundle{}, time.Now()); err != nil {
		return hostTrust{}, err
	}
	loaded, ok, err := store.Load()
	if err != nil {
		return hostTrust{}, err
	}
	if !ok {
		return hostTrust{}, fmt.Errorf("trust bundle update was not persisted")
	}
	current.SignedBundle = &loaded
	current.Legacy = nil
	return current, nil
}

func hostNonceStore(path string) hostnonce.Store {
	if path != "" {
		return hostnonce.FileStore{Path: path}
	}
	return hostnonce.NewMemoryStore()
}

func hostApprovalStore(path string) hostapproval.Store {
	if path != "" {
		return hostapproval.FileStore{Path: path}
	}
	return hostapproval.NewMemoryStore()
}

func activeSigningRoot(bundle model.SignedTrustBundle) (model.TrustBundle, error) {
	key, ok := bundle.Key(bundle.SigningKeyID)
	if !ok {
		return model.TrustBundle{}, fmt.Errorf("signed trust bundle missing signing key %q", bundle.SigningKeyID)
	}
	return key.TrustBundle(), nil
}

func encodeRootPublicKey(keyID string, publicKey ed25519.PublicKey) string {
	return trustref.Encode(keyID, publicKey)
}

func parseRootPublicKey(value string) (model.TrustBundle, error) {
	return trustref.Parse(value)
}

func fetchSignedTrustBundle(ctx context.Context, gatewayURL, trustPin string) (model.SignedTrustBundle, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(gatewayURL, "/")+"/v1/trust-bundle", nil)
	if err != nil {
		return model.SignedTrustBundle{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return model.SignedTrustBundle{}, err
	}
	defer resp.Body.Close()
	var payload struct {
		TrustBundle model.SignedTrustBundle `json:"trust_bundle"`
		Error       string                  `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return model.SignedTrustBundle{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return model.SignedTrustBundle{}, fmt.Errorf("fetch signed trust bundle failed: %s", payload.Error)
	}
	root, err := activeSigningRoot(payload.TrustBundle)
	if err != nil {
		return model.SignedTrustBundle{}, err
	}
	if err := payload.TrustBundle.Verify(root, time.Now()); err != nil {
		return model.SignedTrustBundle{}, err
	}
	if err := root.VerifyPin(trustPin); err != nil {
		return model.SignedTrustBundle{}, err
	}
	return payload.TrustBundle, nil
}

func fetchTrustBundleUpdate(ctx context.Context, gatewayURL, hostID string, currentSequence int, currentHash string) (model.TrustBundleUpdate, error) {
	values := url.Values{}
	values.Set("current_sequence", strconv.Itoa(currentSequence))
	if currentHash != "" {
		values.Set("current_hash", currentHash)
	}
	endpoint := strings.TrimRight(gatewayURL, "/") + "/v1/hosts/" + url.PathEscape(hostID) + "/trust-bundle/update?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return model.TrustBundleUpdate{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return model.TrustBundleUpdate{}, err
	}
	defer resp.Body.Close()
	var payload struct {
		TrustBundleUpdate model.TrustBundleUpdate `json:"trust_bundle_update"`
		Error             string                  `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return model.TrustBundleUpdate{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return model.TrustBundleUpdate{}, fmt.Errorf("fetch trust bundle update failed: %s", payload.Error)
	}
	if payload.TrustBundleUpdate.SchemaVersion != model.TrustBundleUpdateSchemaVersion {
		return model.TrustBundleUpdate{}, fmt.Errorf("unsupported trust bundle update schema %q", payload.TrustBundleUpdate.SchemaVersion)
	}
	return payload.TrustBundleUpdate, nil
}

func fetchTrustBundle(ctx context.Context, gatewayURL, trustPin string) (model.TrustBundle, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(gatewayURL, "/")+"/v1/trust", nil)
	if err != nil {
		return model.TrustBundle{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return model.TrustBundle{}, err
	}
	defer resp.Body.Close()
	var payload struct {
		Trust model.TrustBundle `json:"trust"`
		Error string            `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return model.TrustBundle{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return model.TrustBundle{}, fmt.Errorf("fetch trust bundle failed: %s", payload.Error)
	}
	if _, err := payload.Trust.Ed25519PublicKey(); err != nil {
		return model.TrustBundle{}, err
	}
	if err := payload.Trust.VerifyPin(trustPin); err != nil {
		return model.TrustBundle{}, err
	}
	return payload.Trust, nil
}

func fetchNextJob(ctx context.Context, gatewayURL, hostID string, wait time.Duration) (model.Job, bool, error) {
	endpoint := strings.TrimRight(gatewayURL, "/") + "/v1/hosts/" + url.PathEscape(hostID) + "/jobs/next"
	if wait > 0 {
		waitMS := wait.Milliseconds()
		if waitMS < 1 {
			waitMS = 1
		}
		values := url.Values{}
		values.Set("wait_ms", strconv.FormatInt(waitMS, 10))
		endpoint += "?" + values.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return model.Job{}, false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return model.Job{}, false, err
	}
	defer resp.Body.Close()
	var payload struct {
		Job   *model.Job `json:"job"`
		Error string     `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return model.Job{}, false, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return model.Job{}, false, fmt.Errorf("fetch next job failed: %s", payload.Error)
	}
	if payload.Job == nil {
		return model.Job{}, false, nil
	}
	return *payload.Job, true, nil
}

func waitForHostActive(ctx context.Context, gatewayURL, hostID string, timeout, interval time.Duration) (model.Host, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if interval <= 0 {
		interval = time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		host, err := fetchHost(ctx, gatewayURL, hostID)
		if err != nil {
			return model.Host{}, err
		}
		if host.Status == model.HostStatusActive {
			return host, nil
		}
		if host.Status == model.HostStatusRevoked {
			return model.Host{}, fmt.Errorf("host was revoked before approval")
		}
		if time.Now().After(deadline) {
			return model.Host{}, fmt.Errorf("timed out waiting for host approval")
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return model.Host{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func fetchHost(ctx context.Context, gatewayURL, hostID string) (model.Host, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(gatewayURL, "/")+"/v1/hosts/"+hostID, nil)
	if err != nil {
		return model.Host{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return model.Host{}, err
	}
	defer resp.Body.Close()
	var payload struct {
		Host  model.Host `json:"host"`
		Error string     `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return model.Host{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return model.Host{}, fmt.Errorf("fetch host failed: %s", payload.Error)
	}
	return payload.Host, nil
}

func fetchEvidenceInput(ctx context.Context, gatewayURL, jobID string) (evidence.Input, error) {
	job, err := fetchJob(ctx, gatewayURL, jobID)
	if err != nil {
		return evidence.Input{}, err
	}
	artifacts, err := fetchJobArtifacts(ctx, gatewayURL, jobID)
	if err != nil {
		return evidence.Input{}, err
	}
	events, err := fetchAuditEvents(ctx, gatewayURL)
	if err != nil {
		return evidence.Input{}, err
	}
	return evidence.Input{
		Job:         job,
		Artifacts:   artifacts,
		AuditEvents: events,
		GeneratedAt: time.Now(),
	}, nil
}

func fetchJob(ctx context.Context, gatewayURL, jobID string) (model.Job, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(gatewayURL, "/")+"/v1/jobs/"+url.PathEscape(jobID), nil)
	if err != nil {
		return model.Job{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return model.Job{}, err
	}
	defer resp.Body.Close()
	var payload struct {
		Job   model.Job `json:"job"`
		Error string    `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return model.Job{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return model.Job{}, fmt.Errorf("fetch job failed: %s", payload.Error)
	}
	return payload.Job, nil
}

func fetchJobArtifacts(ctx context.Context, gatewayURL, jobID string) ([]model.Artifact, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(gatewayURL, "/")+"/v1/jobs/"+url.PathEscape(jobID)+"/artifacts", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload struct {
		Artifacts []model.Artifact `json:"artifacts"`
		Error     string           `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return nil, fmt.Errorf("fetch job artifacts failed: %s", payload.Error)
	}
	return payload.Artifacts, nil
}

func fetchAuditEvents(ctx context.Context, gatewayURL string) ([]model.AuditEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(gatewayURL, "/")+"/v1/audit", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload struct {
		Events []model.AuditEvent `json:"events"`
		Error  string             `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return nil, fmt.Errorf("fetch audit events failed: %s", payload.Error)
	}
	return payload.Events, nil
}

func completeJob(ctx context.Context, gatewayURL, hostID, jobID, artifactContent string) (model.Job, error) {
	body, err := json.Marshal(map[string]string{
		"host_id":          hostID,
		"artifact_content": artifactContent,
	})
	if err != nil {
		return model.Job{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(gatewayURL, "/")+"/v1/jobs/"+jobID+"/complete", bytes.NewReader(body))
	if err != nil {
		return model.Job{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return model.Job{}, err
	}
	defer resp.Body.Close()
	var payload struct {
		Job   model.Job `json:"job"`
		Error string    `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return model.Job{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return model.Job{}, fmt.Errorf("complete job failed: %s", payload.Error)
	}
	return payload.Job, nil
}

func failJob(ctx context.Context, gatewayURL, hostID, jobID, reason, artifactContent string) (model.Job, error) {
	body, err := json.Marshal(map[string]string{
		"host_id":          hostID,
		"reason":           reason,
		"artifact_content": artifactContent,
	})
	if err != nil {
		return model.Job{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(gatewayURL, "/")+"/v1/jobs/"+jobID+"/fail", bytes.NewReader(body))
	if err != nil {
		return model.Job{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return model.Job{}, err
	}
	defer resp.Body.Close()
	var payload struct {
		Job   model.Job `json:"job"`
		Error string    `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return model.Job{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return model.Job{}, fmt.Errorf("fail job failed: %s", payload.Error)
	}
	return payload.Job, nil
}

func appendJobArtifact(ctx context.Context, gatewayURL, hostID, jobID, artifactContent string) (model.Job, error) {
	body, err := json.Marshal(map[string]string{
		"host_id":          hostID,
		"artifact_content": artifactContent,
	})
	if err != nil {
		return model.Job{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(gatewayURL, "/")+"/v1/jobs/"+jobID+"/artifact", bytes.NewReader(body))
	if err != nil {
		return model.Job{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return model.Job{}, err
	}
	defer resp.Body.Close()
	var payload struct {
		Job   model.Job `json:"job"`
		Error string    `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return model.Job{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return model.Job{}, fmt.Errorf("append job artifact failed: %s", payload.Error)
	}
	return payload.Job, nil
}

func capabilitiesToStrings(caps []policy.Capability) []string {
	values := make([]string, 0, len(caps))
	for _, cap := range caps {
		values = append(values, string(cap))
	}
	return values
}

func splitCapabilities(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	capabilities := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			capabilities = append(capabilities, part)
		}
	}
	return capabilities
}

func parseJSONStringArray(raw, name string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", name, err)
	}
	return values, nil
}

func parseJSONStringMatrix(raw, name string) ([][]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var values [][]string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", name, err)
	}
	return values, nil
}

func allAcceptanceChecksPassed(checks []acceptance.Check) bool {
	if len(checks) == 0 {
		return false
	}
	for _, check := range checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func Main() {
	app := NewApp(os.Stdout, os.Stderr)
	if err := app.Run(context.Background(), os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "rdev: %v\n", err)
		os.Exit(1)
	}
}
