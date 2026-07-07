package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
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
	"github.com/EitanWong/remote-dev-skillkit/internal/agentinvite"
	"github.com/EitanWong/remote-dev-skillkit/internal/audit"
	"github.com/EitanWong/remote-dev-skillkit/internal/buildinfo"
	"github.com/EitanWong/remote-dev-skillkit/internal/connectionentry"
	"github.com/EitanWong/remote-dev-skillkit/internal/connectionrunner"
	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/depsinstall"
	"github.com/EitanWong/remote-dev-skillkit/internal/enrollmentlifecycle"
	"github.com/EitanWong/remote-dev-skillkit/internal/evidence"
	"github.com/EitanWong/remote-dev-skillkit/internal/evidenceplan"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostapproval"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostawake"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostcap"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostedprovider"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostidentity"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostnonce"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostrunner"
	"github.com/EitanWong/remote-dev-skillkit/internal/hosttrust"
	"github.com/EitanWong/remote-dev-skillkit/internal/httpapi"
	"github.com/EitanWong/remote-dev-skillkit/internal/jobtemplate"
	"github.com/EitanWong/remote-dev-skillkit/internal/mcpstdio"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/operatorauth"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
	"github.com/EitanWong/remote-dev-skillkit/internal/relayadapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/release"
	"github.com/EitanWong/remote-dev-skillkit/internal/service"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
	"github.com/EitanWong/remote-dev-skillkit/internal/skillkit"
	"github.com/EitanWong/remote-dev-skillkit/internal/supportsession"
	"github.com/EitanWong/remote-dev-skillkit/internal/trustref"
	"github.com/EitanWong/remote-dev-skillkit/internal/update"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
	"github.com/EitanWong/remote-dev-skillkit/internal/wsproto"
	"github.com/EitanWong/remote-dev-skillkit/pkg/adapterkit"
)

const exampleAgentJoinBaseURL = "https://agent.example.com"

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
	if len(args) > 1 && isHelpArg(args[1]) && a.printCommandGroupUsage(args[0]) {
		return nil
	}

	switch args[0] {
	case "version":
		return a.version(args[1:])
	case "doctor":
		return a.doctor(ctx)
	case "bootstrap":
		return a.bootstrap(ctx, args[1:])
	case "support-session":
		return a.supportSession(ctx, args[1:])
	case "mcp":
		return a.mcp(args[1:])
	case "host":
		return a.host(ctx, args[1:])
	case "invite":
		return a.invite(ctx, args[1:])
	case "connection-entry":
		return a.connectionEntry(args[1:])
	case "ticket":
		return a.ticket(args[1:])
	case "policy":
		return a.policy(args[1:])
	case "demo":
		return a.demo(args[1:])
	case "gateway":
		return a.gateway(args[1:])
	case "operator-auth":
		return a.operatorAuth(args[1:])
	case "hosted-provider":
		return a.hostedProvider(args[1:])
	case "relay-adapter":
		return a.relayAdapter(args[1:])
	case "release":
		return a.release(args[1:])
	case "update":
		return a.update(ctx, args[1:])
	case "deps":
		return a.deps(ctx, args[1:])
	case "enrollment":
		return a.enrollment(ctx, args[1:])
	case "trust":
		return a.trust(args[1:])
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
	case "adapter":
		return a.adapter(args[1:])
	case "job":
		return a.job(ctx, args[1:])
	case "help", "-h", "--help":
		a.printUsage()
		return nil
	default:
		// Provide a "did you mean?" hint for common near-misses so that agents
		// and users get actionable feedback instead of a bare "unknown command".
		suggestions := map[string]string{
			"hosts":            "host",
			"jobs":             "job",
			"tickets":          "ticket",
			"invites":          "invite",
			"policies":         "policy",
			"gateways":         "gateway",
			"connections":      "connection-entry",
			"connection_entry": "connection-entry",
			"support_session":  "support-session",
			"mcp-serve":        "mcp serve",
			"host-serve":       "host serve",
		}
		if hint, ok := suggestions[args[0]]; ok {
			return fmt.Errorf("unknown command %q — did you mean: rdev %s", args[0], hint)
		}
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "-h" || arg == "--help"
}

func (a App) update(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing update subcommand")
	}
	switch args[0] {
	case "check":
		fs := flag.NewFlagSet("update check", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		repo := fs.String("repo", update.DefaultRepo, "GitHub repository in OWNER/REPO form")
		apiBaseURL := fs.String("api-base-url", update.DefaultAPIBaseURL, "GitHub API base URL")
		currentVersion := fs.String("current-version", buildinfo.Version, "currently installed rdev version")
		tokenFile := fs.String("github-token-file", "", "optional file containing GitHub token for private/rate-limited checks")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		token, err := readOptionalTokenFile(*tokenFile)
		if err != nil {
			return err
		}
		check, err := update.CheckLatest(ctx, http.DefaultClient, update.Options{
			Repo:           *repo,
			APIBaseURL:     *apiBaseURL,
			CurrentVersion: *currentVersion,
			Token:          token,
		})
		if err != nil {
			_ = writeJSON(a.Stdout, check)
			return err
		}
		return writeJSON(a.Stdout, check)
	case "plan":
		fs := flag.NewFlagSet("update plan", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		repo := fs.String("repo", update.DefaultRepo, "GitHub repository in OWNER/REPO form")
		apiBaseURL := fs.String("api-base-url", update.DefaultAPIBaseURL, "GitHub API base URL")
		currentVersion := fs.String("current-version", buildinfo.Version, "currently installed rdev version")
		platform := fs.String("platform", runtime.GOOS+"/"+runtime.GOARCH, "target platform as GOOS/GOARCH")
		tokenFile := fs.String("github-token-file", "", "optional file containing GitHub token for private/rate-limited checks")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		token, err := readOptionalTokenFile(*tokenFile)
		if err != nil {
			return err
		}
		opts := update.Options{
			Repo:           *repo,
			APIBaseURL:     *apiBaseURL,
			CurrentVersion: *currentVersion,
			Platform:       *platform,
			Token:          token,
		}
		check, err := update.CheckLatest(ctx, http.DefaultClient, opts)
		if err != nil {
			_ = writeJSON(a.Stdout, check)
			return err
		}
		plan := update.PlanFromCheck(check, opts)
		return writeJSON(a.Stdout, plan)
	default:
		return fmt.Errorf("unknown update subcommand %q", args[0])
	}
}

func (a App) deps(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing deps subcommand")
	}
	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("deps install", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		tool := fs.String("tool", "", "dependency tool to install: chisel, frpc, tailscale, or wg")
		scope := fs.String("scope", "user", "install scope: user or workspace")
		version := fs.String("version", "", "optional dependency version label")
		platform := fs.String("platform", runtime.GOOS+"/"+runtime.GOARCH, "target platform, for example linux/amd64")
		downloadURL := fs.String("url", "", "download URL for the reviewed helper archive or binary")
		expectedSHA256 := fs.String("expected-sha256", "", "expected SHA-256 for the download")
		installDir := fs.String("install-dir", "", "optional install directory; defaults to a user or workspace rdev tools dir")
		execute := fs.Bool("execute", false, "download, verify, and install; omitted means plan-only")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *tool == "" && fs.NArg() > 0 {
			*tool = fs.Arg(0)
		}
		report, err := depsinstall.Install(ctx, http.DefaultClient, depsinstall.Options{
			Tool:           *tool,
			Scope:          *scope,
			Version:        *version,
			Platform:       *platform,
			URL:            *downloadURL,
			ExpectedSHA256: *expectedSHA256,
			InstallDir:     *installDir,
			Execute:        *execute,
		})
		if err != nil {
			return err
		}
		return writeJSON(a.Stdout, map[string]any{
			"ok":                  report.OK(),
			"schema":              report.SchemaVersion,
			"tool":                report.Tool,
			"scope":               report.Scope,
			"platform":            report.Platform,
			"execute":             report.Execute,
			"executed":            report.Executed,
			"installed_binary":    report.InstalledBinary,
			"dependency_report":   report,
			"recommended_actions": report.RecommendedActions,
		})
	default:
		return fmt.Errorf("unknown deps subcommand %q", args[0])
	}
}

func (a App) enrollment(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing enrollment subcommand")
	}
	switch args[0] {
	case "issue-certificate":
		fs := flag.NewFlagSet("enrollment issue-certificate", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		gatewayURL := fs.String("gateway", "", "gateway base URL")
		out := fs.String("out", "", "output enrollment certificate path")
		rootPublicKey := fs.String("root-public-key", "", "expected enrollment root public key, formatted key_id:base64url_public_key")
		ticketCode := fs.String("ticket-code", "", "ticket code authorized by this certificate")
		name := fs.String("name", "", "host name authorized by this certificate")
		osName := fs.String("os", "", "host operating system authorized by this certificate")
		arch := fs.String("arch", "", "host architecture authorized by this certificate")
		identityKeyID := fs.String("identity-key-id", "", "host identity key id")
		identityPublicKey := fs.String("identity-public-key", "", "host identity public key")
		identityFingerprint := fs.String("identity-fingerprint", "", "host identity fingerprint")
		capabilities := fs.String("capabilities", "", "comma-separated authorized capabilities; defaults to ticket capabilities")
		operatorTokenFile := fs.String("operator-token-file", "", "file containing an operator auth bearer token with issuer role")
		validMinutes := fs.Int("valid-minutes", 60, "certificate validity window in minutes")
		force := fs.Bool("force", false, "overwrite output certificate")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.enrollmentIssueCertificate(ctx, enrollmentIssueCertificateOptions{
			GatewayURL:          *gatewayURL,
			OutPath:             *out,
			RootPublicKey:       *rootPublicKey,
			TicketCode:          *ticketCode,
			Name:                *name,
			OS:                  *osName,
			Arch:                *arch,
			IdentityKeyID:       *identityKeyID,
			IdentityPublicKey:   *identityPublicKey,
			IdentityFingerprint: *identityFingerprint,
			Capabilities:        splitCapabilities(*capabilities),
			OperatorTokenFile:   *operatorTokenFile,
			ValidMinutes:        *validMinutes,
			Force:               *force,
		})
	case "sign-certificate":
		fs := flag.NewFlagSet("enrollment sign-certificate", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		out := fs.String("out", "", "output enrollment certificate path")
		keyPath := fs.String("key", "", "Ed25519 enrollment root signing key file")
		keyID := fs.String("key-id", "enrollment-root", "enrollment root signing key id")
		ticketCode := fs.String("ticket-code", "", "ticket code authorized by this certificate")
		mode := fs.String("mode", "managed", "host mode: attended-temporary, temporary, managed, or break-glass")
		name := fs.String("name", "", "host name authorized by this certificate")
		osName := fs.String("os", "", "host operating system authorized by this certificate")
		arch := fs.String("arch", "", "host architecture authorized by this certificate")
		identityKeyID := fs.String("identity-key-id", "", "host identity key id")
		identityPublicKey := fs.String("identity-public-key", "", "host identity public key")
		identityFingerprint := fs.String("identity-fingerprint", "", "host identity fingerprint")
		capabilities := fs.String("capabilities", "", "comma-separated authorized capabilities")
		validMinutes := fs.Int("valid-minutes", 60, "certificate validity window in minutes")
		force := fs.Bool("force", false, "overwrite output certificate")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.enrollmentSignCertificate(enrollmentSignCertificateOptions{
			OutPath:             *out,
			KeyPath:             *keyPath,
			KeyID:               *keyID,
			TicketCode:          *ticketCode,
			Mode:                *mode,
			Name:                *name,
			OS:                  *osName,
			Arch:                *arch,
			IdentityKeyID:       *identityKeyID,
			IdentityPublicKey:   *identityPublicKey,
			IdentityFingerprint: *identityFingerprint,
			Capabilities:        splitCapabilities(*capabilities),
			ValidMinutes:        *validMinutes,
			Force:               *force,
		})
	case "verify-certificate":
		fs := flag.NewFlagSet("enrollment verify-certificate", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		certificatePath := fs.String("certificate", "", "enrollment certificate JSON path")
		rootPublicKey := fs.String("root-public-key", "", "enrollment root public key, formatted key_id:base64url_public_key")
		revocationsPath := fs.String("revocations", "", "optional signed enrollment revocation list JSON path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.enrollmentVerifyCertificate(*certificatePath, *rootPublicKey, *revocationsPath)
	case "renew-certificate":
		fs := flag.NewFlagSet("enrollment renew-certificate", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		out := fs.String("out", "", "output renewed enrollment certificate path")
		keyPath := fs.String("key", "", "Ed25519 enrollment root signing key file")
		keyID := fs.String("key-id", "enrollment-root", "enrollment root signing key id")
		gatewayURL := fs.String("gateway", "", "optional gateway base URL for hosted renewal")
		rootPublicKey := fs.String("root-public-key", "", "expected enrollment root public key for hosted renewal, formatted key_id:base64url_public_key")
		operatorTokenFile := fs.String("operator-token-file", "", "file containing an operator auth bearer token with issuer role")
		certificatePath := fs.String("certificate", "", "current enrollment certificate JSON path to renew")
		revocationsPath := fs.String("revocations", "", "optional signed enrollment revocation list JSON path checked before renewal")
		validMinutes := fs.Int("valid-minutes", 60, "renewed certificate validity window in minutes")
		force := fs.Bool("force", false, "overwrite output certificate")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.enrollmentRenewCertificate(enrollmentRenewCertificateOptions{
			OutPath:           *out,
			KeyPath:           *keyPath,
			KeyID:             *keyID,
			GatewayURL:        *gatewayURL,
			RootPublicKey:     *rootPublicKey,
			OperatorTokenFile: *operatorTokenFile,
			CertificatePath:   *certificatePath,
			RevocationsPath:   *revocationsPath,
			ValidMinutes:      *validMinutes,
			Force:             *force,
		})
	case "revoke-certificate":
		fs := flag.NewFlagSet("enrollment revoke-certificate", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		out := fs.String("out", "", "output signed enrollment revocation list path")
		current := fs.String("current", "", "optional current signed enrollment revocation list path to extend")
		keyPath := fs.String("key", "", "Ed25519 enrollment root signing key file")
		keyID := fs.String("key-id", "enrollment-root", "enrollment root signing key id")
		certificatePath := fs.String("certificate", "", "enrollment certificate JSON path to revoke")
		reason := fs.String("reason", "", "revocation reason")
		validHours := fs.Int("valid-hours", 168, "revocation list validity window in hours")
		force := fs.Bool("force", false, "overwrite output revocation list")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.enrollmentRevokeCertificate(enrollmentRevokeCertificateOptions{
			OutPath:         *out,
			CurrentPath:     *current,
			KeyPath:         *keyPath,
			KeyID:           *keyID,
			CertificatePath: *certificatePath,
			Reason:          *reason,
			ValidHours:      *validHours,
			Force:           *force,
		})
	case "init-revocations":
		fs := flag.NewFlagSet("enrollment init-revocations", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		out := fs.String("out", "", "output signed empty enrollment revocation list path")
		keyPath := fs.String("key", "", "Ed25519 enrollment root signing key file")
		keyID := fs.String("key-id", "enrollment-root", "enrollment root signing key id")
		validHours := fs.Int("valid-hours", 168, "revocation list validity window in hours")
		force := fs.Bool("force", false, "overwrite output revocation list")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.enrollmentInitRevocations(enrollmentInitRevocationsOptions{
			OutPath:    *out,
			KeyPath:    *keyPath,
			KeyID:      *keyID,
			ValidHours: *validHours,
			Force:      *force,
		})
	case "verify-revocations":
		fs := flag.NewFlagSet("enrollment verify-revocations", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		revocationsPath := fs.String("revocations", "", "signed enrollment revocation list JSON path")
		rootPublicKey := fs.String("root-public-key", "", "enrollment root public key, formatted key_id:base64url_public_key")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.enrollmentVerifyRevocations(*revocationsPath, *rootPublicKey)
	case "fetch-revocations":
		fs := flag.NewFlagSet("enrollment fetch-revocations", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		gatewayURL := fs.String("gateway", "", "gateway base URL")
		rootPublicKey := fs.String("root-public-key", "", "enrollment root public key, formatted key_id:base64url_public_key")
		operatorTokenFile := fs.String("operator-token-file", "", "file containing an operator auth bearer token with issuer role")
		out := fs.String("out", "", "output signed enrollment revocation list JSON path")
		force := fs.Bool("force", false, "overwrite output revocation list")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.enrollmentFetchRevocations(ctx, *gatewayURL, *rootPublicKey, *operatorTokenFile, *out, *force)
	case "lifecycle":
		return a.enrollmentLifecycle(args[1:])
	default:
		return fmt.Errorf("unknown enrollment subcommand %q", args[0])
	}
}

func (a App) enrollmentLifecycle(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing enrollment lifecycle subcommand")
	}
	switch args[0] {
	case "key-custody":
		fs := flag.NewFlagSet("enrollment lifecycle key-custody", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		rootPublicKey := fs.String("root-public-key", "", "enrollment root public key, formatted key_id:base64url_public_key")
		custodian := fs.String("custodian", "", "operator or team responsible for enrollment root custody")
		provider := fs.String("provider", "", "custody provider, for example kms, hsm, secret-manager, or offline")
		rotationDays := fs.Int("rotation-days", 90, "key custody review and rotation interval in days")
		dualControl := fs.Bool("dual-control", true, "require two-person control for enrollment root operations")
		breakGlass := fs.Bool("break-glass-required", true, "require a documented break-glass path")
		out := fs.String("out", "", "output key custody record JSON path")
		force := fs.Bool("force", false, "overwrite output key custody record")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.enrollmentLifecycleKeyCustody(*rootPublicKey, *custodian, *provider, *rotationDays, *dualControl, *breakGlass, *out, *force)
	case "fleet-renewal-plan":
		fs := flag.NewFlagSet("enrollment lifecycle fleet-renewal-plan", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		certificates := fs.String("certificates", "", "JSON file containing an array or {certificates:[...]} enrollment certificates")
		revocations := fs.String("revocations", "", "optional signed enrollment revocation list JSON path")
		rootPublicKey := fs.String("root-public-key", "", "enrollment root public key, formatted key_id:base64url_public_key")
		renewBefore := fs.Duration("renew-before", 24*time.Hour, "renew certificates expiring within this duration")
		renewValidFor := fs.Duration("renew-valid-for", 24*time.Hour, "target renewed certificate validity")
		maxSkew := fs.Duration("max-skew", 30*time.Second, "maximum clock skew allowed when classifying expiry")
		requireRevocations := fs.Bool("require-revocations", true, "require a signed revocation list input")
		out := fs.String("out", "", "output fleet renewal plan JSON path")
		force := fs.Bool("force", false, "overwrite output fleet renewal plan")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.enrollmentLifecycleFleetRenewalPlan(*certificates, *revocations, *rootPublicKey, *renewBefore, *renewValidFor, *maxSkew, *requireRevocations, *out, *force)
	case "emergency-drill":
		fs := flag.NewFlagSet("enrollment lifecycle emergency-drill", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		name := fs.String("name", "", "drill name")
		scenario := fs.String("scenario", "", "emergency scenario, for example enrollment-root-compromise")
		operatorRole := fs.String("operator-role", operatorauth.RoleAdmin, "operator role responsible for the drill")
		rootPublicKey := fs.String("root-public-key", "", "enrollment root public key, formatted key_id:base64url_public_key")
		revocations := fs.String("revocations", "", "signed enrollment revocation list JSON path")
		out := fs.String("out", "", "output emergency drill evidence JSON path")
		force := fs.Bool("force", false, "overwrite output emergency drill evidence")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.enrollmentLifecycleEmergencyDrill(*name, *scenario, *operatorRole, *rootPublicKey, *revocations, *out, *force)
	default:
		return fmt.Errorf("unknown enrollment lifecycle subcommand %q", args[0])
	}
}

type enrollmentSignCertificateOptions struct {
	OutPath             string
	KeyPath             string
	KeyID               string
	TicketCode          string
	Mode                string
	Name                string
	OS                  string
	Arch                string
	IdentityKeyID       string
	IdentityPublicKey   string
	IdentityFingerprint string
	Capabilities        []string
	ValidMinutes        int
	Force               bool
}

type enrollmentIssueCertificateOptions struct {
	GatewayURL          string
	OutPath             string
	RootPublicKey       string
	TicketCode          string
	Name                string
	OS                  string
	Arch                string
	IdentityKeyID       string
	IdentityPublicKey   string
	IdentityFingerprint string
	Capabilities        []string
	OperatorTokenFile   string
	ValidMinutes        int
	Force               bool
}

type enrollmentRevokeCertificateOptions struct {
	OutPath         string
	CurrentPath     string
	KeyPath         string
	KeyID           string
	CertificatePath string
	Reason          string
	ValidHours      int
	Force           bool
}

type enrollmentRenewCertificateOptions struct {
	OutPath           string
	KeyPath           string
	KeyID             string
	GatewayURL        string
	RootPublicKey     string
	OperatorTokenFile string
	CertificatePath   string
	RevocationsPath   string
	ValidMinutes      int
	Force             bool
}

type enrollmentInitRevocationsOptions struct {
	OutPath    string
	KeyPath    string
	KeyID      string
	ValidHours int
	Force      bool
}

func (a App) trust(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing trust subcommand")
	}
	switch args[0] {
	case "init":
		fs := flag.NewFlagSet("trust init", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		out := fs.String("out", "", "output signed trust bundle path")
		bundleID := fs.String("bundle-id", "managed-hosts", "trust bundle id")
		rootKey := fs.String("root-key", "", "Ed25519 root signing key file")
		rootKeyID := fs.String("root-key-id", "trust-root", "root signing key id")
		gatewayKey := fs.String("gateway-key", "", "Ed25519 gateway job-signing key file")
		gatewayKeyID := fs.String("gateway-key-id", "gateway-prod", "gateway job-signing key id")
		validHours := fs.Int("valid-hours", 8760, "bundle validity window in hours")
		force := fs.Bool("force", false, "overwrite output bundle")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.trustInit(trustInitOptions{
			OutPath:      *out,
			BundleID:     *bundleID,
			RootKeyPath:  *rootKey,
			RootKeyID:    *rootKeyID,
			GatewayPath:  *gatewayKey,
			GatewayKeyID: *gatewayKeyID,
			ValidHours:   *validHours,
			Force:        *force,
		})
	case "rotate":
		fs := flag.NewFlagSet("trust rotate", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		current := fs.String("current", "", "current signed trust bundle path")
		out := fs.String("out", "", "output updated signed trust bundle path")
		rootKey := fs.String("root-key", "", "Ed25519 root signing key file matching the current signing key id")
		gatewayKey := fs.String("gateway-key", "", "new Ed25519 gateway job-signing key file")
		gatewayKeyID := fs.String("gateway-key-id", "", "new gateway job-signing key id")
		retireKey := fs.String("retire-key", "", "comma-separated existing key ids to mark retired")
		validHours := fs.Int("valid-hours", 8760, "updated bundle validity window in hours")
		force := fs.Bool("force", false, "overwrite output bundle")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.trustRotate(trustRotateOptions{
			CurrentPath:  *current,
			OutPath:      *out,
			RootKeyPath:  *rootKey,
			GatewayPath:  *gatewayKey,
			GatewayKeyID: *gatewayKeyID,
			RetireKeyIDs: splitCapabilities(*retireKey),
			ValidHours:   *validHours,
			Force:        *force,
		})
	case "revoke":
		fs := flag.NewFlagSet("trust revoke", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		current := fs.String("current", "", "current signed trust bundle path")
		out := fs.String("out", "", "output updated signed trust bundle path")
		rootKey := fs.String("root-key", "", "Ed25519 root signing key file matching the current signing key id")
		keyID := fs.String("key-id", "", "key id to revoke")
		reason := fs.String("reason", "", "revocation reason")
		validHours := fs.Int("valid-hours", 8760, "updated bundle validity window in hours")
		force := fs.Bool("force", false, "overwrite output bundle")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.trustRevoke(trustRevokeOptions{
			CurrentPath: *current,
			OutPath:     *out,
			RootKeyPath: *rootKey,
			KeyID:       *keyID,
			Reason:      *reason,
			ValidHours:  *validHours,
			Force:       *force,
		})
	case "verify":
		fs := flag.NewFlagSet("trust verify", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		bundle := fs.String("bundle", "", "signed trust bundle path")
		rootPublicKey := fs.String("root-public-key", "", "pinned trust root, formatted key_id:base64url_public_key")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.trustVerify(*bundle, *rootPublicKey)
	default:
		return fmt.Errorf("unknown trust subcommand %q", args[0])
	}
}

type trustInitOptions struct {
	OutPath      string
	BundleID     string
	RootKeyPath  string
	RootKeyID    string
	GatewayPath  string
	GatewayKeyID string
	ValidHours   int
	Force        bool
}

type trustRotateOptions struct {
	CurrentPath  string
	OutPath      string
	RootKeyPath  string
	GatewayPath  string
	GatewayKeyID string
	RetireKeyIDs []string
	ValidHours   int
	Force        bool
}

type trustRevokeOptions struct {
	CurrentPath string
	OutPath     string
	RootKeyPath string
	KeyID       string
	Reason      string
	ValidHours  int
	Force       bool
}

type gatewayServeOptions struct {
	Addr                     string
	AuditLog                 string
	StatePath                string
	StorageProvider          string
	StoragePath              string
	SigningKeyPath           string
	SigningKeyID             string
	ManifestSigningKeyPath   string
	ManifestSigningKeyID     string
	EnrollmentRootPublicKey  string
	EnrollmentKeyPath        string
	EnrollmentKeyID          string
	EnrollmentRevocations    string
	TLSCertPath              string
	TLSKeyPath               string
	ClientCAPath             string
	OperatorAuthPath         string
	HostedOperatorAuthPath   string
	OIDCJWKSOperatorAuthPath string
	SAMLOperatorAuthPath     string
	RdevAssetsDir            string
	AutoBuildRdevAssets      bool
	RdevWindowsAMD64Path     string
	RdevDarwinARM64Path      string
	RdevDarwinAMD64Path      string
	RdevLinuxAMD64Path       string
	RdevLinuxARM64Path       string
}

func (a App) operatorAuth(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing operator-auth subcommand")
	}
	switch args[0] {
	case "init":
		fs := flag.NewFlagSet("operator-auth init", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		out := fs.String("out", "", "operator auth JSON path")
		tokenDir := fs.String("token-dir", "", "directory for generated bearer token files")
		force := fs.Bool("force", false, "overwrite existing auth and token files")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.operatorAuthInit(*out, *tokenDir, *force)
	case "verify":
		fs := flag.NewFlagSet("operator-auth verify", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		authFile := fs.String("auth", "", "operator auth JSON path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.operatorAuthVerify(*authFile)
	case "verify-hosted":
		fs := flag.NewFlagSet("operator-auth verify-hosted", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		authFile := fs.String("auth", "", "hosted operator auth JSON path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.operatorAuthVerifyHosted(*authFile)
	case "verify-oidc-jwks":
		fs := flag.NewFlagSet("operator-auth verify-oidc-jwks", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		authFile := fs.String("auth", "", "OIDC JWKS operator auth JSON path")
		tokenFile := fs.String("token-file", "", "optional file containing a compact JWT to verify")
		role := fs.String("role", operatorauth.RoleOperator, "role required when --token-file is supplied")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.operatorAuthVerifyOIDCJWKS(*authFile, *tokenFile, *role)
	case "verify-saml":
		fs := flag.NewFlagSet("operator-auth verify-saml", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		authFile := fs.String("auth", "", "SAML operator auth JSON path")
		responseFile := fs.String("response-file", "", "optional file containing a base64 SAMLResponse to verify")
		role := fs.String("role", operatorauth.RoleOperator, "role required when --response-file is supplied")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.operatorAuthVerifySAML(*authFile, *responseFile, *role)
	default:
		return fmt.Errorf("unknown operator-auth subcommand %q", args[0])
	}
}

func (a App) operatorAuthInit(outPath, tokenDir string, force bool) error {
	if strings.TrimSpace(outPath) == "" {
		return fmt.Errorf("--out is required")
	}
	if strings.TrimSpace(tokenDir) == "" {
		return fmt.Errorf("--token-dir is required")
	}
	result, err := operatorauth.InitDefault(time.Now())
	if err != nil {
		return err
	}
	if err := operatorauth.WriteFile(outPath, result.File, force); err != nil {
		return err
	}
	if err := operatorauth.WriteTokenFiles(tokenDir, result.Tokens, force); err != nil {
		return err
	}
	payload := map[string]any{
		"schema_version":        operatorauth.SchemaVersion,
		"auth_file":             outPath,
		"token_dir":             tokenDir,
		"principal_count":       len(result.File.Principals),
		"roles":                 []string{operatorauth.RoleAdmin, operatorauth.RoleOperator, operatorauth.RoleIssuer, operatorauth.RoleAuditor},
		"tokens_written":        true,
		"tokens_redacted":       true,
		"auth_file_sensitive":   false,
		"token_files_sensitive": true,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) operatorAuthVerify(authFile string) error {
	if strings.TrimSpace(authFile) == "" {
		return fmt.Errorf("--auth is required")
	}
	_, file, err := operatorauth.Load(authFile)
	if err != nil {
		return err
	}
	roleCounts := map[string]int{}
	for _, principal := range file.Principals {
		for _, role := range principal.Roles {
			roleCounts[role]++
		}
	}
	payload := map[string]any{
		"schema_version":  operatorauth.SchemaVersion,
		"auth_file":       authFile,
		"ok":              true,
		"principal_count": len(file.Principals),
		"role_counts":     roleCounts,
		"hash_alg":        file.HashAlg,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) operatorAuthVerifyHosted(authFile string) error {
	if strings.TrimSpace(authFile) == "" {
		return fmt.Errorf("--auth is required")
	}
	_, file, err := operatorauth.LoadHosted(authFile)
	if err != nil {
		return err
	}
	rolesClaim := strings.TrimSpace(file.RolesClaim)
	if rolesClaim == "" {
		rolesClaim = "roles"
	}
	payload := map[string]any{
		"schema_version": file.SchemaVersion,
		"auth_file":      authFile,
		"ok":             true,
		"issuer":         file.Issuer,
		"audience":       file.Audience,
		"roles_claim":    rolesClaim,
		"key_count":      len(file.Keys),
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) operatorAuthVerifyOIDCJWKS(authFile, tokenFile, role string) error {
	if strings.TrimSpace(authFile) == "" {
		return fmt.Errorf("--auth is required")
	}
	verifier, file, err := operatorauth.LoadOIDCJWKS(authFile)
	if err != nil {
		return err
	}
	rolesClaim := strings.TrimSpace(file.RolesClaim)
	if rolesClaim == "" {
		rolesClaim = "roles"
	}
	payload := map[string]any{
		"schema_version":      file.SchemaVersion,
		"auth_file":           authFile,
		"ok":                  true,
		"issuer":              file.Issuer,
		"audience":            file.Audience,
		"jwks_url_configured": strings.TrimSpace(file.JWKSURL) != "",
		"roles_claim":         rolesClaim,
		"key_count":           verifier.KeyCount(),
		"token_verified":      false,
	}
	if strings.TrimSpace(tokenFile) != "" {
		content, err := os.ReadFile(tokenFile)
		if err != nil {
			return err
		}
		token := strings.TrimSpace(string(content))
		if token == "" {
			return fmt.Errorf("token file is empty")
		}
		requiredRole := strings.TrimSpace(role)
		if requiredRole == "" {
			requiredRole = operatorauth.RoleOperator
		}
		if !verifier.AuthorizeBearer("Bearer "+token, requiredRole) {
			return fmt.Errorf("OIDC JWKS token verification failed for role %q", requiredRole)
		}
		claims, err := verifier.VerifyToken(token)
		if err != nil {
			return err
		}
		payload["token_verified"] = true
		payload["subject"] = claims.Subject
		payload["roles"] = claims.Roles
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) operatorAuthVerifySAML(authFile, responseFile, role string) error {
	if strings.TrimSpace(authFile) == "" {
		return fmt.Errorf("--auth is required")
	}
	verifier, file, err := operatorauth.LoadSAML(authFile)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"schema_version":               file.SchemaVersion,
		"auth_file":                    authFile,
		"ok":                           true,
		"idp_issuer":                   file.IDPIssuer,
		"audience":                     file.Audience,
		"assertion_consumer_url":       file.AssertionConsumerURL,
		"role_attribute":               file.RoleAttribute,
		"subject_attribute":            file.SubjectAttribute,
		"certificate_configured":       verifier.CertificateCount() > 0,
		"certificate_count":            verifier.CertificateCount(),
		"response_verified":            false,
		"response_signature_validated": false,
	}
	if strings.TrimSpace(responseFile) != "" {
		content, err := os.ReadFile(responseFile)
		if err != nil {
			return err
		}
		response := strings.TrimSpace(string(content))
		if response == "" {
			return fmt.Errorf("response file is empty")
		}
		requiredRole := strings.TrimSpace(role)
		if requiredRole == "" {
			requiredRole = operatorauth.RoleOperator
		}
		if !verifier.AuthorizeBearer("Bearer "+response, requiredRole) {
			return fmt.Errorf("SAML response verification failed for role %q", requiredRole)
		}
		claims, err := verifier.VerifyResponse(response)
		if err != nil {
			return err
		}
		payload["response_verified"] = true
		payload["response_signature_validated"] = claims.ResponseSignatureValidated
		payload["subject"] = claims.Subject
		payload["roles"] = claims.Roles
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) hostedProvider(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing hosted-provider subcommand")
	}
	switch args[0] {
	case "package":
		fs := flag.NewFlagSet("hosted-provider package", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		out := fs.String("out", "", "output directory for hosted provider package")
		name := fs.String("name", "rdev-hosted-provider", "hosted provider package name")
		storageProvider := fs.String("storage-provider", "file", "storage provider kind: file, postgres, s3-compatible, or redis-stream")
		authProvider := fs.String("auth-provider", "hosted-ed25519-jwt", "auth provider kind: hosted-ed25519-jwt, oidc-jwks, or saml-assertion")
		force := fs.Bool("force", false, "replace an existing output directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.hostedProviderPackage(*out, *name, *storageProvider, *authProvider, *force)
	case "verify":
		fs := flag.NewFlagSet("hosted-provider verify", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		packagePath := fs.String("package", "", "hosted provider package directory or hosted-provider.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.hostedProviderVerify(*packagePath)
	default:
		return fmt.Errorf("unknown hosted-provider subcommand %q", args[0])
	}
}

func (a App) hostedProviderPackage(out, name, storageProvider, authProvider string, force bool) error {
	pkg, err := hostedprovider.Build(hostedprovider.Options{
		OutDir:          out,
		Name:            name,
		StorageProvider: storageProvider,
		AuthProvider:    authProvider,
		GeneratedAt:     time.Now(),
		Force:           force,
	})
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                           pkg.OK(),
		"schema":                       pkg.SchemaVersion,
		"out":                          out,
		"package":                      filepath.Join(out, "hosted-provider.json"),
		"name":                         pkg.Name,
		"storage_provider":             pkg.Storage.Kind,
		"auth_provider":                pkg.Auth.Kind,
		"runtime_contract_schema":      pkg.Runtime.SchemaVersion,
		"runtime_evidence_plan_schema": hostedprovider.RuntimeEvidencePlanSchemaVersion,
		"evidence_plan":                filepath.Join(out, pkg.EvidencePlanPath),
		"runtime_status":               pkg.Runtime.RuntimeStatus,
		"file_count":                   len(pkg.Files),
		"external_mutation":            pkg.ExternalMutation,
		"checks":                       pkg.Checks,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !pkg.OK() {
		return fmt.Errorf("hosted provider package failed")
	}
	return nil
}

func (a App) hostedProviderVerify(packagePath string) error {
	verification, err := hostedprovider.Verify(packagePath)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                           verification.OK(),
		"schema":                       verification.SchemaVersion,
		"package":                      verification.PackagePath,
		"package_dir":                  verification.PackageDir,
		"name":                         verification.Name,
		"storage_provider":             verification.StorageProvider,
		"auth_provider":                verification.AuthProvider,
		"runtime_contract_schema":      hostedprovider.RuntimeContractSchemaVersion,
		"runtime_evidence_plan_schema": hostedprovider.RuntimeEvidencePlanSchemaVersion,
		"checks":                       verification.Checks,
		"files":                        verification.Files,
		"recommended_actions":          verification.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !verification.OK() {
		return fmt.Errorf("hosted provider package verification failed")
	}
	return nil
}

func (a App) relayAdapter(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing relay-adapter subcommand")
	}
	switch args[0] {
	case "package":
		fs := flag.NewFlagSet("relay-adapter package", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		out := fs.String("out", "", "output directory for relay adapter package")
		name := fs.String("name", "", "relay adapter package name")
		adapterKind := fs.String("adapter", "chisel", "connectivity adapter kind: chisel, frpc, ssh-tunnel, headscale-tailscale, or wireguard")
		force := fs.Bool("force", false, "replace an existing output directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.relayAdapterPackage(*out, *name, *adapterKind, *force)
	case "verify":
		fs := flag.NewFlagSet("relay-adapter verify", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		packagePath := fs.String("package", "", "relay adapter package directory or relay-adapter.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.relayAdapterVerify(*packagePath)
	default:
		return fmt.Errorf("unknown relay-adapter subcommand %q", args[0])
	}
}

func (a App) relayAdapterPackage(out, name, adapterKind string, force bool) error {
	pkg, err := relayadapter.Build(relayadapter.Options{
		OutDir:      out,
		Name:        name,
		AdapterKind: adapterKind,
		GeneratedAt: time.Now(),
		Force:       force,
	})
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                              pkg.OK(),
		"schema":                          pkg.SchemaVersion,
		"out":                             out,
		"package":                         filepath.Join(out, "relay-adapter.json"),
		"name":                            pkg.Name,
		"adapter_kind":                    pkg.AdapterKind,
		"helper_tool":                     pkg.Helper.Tool,
		"acceptance_evidence_plan_schema": relayadapter.AcceptanceEvidencePlanSchemaVersion,
		"evidence_plan":                   filepath.Join(out, pkg.EvidencePlanPath),
		"external_mutation":               pkg.ExternalMutation,
		"runner_env":                      pkg.RunnerEnv,
		"file_count":                      len(pkg.Files),
		"approval_required":               pkg.ApprovalRequired,
		"evidence_required":               pkg.EvidenceRequired,
		"checks":                          pkg.Checks,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !pkg.OK() {
		return fmt.Errorf("relay adapter package failed")
	}
	return nil
}

func (a App) relayAdapterVerify(packagePath string) error {
	verification, err := relayadapter.Verify(packagePath)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                              verification.OK(),
		"schema":                          verification.SchemaVersion,
		"package":                         verification.PackagePath,
		"package_dir":                     verification.PackageDir,
		"name":                            verification.Name,
		"adapter_kind":                    verification.AdapterKind,
		"acceptance_evidence_plan_schema": relayadapter.AcceptanceEvidencePlanSchemaVersion,
		"checks":                          verification.Checks,
		"files":                           verification.Files,
		"recommended_actions":             verification.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !verification.OK() {
		return fmt.Errorf("relay adapter package verification failed")
	}
	return nil
}

func (a App) acceptance(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing acceptance subcommand")
	}
	switch args[0] {
	case "fresh-agent-support-session":
		fs := flag.NewFlagSet("acceptance fresh-agent-support-session", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		out := fs.String("out", "", "empty output directory for the fresh-Agent support-session contract report")
		gatewayURL := fs.String("gateway-url", "", "gateway URL to use in the simulated reachable-gateway path; defaults to http://127.0.0.1:8787")
		rdevCommand := fs.String("rdev-command", "rdev", "rdev command name or absolute path to place in generated Agent commands")
		locale := fs.String("locale", "en", "localized user-facing language for handoff/status fields")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceFreshAgentSupportSession(acceptance.FreshAgentSupportSessionOptions{
			OutDir:      *out,
			GatewayURL:  *gatewayURL,
			RdevCommand: *rdevCommand,
			Locale:      *locale,
		})
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
		releaseBundle := fs.String("release-bundle", "", "signed release bundle path on the Mac host, verified by the managed host before registration")
		releaseRootPublicKey := fs.String("release-root-public-key", "", "required release root public key for --release-bundle, formatted key_id:base64url_public_key")
		releaseRequiredArtifacts := fs.String("release-require-artifacts", "rdev,rdev-host,rdev-verify", "comma-separated artifact ids required in --release-bundle")
		force := fs.Bool("force", false, "overwrite an existing service output path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceManagedMacService(ctx, acceptance.ManagedMacServiceOptions{
			RepoRoot:                 *repo,
			OutDir:                   *out,
			BinaryPath:               *binaryPath,
			GatewayURL:               *gatewayURL,
			TicketCode:               *ticketCode,
			ManifestURL:              *manifestURL,
			Label:                    *label,
			PlistOut:                 *plistOut,
			IdentityStore:            *identityStore,
			TrustStore:               *trustStore,
			NonceStore:               *nonceStore,
			ApprovalStore:            *approvalStore,
			WorkspaceLockStore:       *workspaceLockStore,
			LogDir:                   *logDir,
			ReleaseBundle:            *releaseBundle,
			ReleaseRootPublicKey:     *releaseRootPublicKey,
			ReleaseRequiredArtifacts: splitCapabilities(*releaseRequiredArtifacts),
			Force:                    *force,
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
	case "windows-managed-service":
		fs := flag.NewFlagSet("acceptance windows-managed-service", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		out := fs.String("out", "", "empty output directory for the Windows managed service acceptance plan")
		binaryPath := fs.String("binary", "", "absolute Windows path to rdev.exe on the target host")
		gatewayURL := fs.String("gateway", "", "gateway URL for managed ticket enrollment")
		ticketCode := fs.String("ticket-code", "", "managed enrollment ticket code")
		manifestURL := fs.String("manifest-url", "", "signed managed enrollment manifest URL")
		label := fs.String("label", service.DefaultWindowsServiceName, "Windows Service name")
		displayName := fs.String("display-name", "Remote Dev Skillkit Host", "Windows Service display name")
		description := fs.String("description", "Remote Dev Skillkit managed host", "Windows Service description")
		identityStore := fs.String("identity-store", "", "managed host identity store path")
		trustStore := fs.String("trust-store", "", "managed host trust bundle store path")
		nonceStore := fs.String("nonce-store", "", "managed host nonce store path")
		approvalStore := fs.String("approval-store", "", "managed host approval store path")
		workspaceLockStore := fs.String("workspace-lock-store", "", "managed host workspace lock store directory")
		releaseBundle := fs.String("release-bundle", "", "signed release bundle path on the Windows host, verified by the managed host before registration")
		releaseRootPublicKey := fs.String("release-root-public-key", "", "required release root public key for --release-bundle, formatted key_id:base64url_public_key")
		releaseRequiredArtifacts := fs.String("release-require-artifacts", "rdev.exe,rdev-host.exe,rdev-verify.exe", "comma-separated artifact ids required in --release-bundle")
		force := fs.Bool("force", false, "overwrite generated plan if it already exists")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceWindowsManagedService(acceptance.WindowsManagedServiceOptions{
			OutDir:                   *out,
			BinaryPath:               *binaryPath,
			GatewayURL:               *gatewayURL,
			TicketCode:               *ticketCode,
			ManifestURL:              *manifestURL,
			ServiceName:              *label,
			DisplayName:              *displayName,
			Description:              *description,
			IdentityStore:            *identityStore,
			TrustStore:               *trustStore,
			NonceStore:               *nonceStore,
			ApprovalStore:            *approvalStore,
			WorkspaceLockStore:       *workspaceLockStore,
			ReleaseBundle:            *releaseBundle,
			ReleaseRootPublicKey:     *releaseRootPublicKey,
			ReleaseRequiredArtifacts: splitCapabilities(*releaseRequiredArtifacts),
			Force:                    *force,
		})
	case "linux-managed-service":
		fs := flag.NewFlagSet("acceptance linux-managed-service", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		out := fs.String("out", "", "empty output directory for the Linux managed service acceptance plan")
		binaryPath := fs.String("binary", "", "absolute Linux path to rdev on the target host")
		gatewayURL := fs.String("gateway", "", "gateway URL for managed ticket enrollment")
		ticketCode := fs.String("ticket-code", "", "managed enrollment ticket code")
		manifestURL := fs.String("manifest-url", "", "signed managed enrollment manifest URL")
		label := fs.String("label", service.DefaultLinuxSystemdUnitName, "systemd user unit name")
		unitOut := fs.String("unit-out", "", "systemd user unit output path; defaults to <out>/<label>")
		identityStore := fs.String("identity-store", "", "managed host identity store path")
		trustStore := fs.String("trust-store", "", "managed host trust bundle store path")
		nonceStore := fs.String("nonce-store", "", "managed host nonce store path")
		approvalStore := fs.String("approval-store", "", "managed host approval store path")
		workspaceLockStore := fs.String("workspace-lock-store", "", "managed host workspace lock store directory")
		logDir := fs.String("log-dir", "", "managed host log directory")
		releaseBundle := fs.String("release-bundle", "", "signed release bundle path on the Linux host, verified by the managed host before registration")
		releaseRootPublicKey := fs.String("release-root-public-key", "", "required release root public key for --release-bundle, formatted key_id:base64url_public_key")
		releaseRequiredArtifacts := fs.String("release-require-artifacts", "rdev,rdev-host,rdev-verify", "comma-separated artifact ids required in --release-bundle")
		force := fs.Bool("force", false, "overwrite generated unit if it already exists")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceLinuxManagedService(acceptance.LinuxManagedServiceOptions{
			OutDir:                   *out,
			BinaryPath:               *binaryPath,
			GatewayURL:               *gatewayURL,
			TicketCode:               *ticketCode,
			ManifestURL:              *manifestURL,
			UnitName:                 *label,
			UnitOut:                  *unitOut,
			IdentityStore:            *identityStore,
			TrustStore:               *trustStore,
			NonceStore:               *nonceStore,
			ApprovalStore:            *approvalStore,
			WorkspaceLockStore:       *workspaceLockStore,
			LogDir:                   *logDir,
			ReleaseBundle:            *releaseBundle,
			ReleaseRootPublicKey:     *releaseRootPublicKey,
			ReleaseRequiredArtifacts: splitCapabilities(*releaseRequiredArtifacts),
			Force:                    *force,
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
	case "verify-managed-mac-service":
		fs := flag.NewFlagSet("acceptance verify-managed-mac-service", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		plan := fs.String("plan", "", "Managed Mac service acceptance plan path, for example <out>/service-plan.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceVerifyManagedMacService(*plan)
	case "verify-windows-managed-service":
		fs := flag.NewFlagSet("acceptance verify-windows-managed-service", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		plan := fs.String("plan", "", "Windows managed service acceptance plan path, for example <out>/windows-managed-service-plan.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceVerifyWindowsManagedService(*plan)
	case "verify-linux-managed-service":
		fs := flag.NewFlagSet("acceptance verify-linux-managed-service", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		plan := fs.String("plan", "", "Linux managed service acceptance plan path, for example <out>/linux-managed-service-plan.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceVerifyLinuxManagedService(*plan)
	case "verify-relay-adapter-package":
		fs := flag.NewFlagSet("acceptance verify-relay-adapter-package", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		packagePath := fs.String("package", "", "relay adapter acceptance package directory or package.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceVerifyRelayAdapterPackage(*packagePath)
	case "verify-hosted-provider-runtime-package":
		fs := flag.NewFlagSet("acceptance verify-hosted-provider-runtime-package", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		packagePath := fs.String("package", "", "hosted provider runtime acceptance package directory or package.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceVerifyHostedProviderRuntimePackage(*packagePath)
	case "verify-post-release-download-package":
		fs := flag.NewFlagSet("acceptance verify-post-release-download-package", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		packagePath := fs.String("package", "", "post-release download acceptance package directory or package.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceVerifyPostReleaseDownloadPackage(*packagePath)
	case "scaffold-evidence":
		fs := flag.NewFlagSet("acceptance scaffold-evidence", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		plan := fs.String("plan", "", "runtime-evidence-plan.json or acceptance-evidence-plan.json")
		hostedPackage := fs.String("hosted-provider-package", "", "hosted provider package directory or hosted-provider.json; resolves runtime-evidence-plan.json")
		relayPackage := fs.String("relay-adapter-package", "", "relay adapter package directory or relay-adapter.json; resolves acceptance-evidence-plan.json")
		out := fs.String("out", "", "empty output directory for the evidence collection scaffold")
		packageDir := fs.String("package-dir", "", "optional package directory; defaults to the selected package directory or plan parent")
		createPlaceholders := fs.Bool("create-placeholders", false, "write obvious placeholder evidence files; replace them before packaging")
		force := fs.Bool("force", false, "replace an existing output directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceScaffoldEvidence(evidenceplan.Options{
			PlanPath:                  *plan,
			HostedProviderPackagePath: *hostedPackage,
			RelayAdapterPackagePath:   *relayPackage,
			OutDir:                    *out,
			PackageDir:                *packageDir,
			CreatePlaceholders:        *createPlaceholders,
			Force:                     *force,
			GeneratedAt:               time.Now(),
		})
	case "evidence-status":
		fs := flag.NewFlagSet("acceptance evidence-status", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		scaffold := fs.String("scaffold", "", "scaffold directory or scaffold-report.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceEvidenceStatus(evidenceplan.StatusOptions{
			ScaffoldPath: *scaffold,
			GeneratedAt:  time.Now(),
		})
	case "scaffold-post-release-download":
		fs := flag.NewFlagSet("acceptance scaffold-post-release-download", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		postReleaseInstallDir := fs.String("post-release-install-dir", "", "directory containing post-release-install-plan.json and post-release-install-verification.json")
		plan := fs.String("plan", "", "post-release-install-plan.json from scripts/github/plan-post-release-install.sh")
		planVerification := fs.String("plan-verification", "", "rdev.post-release-install-verification.v1 output from verify-post-release-install-plan.sh")
		out := fs.String("out", "", "empty output directory for post-release download evidence scaffold")
		createPlaceholders := fs.Bool("create-placeholders", false, "write obvious placeholder evidence files; replace them before packaging")
		force := fs.Bool("force", false, "replace an existing output directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceScaffoldPostReleaseDownload(acceptance.PostReleaseDownloadScaffoldOptions{
			PostReleaseInstallDir: *postReleaseInstallDir,
			PlanPath:              *plan,
			PlanVerificationPath:  *planVerification,
			OutDir:                *out,
			CreatePlaceholders:    *createPlaceholders,
			Force:                 *force,
			Now:                   time.Now(),
		})
	case "post-release-evidence-status":
		fs := flag.NewFlagSet("acceptance post-release-evidence-status", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		scaffold := fs.String("scaffold", "", "post-release scaffold directory or scaffold-report.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptancePostReleaseEvidenceStatus(acceptance.PostReleaseDownloadStatusOptions{
			ScaffoldPath: *scaffold,
			Now:          time.Now(),
		})
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
	case "package-managed-mac-service":
		fs := flag.NewFlagSet("acceptance package-managed-mac-service", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		plan := fs.String("plan", "", "Managed Mac service acceptance plan path")
		out := fs.String("out", "", "empty output directory for the packaged managed Mac service evidence")
		reviewTranscript := fs.String("review-transcript", "", "plutil/cat review transcript for the LaunchAgent plist")
		startTranscript := fs.String("start-transcript", "", "rdev host service-control --platform macos --action start --execute transcript")
		inspectTranscript := fs.String("inspect-transcript", "", "rdev host service-control --platform macos --action inspect --execute transcript")
		logs := fs.String("logs", "", "managed host stdout/stderr log excerpt")
		releaseGate := fs.String("release-gate", "", "rdev host startup release-gate JSON/output")
		auditPath := fs.String("audit", "", "audit export or transcript for host registration, approvals, jobs, revocation, and completion")
		reconnect := fs.String("reconnect", "", "logout/login or reboot reconnect evidence transcript")
		managedReport := fs.String("managed-report", "", "service-backed rdev acceptance managed-mac report.json")
		stopTranscript := fs.String("stop-transcript", "", "rdev host service-control --platform macos --action stop --execute transcript")
		uninstallTranscript := fs.String("uninstall-transcript", "", "rdev host uninstall-service --platform macos transcript")
		notes := fs.String("notes", "", "optional operator notes file")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptancePackageManagedMacService(acceptance.ManagedMacServicePackageOptions{
			PlanPath:                *plan,
			OutDir:                  *out,
			ReviewTranscriptPath:    *reviewTranscript,
			StartTranscriptPath:     *startTranscript,
			InspectTranscriptPath:   *inspectTranscript,
			LogsPath:                *logs,
			ReleaseGatePath:         *releaseGate,
			AuditPath:               *auditPath,
			ReconnectPath:           *reconnect,
			ManagedReportPath:       *managedReport,
			StopTranscriptPath:      *stopTranscript,
			UninstallTranscriptPath: *uninstallTranscript,
			NotesPath:               *notes,
		})
	case "package-linux-managed-service":
		fs := flag.NewFlagSet("acceptance package-linux-managed-service", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		plan := fs.String("plan", "", "Linux managed service acceptance plan path")
		out := fs.String("out", "", "empty output directory for the packaged Linux managed-service evidence")
		startTranscript := fs.String("start-transcript", "", "systemctl --user daemon-reload and enable --now transcript")
		statusTranscript := fs.String("status-transcript", "", "systemctl --user status transcript")
		logs := fs.String("logs", "", "journalctl --user -u transcript or service log excerpt")
		releaseGate := fs.String("release-gate", "", "rdev host startup release-gate JSON/output")
		auditPath := fs.String("audit", "", "audit export or transcript for host registration, approvals, jobs, revocation, and completion")
		reconnect := fs.String("reconnect", "", "logout/reboot reconnect evidence transcript")
		jobEvidenceDir := fs.String("job-evidence-dir", "", "directory containing managed coding/repair evidence bundle")
		stopTranscript := fs.String("stop-transcript", "", "systemctl --user disable --now transcript")
		uninstallTranscript := fs.String("uninstall-transcript", "", "rdev host uninstall-service transcript")
		notes := fs.String("notes", "", "optional operator notes file")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptancePackageLinuxManagedService(acceptance.LinuxManagedServicePackageOptions{
			PlanPath:                *plan,
			OutDir:                  *out,
			StartTranscriptPath:     *startTranscript,
			StatusTranscriptPath:    *statusTranscript,
			LogsPath:                *logs,
			ReleaseGatePath:         *releaseGate,
			AuditPath:               *auditPath,
			ReconnectPath:           *reconnect,
			JobEvidenceDir:          *jobEvidenceDir,
			StopTranscriptPath:      *stopTranscript,
			UninstallTranscriptPath: *uninstallTranscript,
			NotesPath:               *notes,
		})
	case "package-relay-adapter":
		fs := flag.NewFlagSet("acceptance package-relay-adapter", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		relayPackage := fs.String("relay-package", "", "verified relay adapter package directory or relay-adapter.json")
		out := fs.String("out", "", "empty output directory for packaged relay adapter evidence")
		evidenceDir := fs.String("evidence-dir", "", "directory written by rdev connection-entry run --evidence-dir")
		runnerResult := fs.String("runner-result", "", "Connection Entry runner result JSON selecting a standard connectivity adapter path")
		helperTranscript := fs.String("helper-transcript", "", "helper start transcript or supervisor evidence")
		gatewayStatus := fs.String("gateway-status", "", "gateway health/status JSON or transcript")
		hostStatus := fs.String("host-status", "", "host registration/status JSON or transcript")
		connectionStatus := fs.String("connection-status", "", "support-session status JSON or connection supervision output")
		auditPath := fs.String("audit", "", "audit export or transcript covering helper start, registration, and cleanup")
		notes := fs.String("notes", "", "optional operator notes file")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptancePackageRelayAdapter(acceptance.RelayAdapterPackageOptions{
			RelayAdapterPackagePath: *relayPackage,
			OutDir:                  *out,
			EvidenceDirPath:         *evidenceDir,
			RunnerResultPath:        *runnerResult,
			HelperTranscriptPath:    *helperTranscript,
			GatewayStatusPath:       *gatewayStatus,
			HostStatusPath:          *hostStatus,
			ConnectionStatusPath:    *connectionStatus,
			AuditPath:               *auditPath,
			NotesPath:               *notes,
		})
	case "package-hosted-provider-runtime":
		fs := flag.NewFlagSet("acceptance package-hosted-provider-runtime", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		hostedPackage := fs.String("hosted-provider-package", "", "verified hosted provider package directory or hosted-provider.json")
		out := fs.String("out", "", "empty output directory for packaged hosted provider runtime evidence")
		evidenceDir := fs.String("evidence-dir", "", "directory containing standard hosted provider runtime evidence files")
		gatewayStartup := fs.String("gateway-startup", "", "gateway startup or deployment transcript")
		storageVerification := fs.String("storage-verification", "", "storage provider verification output")
		authVerification := fs.String("auth-verification", "", "hosted auth verification output")
		backupEvidence := fs.String("backup-evidence", "", "backup evidence or single-node smoke backup note")
		restoreEvidence := fs.String("restore-evidence", "", "restore evidence or single-node smoke restore note")
		retentionEvidence := fs.String("retention-evidence", "", "retention policy evidence")
		roleMappingEvidence := fs.String("role-mapping-evidence", "", "role mapping and authorization probe evidence")
		failureModeEvidence := fs.String("failure-mode-evidence", "", "failure-mode evidence for storage/auth outages or rejected credentials")
		auditPath := fs.String("audit", "", "audit export or transcript covering startup, authz probes, storage checks, and cleanup")
		notes := fs.String("notes", "", "optional operator notes file")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptancePackageHostedProviderRuntime(acceptance.HostedProviderRuntimePackageOptions{
			HostedProviderPackagePath: *hostedPackage,
			OutDir:                    *out,
			EvidenceDirPath:           *evidenceDir,
			GatewayStartupPath:        *gatewayStartup,
			StorageVerificationPath:   *storageVerification,
			AuthVerificationPath:      *authVerification,
			BackupEvidencePath:        *backupEvidence,
			RestoreEvidencePath:       *restoreEvidence,
			RetentionEvidencePath:     *retentionEvidence,
			RoleMappingEvidencePath:   *roleMappingEvidence,
			FailureModeEvidencePath:   *failureModeEvidence,
			AuditPath:                 *auditPath,
			NotesPath:                 *notes,
		})
	case "package-post-release-download":
		fs := flag.NewFlagSet("acceptance package-post-release-download", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		scaffold := fs.String("scaffold", "", "post-release evidence scaffold directory or scaffold-report.json")
		plan := fs.String("plan", "", "post-release-install-plan.json from scripts/github/plan-post-release-install.sh")
		planVerification := fs.String("plan-verification", "", "rdev.post-release-install-verification.v1 output from verify-post-release-install-plan.sh")
		out := fs.String("out", "", "empty output directory for packaged post-release download evidence")
		evidenceDir := fs.String("evidence-dir", "", "directory containing per-platform transcripts and verification JSON named <target-slug>-transcript.txt, <target-slug>-candidate-verify.json, and <target-slug>-bundle-verify.json")
		skillkitEvidenceDir := fs.String("skillkit-evidence-dir", "", "directory containing skillkit-transcript.txt and skillkit-verify.json when the plan includes Skillkit")
		notes := fs.String("notes", "", "optional operator notes file")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptancePackagePostReleaseDownload(acceptance.PostReleaseDownloadPackageOptions{
			ScaffoldPath:         *scaffold,
			PlanPath:             *plan,
			PlanVerificationPath: *planVerification,
			OutDir:               *out,
			EvidenceDir:          *evidenceDir,
			SkillkitEvidenceDir:  *skillkitEvidenceDir,
			NotesPath:            *notes,
		})
	case "release-evidence-index":
		fs := flag.NewFlagSet("acceptance release-evidence-index", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		out := fs.String("out", "", "empty output directory for the release evidence index")
		hostedProviderRuntimePackage := fs.String("hosted-provider-runtime-package", "", "hosted provider runtime acceptance package directory or package.json")
		relayAdapterPackages := fs.String("relay-adapter-package", "", "comma-separated relay/connectivity acceptance package directories or package.json files")
		postReleaseDownloadPackage := fs.String("post-release-download-package", "", "post-release download acceptance package directory or package.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.acceptanceReleaseEvidenceIndex(acceptance.ReleaseEvidenceIndexOptions{
			OutDir:                           *out,
			HostedProviderRuntimePackagePath: *hostedProviderRuntimePackage,
			RelayAdapterPackagePaths:         splitCommaList(*relayAdapterPackages),
			PostReleaseDownloadPackagePath:   *postReleaseDownloadPackage,
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

func (a App) acceptanceFreshAgentSupportSession(opts acceptance.FreshAgentSupportSessionOptions) error {
	report, err := acceptance.RunFreshAgentSupportSession(opts)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                        allAcceptanceChecksPassed(report.Checks),
		"schema":                    report.SchemaVersion,
		"out":                       report.OutDir,
		"report":                    filepath.Join(report.OutDir, "report.json"),
		"gateway_url":               report.GatewayURL,
		"checks":                    report.Checks,
		"recommended_next_steps":    report.RecommendedNextSteps,
		"real_environment_required": report.RealEnvironmentRequired,
		"note":                      "local contract gate only; no remote host, gateway listener, tunnel, service, or external network was started",
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

func (a App) acceptanceWindowsManagedService(opts acceptance.WindowsManagedServiceOptions) error {
	plan, err := acceptance.RunWindowsManagedServicePlan(opts)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                  allAcceptanceChecksPassed(plan.Checks),
		"schema":              plan.SchemaVersion,
		"out":                 plan.OutDir,
		"plan":                filepath.Join(plan.OutDir, "windows-managed-service-plan.json"),
		"service_name":        plan.Service.ServiceName,
		"display_name":        plan.Service.DisplayName,
		"args":                plan.Service.Args,
		"bin_path":            plan.Service.BinPath,
		"start_type":          plan.Service.StartType,
		"checks":              plan.Checks,
		"commands":            plan.Commands,
		"required_evidence":   plan.RequiredEvidence,
		"recommended_actions": plan.RecommendedActions,
		"note":                "plan written only; no PowerShell or sc.exe command was executed by this command",
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) acceptanceLinuxManagedService(opts acceptance.LinuxManagedServiceOptions) error {
	plan, err := acceptance.RunLinuxManagedServicePlan(opts)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                  allAcceptanceChecksPassed(plan.Checks),
		"schema":              plan.SchemaVersion,
		"out":                 plan.OutDir,
		"plan":                filepath.Join(plan.OutDir, "linux-managed-service-plan.json"),
		"unit":                plan.UnitPath,
		"unit_name":           plan.Unit.UnitName,
		"exec_start":          plan.Unit.ExecStart,
		"restart":             plan.Unit.Restart,
		"restart_sec":         plan.Unit.RestartSec,
		"checks":              plan.Checks,
		"commands":            plan.Commands,
		"required_evidence":   plan.RequiredEvidence,
		"recommended_actions": plan.RecommendedActions,
		"note":                "unit and plan written only; no systemctl command was executed by this command",
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

func (a App) acceptanceVerifyManagedMacService(planPath string) error {
	verification, err := acceptance.VerifyManagedMacServicePlan(planPath)
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
		return fmt.Errorf("managed Mac service acceptance plan verification failed")
	}
	return nil
}

func (a App) acceptanceVerifyWindowsManagedService(planPath string) error {
	verification, err := acceptance.VerifyWindowsManagedServicePlan(planPath)
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
		return fmt.Errorf("windows managed service acceptance plan verification failed")
	}
	return nil
}

func (a App) acceptanceVerifyLinuxManagedService(planPath string) error {
	verification, err := acceptance.VerifyLinuxManagedServicePlan(planPath)
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
		return fmt.Errorf("linux managed service acceptance plan verification failed")
	}
	return nil
}

func (a App) acceptanceVerifyRelayAdapterPackage(packagePath string) error {
	verification, err := acceptance.VerifyRelayAdapterAcceptancePackage(packagePath)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                  verification.OK(),
		"schema":              verification.SchemaVersion,
		"package":             verification.PackagePath,
		"package_schema":      verification.PackageSchema,
		"selected_path":       verification.SelectedPath,
		"accepted_paths":      verification.AcceptedPaths,
		"checks":              verification.Checks,
		"files":               verification.Files,
		"recommended_actions": verification.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !verification.OK() {
		return fmt.Errorf("relay adapter acceptance package verification failed")
	}
	return nil
}

func (a App) acceptanceVerifyHostedProviderRuntimePackage(packagePath string) error {
	verification, err := acceptance.VerifyHostedProviderRuntimeAcceptancePackage(packagePath)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                  verification.OK(),
		"schema":              verification.SchemaVersion,
		"package":             verification.PackagePath,
		"package_schema":      verification.PackageSchema,
		"storage_provider":    verification.StorageProvider,
		"auth_provider":       verification.AuthProvider,
		"runtime_claim":       verification.RuntimeClaim,
		"checks":              verification.Checks,
		"files":               verification.Files,
		"recommended_actions": verification.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !verification.OK() {
		return fmt.Errorf("hosted provider runtime acceptance package verification failed")
	}
	return nil
}

func (a App) acceptanceVerifyPostReleaseDownloadPackage(packagePath string) error {
	verification, err := acceptance.VerifyPostReleaseDownloadAcceptancePackage(packagePath)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                  verification.OK(),
		"schema":              verification.SchemaVersion,
		"package":             verification.PackagePath,
		"package_schema":      verification.PackageSchema,
		"repo":                verification.Repo,
		"tag":                 verification.Tag,
		"platform_targets":    verification.PlatformTargets,
		"skillkit_included":   verification.SkillkitIncluded,
		"checks":              verification.Checks,
		"files":               verification.Files,
		"recommended_actions": verification.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !verification.OK() {
		return fmt.Errorf("post-release download acceptance package verification failed")
	}
	return nil
}

func (a App) acceptanceScaffoldEvidence(opts evidenceplan.Options) error {
	scaffold, err := evidenceplan.Build(opts)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                  scaffold.OK,
		"schema":              scaffold.SchemaVersion,
		"plan_schema":         scaffold.PlanSchema,
		"plan_kind":           scaffold.PlanKind,
		"out":                 scaffold.OutDir,
		"package_dir":         scaffold.PackageDir,
		"report":              scaffold.ReportPath,
		"checklist":           scaffold.ChecklistPath,
		"plan_copy":           scaffold.PlanCopyPath,
		"ready_for_packaging": scaffold.ReadyForPackaging,
		"create_placeholders": scaffold.CreatePlaceholders,
		"evidence_files":      scaffold.EvidenceFiles,
		"commands":            scaffold.Commands,
		"checks":              scaffold.Checks,
		"recommended_actions": scaffold.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !scaffold.OK {
		return fmt.Errorf("acceptance evidence scaffold failed")
	}
	return nil
}

func (a App) acceptanceEvidenceStatus(opts evidenceplan.StatusOptions) error {
	status, err := evidenceplan.StatusForScaffold(opts)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                  status.OK,
		"schema":              status.SchemaVersion,
		"ready_for_packaging": status.ReadyForPackaging,
		"scaffold":            status.ScaffoldPath,
		"report":              status.ReportPath,
		"plan_schema":         status.PlanSchema,
		"plan_kind":           status.PlanKind,
		"out":                 status.OutDir,
		"package_dir":         status.PackageDir,
		"required_ready":      status.RequiredReady,
		"required_total":      status.RequiredTotal,
		"placeholder_count":   status.PlaceholderCount,
		"missing_count":       status.MissingCount,
		"empty_count":         status.EmptyCount,
		"evidence_files":      status.EvidenceFiles,
		"commands":            status.Commands,
		"checks":              status.Checks,
		"recommended_actions": status.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !status.ReadyForPackaging {
		return fmt.Errorf("acceptance evidence is not ready for packaging")
	}
	return nil
}

func (a App) acceptanceScaffoldPostReleaseDownload(opts acceptance.PostReleaseDownloadScaffoldOptions) error {
	scaffold, err := acceptance.ScaffoldPostReleaseDownloadEvidence(opts)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                     scaffold.OK,
		"schema":                 scaffold.SchemaVersion,
		"ready_for_packaging":    scaffold.ReadyForPackaging,
		"out":                    scaffold.OutDir,
		"report":                 scaffold.ReportPath,
		"checklist":              scaffold.ChecklistPath,
		"plan_copy":              scaffold.PlanCopyPath,
		"plan_verification_copy": scaffold.PlanVerificationCopy,
		"repo":                   scaffold.Repo,
		"tag":                    scaffold.Tag,
		"platform_evidence_dir":  scaffold.PlatformEvidenceDir,
		"skillkit_evidence_dir":  scaffold.SkillkitEvidenceDir,
		"skillkit_included":      scaffold.SkillkitIncluded,
		"create_placeholders":    scaffold.CreatePlaceholders,
		"evidence_files":         scaffold.EvidenceFiles,
		"commands":               scaffold.Commands,
		"checks":                 scaffold.Checks,
		"recommended_actions":    scaffold.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !scaffold.OK {
		return fmt.Errorf("post-release download evidence scaffold failed")
	}
	return nil
}

func (a App) acceptancePostReleaseEvidenceStatus(opts acceptance.PostReleaseDownloadStatusOptions) error {
	status, err := acceptance.StatusPostReleaseDownloadEvidence(opts)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                  status.OK,
		"schema":              status.SchemaVersion,
		"ready_for_packaging": status.ReadyForPackaging,
		"scaffold":            status.ScaffoldPath,
		"report":              status.ReportPath,
		"repo":                status.Repo,
		"tag":                 status.Tag,
		"required_ready":      status.RequiredReady,
		"required_total":      status.RequiredTotal,
		"placeholder_count":   status.PlaceholderCount,
		"missing_count":       status.MissingCount,
		"empty_count":         status.EmptyCount,
		"evidence_files":      status.EvidenceFiles,
		"commands":            status.Commands,
		"checks":              status.Checks,
		"recommended_actions": status.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !status.ReadyForPackaging {
		return fmt.Errorf("post-release download evidence is not ready for packaging")
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

func (a App) acceptancePackageManagedMacService(opts acceptance.ManagedMacServicePackageOptions) error {
	pkg, err := acceptance.PackageManagedMacServiceEvidence(opts)
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
		return fmt.Errorf("managed Mac service acceptance package verification failed")
	}
	return nil
}

func (a App) acceptancePackageLinuxManagedService(opts acceptance.LinuxManagedServicePackageOptions) error {
	pkg, err := acceptance.PackageLinuxManagedServiceEvidence(opts)
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
		return fmt.Errorf("linux managed service acceptance package verification failed")
	}
	return nil
}

func (a App) acceptancePackageRelayAdapter(opts acceptance.RelayAdapterPackageOptions) error {
	pkg, err := acceptance.PackageRelayAdapterEvidence(opts)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                    pkg.OK(),
		"schema":                pkg.SchemaVersion,
		"out":                   pkg.OutDir,
		"package":               filepath.Join(pkg.OutDir, "package.json"),
		"checksums":             filepath.Join(pkg.OutDir, "checksums.txt"),
		"selected_path":         pkg.SelectedPath,
		"accepted_paths":        pkg.AcceptedPaths,
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
		return fmt.Errorf("relay adapter acceptance package verification failed")
	}
	return nil
}

func (a App) acceptancePackageHostedProviderRuntime(opts acceptance.HostedProviderRuntimePackageOptions) error {
	pkg, err := acceptance.PackageHostedProviderRuntimeEvidence(opts)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                    pkg.OK(),
		"schema":                pkg.SchemaVersion,
		"out":                   pkg.OutDir,
		"package":               filepath.Join(pkg.OutDir, "package.json"),
		"checksums":             filepath.Join(pkg.OutDir, "checksums.txt"),
		"storage_provider":      pkg.StorageProvider,
		"auth_provider":         pkg.AuthProvider,
		"runtime_claim":         pkg.RuntimeClaim,
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
		return fmt.Errorf("hosted provider runtime acceptance package verification failed")
	}
	return nil
}

func (a App) acceptancePackagePostReleaseDownload(opts acceptance.PostReleaseDownloadPackageOptions) error {
	pkg, err := acceptance.PackagePostReleaseDownloadEvidence(opts)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                    pkg.OK(),
		"schema":                pkg.SchemaVersion,
		"out":                   pkg.OutDir,
		"package":               filepath.Join(pkg.OutDir, "package.json"),
		"checksums":             filepath.Join(pkg.OutDir, "checksums.txt"),
		"repo":                  pkg.Repo,
		"tag":                   pkg.Tag,
		"platform_targets":      pkg.PlatformTargets,
		"skillkit_included":     pkg.SkillkitIncluded,
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
		return fmt.Errorf("post-release download acceptance package verification failed")
	}
	return nil
}

func (a App) acceptanceReleaseEvidenceIndex(opts acceptance.ReleaseEvidenceIndexOptions) error {
	index, err := acceptance.BuildReleaseEvidenceIndex(opts)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                      index.OK,
		"schema":                  index.SchemaVersion,
		"out":                     opts.OutDir,
		"index":                   filepath.Join(opts.OutDir, index.IndexPath),
		"checksums":               filepath.Join(opts.OutDir, index.ChecksumsPath),
		"hosted_provider_runtime": index.HostedProviderRuntime,
		"relay_adapters":          index.RelayAdapters,
		"post_release_download":   index.PostReleaseDownload,
		"checks":                  index.Checks,
		"required_evidence":       index.RequiredEvidence,
		"recommended_actions":     index.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !index.OK {
		return fmt.Errorf("release evidence index is incomplete")
	}
	return nil
}

func (a App) version(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	jsonOut := fs.Bool("json", false, "print version and install metadata as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(a.Stdout, rdevRuntimeInfo("."))
	}
	_, err := fmt.Fprintf(a.Stdout, "%s %s\n", buildinfo.Name, buildinfo.Version)
	return err
}

func (a App) doctor(ctx context.Context) error {
	runtimeInfo := rdevRuntimeInfo(".")
	ok, diagnostics, refreshActions := runtimeInfoHealth(runtimeInfo)
	agentNextActions := []string{
		"if rdev.version is old or commit/source_root is unknown, rebuild or update rdev before starting a support session",
		"use rdev support-session connect --start for connect-this-computer workflows",
	}
	agentNextActions = append(agentNextActions, refreshActions...)
	return writeJSON(a.Stdout, map[string]any{
		"schema_version":     "rdev.doctor.v1",
		"ok":                 ok,
		"diagnostics":        diagnostics,
		"rdev":               runtimeInfo,
		"host_capabilities":  hostcap.Detect(ctx),
		"agent_next_actions": agentNextActions,
	})
}

func runtimeInfoHealth(runtimeInfo map[string]any) (bool, []string, []string) {
	ok := true
	diagnostics := []string{}
	actions := []string{}
	addSkillProblem := func(scope string, status map[string]any) {
		if status == nil || status["ok"] != false {
			return
		}
		ok = false
		parts := []string{scope}
		if stale := stringSliceFromAny(status["stale_skills"]); len(stale) > 0 {
			parts = append(parts, "stale="+strings.Join(stale, ","))
		}
		if missing := stringSliceFromAny(status["missing_skills"]); len(missing) > 0 {
			parts = append(parts, "missing="+strings.Join(missing, ","))
		}
		if mismatches := stringSliceFromAny(status["manifest_mismatch_skills"]); len(mismatches) > 0 {
			parts = append(parts, "tampered_or_overwritten="+strings.Join(mismatches, ","))
		}
		if manifestMissing := stringSliceFromAny(status["manifest_missing_skills"]); len(manifestMissing) > 0 {
			parts = append(parts, "manifest_missing="+strings.Join(manifestMissing, ","))
		}
		if staleRefs := stringSliceFromAny(status["stale_reference_files"]); len(staleRefs) > 0 {
			parts = append(parts, "stale_references="+strings.Join(staleRefs, ","))
		}
		if missingRefs := stringSliceFromAny(status["missing_reference_files"]); len(missingRefs) > 0 {
			parts = append(parts, "missing_references="+strings.Join(missingRefs, ","))
		}
		if refMismatches := stringSliceFromAny(status["manifest_mismatch_reference_files"]); len(refMismatches) > 0 {
			parts = append(parts, "tampered_or_overwritten_references="+strings.Join(refMismatches, ","))
		}
		if refMissing := stringSliceFromAny(status["manifest_missing_reference_files"]); len(refMissing) > 0 {
			parts = append(parts, "manifest_missing_references="+strings.Join(refMissing, ","))
		}
		if status["install_manifest_present"] == false {
			parts = append(parts, "install_manifest=missing")
		}
		diagnostics = append(diagnostics, "skillkit install is not healthy: "+strings.Join(parts, " "))
		actions = append(actions, "refresh the affected Skillkit install with `rdev skillkit install --execute`; if the target framework is unclear, run `rdev skillkit plan-install` first and ask one short question only if the target directory remains ambiguous")
	}

	for _, manifest := range mapSliceFromAny(runtimeInfo["install_manifests"]) {
		scope := strings.TrimSpace(stringFromMap(manifest, "framework"))
		targetDir := strings.TrimSpace(stringFromMap(manifest, "target_dir"))
		manifestPath := strings.TrimSpace(stringFromMap(manifest, "path"))
		if scope == "" {
			scope = "manifest"
		}
		if targetDir != "" {
			scope += " target=" + targetDir
		}
		if manifestPath != "" {
			scope += " manifest=" + manifestPath
		}
		if manifestErr := strings.TrimSpace(stringFromMap(manifest, "error")); manifestErr != "" {
			ok = false
			diagnostics = append(diagnostics, "skillkit install manifest is unreadable: "+scope+" error="+manifestErr)
			actions = append(actions, "refresh the affected Skillkit install with `rdev skillkit install --execute`; if the target framework is unclear, run `rdev skillkit plan-install` first and ask one short question only if the target directory remains ambiguous")
			continue
		}
		status, _ := manifest["skill_status"].(map[string]any)
		if manifestPath != "" && status == nil {
			ok = false
			diagnostics = append(diagnostics, "skillkit install manifest is not verifiable: "+scope+" missing target_dir or skill files")
			actions = append(actions, "refresh the affected Skillkit install with `rdev skillkit install --execute`; if the target framework is unclear, run `rdev skillkit plan-install` first and ask one short question only if the target directory remains ambiguous")
			continue
		}
		addSkillProblem(scope, status)
	}
	for _, target := range mapSliceFromAny(runtimeInfo["detected_skill_install_targets"]) {
		scope := strings.TrimSpace(stringFromMap(target, "framework"))
		targetDir := strings.TrimSpace(stringFromMap(target, "target_dir"))
		if scope == "" {
			scope = "legacy skill target"
		} else {
			scope = "legacy " + scope
		}
		if targetDir != "" {
			scope += " target=" + targetDir
		}
		status, _ := target["skill_status"].(map[string]any)
		addSkillProblem(scope, status)
	}
	return ok, dedupeStrings(diagnostics), dedupeStrings(actions)
}

func rdevRuntimeInfo(repoRootHint string) map[string]any {
	currentExecutable, _ := os.Executable()
	pathRdev, _ := exec.LookPath("rdev")
	sourceRoot := strings.TrimSpace(buildinfo.SourceRoot)
	sourceRootSource := "buildinfo"
	if sourceRoot == "" || sourceRoot == "unknown" {
		sourceRoot = ""
		sourceRootSource = ""
	}
	if envSource := strings.TrimSpace(os.Getenv("RDEV_SOURCE_ROOT")); envSource != "" {
		if found := findSupportSessionRepoRoot(envSource); found != "" {
			sourceRoot = found
			sourceRootSource = "env:RDEV_SOURCE_ROOT"
		}
	}
	if sourceRoot == "" {
		if found := findSupportSessionRepoRoot(repoRootHint); found != "" && supportSessionRepoRootValid(found) {
			sourceRoot = found
			sourceRootSource = "repo-root-hint"
		}
	}
	if sourceRoot == "" {
		if found := findSupportSessionRepoRoot("."); found != "" && supportSessionRepoRootValid(found) {
			sourceRoot = found
			sourceRootSource = "cwd"
		}
	}
	if sourceRoot == "" && currentExecutable != "" {
		if found := findSupportSessionRepoRoot(filepath.Dir(currentExecutable)); found != "" && supportSessionRepoRootValid(found) {
			sourceRoot = found
			sourceRootSource = "current-executable-parent"
		}
	}
	manifestCandidates := installManifestCandidates(sourceRoot)
	manifests := make([]map[string]any, 0, len(manifestCandidates))
	for _, candidate := range manifestCandidates {
		if manifest := readInstallManifestSummary(candidate); manifest != nil {
			if status := skillInstallStatus(sourceRoot, manifest); status != nil {
				manifest["skill_status"] = status
			}
			manifests = append(manifests, manifest)
		}
	}
	detectedSkillTargets := detectedSkillInstallTargets(sourceRoot, manifests)
	return map[string]any{
		"schema_version":                 "rdev.runtime-info.v1",
		"name":                           buildinfo.Name,
		"version":                        buildinfo.Version,
		"commit":                         buildinfo.Commit,
		"build_time":                     buildinfo.BuildTime,
		"build_source_root":              buildinfo.SourceRoot,
		"source_root":                    sourceRoot,
		"source_root_source":             sourceRootSource,
		"source_root_valid":              supportSessionRepoRootValid(sourceRoot),
		"current_executable":             currentExecutable,
		"path_rdev":                      pathRdev,
		"path_rdev_is_current":           pathRdev != "" && currentExecutable != "" && samePath(pathRdev, currentExecutable),
		"install_manifests":              manifests,
		"install_manifest_count":         len(manifests),
		"detected_skill_install_targets": detectedSkillTargets,
		"detected_skill_target_count":    len(detectedSkillTargets),
	}
}

func supportSessionRepoRootValid(root string) bool {
	root = strings.TrimSpace(root)
	return root != "" && pathExists(filepath.Join(root, "go.mod")) && pathExists(filepath.Join(root, "cmd", "rdev", "main.go"))
}

func installManifestCandidates(sourceRoot string) []string {
	candidates := []string{}
	if strings.TrimSpace(sourceRoot) != "" {
		candidates = append(candidates, filepath.Join(sourceRoot, ".remote-dev-skillkit", "install.json"))
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidates = append(candidates,
			filepath.Join(home, ".remote-dev-skillkit", "install.json"),
			filepath.Join(home, ".codex", "skills", ".remote-dev-skillkit", "install.json"),
			filepath.Join(home, ".claude", "skills", ".remote-dev-skillkit", "install.json"),
			filepath.Join(home, ".opencode", "skills", ".remote-dev-skillkit", "install.json"),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		for dir := wd; ; dir = filepath.Dir(dir) {
			candidates = append(candidates, filepath.Join(dir, ".remote-dev-skillkit", "install.json"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	return dedupePaths(candidates)
}

func readInstallManifestSummary(path string) map[string]any {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(content, &payload); err != nil {
		return map[string]any{"path": path, "error": err.Error()}
	}
	return map[string]any{
		"path":            path,
		"schema_version":  stringFromMap(payload, "schema_version"),
		"bundle_dir":      stringFromMap(payload, "bundle_dir"),
		"target_dir":      stringFromMap(payload, "target_dir"),
		"framework":       stringFromMap(payload, "framework"),
		"installed_at":    stringFromMap(payload, "installed_at"),
		"skill_files":     payload["skill_files"],
		"reference_files": payload["reference_files"],
		"mcp_tools_json":  stringFromMap(payload, "mcp_tools_json"),
		"framework_doc":   stringFromMap(payload, "framework_doc"),
	}
}

func detectedSkillInstallTargets(sourceRoot string, manifests []map[string]any) []map[string]any {
	if strings.TrimSpace(sourceRoot) == "" {
		return nil
	}
	manifestTargets := map[string]bool{}
	for _, manifest := range manifests {
		targetDir := strings.TrimSpace(stringFromMap(manifest, "target_dir"))
		if targetDir != "" {
			manifestTargets[targetDir] = true
		}
	}
	out := []map[string]any{}
	for _, candidate := range commonSkillTargetCandidates() {
		if manifestTargets[candidate.Path] {
			continue
		}
		status := skillInstallStatus(sourceRoot, map[string]any{"target_dir": candidate.Path})
		if status == nil {
			continue
		}
		skillCount, _ := status["skill_count"].(int)
		missing, _ := status["missing_skills"].([]string)
		if skillCount == 0 || len(missing) == skillCount {
			continue
		}
		out = append(out, map[string]any{
			"schema_version":         "rdev.detected-skill-install-target.v1",
			"framework":              candidate.Framework,
			"target_dir":             candidate.Path,
			"install_manifest_found": false,
			"skill_status":           status,
			"next_action":            "Refresh this Skillkit install with rdev skillkit install --execute so future diagnostics can read .remote-dev-skillkit/install.json.",
		})
	}
	return out
}

type skillTargetCandidate struct {
	Framework string
	Path      string
}

func commonSkillTargetCandidates() []skillTargetCandidate {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil
	}
	candidates := []skillTargetCandidate{
		{Framework: "codex", Path: filepath.Join(home, ".codex", "skills")},
		{Framework: "claude-code", Path: filepath.Join(home, ".claude", "skills")},
		{Framework: "hermes", Path: filepath.Join(home, ".hermes", "skills")},
		{Framework: "openclaw", Path: filepath.Join(home, ".openclaw", "skills")},
	}
	if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
		candidates = append(candidates, skillTargetCandidate{Framework: "opencode", Path: filepath.Join(appData, "opencode", "skills")})
	} else {
		candidates = append(candidates, skillTargetCandidate{Framework: "opencode", Path: filepath.Join(home, ".config", "opencode", "skills")})
	}
	envs := map[string]string{
		"codex":             "RDEV_CODEX_SKILLS_DIR",
		"claude-code":       "RDEV_CLAUDE_CODE_SKILLS_DIR",
		"hermes":            "RDEV_HERMES_SKILLS_DIR",
		"openclaw":          "RDEV_OPENCLAW_SKILLS_DIR",
		"opencode":          "RDEV_OPENCODE_SKILLS_DIR",
		"generic-mcp-agent": "RDEV_GENERIC_AGENT_SKILLS_DIR",
	}
	for framework, envName := range envs {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			candidates = append(candidates, skillTargetCandidate{Framework: framework, Path: value})
		}
	}
	deduped := []skillTargetCandidate{}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		path := strings.TrimSpace(candidate.Path)
		if expanded := expandHomePath(path); expanded != "" {
			path = expanded
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		candidate.Path = path
		deduped = append(deduped, candidate)
	}
	return deduped
}

func expandHomePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func skillInstallStatus(sourceRoot string, manifest map[string]any) map[string]any {
	sourceRoot = strings.TrimSpace(sourceRoot)
	targetDir := strings.TrimSpace(stringFromMap(manifest, "target_dir"))
	if targetDir == "" {
		return nil
	}
	installManifestPresent := stringFromMap(manifest, "schema_version") == "rdev.skillkit-install-manifest.v1"
	skills := []string{"safe-remote-support", "host-triage", "remote-vibe-coding", "remote-job-review"}
	items := make([]map[string]any, 0, len(skills))
	stale := []string{}
	missing := []string{}
	manifestMismatches := []string{}
	manifestMissing := []string{}
	manifestFiles := installManifestSkillFiles(manifest)
	for _, skill := range skills {
		sourceHash := ""
		if sourceRoot != "" {
			sourceHash = fileSHA256(filepath.Join(sourceRoot, "skills", skill, "SKILL.md"))
		}
		installedPath := filepath.Join(targetDir, skill, "SKILL.md")
		installedHash := fileSHA256(installedPath)
		manifestHash := manifestFiles[skill]
		sourceUpToDate := sourceHash != "" && installedHash != "" && sourceHash == installedHash
		manifestUpToDate := manifestHash != "" && installedHash != "" && manifestHash == "sha256:"+installedHash
		item := map[string]any{
			"name":                  skill,
			"source_sha256":         sourceHash,
			"installed_sha256":      installedHash,
			"manifest_sha256":       manifestHash,
			"installed_path":        installedPath,
			"source_up_to_date":     sourceUpToDate,
			"manifest_up_to_date":   manifestUpToDate,
			"up_to_date":            sourceUpToDate,
			"integrity_up_to_date":  manifestUpToDate,
			"manifest_hash_present": manifestHash != "",
		}
		if installedHash == "" || (sourceRoot != "" && sourceHash == "") {
			missing = append(missing, skill)
		} else if sourceHash != "" && sourceHash != installedHash {
			stale = append(stale, skill)
		}
		if len(manifestFiles) > 0 {
			if manifestHash == "" {
				manifestMissing = append(manifestMissing, skill)
			} else if installedHash == "" || manifestHash != "sha256:"+installedHash {
				manifestMismatches = append(manifestMismatches, skill)
			}
		}
		items = append(items, item)
	}
	integrityKnown := len(manifestFiles) > 0
	referenceItems, staleReferences, missingReferences, referenceManifestMismatches, referenceManifestMissing, referenceIntegrityKnown := skillReferenceInstallStatus(sourceRoot, targetDir, manifest)
	ok := installManifestPresent &&
		len(stale) == 0 &&
		len(missing) == 0 &&
		integrityKnown &&
		len(manifestMismatches) == 0 &&
		len(manifestMissing) == 0 &&
		referenceIntegrityKnown &&
		len(staleReferences) == 0 &&
		len(missingReferences) == 0 &&
		len(referenceManifestMismatches) == 0 &&
		len(referenceManifestMissing) == 0
	return map[string]any{
		"schema_version":                    "rdev.skill-install-status.v1",
		"ok":                                ok,
		"install_manifest_present":          installManifestPresent,
		"source_status_known":               sourceRoot != "",
		"integrity_status_known":            integrityKnown,
		"manifest_integrity_ok":             integrityKnown && len(manifestMismatches) == 0 && len(manifestMissing) == 0,
		"reference_integrity_status_known":  referenceIntegrityKnown,
		"reference_manifest_integrity_ok":   referenceIntegrityKnown && len(referenceManifestMismatches) == 0 && len(referenceManifestMissing) == 0,
		"skill_count":                       len(skills),
		"stale_skills":                      stale,
		"missing_skills":                    missing,
		"manifest_mismatch_skills":          manifestMismatches,
		"manifest_missing_skills":           manifestMissing,
		"stale_reference_files":             staleReferences,
		"missing_reference_files":           missingReferences,
		"manifest_mismatch_reference_files": referenceManifestMismatches,
		"manifest_missing_reference_files":  referenceManifestMissing,
		"skills":                            items,
		"reference_files":                   referenceItems,
	}
}

func skillReferenceInstallStatus(sourceRoot, targetDir string, manifest map[string]any) ([]map[string]any, []string, []string, []string, []string, bool) {
	specs := installReferenceSpecs(sourceRoot, targetDir, manifest)
	manifestFiles := installManifestReferenceFiles(manifest)
	items := make([]map[string]any, 0, len(specs))
	stale := []string{}
	missing := []string{}
	manifestMismatches := []string{}
	manifestMissing := []string{}
	for _, spec := range specs {
		installedHash := fileSHA256(spec.InstalledPath)
		sourceHash := ""
		if spec.SourcePath != "" {
			sourceHash = fileSHA256(spec.SourcePath)
		}
		manifestHash := manifestFiles[spec.Name]
		sourceUpToDate := sourceHash != "" && installedHash != "" && sourceHash == installedHash
		manifestUpToDate := manifestHash != "" && installedHash != "" && manifestHash == "sha256:"+installedHash
		item := map[string]any{
			"name":                  spec.Name,
			"relative_path":         spec.RelativePath,
			"installed_path":        spec.InstalledPath,
			"source_path":           spec.SourcePath,
			"source_sha256":         sourceHash,
			"installed_sha256":      installedHash,
			"manifest_sha256":       manifestHash,
			"source_up_to_date":     sourceUpToDate,
			"manifest_up_to_date":   manifestUpToDate,
			"up_to_date":            sourceHash != "" && sourceUpToDate,
			"integrity_up_to_date":  manifestUpToDate,
			"manifest_hash_present": manifestHash != "",
		}
		if installedHash == "" {
			missing = append(missing, spec.Name)
		} else if sourceHash != "" && sourceHash != installedHash {
			stale = append(stale, spec.Name)
		}
		if len(manifestFiles) > 0 {
			if manifestHash == "" {
				manifestMissing = append(manifestMissing, spec.Name)
			} else if installedHash == "" || manifestHash != "sha256:"+installedHash {
				manifestMismatches = append(manifestMismatches, spec.Name)
			}
		} else {
			manifestMissing = append(manifestMissing, spec.Name)
		}
		items = append(items, item)
	}
	return items, stale, missing, manifestMismatches, manifestMissing, len(manifestFiles) > 0
}

type installReferenceSpec struct {
	Name          string
	RelativePath  string
	InstalledPath string
	SourcePath    string
}

func installReferenceSpecs(sourceRoot, targetDir string, manifest map[string]any) []installReferenceSpec {
	refRoot := filepath.Join(targetDir, ".remote-dev-skillkit")
	frameworkDoc := strings.TrimSpace(stringFromMap(manifest, "framework_doc"))
	frameworkDocRel := ""
	if frameworkDoc != "" {
		frameworkDocRel = filepath.ToSlash(filepath.Join("frameworks", filepath.Base(frameworkDoc)))
	}
	if frameworkDocRel == "" {
		frameworkDocRel = "frameworks/codex.md"
	}
	bundleDir := strings.TrimSpace(stringFromMap(manifest, "bundle_dir"))
	specs := []installReferenceSpec{
		{
			Name:          "mcp-tools",
			RelativePath:  "mcp/tools.json",
			InstalledPath: filepath.Join(refRoot, "mcp", "tools.json"),
		},
		{
			Name:          "framework-doc",
			RelativePath:  frameworkDocRel,
			InstalledPath: filepath.Join(refRoot, filepath.FromSlash(frameworkDocRel)),
		},
	}
	if sourceRoot != "" {
		specs[0].SourcePath = filepath.Join(sourceRoot, "mcp", "tools.json")
	}
	if bundleDir != "" {
		specs[1].SourcePath = filepath.Join(bundleDir, filepath.FromSlash(frameworkDocRel))
	}
	return specs
}

func installManifestSkillFiles(manifest map[string]any) map[string]string {
	out := map[string]string{}
	raw, ok := manifest["skill_files"].([]any)
	if !ok {
		return out
	}
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(stringFromMap(m, "name"))
		hash := strings.TrimSpace(stringFromMap(m, "sha256"))
		if name == "" || hash == "" {
			continue
		}
		out[name] = hash
	}
	return out
}

func installManifestReferenceFiles(manifest map[string]any) map[string]string {
	out := map[string]string{}
	raw, ok := manifest["reference_files"].([]any)
	if !ok {
		return out
	}
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(stringFromMap(m, "name"))
		hash := strings.TrimSpace(stringFromMap(m, "sha256"))
		if name == "" || hash == "" {
			continue
		}
		out[name] = hash
	}
	return out
}

func fileSHA256(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func samePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr == nil {
		left = leftAbs
	}
	if rightErr == nil {
		right = rightAbs
	}
	leftEval, leftErr := filepath.EvalSymlinks(left)
	rightEval, rightErr := filepath.EvalSymlinks(right)
	if leftErr == nil {
		left = leftEval
	}
	if rightErr == nil {
		right = rightEval
	}
	return left == right
}

func dedupePaths(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func dedupeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func (a App) bootstrap(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing bootstrap subcommand")
	}
	switch args[0] {
	case "agent-plan":
		fs := flag.NewFlagSet("bootstrap agent-plan", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		repoRoot := fs.String("repo-root", ".", "checked-out remote-dev-skillkit repository root")
		framework := fs.String("framework", "", "optional detected agent framework")
		remoteRequested := fs.Bool("remote-requested", false, "include remote-host Connection Entry defaults")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return writeJSON(a.Stdout, buildAgentBootstrapPlan(ctx, *repoRoot, *framework, *remoteRequested))
	default:
		return fmt.Errorf("unknown bootstrap subcommand %q", args[0])
	}
}

func buildAgentBootstrapPlan(ctx context.Context, repoRoot, framework string, remoteRequested bool) map[string]any {
	rdevPath, _ := exec.LookPath("rdev")
	currentExecutable, _ := os.Executable()
	absRepoRoot, err := filepath.Abs(strings.TrimSpace(repoRoot))
	if err != nil || strings.TrimSpace(repoRoot) == "" {
		absRepoRoot = strings.TrimSpace(repoRoot)
	}
	repoLooksValid := pathExists(filepath.Join(absRepoRoot, "go.mod")) && pathExists(filepath.Join(absRepoRoot, "cmd", "rdev", "main.go"))
	goPath, _ := exec.LookPath("go")
	gitPath, _ := exec.LookPath("git")
	detected := hostcap.Detect(ctx)
	if strings.TrimSpace(framework) == "" {
		framework = "unknown-probe-required"
	}
	localMCPCommand := skillkit.RecommendedRdevCommand()
	goBinRdev := skillkit.InstalledGoBinRdevForDiagnostics()
	currentExecutableUsable := strings.TrimSpace(currentExecutable) != "" &&
		strings.Contains(filepath.Base(currentExecutable), "rdev") &&
		!strings.Contains(filepath.ToSlash(currentExecutable), "/go-build/")
	recoveryActions := []map[string]any{
		{
			"id":      "use-existing-rdev",
			"status":  statusFromBool(strings.TrimSpace(rdevPath) != ""),
			"command": []string{"rdev", "doctor"},
			"notes":   []string{"Prefer an already installed rdev when it passes rdev doctor."},
		},
		{
			"id":      "use-go-bin-rdev",
			"status":  statusFromBool(strings.TrimSpace(goBinRdev) != ""),
			"command": []string{goBinRdev, "doctor"},
			"notes":   []string{"Use this when go install created rdev under GOBIN/GOPATH/bin but that directory is not on PATH.", "Configure MCP with the absolute binary path instead of a bare rdev command."},
		},
		{
			"id":      "use-current-executable",
			"status":  statusFromBool(currentExecutableUsable),
			"command": []string{currentExecutable, "doctor"},
			"notes":   []string{"When the agent is already running a local rdev binary outside PATH, use that absolute binary for MCP config."},
		},
		{
			"id":      "build-from-checkout",
			"status":  statusFromBool(repoLooksValid && strings.TrimSpace(goPath) != ""),
			"command": []string{"go", "install", "./cmd/rdev"},
			"cwd":     absRepoRoot,
			"notes":   []string{"Use this when rdev is missing but Go and a clean checkout are available.", "Run rdev doctor after install and resolve the final binary path before editing MCP config."},
		},
		{
			"id":      "run-from-checkout-with-go",
			"status":  statusFromBool(repoLooksValid && strings.TrimSpace(goPath) != ""),
			"command": []string{"go", "run", "./cmd/rdev", "doctor"},
			"cwd":     absRepoRoot,
			"notes":   []string{"Temporary fallback for bootstrap only; prefer go install before long-lived MCP stdio."},
		},
		{
			"id":      "clone-then-build",
			"status":  statusFromBool(strings.TrimSpace(gitPath) != "" && strings.TrimSpace(goPath) != ""),
			"command": []string{"git", "clone", "https://github.com/EitanWong/remote-dev-skillkit", "<safe-user-or-workspace-dir>"},
			"notes":   []string{"Use only when no current checkout exists. Inspect for local changes before updating an existing checkout."},
		},
		{
			"id":      "signed-release-download",
			"status":  "requires-published-release-and-release-root",
			"command": []string{"rdev", "update", "plan", "--repo", "EitanWong/remote-dev-skillkit"},
			"notes":   []string{"Use only after a signed GitHub Release exists and the release root is configured. Verify checksums and signed release-bundle.json before replacing binaries."},
		},
	}
	askOnlyWhen := []string{
		"company or owner authorization for remote support is not confirmed",
		"managed persistence, service installation, firewall, DNS, route, driver, credential, paid relay, cloud, or security-policy change is needed",
		"framework skill target or MCP config path remains ambiguous after read-only probes",
		"gateway or relay credentials are required and cannot be discovered from approved local config",
	}
	doNotAskFor := []string{
		"target OS before starting the standard support-session connect flow; let package catalog, join page, and target-side probes select it when package materialization is needed",
		"ticket code, manifest root, gateway URL, transport, release root, checksum, or helper argv assembly",
		"temporary vs managed mode when ownership is clear; use attended-temporary for third-party/one-off and managed for owned recurring hosts",
		"rdev path before trying PATH, current executable, checkout build, and safe clone/build recovery",
	}
	remoteDefaults := map[string]any{
		"requested":             remoteRequested,
		"default_unknown_owner": "attended-temporary",
		"owned_recurring_mode":  "managed-after-explicit-persistence-approval",
		"third_party_mode":      "attended-temporary",
		"first_human_question":  "Please confirm that company policy and the device owner allow a visible temporary Remote Dev Skillkit support session on this machine.",
		"agent_should_continue_after_confirmation": []string{
			"call rdev.support_session.connect or run rdev support-session connect",
			"if no gateway is running, run the returned visible foreground rdev support-session connect --start command",
			"send only handoff_text_file.path or target_handoff_envelope.full_text verbatim to the target side",
			"wait through connection_supervision, foreground_feedback, status_file.path, or rdev.support_session.status",
			"use connection_entry_runner_recommendation only for reviewed package materialization, managed owned-host planning, or restrictive-network recovery",
		},
		"safe_defaults": []string{
			"visible foreground session",
			"no hidden persistence",
			"no service installation",
			"no execution-policy weakening",
			"no UAC/sudo bypass",
			"user-level shell and scoped filesystem access only until further approval",
		},
		"connection_selection": []string{
			"local MCP stdio for this agent runtime",
			"local/dev gateway for same-machine tests",
			"LAN/private gateway when probes show reachability",
			"hosted gateway when configured",
			"existing authorized SSH tunnel",
			"configured open-source/free relay or mesh helper such as frp, Chisel, headscale/Tailscale-compatible mesh, or WireGuard",
		},
	}
	return map[string]any{
		"schema_version":             "rdev.agent-bootstrap-plan.v1",
		"ok":                         true,
		"repo":                       "EitanWong/remote-dev-skillkit",
		"repo_url":                   "https://github.com/EitanWong/remote-dev-skillkit",
		"framework":                  framework,
		"repo_root":                  absRepoRoot,
		"repo_root_valid":            repoLooksValid,
		"detected_os":                runtime.GOOS,
		"detected_arch":              runtime.GOARCH,
		"detected_host_capabilities": detected,
		"rdev_path":                  rdevPath,
		"current_executable":         currentExecutable,
		"go_path":                    goPath,
		"go_bin_rdev":                goBinRdev,
		"git_path":                   gitPath,
		"local_mcp": map[string]any{
			"mode":        "stdio",
			"command":     localMCPCommand,
			"args":        []string{"mcp", "serve"},
			"gateway_url": "optional-for-local-agent-install",
		},
		"rdev_recovery_order": recoveryActions,
		"skillkit_steps": []string{
			"run rdev doctor",
			"run rdev mcp tools",
			"export Skillkit bundle without --gateway-url for local agent installs",
			"verify Skillkit bundle",
			"plan install for codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent",
			"verify install plan",
			"dry-run the matching rdev skillkit install",
			"execute only when the target directory is clear and safe",
		},
		"remote_host_defaults": remoteDefaults,
		"ask_only_when":        askOnlyWhen,
		"do_not_ask_for":       doNotAskFor,
		"forbidden_actions": []string{
			"hidden installation",
			"ExecutionPolicy Bypass or OS policy weakening",
			"UAC/sudo bypass",
			"unapproved service persistence",
			"unapproved firewall/DNS/route/driver/cloud/paid relay mutation",
			"hardcoded private paths, secrets, server addresses, ticket codes, or dates",
		},
		"report_fields": []string{
			"rdev_recovered_by",
			"framework",
			"skill_target",
			"mcp_configured_or_snippet",
			"local_mcp_command",
			"connection_entry_mode",
			"remaining_required_human_decision",
			"verification_commands",
		},
	}
}

func statusFromBool(ok bool) string {
	if ok {
		return "available"
	}
	return "unavailable"
}

func agentRdevCommand(command string) string {
	if command = strings.TrimSpace(command); command != "" {
		return command
	}
	return skillkit.RecommendedRdevCommand()
}

func pathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func (a App) supportSession(ctx context.Context, args []string) error {
	if len(args) == 0 {
		a.printSupportSessionUsage()
		return nil
	}
	switch args[0] {
	case "help", "-h", "--help":
		a.printSupportSessionUsage()
		return nil
	}
	switch args[0] {
	case "connect":
		fs := flag.NewFlagSet("support-session connect", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		repoRoot := fs.String("repo-root", ".", "checked-out remote-dev-skillkit repository root")
		workDir := fs.String("work-dir", "", "session working directory for gateway state, keys, audit, and helper assets")
		addr := fs.String("addr", "0.0.0.0:8787", "foreground gateway listen address")
		gatewayURL := fs.String("gateway-url", "", "already reachable gateway URL; omit to use configured RDEV_*_GATEWAY_URL or return cli_start_now_command")
		target := fs.String("target", "auto", "target platform hint: auto, windows, macos, linux")
		reason := fs.String("reason", "visible temporary remote support", "support session reason")
		ttl := fs.Int("ttl-seconds", 7200, "temporary invite TTL in seconds")
		autoApprove := fs.Bool("auto-approve", true, "auto-approve the first attended-temporary host created by this standard session ticket")
		locale := fs.String("locale", "auto", "localized target-user instruction language, for example auto, en, zh-CN, ja, ko, es, fr, de, or pt-BR")
		operatorTokenFile := fs.String("operator-token-file", "", "file containing an operator auth bearer token")
		rdevCommand := fs.String("rdev-command", "", "command name or absolute path for generated local Agent commands; default auto-detects a stable rdev binary")
		start := fs.Bool("start", false, "when no reachable gateway is configured, start the standard visible foreground gateway in this process")
		readyFile := fs.String("ready-file", "", "write the started support-session JSON payload to this file when --start is used")
		statusFile := fs.String("status-file", "", "write the latest foreground support-session status event to this file when --start is used")
		handoffTextFile := fs.String("handoff-text-file", "", "write the exact target-side human handoff text to this file when --start is used")
		connectedReportFile := fs.String("connected-report-file", "", "write the exact connected user report text to this file when --start is used")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		return a.supportSessionConnect(ctx, supportSessionConnectOptions{
			RepoRoot:            *repoRoot,
			WorkDir:             *workDir,
			Addr:                *addr,
			GatewayURL:          *gatewayURL,
			Target:              *target,
			Reason:              *reason,
			TTLSeconds:          *ttl,
			AutoApprove:         *autoApprove,
			Locale:              *locale,
			OperatorTokenFile:   *operatorTokenFile,
			RdevCommand:         *rdevCommand,
			Start:               *start,
			ReadyFile:           *readyFile,
			StatusFile:          *statusFile,
			HandoffTextFile:     *handoffTextFile,
			ConnectedReportFile: *connectedReportFile,
		})
	case "handoff":
		fs := flag.NewFlagSet("support-session handoff", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		repoRoot := fs.String("repo-root", ".", "checked-out remote-dev-skillkit repository root")
		workDir := fs.String("work-dir", "", "session working directory for gateway state, keys, audit, and helper assets")
		addr := fs.String("addr", "0.0.0.0:8787", "foreground gateway listen address")
		gatewayURL := fs.String("gateway-url", "", "already reachable gateway URL; omit when no gateway is running")
		target := fs.String("target", "auto", "target platform hint: auto, windows, macos, linux")
		reason := fs.String("reason", "visible temporary remote support", "support session reason")
		ttl := fs.Int("ttl-seconds", 7200, "temporary invite TTL in seconds")
		autoApprove := fs.Bool("auto-approve", true, "auto-approve the first attended-temporary host created by this standard session ticket")
		locale := fs.String("locale", "auto", "localized target-user instruction language, for example auto, en, zh-CN, ja, ko, es, fr, de, or pt-BR")
		rdevCommand := fs.String("rdev-command", "", "command name or absolute path for generated local Agent commands; default auto-detects a stable rdev binary")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if *ttl < 60 || *ttl > 86400 {
			return fmt.Errorf("ttl-seconds must be between 60 and 86400")
		}
		return writeJSON(a.Stdout, supportsession.BuildHandoff(supportsession.HandoffOptions{
			RepoRoot:    *repoRoot,
			WorkDir:     *workDir,
			Addr:        *addr,
			GatewayURL:  *gatewayURL,
			Target:      *target,
			Reason:      *reason,
			TTLSeconds:  *ttl,
			AutoApprove: *autoApprove,
			Locale:      *locale,
			RdevCommand: agentRdevCommand(*rdevCommand),
		}))
	case "prepare":
		fs := flag.NewFlagSet("support-session prepare", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		repoRoot := fs.String("repo-root", ".", "checked-out remote-dev-skillkit repository root")
		workDir := fs.String("work-dir", "", "session working directory for gateway state, keys, audit, and helper assets")
		addr := fs.String("addr", "0.0.0.0:8787", "foreground gateway listen address")
		gatewayURL := fs.String("gateway-url", "", "gateway URL reachable by the target host; defaults to the best local candidate for --addr")
		target := fs.String("target", "auto", "target platform hint: auto, windows, macos, linux")
		buildAssets := fs.Bool("build-assets", false, "build missing platform rdev helper assets from the checkout")
		rdevCommand := fs.String("rdev-command", "", "command name or absolute path for generated local Agent commands; default auto-detects a stable rdev binary")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		return a.supportSessionPrepare(ctx, supportSessionPrepareOptions{
			RepoRoot:    *repoRoot,
			WorkDir:     *workDir,
			Addr:        *addr,
			GatewayURL:  *gatewayURL,
			Target:      *target,
			BuildAssets: *buildAssets,
			RdevCommand: *rdevCommand,
		})
	case "start":
		fs := flag.NewFlagSet("support-session start", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		repoRoot := fs.String("repo-root", ".", "checked-out remote-dev-skillkit repository root")
		addr := fs.String("addr", "0.0.0.0:8787", "foreground gateway listen address")
		gatewayURL := fs.String("gateway-url", "", "gateway URL reachable by the target host; defaults to the best local candidate for --addr")
		workDir := fs.String("work-dir", "", "session working directory for gateway state, keys, audit, and helper assets")
		target := fs.String("target", "auto", "target platform hint: auto, windows, macos, linux")
		reason := fs.String("reason", "visible temporary remote support", "support session reason")
		ttl := fs.Int("ttl-seconds", 7200, "temporary invite TTL in seconds")
		autoApprove := fs.Bool("auto-approve", true, "auto-approve the first attended-temporary host created by this standard session ticket")
		locale := fs.String("locale", "auto", "localized target-user instruction language, for example auto, en, zh-CN, ja, ko, es, fr, de, or pt-BR")
		rdevCommand := fs.String("rdev-command", "", "command name or absolute path for generated local Agent commands; default auto-detects a stable rdev binary")
		readyFile := fs.String("ready-file", "", "write the started support-session JSON payload to this file before serving; defaults to the session work dir")
		statusFile := fs.String("status-file", "", "write the latest foreground support-session status event to this file before serving; defaults to the session work dir")
		handoffTextFile := fs.String("handoff-text-file", "", "write the exact target-side human handoff text to this file before serving; defaults to the session work dir")
		connectedReportFile := fs.String("connected-report-file", "", "write the exact connected user report text to this file after the target connects; defaults to the session work dir")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		return a.supportSessionStart(ctx, supportSessionStartOptions{
			RepoRoot:            *repoRoot,
			Addr:                *addr,
			GatewayURL:          *gatewayURL,
			WorkDir:             *workDir,
			Target:              *target,
			Reason:              *reason,
			TTLSeconds:          *ttl,
			AutoApprove:         *autoApprove,
			Locale:              *locale,
			RdevCommand:         *rdevCommand,
			ReadyFile:           *readyFile,
			StatusFile:          *statusFile,
			HandoffTextFile:     *handoffTextFile,
			ConnectedReportFile: *connectedReportFile,
		})
	case "create":
		fs := flag.NewFlagSet("support-session create", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		gatewayURL := fs.String("gateway-url", "", "gateway URL reachable by the target host")
		target := fs.String("target", "auto", "target platform hint: auto, windows, macos, linux")
		reason := fs.String("reason", "visible temporary remote support", "support session reason")
		ttl := fs.Int("ttl-seconds", 7200, "temporary invite TTL in seconds")
		autoApprove := fs.Bool("auto-approve", true, "auto-approve the first attended-temporary host created by this standard session ticket")
		locale := fs.String("locale", "auto", "localized target-user instruction language, for example auto, en, zh-CN, ja, ko, es, fr, de, or pt-BR")
		operatorTokenFile := fs.String("operator-token-file", "", "file containing an operator auth bearer token")
		rdevCommand := fs.String("rdev-command", "", "command name or absolute path for generated local Agent commands; default auto-detects a stable rdev binary")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		return a.supportSessionCreate(ctx, supportSessionCreateOptions{
			GatewayURL:        *gatewayURL,
			Target:            *target,
			Reason:            *reason,
			TTLSeconds:        *ttl,
			AutoApprove:       *autoApprove,
			Locale:            *locale,
			OperatorTokenFile: *operatorTokenFile,
			RdevCommand:       *rdevCommand,
		})
	case "plan":
		fs := flag.NewFlagSet("support-session plan", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		repoRoot := fs.String("repo-root", ".", "checked-out remote-dev-skillkit repository root")
		workDir := fs.String("work-dir", "", "session working directory for gateway state, keys, logs, and generated helpers")
		gatewayURL := fs.String("gateway-url", "", "gateway URL reachable by the target host")
		addr := fs.String("addr", "0.0.0.0:8787", "gateway listen address for the generated start command")
		target := fs.String("target", "auto", "target platform hint: auto, windows, macos, linux")
		reason := fs.String("reason", "visible temporary remote support", "support session reason")
		ttl := fs.Int("ttl-seconds", 7200, "temporary invite TTL in seconds")
		autoApprove := fs.Bool("auto-approve", true, "auto-approve the first attended-temporary host created by this standard session ticket")
		locale := fs.String("locale", "auto", "localized target-user instruction language, for example auto, en, zh-CN, ja, ko, es, fr, de, or pt-BR")
		rdevCommand := fs.String("rdev-command", "", "command name or absolute path for generated local Agent commands; default auto-detects a stable rdev binary")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		return writeJSON(a.Stdout, supportsession.BuildPlan(ctx, supportsession.Options{
			RepoRoot:    *repoRoot,
			WorkDir:     *workDir,
			GatewayURL:  *gatewayURL,
			Addr:        *addr,
			Target:      *target,
			Reason:      *reason,
			TTLSeconds:  *ttl,
			AutoApprove: *autoApprove,
			Locale:      *locale,
			RdevCommand: agentRdevCommand(*rdevCommand),
		}))
	case "status":
		fs := flag.NewFlagSet("support-session status", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		gatewayURL := fs.String("gateway-url", "", "gateway URL that created the Connection Entry")
		ticketCode := fs.String("ticket-code", "", "Connection Entry ticket code to watch")
		locale := fs.String("locale", "auto", "feedback language, for example auto, en, or zh-CN")
		wait := fs.Bool("wait", false, "wait until a host connects or approval is pending")
		timeout := fs.Duration("timeout", 2*time.Minute, "maximum wait duration when --wait is set")
		timeoutSeconds := fs.Int("timeout-seconds", 0, "alias for --timeout, in whole seconds")
		interval := fs.Duration("interval", time.Second, "poll interval when --wait is set")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if *timeoutSeconds > 0 {
			*timeout = time.Duration(*timeoutSeconds) * time.Second
		}
		status, err := supportSessionStatus(ctx, http.DefaultClient, supportSessionStatusOptions{
			GatewayURL: *gatewayURL,
			TicketCode: *ticketCode,
			Locale:     *locale,
			Wait:       *wait,
			Timeout:    *timeout,
			Interval:   *interval,
		})
		if err != nil {
			return err
		}
		return writeJSON(a.Stdout, status)
	case "recover":
		fs := flag.NewFlagSet("support-session recover", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		gatewayURL := fs.String("gateway-url", "", "gateway URL that created the Connection Entry")
		ticketCode := fs.String("ticket-code", "", "Connection Entry ticket code to recover")
		locale := fs.String("locale", "auto", "feedback language, for example auto, en, or zh-CN")
		operatorTokenFile := fs.String("operator-token-file", "", "file containing an operator bearer token")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		return a.supportSessionRecover(ctx, supportSessionRecoverOptions{
			GatewayURL:        *gatewayURL,
			TicketCode:        *ticketCode,
			Locale:            *locale,
			OperatorTokenFile: *operatorTokenFile,
		})
	case "report":
		fs := flag.NewFlagSet("support-session report", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		gatewayURL := fs.String("gateway-url", "", "gateway URL that owns the connected support session")
		hostID := fs.String("host-id", "", "active host ID to report on")
		ticketCode := fs.String("ticket-code", "", "support-session ticket code; when set, rdev selects the single active host and includes stale-host diagnostics")
		locale := fs.String("locale", "auto", "report language hint, for example auto, en, or zh-CN")
		operatorTokenFile := fs.String("operator-token-file", "", "file containing an operator bearer token")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		return a.supportSessionReport(ctx, supportSessionReportOptions{
			GatewayURL:        *gatewayURL,
			HostID:            *hostID,
			TicketCode:        *ticketCode,
			Locale:            *locale,
			OperatorTokenFile: *operatorTokenFile,
		})
	case "audit-capabilities":
		fs := flag.NewFlagSet("support-session audit-capabilities", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		gatewayURL := fs.String("gateway-url", "", "gateway URL that owns the connected support session")
		hostID := fs.String("host-id", "", "active host ID to audit")
		timeout := fs.Duration("timeout", 90*time.Second, "maximum duration for the audit")
		timeoutSeconds := fs.Int("timeout-seconds", 0, "alias for --timeout, in whole seconds")
		operatorTokenFile := fs.String("operator-token-file", "", "file containing an operator bearer token")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if *timeoutSeconds > 0 {
			*timeout = time.Duration(*timeoutSeconds) * time.Second
		}
		return a.supportSessionAuditCapabilities(ctx, supportSessionAuditCapabilitiesOptions{
			GatewayURL:        *gatewayURL,
			HostID:            *hostID,
			Timeout:           *timeout,
			OperatorTokenFile: *operatorTokenFile,
		})
	default:
		return fmt.Errorf("unknown support-session subcommand %q", args[0])
	}
}

type supportSessionStartOptions struct {
	RepoRoot            string
	Addr                string
	GatewayURL          string
	WorkDir             string
	Target              string
	Reason              string
	TTLSeconds          int
	AutoApprove         bool
	Locale              string
	RdevCommand         string
	ReadyFile           string
	StatusFile          string
	HandoffTextFile     string
	ConnectedReportFile string
}

type supportSessionConnectOptions struct {
	RepoRoot            string
	WorkDir             string
	Addr                string
	GatewayURL          string
	Target              string
	Reason              string
	TTLSeconds          int
	AutoApprove         bool
	Locale              string
	OperatorTokenFile   string
	RdevCommand         string
	Start               bool
	ReadyFile           string
	StatusFile          string
	HandoffTextFile     string
	ConnectedReportFile string
}

type supportSessionPrepareOptions struct {
	RepoRoot    string
	WorkDir     string
	Addr        string
	GatewayURL  string
	Target      string
	BuildAssets bool
	RdevCommand string
}

type supportSessionCreateOptions struct {
	GatewayURL        string
	Target            string
	Reason            string
	TTLSeconds        int
	AutoApprove       bool
	Locale            string
	OperatorTokenFile string
	RdevCommand       string
}

func (a App) supportSessionPrepare(ctx context.Context, opts supportSessionPrepareOptions) error {
	opts.RdevCommand = agentRdevCommand(opts.RdevCommand)
	prepared, err := prepareSupportSessionEnvironment(ctx, opts)
	if err != nil {
		return err
	}
	return writeJSON(a.Stdout, prepared)
}

func (a App) supportSessionConnect(ctx context.Context, opts supportSessionConnectOptions) error {
	opts.RdevCommand = agentRdevCommand(opts.RdevCommand)
	if opts.TTLSeconds < 60 || opts.TTLSeconds > 86400 {
		return fmt.Errorf("ttl-seconds must be between 60 and 86400")
	}
	if opts.Start {
		return a.supportSessionStart(ctx, supportSessionStartOptions{
			RepoRoot:            opts.RepoRoot,
			Addr:                opts.Addr,
			GatewayURL:          opts.GatewayURL,
			WorkDir:             opts.WorkDir,
			Target:              opts.Target,
			Reason:              opts.Reason,
			TTLSeconds:          opts.TTLSeconds,
			AutoApprove:         opts.AutoApprove,
			Locale:              opts.Locale,
			RdevCommand:         opts.RdevCommand,
			ReadyFile:           opts.ReadyFile,
			StatusFile:          opts.StatusFile,
			HandoffTextFile:     opts.HandoffTextFile,
			ConnectedReportFile: opts.ConnectedReportFile,
		})
	}
	gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	if gatewayURL == "" {
		gatewayURL, _ = supportsession.ConfiguredGatewayURLCandidate()
	}
	if gatewayURL == "" {
		handoff := supportsession.BuildHandoff(supportsession.HandoffOptions{
			RepoRoot:    opts.RepoRoot,
			WorkDir:     opts.WorkDir,
			Addr:        opts.Addr,
			Target:      opts.Target,
			Reason:      opts.Reason,
			TTLSeconds:  opts.TTLSeconds,
			AutoApprove: opts.AutoApprove,
			Locale:      opts.Locale,
			RdevCommand: opts.RdevCommand,
		})
		return writeJSON(a.Stdout, supportsession.BuildConnectFromHandoff(handoff))
	}
	created, err := createSupportSessionPayload(ctx, supportSessionCreateOptions{
		GatewayURL:        gatewayURL,
		Target:            opts.Target,
		Reason:            opts.Reason,
		TTLSeconds:        opts.TTLSeconds,
		AutoApprove:       opts.AutoApprove,
		Locale:            opts.Locale,
		OperatorTokenFile: opts.OperatorTokenFile,
		RdevCommand:       opts.RdevCommand,
	})
	if err != nil {
		return err
	}
	return writeJSON(a.Stdout, supportsession.BuildConnectFromCreated(created))
}

// startBestAvailableTunnel tries ephemeral public tunnel providers in order
// until one succeeds. The order is:
//
//  1. cloudflared quick-tunnel (zero-auth, ephemeral fallback)
//  2. localhost.run via SSH (fallback, needs only openssh client)
//
// If none succeed it logs a warning and returns ("", noop) — the caller
// falls back to LAN-only connectivity.
func startBestAvailableTunnel(ctx context.Context, stderr io.Writer, localListenURL, localPort string) (string, context.CancelFunc) {
	// --- Provider 1: cloudflared ---
	_, _ = fmt.Fprintf(stderr, "[rdev] trying cloudflared quick-tunnel for %s ...\n", localListenURL)
	tunnelURL, cancel, err := startCloudflaredQuickTunnel(ctx, stderr, localListenURL)
	if err == nil {
		_, _ = fmt.Fprintf(stderr, "[rdev] cloudflared quick-tunnel active: %s\n", tunnelURL)
		return tunnelURL, cancel
	}
	_, _ = fmt.Fprintf(stderr, "[rdev] cloudflared failed: %v\n", err)

	// --- Provider 2: localhost.run (SSH reverse tunnel) ---
	_, _ = fmt.Fprintf(stderr, "[rdev] trying localhost.run SSH tunnel for port %s ...\n", localPort)
	tunnelURL, cancel, err = startLocalhostRunTunnel(ctx, stderr, localPort)
	if err == nil {
		_, _ = fmt.Fprintf(stderr, "[rdev] localhost.run tunnel active: %s\n", tunnelURL)
		return tunnelURL, cancel
	}
	_, _ = fmt.Fprintf(stderr, "[rdev] localhost.run failed: %v\n", err)

	_, _ = fmt.Fprintf(stderr, "[rdev] all public tunnel providers failed; falling back to LAN gateway\n")
	return "", func() {}
}

type cloudflaredStableTunnelConfig struct {
	GatewayURL string
	Argv       []string
	Preview    []string
	Source     string
	Mode       string
}

func configuredCloudflaredStableTunnelConfig(cfPath, localURL string) (cloudflaredStableTunnelConfig, bool, error) {
	gatewayURL := strings.TrimRight(strings.TrimSpace(os.Getenv("RDEV_CLOUDFLARED_NAMED_TUNNEL_URL")), "/")
	gatewaySource := "RDEV_CLOUDFLARED_NAMED_TUNNEL_URL"
	if gatewayURL == "" {
		gatewayURL = strings.TrimRight(strings.TrimSpace(os.Getenv("RDEV_CLOUDFLARED_GATEWAY_URL")), "/")
		gatewaySource = "RDEV_CLOUDFLARED_GATEWAY_URL"
	}
	if gatewayURL == "" {
		return cloudflaredStableTunnelConfig{}, false, nil
	}
	rawArgv := strings.TrimSpace(os.Getenv("RDEV_CLOUDFLARED_NAMED_TUNNEL_START_ARGV_JSON"))
	argvSource := "RDEV_CLOUDFLARED_NAMED_TUNNEL_START_ARGV_JSON"
	if rawArgv == "" {
		rawArgv = strings.TrimSpace(os.Getenv("RDEV_CLOUDFLARED_START_ARGV_JSON"))
		argvSource = "RDEV_CLOUDFLARED_START_ARGV_JSON"
	}
	if rawArgv != "" {
		argv, err := parseCloudflaredStableStartArgv(rawArgv, argvSource, localURL)
		if err != nil {
			return cloudflaredStableTunnelConfig{}, true, err
		}
		return cloudflaredStableTunnelConfig{
			GatewayURL: gatewayURL,
			Argv:       argv,
			Preview:    redactCloudflaredArgv(argv),
			Source:     gatewaySource + "+" + argvSource,
			Mode:       "configured-start-argv",
		}, true, nil
	}
	tokenFile := strings.TrimSpace(os.Getenv("RDEV_CLOUDFLARED_TUNNEL_TOKEN_FILE"))
	if tokenFile != "" {
		argv := []string{cfPath, "tunnel", "--protocol", "http2", "--url", localURL, "run", "--token-file", tokenFile}
		return cloudflaredStableTunnelConfig{
			GatewayURL: gatewayURL,
			Argv:       argv,
			Preview:    redactCloudflaredArgv(argv),
			Source:     gatewaySource + "+RDEV_CLOUDFLARED_TUNNEL_TOKEN_FILE",
			Mode:       "token-file",
		}, true, nil
	}
	token := strings.TrimSpace(os.Getenv("RDEV_CLOUDFLARED_TUNNEL_TOKEN"))
	if token != "" {
		argv := []string{cfPath, "tunnel", "--protocol", "http2", "--url", localURL, "run", "--token", token}
		return cloudflaredStableTunnelConfig{
			GatewayURL: gatewayURL,
			Argv:       argv,
			Preview:    redactCloudflaredArgv(argv),
			Source:     gatewaySource + "+RDEV_CLOUDFLARED_TUNNEL_TOKEN",
			Mode:       "token",
		}, true, nil
	}
	tunnelName := strings.TrimSpace(os.Getenv("RDEV_CLOUDFLARED_TUNNEL_NAME"))
	if tunnelName != "" {
		argv := []string{cfPath, "tunnel", "--protocol", "http2", "--url", localURL, "run", tunnelName}
		return cloudflaredStableTunnelConfig{
			GatewayURL: gatewayURL,
			Argv:       argv,
			Preview:    redactCloudflaredArgv(argv),
			Source:     gatewaySource + "+RDEV_CLOUDFLARED_TUNNEL_NAME",
			Mode:       "named-tunnel",
		}, true, nil
	}
	return cloudflaredStableTunnelConfig{}, false, nil
}

func cloudflaredStableTunnelStartRequested() bool {
	hasURL := strings.TrimSpace(os.Getenv("RDEV_CLOUDFLARED_NAMED_TUNNEL_URL")) != "" ||
		strings.TrimSpace(os.Getenv("RDEV_CLOUDFLARED_GATEWAY_URL")) != ""
	if !hasURL {
		return false
	}
	for _, name := range []string{
		"RDEV_CLOUDFLARED_NAMED_TUNNEL_START_ARGV_JSON",
		"RDEV_CLOUDFLARED_START_ARGV_JSON",
		"RDEV_CLOUDFLARED_TUNNEL_TOKEN_FILE",
		"RDEV_CLOUDFLARED_TUNNEL_TOKEN",
		"RDEV_CLOUDFLARED_TUNNEL_NAME",
	} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return true
		}
	}
	return false
}

func parseCloudflaredStableStartArgv(raw, envName, localURL string) ([]string, error) {
	var argv []string
	if err := json.Unmarshal([]byte(raw), &argv); err != nil {
		return nil, fmt.Errorf("%s must be a JSON argv array: %w", envName, err)
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("%s must contain at least one argv item", envName)
	}
	for i, arg := range argv {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			return nil, fmt.Errorf("%s arg %d is empty", envName, i)
		}
		if strings.ContainsAny(arg, "\x00\r\n") {
			return nil, fmt.Errorf("%s arg %d contains an unsafe control character", envName, i)
		}
		argv[i] = expandCloudflaredStartArg(arg, localURL)
	}
	if base := strings.ToLower(filepath.Base(argv[0])); base != "cloudflared" && base != "cloudflared.exe" {
		return nil, fmt.Errorf("%s must start with a cloudflared executable, got %q", envName, filepath.Base(argv[0]))
	}
	joined := strings.ToLower(strings.Join(argv, "\x00"))
	for _, forbidden := range []string{"executionpolicy", "encodedcommand", "bypass", "powershell", "cmd.exe", "bash", " zsh", " sh ", "nohup"} {
		if strings.Contains(joined, forbidden) {
			return nil, fmt.Errorf("%s contains unsafe shell or policy token %q", envName, forbidden)
		}
	}
	return argv, nil
}

func expandCloudflaredStartArg(arg, localURL string) string {
	replacements := map[string]string{
		"{local_url}":       localURL,
		"{{local_url}}":     localURL,
		"$RDEV_LOCAL_URL":   localURL,
		"${RDEV_LOCAL_URL}": localURL,
	}
	for marker, value := range replacements {
		arg = strings.ReplaceAll(arg, marker, value)
	}
	return arg
}

func redactCloudflaredArgv(argv []string) []string {
	out := append([]string(nil), argv...)
	for i, arg := range out {
		switch arg {
		case "--token", "--credentials-contents":
			if i+1 < len(out) {
				out[i+1] = "<redacted>"
			}
		}
	}
	return out
}

func startConfiguredCloudflaredStableTunnel(ctx context.Context, stderr io.Writer, cfg cloudflaredStableTunnelConfig) (string, context.CancelFunc, error) {
	if len(cfg.Argv) == 0 {
		return "", func() {}, fmt.Errorf("cloudflared stable tunnel argv is empty")
	}
	tunnelCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(tunnelCtx, cfg.Argv[0], cfg.Argv[1:]...)
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return "", func() {}, fmt.Errorf("cloudflared stable tunnel start failed: %w", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		cancel()
		if err != nil {
			return "", func() {}, fmt.Errorf("cloudflared stable tunnel exited during startup: %w", err)
		}
		return "", func() {}, fmt.Errorf("cloudflared stable tunnel exited during startup")
	case <-time.After(3 * time.Second):
		return cfg.GatewayURL, func() {
			cancel()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
			}
		}, nil
	}
}

// startLocalhostRunTunnel creates a public HTTPS tunnel through localhost.run
// using only an OpenSSH client (available on macOS, Linux, and Windows 10+).
// It runs: ssh -o StrictHostKeyChecking=no -R 80:localhost:PORT nokey@localhost.run
// and scans stdout/stderr for the assigned https://*.localhost.run URL.
func startLocalhostRunTunnel(ctx context.Context, stderr io.Writer, localPort string) (string, context.CancelFunc, error) {
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return "", func() {}, fmt.Errorf("ssh not found in PATH: %w", err)
	}
	tunnelCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(tunnelCtx, sshPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ExitOnForwardFailure=yes",
		"-R", "80:localhost:"+localPort,
		"nokey@localhost.run",
	)
	pr, pw := io.Pipe()
	combined := io.MultiWriter(pw, stderr)
	cmd.Stdout = combined
	cmd.Stderr = combined
	if err := cmd.Start(); err != nil {
		cancel()
		_ = pw.Close()
		return "", func() {}, fmt.Errorf("localhost.run ssh start failed: %w", err)
	}
	urlCh := make(chan string, 1)
	go func() {
		defer pw.Close()
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			line := scanner.Text()
			if idx := strings.Index(line, "https://"); idx >= 0 {
				rest := line[idx:]
				end := strings.IndexAny(rest, " \t\n\r")
				if end < 0 {
					end = len(rest)
				}
				candidate := strings.TrimRight(rest[:end], "/")
				if strings.Contains(candidate, ".localhost.run") || strings.Contains(candidate, ".lhr.life") {
					select {
					case urlCh <- candidate:
					default:
					}
				}
			}
		}
	}()
	select {
	case tunnelURL := <-urlCh:
		return tunnelURL, cancel, nil
	case <-time.After(20 * time.Second):
		cancel()
		_ = pr.Close()
		return "", func() {}, fmt.Errorf("localhost.run did not print a tunnel URL within 20s")
	}
}

// in the background and waits up to 30 seconds for it to print a public HTTPS tunnel URL.
//
// It always uses --protocol http2 first because many networks (particularly those behind
// corporate firewalls or in China) block UDP/QUIC. If http2 fails to produce a URL within
// the timeout the function returns an error; the caller can retry or fall back to another
// tunnel provider.
//
// The returned cancel function must be called when the session ends to clean up the
// cloudflared process.
//
// On success it returns (tunnelURL, cancel, nil).
// On failure it returns ("", noop, err).
func startCloudflaredQuickTunnel(ctx context.Context, stderr io.Writer, localURL string) (string, context.CancelFunc, error) {
	cfPath, err := exec.LookPath("cloudflared")
	if err != nil {
		return "", func() {}, fmt.Errorf("cloudflared not found in PATH: %w", err)
	}

	// Attempt 1: force HTTP/2 (TCP-based, works in networks that block QUIC/UDP).
	tunnelURL, cancel, err := startCloudflaredWithProtocol(ctx, cfPath, stderr, localURL, "http2", 25*time.Second)
	if err == nil {
		return tunnelURL, cancel, nil
	}
	_, _ = fmt.Fprintf(stderr, "[rdev] cloudflared http2 attempt failed (%v); retrying without protocol flag\n", err)

	// Attempt 2: let cloudflared choose the protocol (QUIC, then http2 fallback).
	// Some older cloudflared versions do not accept --protocol http2, so this
	// second attempt keeps backward compatibility.
	return startCloudflaredWithProtocol(ctx, cfPath, stderr, localURL, "", 20*time.Second)
}

// startCloudflaredWithProtocol is the inner helper that launches a single cloudflared
// process with an optional --protocol flag and scans its stderr for a trycloudflare.com URL.
func startCloudflaredWithProtocol(ctx context.Context, cfPath string, stderr io.Writer, localURL, protocol string, timeout time.Duration) (string, context.CancelFunc, error) {
	tunnelCtx, cancel := context.WithCancel(ctx)

	args := []string{"tunnel"}
	if protocol != "" {
		args = append(args, "--protocol", protocol)
	}
	args = append(args, "--url", localURL)

	cmd := exec.CommandContext(tunnelCtx, cfPath, args...)
	// cloudflared prints the tunnel URL to stderr
	pr, pw := io.Pipe()
	cmd.Stderr = io.MultiWriter(pw, stderr)
	if err := cmd.Start(); err != nil {
		cancel()
		_ = pw.Close()
		return "", func() {}, fmt.Errorf("cloudflared start failed (protocol=%q): %w", protocol, err)
	}

	// Scan stderr for the tunnel URL (looks like https://*.trycloudflare.com).
	// Candidates must pass URL parsing to prevent garbled error lines from being
	// accepted as tunnel addresses (cloudflared occasionally writes error text on
	// the same line as a partial URL when the tunnel fails to establish).
	urlCh := make(chan string, 1)
	go func() {
		defer pw.Close()
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			line := scanner.Text()
			if idx := strings.Index(line, "https://"); idx >= 0 {
				rest := line[idx:]
				end := strings.IndexAny(rest, " \t\n\r|")
				if end < 0 {
					end = len(rest)
				}
				candidate := strings.TrimRight(rest[:end], "/")
				if !strings.Contains(candidate, ".trycloudflare.com") {
					continue
				}
				// Strict validation: must parse as a URL with https scheme and
				// a non-empty host that does not contain whitespace or error
				// keywords that indicate cloudflared wrote a diagnostic line
				// rather than an actual tunnel address.
				u, err := url.Parse(candidate)
				if err != nil || u.Scheme != "https" || u.Host == "" {
					continue
				}
				if strings.ContainsAny(u.Host, " \t\n\r") {
					continue
				}
				select {
				case urlCh <- candidate:
				default:
				}
			}
		}
	}()

	select {
	case tunnelURL := <-urlCh:
		return tunnelURL, cancel, nil
	case <-time.After(timeout):
		cancel()
		_ = pr.Close()
		return "", func() {}, fmt.Errorf("cloudflared (protocol=%q) did not print a tunnel URL within %v", protocol, timeout)
	}
}

func (a App) supportSessionStart(ctx context.Context, opts supportSessionStartOptions) error {
	if opts.TTLSeconds < 60 || opts.TTLSeconds > 86400 {
		return fmt.Errorf("ttl-seconds must be between 60 and 86400")
	}
	opts.RdevCommand = agentRdevCommand(opts.RdevCommand)
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = "0.0.0.0:8787"
	}
	// Automatically find a free port when the preferred address is already bound.
	// This avoids cryptic "bind: address already in use" errors when multiple
	// support sessions are started on the same machine (e.g. concurrent agent runs).
	addr = findFreeAddr(addr)
	localPort := extractPort(addr, "8787")
	localListenURL := "http://127.0.0.1:" + localPort
	gatewayURL, candidates := supportsession.ResolveGatewayURL(addr, opts.GatewayURL)
	if stableGatewayURL := firstStableGatewayURL(candidates); stableGatewayURL != "" {
		gatewayURL = stableGatewayURL
	}

	stableTunnelStartFailed := false
	if strings.TrimSpace(opts.GatewayURL) == "" {
		cfPath, cfErr := exec.LookPath("cloudflared")
		if cfErr != nil {
			if cloudflaredStableTunnelStartRequested() {
				stableTunnelStartFailed = true
				_, _ = fmt.Fprintf(a.Stderr, "[rdev] configured Cloudflare stable tunnel requested but cloudflared is not in PATH: %v\n", cfErr)
			}
		} else {
			cfg, ok, err := configuredCloudflaredStableTunnelConfig(cfPath, localListenURL)
			if err != nil {
				stableTunnelStartFailed = true
				_, _ = fmt.Fprintf(a.Stderr, "[rdev] configured Cloudflare stable tunnel is invalid: %v\n", err)
			} else if ok {
				_, _ = fmt.Fprintf(a.Stderr, "[rdev] starting configured Cloudflare stable tunnel for %s with %s (%s)\n", cfg.GatewayURL, cfg.Mode, strings.Join(cfg.Preview, " "))
				tunnelURL, tunnelCancel, err := startConfiguredCloudflaredStableTunnel(ctx, a.Stderr, cfg)
				if err == nil {
					defer tunnelCancel()
					gatewayURL = tunnelURL
					_ = os.Setenv("RDEV_CLOUDFLARED_GATEWAY_URL", tunnelURL)
					_, _ = fmt.Fprintf(a.Stderr, "[rdev] configured Cloudflare stable tunnel active: %s\n", tunnelURL)
				} else {
					stableTunnelStartFailed = true
					_, _ = fmt.Fprintf(a.Stderr, "[rdev] configured Cloudflare stable tunnel failed: %v\n", err)
				}
			}
		}
	}

	// Start an ephemeral public tunnel when no stable
	// (hosted/cloudflared/relay/mesh/vpn/ssh) gateway is already configured, or
	// when the configured Cloudflare stable tunnel failed to start. The fallback
	// providers are tried in order: cloudflared Quick Tunnel (HTTP/2 first, then
	// provider default) → localhost.run SSH. LAN-only fallback applies only when
	// all providers fail.
	summary := supportsession.GatewayCandidateSummary(candidates)
	needsPublicTunnel, _ := summary["needs_public_tunnel"].(bool)
	if shouldStartManagedPublicTunnel(needsPublicTunnel || stableTunnelStartFailed, opts.GatewayURL) {
		tunnelURL, tunnelCancel := startBestAvailableTunnel(ctx, a.Stderr, localListenURL, localPort)
		if tunnelURL != "" {
			defer tunnelCancel()
			gatewayURL = tunnelURL
			_ = os.Setenv("RDEV_CLOUDFLARED_GATEWAY_URL", tunnelURL)
		}
	}
	workDir := strings.TrimSpace(opts.WorkDir)
	if workDir == "" {
		if wd, err := os.Getwd(); err == nil {
			if repoRoot := findSupportSessionRepoRoot(wd); repoRoot != "" {
				workDir = filepath.Join(repoRoot, "work", "rdev-support-session")
			}
		}
		if workDir == "" {
			workDir = filepath.Join(".", "work", "rdev-support-session")
		}
	}
	if absWorkDir, err := filepath.Abs(workDir); err == nil {
		workDir = absWorkDir
	}
	readyFile := strings.TrimSpace(opts.ReadyFile)
	if readyFile == "" {
		readyFile = filepath.Join(workDir, "support-session-ready.json")
	}
	if absReadyFile, err := filepath.Abs(readyFile); err == nil {
		readyFile = absReadyFile
	}
	statusFile := strings.TrimSpace(opts.StatusFile)
	if statusFile == "" {
		statusFile = filepath.Join(workDir, "support-session-status.json")
	}
	if absStatusFile, err := filepath.Abs(statusFile); err == nil {
		statusFile = absStatusFile
	}
	handoffTextFile := strings.TrimSpace(opts.HandoffTextFile)
	if handoffTextFile == "" {
		handoffTextFile = filepath.Join(workDir, "target-handoff.txt")
	}
	if absHandoffTextFile, err := filepath.Abs(handoffTextFile); err == nil {
		handoffTextFile = absHandoffTextFile
	}
	connectedReportFile := strings.TrimSpace(opts.ConnectedReportFile)
	if connectedReportFile == "" {
		connectedReportFile = filepath.Join(workDir, "connected-report.txt")
	}
	if absConnectedReportFile, err := filepath.Abs(connectedReportFile); err == nil {
		connectedReportFile = absConnectedReportFile
	}
	repoRoot := strings.TrimSpace(opts.RepoRoot)
	if repoRoot == "" {
		repoRoot = "."
	}
	repoRoot = resolveSupportSessionRepoRoot(repoRoot)
	prepared, err := prepareSupportSessionEnvironment(ctx, supportSessionPrepareOptions{
		RepoRoot:    repoRoot,
		WorkDir:     workDir,
		Addr:        addr,
		GatewayURL:  gatewayURL,
		Target:      opts.Target,
		BuildAssets: true,
		RdevCommand: opts.RdevCommand,
	})
	if err != nil {
		return err
	}
	gatewayCandidates, _ := prepared["gateway_url_candidates"].([]supportsession.GatewayURLCandidate)

	// Pre-check: verify that platform helper binaries were actually built into
	// workDir/bin before the gateway starts serving /assets/* routes.
	//
	// This catches the common mistake of running `prepare --build-assets` with
	// one --work-dir and then `connect --start` with a different --work-dir,
	// which leaves the gateway unable to serve the helper and causes a 404 on
	// the target side. Fail closed: the one-command target handoff must never be
	// generated until target-side self-repair assets are ready.
	if assetReport, ok := prepared["asset_report"].(map[string]any); ok {
		allReady, _ := assetReport["all_ready"].(bool)
		if !allReady {
			// Identify which platform binary is missing so the error message is
			// actionable rather than generic.
			binDir := filepath.Join(workDir, "bin")
			type platformCheck struct {
				name string
				path string
			}
			checks := []platformCheck{
				{"windows-amd64", filepath.Join(binDir, "rdev-windows-amd64.exe")},
				{"darwin-arm64", filepath.Join(binDir, "rdev-darwin-arm64")},
				{"darwin-amd64", filepath.Join(binDir, "rdev-darwin-amd64")},
				{"linux-amd64", filepath.Join(binDir, "rdev-linux-amd64")},
				{"linux-arm64", filepath.Join(binDir, "rdev-linux-arm64")},
			}
			var missing []string
			for _, c := range checks {
				if _, statErr := os.Stat(c.path); os.IsNotExist(statErr) {
					missing = append(missing, c.name)
				}
			}
			return fmt.Errorf("support-session connect cannot generate a target handoff until helper assets are ready; missing platform helpers %v in %s; rerun the standard recovery command: rdev support-session connect --start --repo-root %s --work-dir %s", missing, binDir, repoRoot, workDir)
		}
	}

	if err := os.MkdirAll(filepath.Join(workDir, "bin"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(workDir, ".rdev", "keys"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(workDir, ".rdev", "gateway"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(workDir, ".rdev", "audit"), 0o700); err != nil {
		return err
	}
	signingKeyPath := filepath.Join(workDir, ".rdev", "keys", "gateway-signing-key.json")
	manifestKeyPath := filepath.Join(workDir, ".rdev", "keys", "manifest-root-key.json")
	statePath := filepath.Join(workDir, ".rdev", "gateway", "state.json")
	auditLogPath := filepath.Join(workDir, ".rdev", "audit", "events.jsonl")
	key, _, err := signing.LoadOrCreate(signingKeyPath, signing.DefaultKeyID)
	if err != nil {
		return err
	}
	gw := gateway.NewMemoryGatewayWithSigningKey(time.Now, key.ID, key.PublicKey, key.PrivateKey)
	store, err := gateway.NewFileStateStore(statePath)
	if err != nil {
		return err
	}
	if _, _, err := store.LoadInto(gw); err != nil {
		return err
	}
	manifestKey, _, err := signing.LoadOrCreate(manifestKeyPath, "manifest-dev")
	if err != nil {
		return err
	}
	gw.WithManifestSigningKey(manifestKey.ID, manifestKey.PublicKey, manifestKey.PrivateKey)
	auditStore := audit.NewJSONLStore(auditLogPath)
	gw.WithAuditSink(&auditStore)
	metadata := map[string]string{
		"connection_entry":  "standard-visible",
		"approval_contract": "target-consent-scoped-ticket",
	}
	if opts.AutoApprove {
		metadata["auto_approve"] = "attended-temporary"
	}
	ticket, err := gw.CreateTicketWithMetadata(
		model.HostModeAttendedTemporary,
		opts.TTLSeconds,
		cliPolicyCapabilitiesToStrings(policy.TemporaryDefaults()),
		opts.Reason,
		metadata,
	)
	if err != nil {
		return err
	}
	if _, err := store.SaveFrom(gw); err != nil {
		return err
	}
	server := httpapi.NewServerWithStateStore(gw, store)
	server.Assets = httpapi.AssetConfig{
		RdevWindowsAMD64Path: filepath.Join(workDir, "bin", "rdev-windows-amd64.exe"),
		RdevDarwinARM64Path:  filepath.Join(workDir, "bin", "rdev-darwin-arm64"),
		RdevDarwinAMD64Path:  filepath.Join(workDir, "bin", "rdev-darwin-amd64"),
		RdevLinuxAMD64Path:   filepath.Join(workDir, "bin", "rdev-linux-amd64"),
		RdevLinuxARM64Path:   filepath.Join(workDir, "bin", "rdev-linux-arm64"),
	}
	created := supportsession.BuildCreated(supportsession.CreatedOptions{
		GatewayURL:            gatewayURL,
		GatewayURLCandidates:  gatewayCandidates,
		Ticket:                ticket,
		ManifestRootPublicKey: rootPublicKeyString(gw.ManifestRoot()),
		Target:                opts.Target,
		Locale:                opts.Locale,
		RdevCommand:           opts.RdevCommand,
		AutoApprove:           opts.AutoApprove,
	})
	started := supportsession.BuildStarted(supportsession.StartedOptions{
		Addr:                      addr,
		GatewayURL:                gatewayURL,
		WorkDir:                   workDir,
		ReadyFile:                 readyFile,
		StatusFile:                statusFile,
		HandoffTextFile:           handoffTextFile,
		ConnectedReportFile:       connectedReportFile,
		Created:                   created,
		AssetReport:               prepared["asset_report"],
		ConnectionReadiness:       prepared["connection_readiness"],
		ConnectivityStrategy:      prepared["connectivity_strategy"],
		GatewayCandidatePreflight: prepared["gateway_candidate_preflight"],
	})
	if err := writeJSONFile0600(readyFile, started); err != nil {
		return err
	}
	if err := writeSupportSessionHandoffTextFile0600(handoffTextFile, started); err != nil {
		return err
	}
	if err := writeJSON(a.Stdout, started); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.Stderr, "rdev support session ready payload written to %s\n", readyFile)
	_, _ = fmt.Fprintf(a.Stderr, "rdev support session target handoff written to %s\n", handoffTextFile)
	_, _ = fmt.Fprintf(a.Stderr, "rdev support session status file writing to %s\n", statusFile)
	_, _ = fmt.Fprintf(a.Stderr, "rdev support session connected report will be written to %s\n", connectedReportFile)
	_, _ = fmt.Fprintf(a.Stderr, "rdev support session gateway listening on %s\n", gatewayURL)
	go watchForegroundSupportSession(ctx, a.Stderr, statusFile, connectedReportFile, gw, ticket.Code, opts.Locale, gatewayURL)
	return listenAndServeGatewayContext(ctx, addr, server.Handler(), nil)
}

func watchForegroundSupportSession(ctx context.Context, out io.Writer, statusFile, connectedReportFile string, gw *gateway.MemoryGateway, ticketCode, locale, gatewayURL string) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	seenPending := false
	writeSupportSessionEvent(out, statusFile, "waiting", supportsession.BuildStatus(supportsession.StatusOptions{
		TicketCode: ticketCode,
		Hosts:      gw.HostsForTicketCode(ticketCode, ""),
		Locale:     locale,
		GatewayURL: gatewayURL,
	}))
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			status := supportsession.BuildStatus(supportsession.StatusOptions{
				TicketCode: ticketCode,
				Hosts:      gw.HostsForTicketCode(ticketCode, ""),
				Locale:     locale,
				GatewayURL: gatewayURL,
			})
			if status["connected"] == true {
				_ = writeSupportSessionConnectedReportFile0600(connectedReportFile, status)
				writeSupportSessionEvent(out, statusFile, "connected", status)
				return
			}
			if status["status"] == "pending-approval" && !seenPending {
				seenPending = true
				writeSupportSessionEvent(out, statusFile, "pending-approval", status)
			}
		}
	}
}

func writeSupportSessionEvent(out io.Writer, statusFile, event string, status map[string]any) {
	payload := map[string]any{
		"schema_version": "rdev.support-session-foreground-event.v1",
		"event":          event,
		"status":         status,
		"agent_rule":     "when event=connected or status.connected=true, immediately report status.connected_next_steps.user_report before creating jobs",
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if strings.TrimSpace(statusFile) != "" {
		_ = writeJSONFile0600(statusFile, payload)
	}
	_, _ = fmt.Fprintf(out, "rdev support session event: %s\n", string(content))
}

func (a App) supportSessionCreate(ctx context.Context, opts supportSessionCreateOptions) error {
	created, err := createSupportSessionPayload(ctx, opts)
	if err != nil {
		return err
	}
	return writeJSON(a.Stdout, created)
}

func createSupportSessionPayload(ctx context.Context, opts supportSessionCreateOptions) (map[string]any, error) {
	opts.RdevCommand = agentRdevCommand(opts.RdevCommand)
	gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	if gatewayURL == "" {
		gatewayURL, _ = supportsession.ConfiguredGatewayURLCandidate()
	}
	if gatewayURL == "" {
		return nil, fmt.Errorf("support-session create requires --gateway-url or a configured RDEV_*_GATEWAY_URL; run rdev support-session connect --start if no reachable gateway is running yet")
	}
	if opts.TTLSeconds < 60 || opts.TTLSeconds > 86400 {
		return nil, fmt.Errorf("ttl-seconds must be between 60 and 86400")
	}
	payload, err := createGatewayInviteTicket(ctx, http.DefaultClient, inviteCreateOptions{
		GatewayURL:        gatewayURL,
		Mode:              model.HostModeAttendedTemporary,
		TTLSeconds:        opts.TTLSeconds,
		Reason:            opts.Reason,
		Capabilities:      cliPolicyCapabilitiesToStrings(policy.TemporaryDefaults()),
		Transport:         "auto",
		NetworkScope:      "auto",
		AuthorityProfile:  "standard",
		OperatorTokenFile: opts.OperatorTokenFile,
		RdevCommand:       opts.RdevCommand,
		Once:              false,
		AutoApprove:       opts.AutoApprove,
	})
	if err != nil {
		return nil, err
	}
	created := supportsession.BuildCreated(supportsession.CreatedOptions{
		GatewayURL:               gatewayURL,
		GatewayURLCandidates:     supportsession.GatewayURLCandidatesFromIPs("0.0.0.0:8787", gatewayURL, nil),
		JoinURL:                  payload.JoinURL,
		ManifestURL:              payload.ManifestURL,
		ManifestRootPublicKey:    payload.ManifestRootPublicKey,
		Ticket:                   payload.Ticket,
		Target:                   opts.Target,
		Locale:                   opts.Locale,
		RdevCommand:              opts.RdevCommand,
		AutoApprove:              opts.AutoApprove,
		TargetBootstrapReadiness: probeTargetBootstrapReadiness(ctx, http.DefaultClient, gatewayURL, opts.Target),
	})
	return created, nil
}

func probeTargetBootstrapReadiness(ctx context.Context, client *http.Client, gatewayURL, target string) map[string]any {
	target = normalizeSupportSessionTarget(target)
	assets := supportSessionRequiredAssets(target)
	results := make([]map[string]any, 0, len(assets))
	allReady := true
	for _, asset := range assets {
		status := probeGatewayAsset(ctx, client, gatewayURL, asset)
		if status["ok"] != true {
			allReady = false
		}
		results = append(results, status)
	}
	return map[string]any{
		"schema_version": "rdev.support-session-target-bootstrap-readiness.v1",
		"target":         target,
		"all_ready":      allReady,
		"probed":         len(assets) > 0,
		"assets":         results,
		"agent_rule":     "if all_ready is false for a platform terminal command, run rdev support-session connect --start or prepare --build-assets instead of asking the target user to install rdev manually",
	}
}

func supportSessionRequiredAssets(target string) []string {
	switch target {
	case "windows":
		return []string{"rdev-windows-amd64.exe.sha256"}
	case "macos":
		return []string{"rdev-darwin-arm64.sha256", "rdev-darwin-amd64.sha256"}
	case "linux":
		return []string{"rdev-linux-amd64.sha256", "rdev-linux-arm64.sha256"}
	default:
		return []string{"rdev-windows-amd64.exe.sha256", "rdev-darwin-arm64.sha256", "rdev-darwin-amd64.sha256", "rdev-linux-amd64.sha256", "rdev-linux-arm64.sha256"}
	}
}

func probeGatewayAsset(ctx context.Context, client *http.Client, gatewayURL, asset string) map[string]any {
	endpoint := strings.TrimRight(strings.TrimSpace(gatewayURL), "/") + "/assets/" + asset
	report := map[string]any{
		"asset": asset,
		"url":   endpoint,
		"ok":    false,
	}
	if strings.TrimSpace(gatewayURL) == "" {
		report["error"] = "gateway_url is required"
		return report
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		report["error"] = err.Error()
		return report
	}
	resp, err := doGatewayRequest(client, req)
	if err != nil {
		report["error"] = err.Error()
		return report
	}
	defer resp.Body.Close()
	report["status_code"] = resp.StatusCode
	report["ok"] = resp.StatusCode >= 200 && resp.StatusCode < 300
	return report
}

func normalizeSupportSessionTarget(target string) string {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "windows", "win":
		return "windows"
	case "macos", "darwin", "mac":
		return "macos"
	case "linux":
		return "linux"
	default:
		return "auto"
	}
}

func prepareSupportSessionEnvironment(ctx context.Context, opts supportSessionPrepareOptions) (map[string]any, error) {
	opts.RepoRoot = resolveSupportSessionRepoRoot(opts.RepoRoot)
	opts.RdevCommand = agentRdevCommand(opts.RdevCommand)
	return supportsession.Prepare(ctx, supportsession.PrepareOptions{
		RepoRoot:    opts.RepoRoot,
		WorkDir:     opts.WorkDir,
		Addr:        opts.Addr,
		GatewayURL:  opts.GatewayURL,
		Target:      opts.Target,
		BuildAssets: opts.BuildAssets,
		RdevCommand: opts.RdevCommand,
	})
}

func findSupportSessionRepoRoot(start string) string {
	start = strings.TrimSpace(start)
	if start == "" {
		return ""
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	if info, statErr := os.Stat(dir); statErr == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if supportSessionRepoRootValid(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func resolveSupportSessionRepoRoot(hint string) string {
	candidates := []string{
		strings.TrimSpace(hint),
		strings.TrimSpace(os.Getenv("RDEV_SOURCE_ROOT")),
	}
	if buildinfo.SourceRoot != "" && buildinfo.SourceRoot != "unknown" {
		candidates = append(candidates, buildinfo.SourceRoot)
	}
	if currentExecutable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(currentExecutable))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	for _, candidate := range candidates {
		if found := findSupportSessionRepoRoot(candidate); found != "" {
			return found
		}
	}
	if strings.TrimSpace(hint) != "" {
		return hint
	}
	return "."
}

func cliPolicyCapabilitiesToStrings(caps []policy.Capability) []string {
	values := make([]string, 0, len(caps))
	for _, cap := range caps {
		values = append(values, string(cap))
	}
	return values
}

func rootPublicKeyString(root model.TrustBundle) string {
	if root.SigningKeyID == "" || root.PublicKey == "" {
		return ""
	}
	return root.SigningKeyID + ":" + root.PublicKey
}

type supportSessionStatusOptions struct {
	GatewayURL string
	TicketCode string
	Locale     string
	Wait       bool
	Timeout    time.Duration
	Interval   time.Duration
}

type supportSessionRecoverOptions struct {
	GatewayURL        string
	TicketCode        string
	Locale            string
	OperatorTokenFile string
}

type supportSessionReportOptions struct {
	GatewayURL        string
	HostID            string
	TicketCode        string
	Locale            string
	OperatorTokenFile string
}

func supportSessionStatus(ctx context.Context, client *http.Client, opts supportSessionStatusOptions) (map[string]any, error) {
	gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	if gatewayURL == "" {
		gatewayURL, _ = supportsession.ConfiguredGatewayURLCandidate()
	}
	if gatewayURL == "" {
		return nil, fmt.Errorf("support-session status requires --gateway-url or a configured RDEV_*_GATEWAY_URL")
	}
	if strings.TrimSpace(opts.TicketCode) == "" {
		return nil, fmt.Errorf("support-session status requires --ticket-code")
	}
	opts.GatewayURL = gatewayURL
	if opts.Interval <= 0 {
		opts.Interval = time.Second
	}
	deadline := time.Now().Add(opts.Timeout)
	for {
		status, err := fetchSupportSessionStatus(ctx, client, opts)
		if err != nil {
			return nil, err
		}
		if !opts.Wait || status["connected"] == true || status["status"] == "pending-approval" {
			return status, nil
		}
		if opts.Timeout > 0 && time.Now().After(deadline) {
			status["ok"] = false
			status["timed_out"] = true
			status["next_action"] = "Keep waiting, or check gateway reachability, network path, and target command output."
			statusText, _ := status["status"].(string)
			status["connection_recovery"] = supportsession.BuildConnectionRecovery(supportsession.ConnectionRecoveryOptions{
				Status:     statusText,
				TicketCode: opts.TicketCode,
				Locale:     opts.Locale,
				TimedOut:   true,
			})
			return status, nil
		}
		timer := time.NewTimer(opts.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func fetchSupportSessionStatus(ctx context.Context, client *http.Client, opts supportSessionStatusOptions) (map[string]any, error) {
	endpoint := strings.TrimRight(opts.GatewayURL, "/") + "/v1/support-session/status"
	values := url.Values{}
	values.Set("ticket_code", opts.TicketCode)
	if strings.TrimSpace(opts.Locale) != "" {
		values.Set("locale", opts.Locale)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := doGatewayRequest(client, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if message, _ := payload["error"].(string); message != "" {
			return nil, fmt.Errorf("support-session status failed: %s", message)
		}
		return nil, fmt.Errorf("support-session status failed: %s", resp.Status)
	}
	return payload, nil
}

func (a App) supportSessionRecover(ctx context.Context, opts supportSessionRecoverOptions) error {
	gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	if gatewayURL == "" {
		gatewayURL, _ = supportsession.ConfiguredGatewayURLCandidate()
	}
	if gatewayURL == "" {
		return fmt.Errorf("support-session recover requires --gateway-url or a configured RDEV_*_GATEWAY_URL")
	}
	if strings.TrimSpace(opts.TicketCode) == "" {
		return fmt.Errorf("support-session recover requires --ticket-code")
	}
	token := loadOperatorToken(opts.OperatorTokenFile)
	status, err := supportSessionStatus(ctx, http.DefaultClient, supportSessionStatusOptions{
		GatewayURL: gatewayURL,
		TicketCode: opts.TicketCode,
		Locale:     opts.Locale,
	})
	if err != nil {
		return err
	}
	staleHosts := mapSlice(status["stale_hosts"])
	recoveredHosts := []map[string]any{}
	canceledJobs := []map[string]any{}
	errorsOut := []string{}
	for _, host := range staleHosts {
		hostID, _ := host["id"].(string)
		if strings.TrimSpace(hostID) == "" {
			continue
		}
		jobs, err := fetchGatewayJobsForHost(ctx, gatewayURL, hostID, token)
		if err != nil {
			errorsOut = append(errorsOut, fmt.Sprintf("list jobs for %s: %v", hostID, err))
		} else {
			for _, job := range jobs {
				jobID, _ := job["id"].(string)
				jobStatus, _ := job["status"].(string)
				if jobID == "" || !recoverableJobStatus(jobStatus) {
					continue
				}
				if _, _, err := postGatewayJSON(ctx, gatewayURL+"/v1/jobs/"+url.PathEscape(jobID)+"/cancel", map[string]any{
					"reason": "support-session recover canceled job for stale host " + hostID,
				}, token); err != nil {
					errorsOut = append(errorsOut, fmt.Sprintf("cancel job %s: %v", jobID, err))
					continue
				}
				canceledJobs = append(canceledJobs, map[string]any{
					"id":              jobID,
					"previous_status": jobStatus,
					"host_id":         hostID,
				})
			}
		}
		if _, _, err := postGatewayJSON(ctx, gatewayURL+"/v1/hosts/"+url.PathEscape(hostID)+"/revoke", map[string]any{
			"reason": "support-session recover revoked stale host",
		}, token); err != nil {
			errorsOut = append(errorsOut, fmt.Sprintf("revoke host %s: %v", hostID, err))
			continue
		}
		recoveredHosts = append(recoveredHosts, map[string]any{
			"id":              hostID,
			"previous_status": host["status"],
			"name":            host["name"],
			"os":              host["os"],
			"arch":            host["arch"],
		})
	}
	statusAfter, statusErr := supportSessionStatus(ctx, http.DefaultClient, supportSessionStatusOptions{
		GatewayURL: gatewayURL,
		TicketCode: opts.TicketCode,
		Locale:     opts.Locale,
	})
	if statusErr != nil {
		errorsOut = append(errorsOut, "status after recovery: "+statusErr.Error())
	}
	payload := map[string]any{
		"schema_version":      "rdev.support-session-recovery.v1",
		"ok":                  len(errorsOut) == 0,
		"gateway_url":         gatewayURL,
		"ticket_code":         opts.TicketCode,
		"stale_hosts_seen":    len(staleHosts),
		"hosts_revoked":       recoveredHosts,
		"jobs_canceled":       canceledJobs,
		"errors":              errorsOut,
		"status_before":       status,
		"status_after":        statusAfter,
		"agent_next_step":     "If status_after.connected is false, resend the existing target_handoff_envelope.full_text when the ticket is still active, or create a fresh support-session with rdev support-session connect --start.",
		"human_surface_rule":  "Do not ask the target human to assemble manifest, root key, gateway URL, transport, or ticket values.",
		"standard_next_tools": []string{"rdev support-session status --wait", "rdev support-session connect --start"},
	}
	return writeJSON(a.Stdout, payload)
}

func mapSlice(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func recoverableJobStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "queued", "running":
		return true
	default:
		return false
	}
}

func fetchGatewayJobsForHost(ctx context.Context, gatewayURL, hostID, bearerToken string) ([]map[string]any, error) {
	endpoint := strings.TrimRight(gatewayURL, "/") + "/v1/jobs?host_id=" + url.QueryEscape(hostID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(bearerToken) != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := doGatewayRequest(http.DefaultClient, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if message, _ := payload["error"].(string); message != "" {
			return nil, errors.New(message)
		}
		return nil, errors.New(resp.Status)
	}
	return mapSlice(payload["jobs"]), nil
}

func (a App) supportSessionReport(ctx context.Context, opts supportSessionReportOptions) error {
	gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	if gatewayURL == "" {
		gatewayURL, _ = supportsession.ConfiguredGatewayURLCandidate()
	}
	if gatewayURL == "" {
		return fmt.Errorf("support-session report requires --gateway-url or a configured RDEV_*_GATEWAY_URL")
	}
	hostID := strings.TrimSpace(opts.HostID)
	ticketCode := strings.TrimSpace(opts.TicketCode)
	var status map[string]any
	if ticketCode != "" {
		var statusErr error
		status, statusErr = supportSessionStatus(ctx, http.DefaultClient, supportSessionStatusOptions{
			GatewayURL: gatewayURL,
			TicketCode: ticketCode,
			Locale:     opts.Locale,
		})
		if statusErr != nil {
			return statusErr
		}
		activeHosts := mapSlice(status["active_hosts"])
		if hostID == "" {
			if len(activeHosts) == 1 {
				hostID = stringFromMap(activeHosts[0], "id")
			} else {
				return writeJSON(a.Stdout, supportSessionTicketReportWithoutSelectedHost(gatewayURL, ticketCode, status, len(activeHosts)))
			}
		}
	}
	if hostID == "" {
		return fmt.Errorf("support-session report requires --host-id or --ticket-code")
	}
	token := loadOperatorToken(opts.OperatorTokenFile)
	hostPayload, err := fetchHostJSON(ctx, gatewayURL, hostID, token)
	if err != nil {
		return err
	}
	host := nestedMapOrSelf(hostPayload, "host")
	jobs, err := fetchGatewayJobsForHost(ctx, gatewayURL, hostID, token)
	if err != nil {
		return err
	}
	jobReports := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		jobID := stringFromMap(job, "id")
		artifactSummary := ""
		artifactCount := 0
		if jobID != "" {
			if artifactsPayload, artifactErr := fetchJobArtifactsJSON(ctx, gatewayURL, jobID, token); artifactErr == nil {
				if artifacts, _ := artifactsPayload["artifacts"].([]any); artifacts != nil {
					artifactCount = len(artifacts)
				}
				artifactSummary = summarizeFirstArtifact(artifactsPayload)
			}
		}
		jobReports = append(jobReports, map[string]any{
			"job_id":           jobID,
			"status":           stringFromMap(job, "status"),
			"adapter":          stringFromMap(job, "adapter"),
			"intent":           stringFromMap(job, "intent"),
			"artifact_count":   artifactCount,
			"artifact_summary": artifactSummary,
		})
	}
	report := map[string]any{
		"schema_version":              "rdev.support-session-report.v1",
		"ok":                          true,
		"gateway_url":                 gatewayURL,
		"ticket_code":                 ticketCode,
		"host_id":                     hostID,
		"recommended_job_host_id":     hostID,
		"recommended_job_host_source": "explicit_host_id",
		"host":                        host,
		"jobs":                        jobReports,
		"human_report":                supportSessionHumanReport(host, jobReports),
		"next_action":                 "Use this report instead of hand-written curl summaries; create jobs only for recommended_job_host_id and revoke the temporary session when remote work is complete.",
		"stale_host_rule":             "Do not create jobs for stale_hosts; run rdev support-session recover if stale hosts or queued jobs accumulated.",
	}
	if status != nil {
		report["status"] = status
		report["active_hosts"] = status["active_hosts"]
		report["stale_hosts"] = status["stale_hosts"]
		report["pending_hosts"] = status["pending_hosts"]
		report["host_count"] = status["host_count"]
		report["recommended_job_host_source"] = "ticket_single_active_host"
	}
	return writeJSON(a.Stdout, report)
}

func supportSessionTicketReportWithoutSelectedHost(gatewayURL, ticketCode string, status map[string]any, activeCount int) map[string]any {
	ok := false
	nextAction := "No active host is job-ready for this ticket. Wait with rdev support-session status --wait or run rdev support-session recover if stale hosts are present."
	if activeCount > 1 {
		nextAction = "Multiple active hosts are registered for this ticket; choose the intended host explicitly with --host-id before creating jobs."
	}
	return map[string]any{
		"schema_version":          "rdev.support-session-report.v1",
		"ok":                      ok,
		"gateway_url":             gatewayURL,
		"ticket_code":             ticketCode,
		"host_id":                 "",
		"recommended_job_host_id": "",
		"status":                  status,
		"active_hosts":            status["active_hosts"],
		"stale_hosts":             status["stale_hosts"],
		"pending_hosts":           status["pending_hosts"],
		"host_count":              status["host_count"],
		"next_action":             nextAction,
		"stale_host_rule":         "Do not create jobs for stale_hosts; stale means the runner is not job-ready.",
	}
}

func supportSessionHumanReport(host map[string]any, jobs []map[string]any) string {
	var b strings.Builder
	hostName := firstReportField(host, "name", "hostname", "id")
	hostOS := firstReportField(host, "os")
	hostArch := firstReportField(host, "arch")
	if hostName == "" {
		hostName = "unknown-host"
	}
	_, _ = fmt.Fprintf(&b, "Remote Dev Skillkit support-session report\n")
	_, _ = fmt.Fprintf(&b, "- Host: %s", hostName)
	if hostOS != "" || hostArch != "" {
		_, _ = fmt.Fprintf(&b, " (%s %s)", hostOS, hostArch)
	}
	_, _ = fmt.Fprintf(&b, "\n- Jobs reviewed: %d\n", len(jobs))
	for _, job := range jobs {
		_, _ = fmt.Fprintf(&b, "- %s: %s", job["job_id"], job["status"])
		if intent, _ := job["intent"].(string); intent != "" {
			_, _ = fmt.Fprintf(&b, " - %s", intent)
		}
		if summary, _ := job["artifact_summary"].(string); summary != "" {
			_, _ = fmt.Fprintf(&b, " | evidence: %s", oneLine(summary))
		}
		_, _ = fmt.Fprint(&b, "\n")
	}
	_, _ = fmt.Fprint(&b, "- Cleanup: revoke or close the temporary session when finished.")
	return b.String()
}

func firstReportField(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringFromMap(payload, key); value != "" {
			return value
		}
	}
	return ""
}

func oneLine(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 240 {
		return value[:240] + "..."
	}
	return value
}

func postGatewayJSON(ctx context.Context, endpoint string, body any, bearerToken string) (map[string]any, int, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(bearerToken) != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := doGatewayRequest(http.DefaultClient, req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if message, _ := payload["error"].(string); message != "" {
			return payload, resp.StatusCode, errors.New(message)
		}
		return payload, resp.StatusCode, errors.New(resp.Status)
	}
	return payload, resp.StatusCode, nil
}

type supportSessionAuditCapabilitiesOptions struct {
	GatewayURL        string
	HostID            string
	Timeout           time.Duration
	OperatorTokenFile string
}

type supportSessionAuditProbe struct {
	Name        string
	Capability  string
	Adapter     string
	Intent      string
	Policy      map[string]any
	FailureIsOK bool
}

func (a App) supportSessionAuditCapabilities(ctx context.Context, opts supportSessionAuditCapabilitiesOptions) error {
	gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	if gatewayURL == "" {
		gatewayURL, _ = supportsession.ConfiguredGatewayURLCandidate()
	}
	if gatewayURL == "" {
		return fmt.Errorf("support-session audit-capabilities requires --gateway-url or a configured RDEV_*_GATEWAY_URL")
	}
	if strings.TrimSpace(opts.HostID) == "" {
		return fmt.Errorf("--host-id is required")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	token := loadOperatorToken(opts.OperatorTokenFile)
	host, err := fetchHostJSON(ctx, gatewayURL, opts.HostID, token)
	if err != nil {
		return err
	}
	hostOS, _ := host["os"].(string)
	probes := auditCapabilityProbes(hostOS)
	results := make([]map[string]any, 0, len(probes))
	ok := true
	for _, probe := range probes {
		created, err := createJobJSON(ctx, gatewayURL, opts.HostID, probe.Adapter, probe.Intent, probe.Policy, token)
		if err != nil {
			ok = false
			results = append(results, map[string]any{
				"name":       probe.Name,
				"capability": probe.Capability,
				"status":     "create_failed",
				"ok":         false,
				"error":      err.Error(),
			})
			continue
		}
		jobID := stringFromNestedMap(created, "job", "id")
		if jobID == "" {
			ok = false
			results = append(results, map[string]any{
				"name":       probe.Name,
				"capability": probe.Capability,
				"status":     "create_failed",
				"ok":         false,
				"error":      "gateway response missing job.id",
			})
			continue
		}
		job, waitErr := waitJobJSON(ctx, gatewayURL, jobID, token, 500*time.Millisecond)
		jobStatus := stringFromMap(job, "status")
		if waitErr != nil {
			ok = false
			results = append(results, map[string]any{
				"name":       probe.Name,
				"capability": probe.Capability,
				"job_id":     jobID,
				"status":     "wait_failed",
				"ok":         false,
				"error":      waitErr.Error(),
			})
			continue
		}
		passed := jobStatus == string(model.JobStatusSucceeded)
		if probe.FailureIsOK && (jobStatus == string(model.JobStatusFailed) || jobStatus == string(model.JobStatusCanceled)) {
			passed = true
		}
		if !passed {
			ok = false
		}
		artifactSummary := ""
		if artifacts, artifactErr := fetchJobArtifactsJSON(ctx, gatewayURL, jobID, token); artifactErr == nil {
			artifactSummary = summarizeFirstArtifact(artifacts)
		}
		results = append(results, map[string]any{
			"name":             probe.Name,
			"capability":       probe.Capability,
			"job_id":           jobID,
			"job_status":       jobStatus,
			"ok":               passed,
			"expected_failure": probe.FailureIsOK,
			"artifact_summary": artifactSummary,
		})
	}
	report := map[string]any{
		"schema_version": "rdev.support-session-capability-audit.v1",
		"ok":             ok,
		"gateway_url":    gatewayURL,
		"host":           host,
		"results":        results,
		"next_action":    "Use rdev job create/wait/artifacts for scoped work only after this audit is ok.",
	}
	if err := writeJSON(a.Stdout, report); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("support-session capability audit failed")
	}
	return nil
}

func auditCapabilityProbes(hostOS string) []supportSessionAuditProbe {
	if strings.EqualFold(hostOS, "windows") {
		return []supportSessionAuditProbe{
			{Name: "identity", Capability: "shell.user", Adapter: "shell", Intent: "capability audit identity", Policy: shellAuditPolicy([]string{"shell.user"}, []string{"cmd", "/c", "hostname && whoami && cd"}, []string{"cmd"})},
			{Name: "fs_read", Capability: "fs.read", Adapter: "shell", Intent: "capability audit scoped read", Policy: shellAuditPolicy([]string{"shell.user", "fs.read"}, []string{"cmd", "/c", "dir /b ."}, []string{"cmd"})},
			{Name: "fs_write_scoped", Capability: "fs.write.scoped", Adapter: "shell", Intent: "capability audit scoped write", Policy: shellAuditPolicyWithWriteScope([]string{"shell.user", "fs.write.scoped"}, []string{"cmd", "/c", "echo rdev-audit> rdev_capability_audit.txt && type rdev_capability_audit.txt && del rdev_capability_audit.txt && if not exist rdev_capability_audit.txt echo deleted"}, []string{"cmd"}, []string{"."})},
			{Name: "process_inspect", Capability: "process.inspect", Adapter: "shell", Intent: "capability audit process inspect", Policy: shellAuditPolicy([]string{"shell.user", "process.inspect"}, []string{"tasklist"}, []string{"tasklist"})},
		}
	}
	return []supportSessionAuditProbe{
		{Name: "identity", Capability: "shell.user", Adapter: "shell", Intent: "capability audit identity", Policy: shellAuditPolicy([]string{"shell.user"}, []string{"sh", "-c", "hostname && whoami && pwd"}, []string{"sh"})},
		{Name: "fs_read", Capability: "fs.read", Adapter: "shell", Intent: "capability audit scoped read", Policy: shellAuditPolicy([]string{"shell.user", "fs.read"}, []string{"sh", "-c", "ls -la . | head -40"}, []string{"sh"})},
		{Name: "fs_write_scoped", Capability: "fs.write.scoped", Adapter: "shell", Intent: "capability audit scoped write", Policy: shellAuditPolicyWithWriteScope([]string{"shell.user", "fs.write.scoped"}, []string{"sh", "-c", "printf rdev-audit > rdev_capability_audit.txt && cat rdev_capability_audit.txt && rm rdev_capability_audit.txt && test ! -e rdev_capability_audit.txt && echo deleted"}, []string{"sh"}, []string{"."})},
		{Name: "process_inspect", Capability: "process.inspect", Adapter: "shell", Intent: "capability audit process inspect", Policy: shellAuditPolicy([]string{"shell.user", "process.inspect"}, []string{"sh", "-c", "ps -o pid,comm= -p $$"}, []string{"sh"})},
	}
}

func shellAuditPolicy(capabilities, argv, allowCommands []string) map[string]any {
	return shellAuditPolicyWithWriteScope(capabilities, argv, allowCommands, nil)
}

func shellAuditPolicyWithWriteScope(capabilities, argv, allowCommands, writeScope []string) map[string]any {
	policy := map[string]any{
		"workspace_root":       ".",
		"capabilities":         capabilities,
		"argv":                 argv,
		"allow_commands":       allowCommands,
		"max_duration_seconds": 10,
		"max_output_bytes":     12000,
		"network":              "default-deny",
	}
	if len(writeScope) > 0 {
		policy["write_scope"] = writeScope
	}
	return policy
}

func fetchHostJSON(ctx context.Context, gatewayURL, hostID, bearerToken string) (map[string]any, error) {
	endpoint := strings.TrimRight(gatewayURL, "/") + "/v1/hosts/" + url.PathEscape(hostID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := doGatewayRequest(http.DefaultClient, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch host failed: %s", responseErrorMessage(payload, resp.Status))
	}
	host, _ := payload["host"].(map[string]any)
	if host == nil {
		return nil, fmt.Errorf("fetch host failed: response missing host")
	}
	return host, nil
}

func createJobJSON(ctx context.Context, gatewayURL, hostID, adapter, intent string, policy map[string]any, bearerToken string) (map[string]any, error) {
	endpoint := strings.TrimRight(gatewayURL, "/") + "/v1/jobs"
	body, err := json.Marshal(map[string]any{"host_id": hostID, "adapter": adapter, "intent": intent, "policy": policy})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := doGatewayRequest(http.DefaultClient, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return payload, fmt.Errorf("create job failed: %s", responseErrorMessage(payload, resp.Status))
	}
	return payload, nil
}

func waitJobJSON(ctx context.Context, gatewayURL, jobID, bearerToken string, interval time.Duration) (map[string]any, error) {
	if interval <= 0 {
		interval = time.Second
	}
	endpoint := strings.TrimRight(gatewayURL, "/") + "/v1/jobs/" + url.PathEscape(jobID)
	for {
		payload, err := fetchJobJSON(ctx, endpoint, bearerToken)
		if err != nil {
			return payload, err
		}
		job := nestedMapOrSelf(payload, "job")
		status := stringFromMap(job, "status")
		switch status {
		case "succeeded", "completed", "failed", "canceled":
			return job, nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return payload, ctx.Err()
		case <-timer.C:
		}
	}
}

func fetchJobArtifactsJSON(ctx context.Context, gatewayURL, jobID, bearerToken string) (map[string]any, error) {
	endpoint := strings.TrimRight(gatewayURL, "/") + "/v1/jobs/" + url.PathEscape(jobID) + "/artifacts"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := doGatewayRequest(http.DefaultClient, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return payload, fmt.Errorf("fetch job artifacts failed: %s", responseErrorMessage(payload, resp.Status))
	}
	return payload, nil
}

func responseErrorMessage(payload map[string]any, fallback string) string {
	if msg, _ := payload["error"].(string); msg != "" {
		return msg
	}
	return fallback
}

func stringFromNestedMap(payload map[string]any, objectKey, fieldKey string) string {
	object, _ := payload[objectKey].(map[string]any)
	if object == nil {
		return ""
	}
	return stringFromMap(object, fieldKey)
}

func nestedMapOrSelf(payload map[string]any, objectKey string) map[string]any {
	object, _ := payload[objectKey].(map[string]any)
	if object != nil {
		return object
	}
	return payload
}

func stringFromMap(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func mapSliceFromAny(value any) []map[string]any {
	if typed, ok := value.([]map[string]any); ok {
		return typed
	}
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if typed, ok := item.(map[string]any); ok {
			out = append(out, typed)
		}
	}
	return out
}

func stringSliceFromAny(value any) []string {
	if typed, ok := value.([]string); ok {
		return typed
	}
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if typed, ok := item.(string); ok {
			out = append(out, typed)
		}
	}
	return out
}

func summarizeFirstArtifact(payload map[string]any) string {
	values, _ := payload["artifacts"].([]any)
	if len(values) == 0 {
		return ""
	}
	first, _ := values[0].(map[string]any)
	content, _ := first["content"].(string)
	content = strings.TrimSpace(content)
	if len(content) > 300 {
		content = content[:300] + "..."
	}
	return content
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
		fs := flag.NewFlagSet("mcp serve", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		gatewayURL := fs.String("gateway-url", "", "proxy host/job/artifact/audit MCP tool calls to this gateway URL; "+
			"overrides RDEV_*_GATEWAY_URL environment variables. "+
			"Per-call gateway_url arguments in tool inputs still take highest precedence.")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		// Resolve effective remote gateway URL: explicit flag > env vars.
		remoteURL := strings.TrimRight(strings.TrimSpace(*gatewayURL), "/")
		if remoteURL == "" {
			remoteURL, _ = supportsession.ConfiguredGatewayURLCandidate()
		}
		var server mcpstdio.Server
		if remoteURL != "" {
			server = mcpstdio.NewServerWithRemoteGateway(gateway.NewMemoryGateway(), remoteURL)
		} else {
			server = mcpstdio.NewServer(gateway.NewMemoryGateway())
		}
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
		gateway := fs.String("gateway", "", "gateway URL; required with --ticket-code unless --manifest-url is used")
		ticketCode := fs.String("ticket-code", "", "one-time ticket code for local dev registration")
		manifestURL := fs.String("manifest-url", "", "signed join manifest URL")
		name := fs.String("name", "", "host display name; defaults to detected hostname")
		once := fs.Bool("once", true, "register once and exit after printing status")
		transport := fs.String("transport", "poll", "host job transport: auto, poll, long-poll, or wss")
		pollInterval := fs.Duration("poll-interval", time.Second, "job polling interval when --once=false")
		longPollTimeout := fs.Duration("long-poll-timeout", 25*time.Second, "long-poll wait duration when --transport=long-poll")
		maxJobs := fs.Int("max-jobs", 1, "maximum jobs to process when --once=false; 0 = unlimited (process jobs until context is cancelled)")
		approvalTimeout := fs.Duration("approval-timeout", 120*time.Second, "maximum time to wait for host approval when --once=false; increase for attended temporary sessions over slow/remote networks")
		trustPin := fs.String("trust-pin", "", "optional gateway signing public key pin, formatted sha256:<hex>")
		gatewayCA := fs.String("gateway-ca", "", "optional PEM CA bundle for the gateway HTTPS certificate")
		gatewayClientCert := fs.String("gateway-client-cert", "", "optional PEM client certificate for gateway mTLS")
		gatewayClientKey := fs.String("gateway-client-key", "", "optional PEM client private key for gateway mTLS")
		trustStore := fs.String("trust-store", "", "optional local signed trust bundle store path for managed hosts")
		identityStore := fs.String("identity-store", "", "optional local host identity key store path")
		identityKeyID := fs.String("identity-key-id", hostidentity.DefaultKeyID, "host identity key id")
		enrollmentCertificate := fs.String("enrollment-certificate", "", "optional host enrollment certificate JSON path")
		enrollmentRootPublicKey := fs.String("enrollment-root-public-key", "", "optional enrollment root public key for host-side enrollment revocation refresh, formatted key_id:base64url_public_key")
		fetchEnrollmentRevocations := fs.Bool("fetch-enrollment-revocations", false, "fetch and verify signed enrollment revocations from the gateway before registration")
		renewEnrollmentCertificate := fs.Bool("renew-enrollment-certificate", false, "renew the enrollment certificate from the gateway before registration when it is near expiry")
		operatorTokenFile := fs.String("operator-token-file", "", "file containing an operator auth bearer token with issuer role for hosted renewal or revocation refresh")
		enrollmentRenewBefore := fs.Duration("enrollment-renew-before", 24*time.Hour, "renew enrollment certificate when it expires within this duration")
		enrollmentRenewValidMinutes := fs.Int("enrollment-renew-valid-minutes", 60, "renewed enrollment certificate validity window in minutes")
		nonceStore := fs.String("nonce-store", "", "optional local host nonce replay cache path")
		approvalStore := fs.String("approval-store", "", "optional local host approval token consumption store path")
		workspaceLockStore := fs.String("workspace-lock-store", "", "optional local workspace lock store directory")
		captureRuntimeFixture := fs.Bool("capture-runtime-fixture", false, "append an adapter runtime fixture artifact for completed, failed, or canceled jobs")
		keepAwake := fs.Bool("keep-awake", true, "best-effort prevention of idle sleep/display sleep while host serve is running; does not bypass OS lock-screen policy")
		manifestRootPublicKey := fs.String("manifest-root-public-key", "", "optional join manifest trust root, formatted key_id:base64url_public_key")
		releaseBundle := fs.String("release-bundle", "", "optional signed release bundle index to verify before host registration")
		releaseRootPublicKey := fs.String("release-root-public-key", "", "required release root public key for --release-bundle, formatted key_id:base64url_public_key")
		releaseRequiredArtifacts := fs.String("release-require-artifacts", "", "comma-separated artifact ids required in --release-bundle")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.hostServe(ctx, hostServeOptions{
			Mode:                        *mode,
			GatewayURL:                  *gateway,
			TicketCode:                  *ticketCode,
			ManifestURL:                 *manifestURL,
			Name:                        *name,
			Once:                        *once,
			Transport:                   *transport,
			PollInterval:                *pollInterval,
			LongPollTimeout:             *longPollTimeout,
			MaxJobs:                     *maxJobs,
			ApprovalTimeout:             *approvalTimeout,
			TrustPin:                    *trustPin,
			GatewayCACertPath:           *gatewayCA,
			GatewayClientCertPath:       *gatewayClientCert,
			GatewayClientKeyPath:        *gatewayClientKey,
			TrustStorePath:              *trustStore,
			IdentityStorePath:           *identityStore,
			IdentityKeyID:               *identityKeyID,
			EnrollmentCertificatePath:   *enrollmentCertificate,
			EnrollmentRootPublicKey:     *enrollmentRootPublicKey,
			FetchEnrollmentRevocations:  *fetchEnrollmentRevocations,
			RenewEnrollmentCertificate:  *renewEnrollmentCertificate,
			OperatorTokenFile:           *operatorTokenFile,
			EnrollmentRenewBefore:       *enrollmentRenewBefore,
			EnrollmentRenewValidMinutes: *enrollmentRenewValidMinutes,
			NonceStorePath:              *nonceStore,
			ApprovalStorePath:           *approvalStore,
			WorkspaceLockStore:          *workspaceLockStore,
			CaptureRuntimeFixture:       *captureRuntimeFixture,
			KeepAwake:                   *keepAwake,
			ManifestRootPublicKey:       *manifestRootPublicKey,
			ReleaseBundlePath:           *releaseBundle,
			ReleaseRootPublicKey:        *releaseRootPublicKey,
			ReleaseRequiredArtifacts:    splitCapabilities(*releaseRequiredArtifacts),
		})
	case "install-service":
		fs := flag.NewFlagSet("host install-service", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		platform := fs.String("platform", "macos", "service platform: macos, linux, or windows")
		label := fs.String("label", "", "managed host service label or systemd unit name")
		binaryPath := fs.String("binary", "", "absolute path to rdev binary; defaults to current executable")
		gatewayURL := fs.String("gateway", "", "gateway URL")
		ticketCode := fs.String("ticket-code", "", "managed enrollment ticket code")
		manifestURL := fs.String("manifest-url", "", "signed managed enrollment manifest URL")
		identityStore := fs.String("identity-store", "", "managed host identity store path")
		trustStore := fs.String("trust-store", "", "managed host trust bundle store path")
		nonceStore := fs.String("nonce-store", "", "managed host nonce store path")
		approvalStore := fs.String("approval-store", "", "managed host approval store path")
		workspaceLockStore := fs.String("workspace-lock-store", "", "managed host workspace lock store directory")
		releaseBundle := fs.String("release-bundle", "", "optional signed release bundle index verified by the managed host before registration")
		releaseRootPublicKey := fs.String("release-root-public-key", "", "required release root public key for --release-bundle, formatted key_id:base64url_public_key")
		releaseRequiredArtifacts := fs.String("release-require-artifacts", "", "comma-separated artifact ids required in --release-bundle")
		logDir := fs.String("log-dir", "", "managed host log directory")
		plistOut := fs.String("plist-out", "", "LaunchAgent plist output path; defaults to ~/Library/LaunchAgents/<label>.plist on macOS")
		unitOut := fs.String("unit-out", "", "systemd user unit output path; defaults to ~/.config/systemd/user/<unit>.service on Linux")
		force := fs.Bool("force", false, "overwrite an existing plist output path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.hostInstallService(hostInstallServiceOptions{
			Platform:                 *platform,
			Label:                    *label,
			BinaryPath:               *binaryPath,
			GatewayURL:               *gatewayURL,
			TicketCode:               *ticketCode,
			ManifestURL:              *manifestURL,
			IdentityStorePath:        *identityStore,
			TrustStorePath:           *trustStore,
			NonceStorePath:           *nonceStore,
			ApprovalStorePath:        *approvalStore,
			WorkspaceLockStore:       *workspaceLockStore,
			ReleaseBundlePath:        *releaseBundle,
			ReleaseRootPublicKey:     *releaseRootPublicKey,
			ReleaseRequiredArtifacts: splitCapabilities(*releaseRequiredArtifacts),
			LogDir:                   *logDir,
			PlistOut:                 *plistOut,
			UnitOut:                  *unitOut,
			Force:                    *force,
		})
	case "service-status":
		fs := flag.NewFlagSet("host service-status", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		platform := fs.String("platform", "macos", "service platform: macos, linux, or windows")
		label := fs.String("label", "", "managed host service label or systemd unit name")
		plistPath := fs.String("plist", "", "LaunchAgent plist path; defaults to ~/Library/LaunchAgents/<label>.plist on macOS")
		unitPath := fs.String("unit", "", "systemd user unit path; defaults to ~/.config/systemd/user/<unit>.service on Linux")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.hostServiceStatus(hostServiceOptions{
			Platform: *platform,
			Label:    *label,
			Plist:    *plistPath,
			Unit:     *unitPath,
		})
	case "service-control":
		fs := flag.NewFlagSet("host service-control", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		platform := fs.String("platform", "macos", "service platform: macos, linux, or windows")
		action := fs.String("action", "", "service action: start, stop, or inspect")
		label := fs.String("label", "", "managed host service label or systemd unit name")
		plistPath := fs.String("plist", "", "LaunchAgent plist path; defaults to ~/Library/LaunchAgents/<label>.plist on macOS")
		unitPath := fs.String("unit", "", "systemd user unit path; defaults to ~/.config/systemd/user/<unit>.service on Linux")
		domain := fs.String("domain", "gui/$(id -u)", "launchctl domain; default is resolved for --execute")
		execute := fs.Bool("execute", false, "execute the OS service manager instead of only printing the planned command")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.hostServiceControl(ctx, hostServiceControlOptions{
			Platform: *platform,
			Action:   *action,
			Label:    *label,
			Plist:    *plistPath,
			Unit:     *unitPath,
			Domain:   *domain,
			Execute:  *execute,
		})
	case "uninstall-service":
		fs := flag.NewFlagSet("host uninstall-service", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		platform := fs.String("platform", "macos", "service platform: macos, linux, or windows")
		label := fs.String("label", "", "managed host service label or systemd unit name")
		plistPath := fs.String("plist", "", "LaunchAgent plist path; defaults to ~/Library/LaunchAgents/<label>.plist on macOS")
		unitPath := fs.String("unit", "", "systemd user unit path; defaults to ~/.config/systemd/user/<unit>.service on Linux")
		force := fs.Bool("force", false, "remove service file even if the embedded label or unit name does not match --label")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.hostUninstallService(hostServiceOptions{
			Platform: *platform,
			Label:    *label,
			Plist:    *plistPath,
			Unit:     *unitPath,
			Force:    *force,
		})
	default:
		return fmt.Errorf("unknown host subcommand %q", args[0])
	}
}

type hostServeOptions struct {
	Mode                        string
	GatewayURL                  string
	TicketCode                  string
	ManifestURL                 string
	Name                        string
	Once                        bool
	Transport                   string
	PollInterval                time.Duration
	LongPollTimeout             time.Duration
	MaxJobs                     int
	ApprovalTimeout             time.Duration
	TrustPin                    string
	GatewayCACertPath           string
	GatewayClientCertPath       string
	GatewayClientKeyPath        string
	TrustStorePath              string
	IdentityStorePath           string
	IdentityKeyID               string
	EnrollmentCertificatePath   string
	EnrollmentRootPublicKey     string
	FetchEnrollmentRevocations  bool
	RenewEnrollmentCertificate  bool
	OperatorTokenFile           string
	EnrollmentRenewBefore       time.Duration
	EnrollmentRenewValidMinutes int
	NonceStorePath              string
	ApprovalStorePath           string
	WorkspaceLockStore          string
	CaptureRuntimeFixture       bool
	KeepAwake                   bool
	ManifestRootPublicKey       string
	ReleaseBundlePath           string
	ReleaseRootPublicKey        string
	ReleaseRequiredArtifacts    []string
	ManifestGatewayCandidates   []model.JoinManifestGatewayCandidate
}

type hostInstallServiceOptions struct {
	Platform                 string
	Label                    string
	BinaryPath               string
	GatewayURL               string
	TicketCode               string
	ManifestURL              string
	IdentityStorePath        string
	TrustStorePath           string
	NonceStorePath           string
	ApprovalStorePath        string
	WorkspaceLockStore       string
	ReleaseBundlePath        string
	ReleaseRootPublicKey     string
	ReleaseRequiredArtifacts []string
	LogDir                   string
	PlistOut                 string
	UnitOut                  string
	Force                    bool
}

type hostServiceOptions struct {
	Platform string
	Label    string
	Plist    string
	Unit     string
	Force    bool
}

type hostServiceControlOptions struct {
	Platform string
	Action   string
	Label    string
	Plist    string
	Unit     string
	Domain   string
	Execute  bool
}

type hostReleaseGateResult struct {
	OK                bool      `json:"ok"`
	Schema            string    `json:"schema"`
	Bundle            string    `json:"bundle"`
	RootKeyID         string    `json:"root_key_id"`
	RequiredArtifacts []string  `json:"required_artifacts,omitempty"`
	VerifiedAt        time.Time `json:"verified_at"`
	ArtifactCount     int       `json:"artifact_count"`
}

func (a App) hostServe(ctx context.Context, opts hostServeOptions) error {
	switch opts.Mode {
	case "temporary", "managed", "break-glass":
	default:
		return fmt.Errorf("unsupported host mode %q", opts.Mode)
	}
	if opts.FetchEnrollmentRevocations {
		if strings.TrimSpace(opts.EnrollmentCertificatePath) == "" {
			return fmt.Errorf("enrollment certificate is required when --fetch-enrollment-revocations is set")
		}
		if strings.TrimSpace(opts.EnrollmentRootPublicKey) == "" {
			return fmt.Errorf("enrollment-root-public-key is required when --fetch-enrollment-revocations is set")
		}
	}
	if opts.RenewEnrollmentCertificate {
		if strings.TrimSpace(opts.EnrollmentCertificatePath) == "" {
			return fmt.Errorf("enrollment certificate is required when --renew-enrollment-certificate is set")
		}
		if strings.TrimSpace(opts.EnrollmentRootPublicKey) == "" {
			return fmt.Errorf("enrollment-root-public-key is required when --renew-enrollment-certificate is set")
		}
		if opts.EnrollmentRenewBefore < 0 {
			return fmt.Errorf("enrollment-renew-before must be non-negative")
		}
		if opts.EnrollmentRenewValidMinutes <= 0 {
			return fmt.Errorf("enrollment-renew-valid-minutes must be positive")
		}
	} else if strings.TrimSpace(opts.EnrollmentRootPublicKey) != "" && !opts.FetchEnrollmentRevocations {
		return fmt.Errorf("--fetch-enrollment-revocations or --renew-enrollment-certificate is required when --enrollment-root-public-key is provided")
	} else if strings.TrimSpace(opts.OperatorTokenFile) != "" && !opts.FetchEnrollmentRevocations {
		return fmt.Errorf("--renew-enrollment-certificate or --fetch-enrollment-revocations is required when --operator-token-file is provided")
	}
	if opts.Transport == "" {
		opts.Transport = "poll"
	}
	switch opts.Transport {
	case "auto", "poll", "long-poll", "wss":
	default:
		return fmt.Errorf("unsupported host transport %q", opts.Transport)
	}
	gatewayClient, err := gatewayHTTPClient(opts)
	if err != nil {
		return err
	}
	releaseGate, err := verifyHostReleaseGate(opts)
	if err != nil {
		return err
	}
	if opts.ManifestURL != "" {
		manifest, err := fetchJoinManifest(ctx, gatewayClient, opts.ManifestURL, opts.TrustPin, opts.ManifestRootPublicKey)
		if err != nil {
			return err
		}
		opts.ManifestGatewayCandidates = manifest.GatewayCandidates
		if strings.TrimSpace(opts.GatewayURL) == "" {
			opts.GatewayURL = selectJoinManifestGatewayURL(ctx, gatewayClient, manifest)
		}
		opts.TicketCode = manifest.TicketCode
		opts.TrustPin = manifest.TrustFingerprint
	}
	if opts.TicketCode == "" {
		_, err := fmt.Fprintf(
			a.Stdout,
			"rdev-host foreground placeholder\nmode=%s\ngateway=%s\nstatus=not-connected\nnote=provide --gateway and --ticket-code to register with a local gateway, or --manifest-url for a signed join manifest\n",
			opts.Mode,
			opts.GatewayURL,
		)
		return err
	}
	if strings.TrimSpace(opts.GatewayURL) == "" {
		return fmt.Errorf("gateway is required when --ticket-code is provided")
	}
	manifestRootVerified := opts.ManifestURL != "" && strings.TrimSpace(opts.ManifestRootPublicKey) != ""
	if !isLocalDevGatewayURL(opts.GatewayURL) && !isSignedManifestGatewayURL(opts.GatewayURL, manifestRootVerified) {
		return fmt.Errorf("non-local gateway registration requires --manifest-url with --manifest-root-public-key and an HTTPS or private/LAN gateway URL")
	}
	identity, identityCreated, err := hostidentity.LoadOrCreate(opts.IdentityStorePath, opts.IdentityKeyID)
	if err != nil {
		return err
	}
	inventory := hostcap.Detect(ctx)
	if opts.Name != "" {
		inventory.Name = opts.Name
	}
	registration := model.HostRegistration{
		TicketCode:          opts.TicketCode,
		Name:                inventory.Name,
		OS:                  inventory.OS,
		Arch:                inventory.Arch,
		Capabilities:        inventory.TemporaryCapabilities,
		IdentityKeyID:       identity.KeyID,
		IdentityPublicKey:   identity.EncodedPublicKey(),
		IdentityFingerprint: identity.Fingerprint(),
	}
	proof, err := model.SignHostRegistration(registration, identity.PrivateKey)
	if err != nil {
		return err
	}
	registration.IdentityProof = &proof
	enrollmentRevocationCount := 0
	enrollmentRevocationsFetched := false
	enrollmentRevocationRoot := ""
	enrollmentRenewed := false
	enrollmentPreviousFingerprint := ""
	enrollmentCertificateFingerprint := ""
	if opts.EnrollmentCertificatePath != "" {
		certificate, err := readEnrollmentCertificateFile(opts.EnrollmentCertificatePath)
		if err != nil {
			return err
		}
		if opts.RenewEnrollmentCertificate {
			root, err := parseRootPublicKey(opts.EnrollmentRootPublicKey)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			if err := model.VerifyHostEnrollmentCertificateSignature(certificate, root, now); err != nil {
				return err
			}
			if !now.Add(opts.EnrollmentRenewBefore).Before(certificate.NotAfter.UTC()) {
				renewed, previousFingerprint, fingerprint, err := renewEnrollmentCertificateFromGateway(ctx, gatewayClient, opts.GatewayURL, certificate, enrollmentRenewCertificateOptions{
					RootPublicKey:     opts.EnrollmentRootPublicKey,
					OperatorTokenFile: opts.OperatorTokenFile,
					ValidMinutes:      opts.EnrollmentRenewValidMinutes,
				})
				if err != nil {
					return err
				}
				if err := writeEnrollmentCertificateFile(opts.EnrollmentCertificatePath, renewed, true); err != nil {
					return err
				}
				certificate = renewed
				enrollmentRenewed = true
				enrollmentPreviousFingerprint = previousFingerprint
				enrollmentCertificateFingerprint = fingerprint
			}
		}
		if opts.FetchEnrollmentRevocations {
			revocations, root, err := fetchEnrollmentRevocationsWithClient(ctx, gatewayClient, opts.GatewayURL, opts.EnrollmentRootPublicKey, opts.OperatorTokenFile)
			if err != nil {
				return err
			}
			now := time.Now()
			if err := model.VerifyHostEnrollmentCertificateSignature(certificate, root, now); err != nil {
				return err
			}
			if err := model.VerifyHostEnrollmentCertificateNotRevoked(certificate, revocations); err != nil {
				return err
			}
			enrollmentRevocationCount = len(revocations.RevokedCertificates)
			enrollmentRevocationsFetched = true
			enrollmentRevocationRoot = root.SigningKeyID
		}
		registration.EnrollmentCertificate = &certificate
	}
	host, hostSecret, err := registerHost(ctx, gatewayClient, opts.GatewayURL, registration)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"mode":      opts.Mode,
		"gateway":   opts.GatewayURL,
		"host":      host,
		"inventory": inventory,
		"identity": map[string]any{
			"key_id":             identity.KeyID,
			"fingerprint":        identity.Fingerprint(),
			"created":            identityCreated,
			"stored":             opts.IdentityStorePath != "",
			"proof_schema":       proof.SchemaVersion,
			"registration_proof": true,
		},
		"status":    "registered-pending-approval",
		"transport": opts.Transport,
		"note":      "registered with gateway; job transport starts after host approval when --once=false",
	}
	if opts.ManifestURL != "" {
		payload["manifest_url"] = opts.ManifestURL
		payload["manifest_gateway_selection"] = map[string]any{
			"selected_gateway_url": opts.GatewayURL,
			"source":               "signed-join-manifest-candidates",
		}
	}
	if registration.EnrollmentCertificate != nil {
		enrollmentSummary := map[string]any{
			"schema":        registration.EnrollmentCertificate.SchemaVersion,
			"issuer_key_id": registration.EnrollmentCertificate.IssuerKeyID,
			"not_after":     registration.EnrollmentCertificate.NotAfter,
		}
		if enrollmentRenewed {
			enrollmentSummary["renewed"] = true
			enrollmentSummary["previous_certificate_fingerprint"] = enrollmentPreviousFingerprint
			enrollmentSummary["certificate_fingerprint"] = enrollmentCertificateFingerprint
		}
		if enrollmentRevocationsFetched {
			enrollmentSummary["revocations_fetched"] = true
			enrollmentSummary["revoked_certificate_count"] = enrollmentRevocationCount
			enrollmentSummary["revocation_root_key_id"] = enrollmentRevocationRoot
		}
		payload["enrollment_certificate"] = enrollmentSummary
	}
	if releaseGate != nil {
		payload["release_gate"] = releaseGate
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if opts.Once {
		return enc.Encode(payload)
	}
	keepAwake := hostawake.Disabled()
	if opts.KeepAwake {
		keepAwake = hostawake.Acquire(ctx)
	}
	defer func() { _ = keepAwake.Close() }()
	payload["keep_awake"] = keepAwake
	if keepAwake.Enabled {
		_, _ = fmt.Fprintf(a.Stderr, "[rdev] keep-awake enabled via %s (%s)\n", keepAwake.Method, keepAwake.Detail)
	} else {
		_, _ = fmt.Fprintf(a.Stderr, "[rdev] keep-awake not active: %s\n", keepAwake.Detail)
	}
	if _, err := waitForHostActive(ctx, gatewayClient, opts.GatewayURL, host.ID, opts.ApprovalTimeout, opts.PollInterval); err != nil {
		return err
	}
	processed, err := a.runHostJobs(ctx, opts, gatewayClient, host.ID, identity.Fingerprint(), hostSecret)
	if err != nil {
		return err
	}
	payload["processed_jobs"] = processed
	payload["status"] = "polling-complete"
	return enc.Encode(payload)
}

func verifyHostReleaseGate(opts hostServeOptions) (*hostReleaseGateResult, error) {
	if strings.TrimSpace(opts.ReleaseBundlePath) == "" {
		if strings.TrimSpace(opts.ReleaseRootPublicKey) != "" || len(opts.ReleaseRequiredArtifacts) > 0 {
			return nil, fmt.Errorf("release bundle is required when release verification options are provided")
		}
		return nil, nil
	}
	root, err := parseRootPublicKey(opts.ReleaseRootPublicKey)
	if err != nil {
		return nil, err
	}
	verification, err := release.VerifyBundle(opts.ReleaseBundlePath, root, opts.ReleaseRequiredArtifacts)
	if err != nil {
		return nil, err
	}
	if !verification.OK() {
		return nil, fmt.Errorf("host release bundle verification failed")
	}
	return &hostReleaseGateResult{
		OK:                true,
		Schema:            verification.SchemaVersion,
		Bundle:            verification.BundlePath,
		RootKeyID:         verification.RootKeyID,
		RequiredArtifacts: append([]string(nil), opts.ReleaseRequiredArtifacts...),
		VerifiedAt:        verification.GeneratedAt,
		ArtifactCount:     len(verification.Artifacts),
	}, nil
}

func gatewayHTTPClient(opts hostServeOptions) (*http.Client, error) {
	tlsConfig, err := gatewayTLSClientConfig(opts)
	if err != nil {
		return nil, err
	}
	var base http.RoundTripper
	if tlsConfig == nil {
		base = http.DefaultTransport
	} else {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = tlsConfig
		base = transport
	}
	return &http.Client{
		Transport: retryingRoundTripper{Base: base, MaxRetries: 3},
	}, nil
}

func gatewayTLSClientConfig(opts hostServeOptions) (*tls.Config, error) {
	if (opts.GatewayClientCertPath == "") != (opts.GatewayClientKeyPath == "") {
		return nil, fmt.Errorf("host serve gateway mTLS requires both --gateway-client-cert and --gateway-client-key")
	}
	if opts.GatewayCACertPath == "" && opts.GatewayClientCertPath == "" {
		return nil, nil
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if opts.GatewayCACertPath != "" {
		content, err := os.ReadFile(opts.GatewayCACertPath)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(content) {
			return nil, fmt.Errorf("host serve --gateway-ca does not contain a valid PEM certificate")
		}
		tlsConfig.RootCAs = pool
	}
	if opts.GatewayClientCertPath != "" {
		certificate, err := tls.LoadX509KeyPair(opts.GatewayClientCertPath, opts.GatewayClientKeyPath)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	return tlsConfig, nil
}

func isLocalDevGatewayURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	switch parsed.Hostname() {
	case "127.0.0.1", "localhost":
		return parsed.Port() != ""
	default:
		return false
	}
}

func isSignedManifestGatewayURL(value string, manifestRootVerified bool) bool {
	if !manifestRootVerified {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	switch parsed.Scheme {
	case "https":
		return true
	case "http":
		return isPrivateOrLANHost(parsed.Hostname()) && parsed.Port() != ""
	default:
		return false
	}
}

func isPrivateOrLANHost(host string) bool {
	normalized := strings.Trim(strings.ToLower(host), "[]")
	switch normalized {
	case "localhost":
		return true
	}
	if strings.HasSuffix(normalized, ".local") || strings.HasSuffix(normalized, ".lan") {
		return true
	}
	ip := net.ParseIP(normalized)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

func (a App) hostInstallService(opts hostInstallServiceOptions) error {
	if opts.Platform == "" {
		opts.Platform = "macos"
	}
	binaryPath := opts.BinaryPath
	if binaryPath == "" {
		current, err := os.Executable()
		if err != nil {
			return err
		}
		binaryPath = current
	}
	if !isServiceBinaryPathAbsolute(opts.Platform, binaryPath) {
		return fmt.Errorf("binary path must be absolute")
	}
	switch opts.Platform {
	case "macos", "darwin":
		return a.hostInstallMacOSService(opts, binaryPath)
	case "linux", "systemd":
		return a.hostInstallLinuxSystemdService(opts, binaryPath)
	case "windows", "win32":
		return a.hostInstallWindowsService(opts, binaryPath)
	default:
		return fmt.Errorf("unsupported service platform %q", opts.Platform)
	}
}

func isServiceBinaryPathAbsolute(platform, path string) bool {
	switch platform {
	case "windows", "win32":
		return isWindowsAbsolutePath(path)
	default:
		return filepath.IsAbs(path)
	}
}

func isWindowsAbsolutePath(path string) bool {
	if len(path) >= 3 {
		drive := path[0]
		if ((drive >= 'a' && drive <= 'z') || (drive >= 'A' && drive <= 'Z')) && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
			return true
		}
	}
	return strings.HasPrefix(path, `\\`)
}

func (a App) hostInstallMacOSService(opts hostInstallServiceOptions, binaryPath string) error {
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
		Label:                    opts.Label,
		BinaryPath:               binaryPath,
		GatewayURL:               opts.GatewayURL,
		TicketCode:               opts.TicketCode,
		ManifestURL:              opts.ManifestURL,
		IdentityStorePath:        opts.IdentityStorePath,
		TrustStorePath:           opts.TrustStorePath,
		NonceStorePath:           opts.NonceStorePath,
		ApprovalStorePath:        opts.ApprovalStorePath,
		WorkspaceLockStorePath:   opts.WorkspaceLockStore,
		ReleaseBundlePath:        opts.ReleaseBundlePath,
		ReleaseRootPublicKey:     opts.ReleaseRootPublicKey,
		ReleaseRequiredArtifacts: opts.ReleaseRequiredArtifacts,
		LogDir:                   opts.LogDir,
		Transport:                "long-poll",
		LongPollTimeout:          "25s",
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
		"platform":          "macos",
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

func (a App) hostInstallLinuxSystemdService(opts hostInstallServiceOptions, binaryPath string) error {
	unitName := opts.Label
	if unitName == "" {
		unitName = service.DefaultLinuxSystemdUnitName
	}
	unitOut := opts.UnitOut
	if unitOut == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		unitOut = service.DefaultLinuxSystemdUserUnitPath(home, unitName)
	}
	unit, err := service.NewLinuxSystemdUserService(service.SystemdUserServiceOptions{
		UnitName:                 unitName,
		BinaryPath:               binaryPath,
		GatewayURL:               opts.GatewayURL,
		TicketCode:               opts.TicketCode,
		ManifestURL:              opts.ManifestURL,
		IdentityStorePath:        opts.IdentityStorePath,
		TrustStorePath:           opts.TrustStorePath,
		NonceStorePath:           opts.NonceStorePath,
		ApprovalStorePath:        opts.ApprovalStorePath,
		WorkspaceLockStorePath:   opts.WorkspaceLockStore,
		ReleaseBundlePath:        opts.ReleaseBundlePath,
		ReleaseRootPublicKey:     opts.ReleaseRootPublicKey,
		ReleaseRequiredArtifacts: opts.ReleaseRequiredArtifacts,
		LogDir:                   opts.LogDir,
		Transport:                "long-poll",
		LongPollTimeout:          "25s",
	})
	if err != nil {
		return err
	}
	content, err := service.RenderLinuxSystemdUserService(unit)
	if err != nil {
		return err
	}
	if err := writeServiceFile(unitOut, content, opts.Force); err != nil {
		return err
	}
	payload := map[string]any{
		"ok":          true,
		"platform":    "linux",
		"unit_name":   unit.UnitName,
		"unit":        unitOut,
		"exec_start":  unit.ExecStart,
		"stdout":      unit.StandardOutput,
		"stderr":      unit.StandardError,
		"restart":     unit.Restart,
		"restart_sec": unit.RestartSec,
		"next":        linuxSystemdNextSteps(unit.UnitName, unitOut),
		"note":        "systemd unit written only; systemctl was not executed by this command",
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) hostInstallWindowsService(opts hostInstallServiceOptions, binaryPath string) error {
	serviceName := opts.Label
	if serviceName == "" {
		serviceName = service.DefaultWindowsServiceName
	}
	winService, err := service.NewWindowsService(service.WindowsServiceOptions{
		ServiceName:              serviceName,
		BinaryPath:               binaryPath,
		GatewayURL:               opts.GatewayURL,
		TicketCode:               opts.TicketCode,
		ManifestURL:              opts.ManifestURL,
		IdentityStorePath:        opts.IdentityStorePath,
		TrustStorePath:           opts.TrustStorePath,
		NonceStorePath:           opts.NonceStorePath,
		ApprovalStorePath:        opts.ApprovalStorePath,
		WorkspaceLockStorePath:   opts.WorkspaceLockStore,
		ReleaseBundlePath:        opts.ReleaseBundlePath,
		ReleaseRootPublicKey:     opts.ReleaseRootPublicKey,
		ReleaseRequiredArtifacts: opts.ReleaseRequiredArtifacts,
		Transport:                "long-poll",
		LongPollTimeout:          "25s",
	})
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":           true,
		"platform":     "windows",
		"service_name": winService.ServiceName,
		"display_name": winService.DisplayName,
		"args":         winService.Args,
		"bin_path":     winService.BinPath,
		"commands":     winService.Commands,
		"shell":        winService.Shell,
		"start_type":   winService.StartType,
		"next":         windowsServiceNextSteps(winService.ServiceName),
		"note":         "dry-run only; use rdev host service-control --platform windows --action start --execute after creating the service with approved commands",
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
	switch opts.Platform {
	case "macos", "darwin":
		return a.hostMacOSServiceStatus(opts)
	case "linux", "systemd":
		return a.hostLinuxSystemdServiceStatus(opts)
	case "windows", "win32":
		return a.hostWindowsServiceStatus(opts)
	default:
		return fmt.Errorf("unsupported service platform %q", opts.Platform)
	}
}

func (a App) hostMacOSServiceStatus(opts hostServiceOptions) error {
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
		"platform": "macos",
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

func (a App) hostLinuxSystemdServiceStatus(opts hostServiceOptions) error {
	unitName := opts.Label
	if unitName == "" {
		unitName = service.DefaultLinuxSystemdUnitName
	}
	unitPath, err := serviceUnitPath(opts)
	if err != nil {
		return err
	}
	status, err := service.InspectLinuxSystemdUserService(unitPath)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":        true,
		"platform":  "linux",
		"unit_name": unitName,
		"unit":      unitPath,
		"status":    status,
		"next":      linuxSystemdNextSteps(unitName, unitPath),
		"note":      "status reads the unit file only; systemctl was not executed by this command",
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) hostWindowsServiceStatus(opts hostServiceOptions) error {
	serviceName := opts.Label
	if serviceName == "" {
		serviceName = service.DefaultWindowsServiceName
	}
	status, err := service.NewWindowsServiceStatus(serviceName)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":           true,
		"platform":     "windows",
		"service_name": status.ServiceName,
		"commands":     status.Commands,
		"shell":        status.Shell,
		"next":         windowsServiceNextSteps(status.ServiceName),
		"note":         "dry-run only; status commands were not executed",
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
	switch opts.Platform {
	case "macos", "darwin":
		return a.hostMacOSServiceControl(ctx, opts)
	case "linux", "systemd":
		return a.hostLinuxSystemdServiceControl(ctx, opts)
	case "windows", "win32":
		return a.hostWindowsServiceControl(ctx, opts)
	default:
		return fmt.Errorf("unsupported service platform %q", opts.Platform)
	}
}

func (a App) hostMacOSServiceControl(ctx context.Context, opts hostServiceControlOptions) error {
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

func (a App) hostLinuxSystemdServiceControl(ctx context.Context, opts hostServiceControlOptions) error {
	unitName := opts.Label
	if unitName == "" {
		unitName = service.DefaultLinuxSystemdUnitName
	}
	if strings.TrimSpace(opts.Action) == "" {
		return fmt.Errorf("service action is required")
	}
	unitPath, err := serviceUnitPath(hostServiceOptions{
		Platform: opts.Platform,
		Label:    unitName,
		Unit:     opts.Unit,
	})
	if err != nil {
		return err
	}
	status, err := service.InspectLinuxSystemdUserService(unitPath)
	if err != nil {
		return err
	}
	if opts.Action == "start" || opts.Action == "stop" {
		if !status.Exists {
			return fmt.Errorf("unit does not exist: %s", unitPath)
		}
	}
	if status.Exists && status.UnitName != unitName {
		return fmt.Errorf("refusing service-control for unit %q; expected %q", status.UnitName, unitName)
	}
	plan, err := service.NewLinuxSystemdControlPlan(service.SystemdControlOptions{
		Action:   opts.Action,
		UnitName: unitName,
		User:     true,
	})
	if err != nil {
		return err
	}
	if opts.Execute {
		results, runErr := runServiceCommands(ctx, plan.Commands)
		payload := map[string]any{
			"ok":        runErr == nil,
			"platform":  "linux",
			"unit_name": unitName,
			"unit":      unitPath,
			"execute":   true,
			"status":    status,
			"command":   plan,
			"results":   results,
			"note":      "systemctl was executed because --execute was set",
		}
		enc := json.NewEncoder(a.Stdout)
		enc.SetIndent("", "  ")
		if encodeErr := enc.Encode(payload); encodeErr != nil {
			return encodeErr
		}
		return runErr
	}
	payload := map[string]any{
		"ok":        true,
		"platform":  "linux",
		"unit_name": unitName,
		"unit":      unitPath,
		"execute":   false,
		"status":    status,
		"command":   plan,
		"note":      "dry-run only; add --execute to run systemctl",
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) hostWindowsServiceControl(ctx context.Context, opts hostServiceControlOptions) error {
	serviceName := opts.Label
	if serviceName == "" {
		serviceName = service.DefaultWindowsServiceName
	}
	if strings.TrimSpace(opts.Action) == "" {
		return fmt.Errorf("service action is required")
	}
	plan, err := service.NewWindowsServiceControlPlan(service.WindowsServiceControlOptions{
		Action:      opts.Action,
		ServiceName: serviceName,
	})
	if err != nil {
		return err
	}
	if opts.Execute {
		results, runErr := runServiceCommands(ctx, plan.Commands)
		payload := map[string]any{
			"ok":           runErr == nil,
			"platform":     "windows",
			"service_name": serviceName,
			"execute":      true,
			"command":      plan,
			"results":      results,
			"note":         "sc.exe was executed because --execute was set",
		}
		enc := json.NewEncoder(a.Stdout)
		enc.SetIndent("", "  ")
		if encodeErr := enc.Encode(payload); encodeErr != nil {
			return encodeErr
		}
		return runErr
	}
	payload := map[string]any{
		"ok":           true,
		"platform":     "windows",
		"service_name": serviceName,
		"execute":      false,
		"command":      plan,
		"next":         windowsServiceNextSteps(serviceName),
		"note":         "dry-run only; add --execute to run sc.exe",
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
	return runServiceCommand(ctx, argv)
}

func runServiceCommands(ctx context.Context, commands [][]string) ([]launchctlRunResult, error) {
	results := make([]launchctlRunResult, 0, len(commands))
	for _, command := range commands {
		result, err := runServiceCommand(ctx, command)
		results = append(results, result)
		if err != nil {
			return results, err
		}
	}
	return results, nil
}

func runServiceCommand(ctx context.Context, argv []string) (launchctlRunResult, error) {
	if len(argv) == 0 {
		return launchctlRunResult{ExitCode: -1}, fmt.Errorf("service command argv is required")
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
	switch opts.Platform {
	case "macos", "darwin":
		return a.hostUninstallMacOSService(opts)
	case "linux", "systemd":
		return a.hostUninstallLinuxSystemdService(opts)
	case "windows", "win32":
		return a.hostUninstallWindowsService(opts)
	default:
		return fmt.Errorf("unsupported service platform %q", opts.Platform)
	}
}

func (a App) hostUninstallMacOSService(opts hostServiceOptions) error {
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
		"platform": "macos",
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

func (a App) hostUninstallLinuxSystemdService(opts hostServiceOptions) error {
	unitName := opts.Label
	if unitName == "" {
		unitName = service.DefaultLinuxSystemdUnitName
	}
	unitPath, err := serviceUnitPath(opts)
	if err != nil {
		return err
	}
	status, err := service.InspectLinuxSystemdUserService(unitPath)
	if err != nil {
		return err
	}
	if status.Exists && status.UnitName != unitName && !opts.Force {
		return fmt.Errorf("refusing to remove unit %q; expected %q", status.UnitName, unitName)
	}
	removed := false
	if status.Exists {
		if err := os.Remove(unitPath); err != nil {
			return err
		}
		removed = true
	}
	payload := map[string]any{
		"ok":        true,
		"platform":  "linux",
		"unit_name": unitName,
		"unit":      unitPath,
		"removed":   removed,
		"previous":  status,
		"next": map[string]string{
			"ensure_stopped": "systemctl --user disable --now " + unitName,
			"reload":         "systemctl --user daemon-reload",
		},
		"note": "unit removal only; systemctl was not executed by this command",
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) hostUninstallWindowsService(opts hostServiceOptions) error {
	serviceName := opts.Label
	if serviceName == "" {
		serviceName = service.DefaultWindowsServiceName
	}
	plan, err := service.NewWindowsServiceUninstallPlan(serviceName)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":           true,
		"platform":     "windows",
		"service_name": serviceName,
		"commands":     plan.Commands,
		"shell":        plan.Shell,
		"note":         "dry-run only; stop/delete commands were not executed",
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

func serviceUnitPath(opts hostServiceOptions) (string, error) {
	unitName := opts.Label
	if unitName == "" {
		unitName = service.DefaultLinuxSystemdUnitName
	}
	if opts.Unit != "" {
		return opts.Unit, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return service.DefaultLinuxSystemdUserUnitPath(home, unitName), nil
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

func linuxSystemdNextSteps(unitName, unitPath string) map[string]string {
	if unitName == "" {
		unitName = service.DefaultLinuxSystemdUnitName
	}
	return map[string]string{
		"reload":    "systemctl --user daemon-reload",
		"start":     "systemctl --user enable --now " + unitName,
		"stop":      "systemctl --user disable --now " + unitName,
		"inspect":   "systemctl --user status " + unitName,
		"logs":      "journalctl --user -u " + unitName + " -n 100 --no-pager",
		"uninstall": "rdev host uninstall-service --platform linux --label " + unitName + " --unit " + unitPath,
	}
}

func windowsServiceNextSteps(serviceName string) map[string]string {
	if serviceName == "" {
		serviceName = service.DefaultWindowsServiceName
	}
	return map[string]string{
		"create":    "run the planned sc.exe create command from rdev host install-service output in an elevated PowerShell session",
		"start":     "sc.exe start " + serviceName,
		"stop":      "sc.exe stop " + serviceName,
		"inspect":   "sc.exe query " + serviceName + " && sc.exe qc " + serviceName,
		"uninstall": "rdev host uninstall-service --platform windows --label " + serviceName,
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
		"joinUrl": exampleJoinURL(ticket.Code),
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) connectionEntry(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing connection-entry subcommand")
	}
	switch args[0] {
	case "plan":
		fs := flag.NewFlagSet("connection-entry plan", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		invitePath := fs.String("invite", "", "invite JSON path from rdev invite create or rdev.invites.create")
		inviteJSON := fs.String("invite-json", "", "inline invite JSON")
		out := fs.String("out", "", "empty output directory for connection-entry materialization files")
		targetOS := fs.String("target-os", runtime.GOOS, "target OS: windows, darwin, or linux")
		ownership := fs.String("ownership", "", "target ownership: owned or third-party; inferred from invite mode when omitted")
		sessionMode := fs.String("session-mode", "", "session mode override: attended-temporary, managed, or break-glass")
		releaseBundleURL := fs.String("release-bundle-url", "", "signed release bundle index URL for target-side package verification")
		releaseBundlePath := fs.String("release-bundle", "", "target-local signed release bundle path for owned managed-service entries")
		releaseBundleRequiredArtifacts := fs.String("release-bundle-required-artifacts", "", "comma-separated artifact ids required in the release bundle")
		releaseRootPublicKey := fs.String("release-root-public-key", "", "pinned release root public key")
		managedBinaryPath := fs.String("managed-binary", "", "target-local absolute rdev binary path for owned managed-service entries")
		managedServiceName := fs.String("managed-service-name", "", "optional Windows managed service name")
		managedServiceLabel := fs.String("managed-service-label", "", "optional macOS LaunchAgent label")
		managedUnitName := fs.String("managed-unit-name", "", "optional Linux systemd user unit name")
		windowsHostDownloadURL := fs.String("windows-host-download-url", "", "rdev-host.exe download URL for Windows temporary entry materialization")
		windowsHostSHA256 := fs.String("windows-host-sha256", "", "expected SHA-256 for rdev-host.exe")
		windowsVerifierDownloadURL := fs.String("windows-verifier-download-url", "", "rdev-verify.exe download URL")
		windowsVerifierSHA256 := fs.String("windows-verifier-sha256", "", "expected SHA-256 for rdev-verify.exe")
		windowsBootstrapScriptURL := fs.String("windows-bootstrap-script-url", "", "optional URL for downloading windows-temporary.ps1 on the target host")
		windowsBootstrapScriptSHA256 := fs.String("windows-bootstrap-script-sha256", "", "expected SHA-256 for windows-temporary.ps1")
		windowsBootstrapScriptPath := fs.String("windows-bootstrap-script", "", "local windows-temporary.ps1 path; defaults to scripts/bootstrap/windows-temporary.ps1")
		hostName := fs.String("host-name", "", "optional target host display name")
		targetArch := fs.String("target-arch", runtime.GOARCH, "target architecture: amd64 or arm64")
		rdevCommand := fs.String("rdev-command", "rdev", "rdev command embedded in the generated Connection Entry runner launcher")
		force := fs.Bool("force", false, "overwrite generated nested Windows launcher files when supported")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		plan, err := connectionentry.FromInvite(connectionentry.Options{
			InviteJSON:                     *inviteJSON,
			InvitePath:                     *invitePath,
			OutDir:                         *out,
			TargetOS:                       *targetOS,
			Ownership:                      *ownership,
			SessionMode:                    *sessionMode,
			ReleaseBundleURL:               *releaseBundleURL,
			ReleaseBundleRequiredArtifacts: *releaseBundleRequiredArtifacts,
			ReleaseBundlePath:              *releaseBundlePath,
			ReleaseRootPublicKey:           *releaseRootPublicKey,
			ManagedBinaryPath:              *managedBinaryPath,
			ManagedServiceName:             *managedServiceName,
			ManagedServiceLabel:            *managedServiceLabel,
			ManagedUnitName:                *managedUnitName,
			WindowsHostDownloadURL:         *windowsHostDownloadURL,
			WindowsHostExpectedSHA256:      *windowsHostSHA256,
			WindowsVerifierDownloadURL:     *windowsVerifierDownloadURL,
			WindowsVerifierExpectedSHA256:  *windowsVerifierSHA256,
			WindowsBootstrapScriptURL:      *windowsBootstrapScriptURL,
			WindowsBootstrapScriptSHA256:   *windowsBootstrapScriptSHA256,
			WindowsBootstrapScriptPath:     *windowsBootstrapScriptPath,
			HostName:                       *hostName,
			TargetArch:                     *targetArch,
			RdevCommand:                    *rdevCommand,
			Force:                          *force,
		})
		if err != nil {
			return err
		}
		return writeJSON(a.Stdout, map[string]any{
			"ok":                 connectionEntryChecksPassed(plan.Checks),
			"schema":             plan.SchemaVersion,
			"plan":               plan,
			"out":                plan.OutDir,
			"human_message":      plan.HumanMessagePath,
			"entry_package_plan": plan.EntryPackagePlan,
			"runner_plan":        plan.RunnerPlan,
			"missing_inputs":     plan.MissingInputs,
			"generated_files":    plan.GeneratedFiles,
		})
	case "run":
		fs := flag.NewFlagSet("connection-entry run", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		manifestPath := fs.String("runner-manifest", "", "Connection Entry runner manifest path")
		rdevCommand := fs.String("rdev-command", "rdev", "rdev command to execute for host serve")
		dryRun := fs.Bool("dry-run", false, "probe and print selected path without starting host serve")
		probeTimeout := fs.Duration("probe-timeout", 5*time.Second, "per-path gateway probe timeout")
		extraHostArgs := fs.String("host-args", "", "optional comma-separated extra rdev host serve args")
		resultOut := fs.String("result-out", "", "optional path to write the raw Connection Entry runner result JSON for acceptance evidence")
		helperTranscriptOut := fs.String("helper-transcript-out", "", "optional path to write standard helper transcript evidence for relay/connectivity acceptance")
		evidenceDir := fs.String("evidence-dir", "", "optional directory to write runner-result.json, helper-transcript.txt, gateway-status.json, host-status.json, connection-status.json, and audit.jsonl")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, err := connectionrunner.Run(connectionrunner.RunOptions{
			ManifestPath:  *manifestPath,
			RdevCommand:   *rdevCommand,
			DryRun:        *dryRun,
			ProbeTimeout:  *probeTimeout,
			ExtraHostArgs: splitCapabilities(*extraHostArgs),
		})
		if err != nil {
			return err
		}
		if strings.TrimSpace(*resultOut) != "" {
			if err := writeConnectionRunnerResult(*resultOut, result); err != nil {
				return err
			}
		}
		if strings.TrimSpace(*helperTranscriptOut) != "" {
			if err := writeConnectionRunnerHelperTranscript(*helperTranscriptOut, result); err != nil {
				return err
			}
		}
		var evidenceReport connectionrunner.EvidenceReport
		if strings.TrimSpace(*evidenceDir) != "" {
			var err error
			evidenceReport, err = connectionrunner.WriteAcceptanceEvidence(*evidenceDir, result, time.Now().UTC())
			if err != nil {
				return err
			}
		}
		payload := map[string]any{
			"ok":                     result.SelectedPath != "" && len(result.ManualActionRequired) == 0,
			"schema":                 result.SchemaVersion,
			"runner_result":          result,
			"selected_path":          result.SelectedPath,
			"selected_transport":     result.SelectedTransport,
			"helper_transcript":      strings.TrimSpace(*helperTranscriptOut),
			"manual_action_required": result.ManualActionRequired,
			"approval_required":      result.ApprovalRequired,
		}
		if strings.TrimSpace(*evidenceDir) != "" {
			payload["evidence_report"] = evidenceReport
		}
		return writeJSON(a.Stdout, payload)
	default:
		return fmt.Errorf("unknown connection-entry subcommand %q", args[0])
	}
}

func writeConnectionRunnerResult(path string, result connectionrunner.RunResult) error {
	content, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o600)
}

func writeConnectionRunnerHelperTranscript(path string, result connectionrunner.RunResult) error {
	content := []byte(connectionrunner.HelperTranscriptTextForEvidence(result))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o600)
}

func (a App) invite(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing invite subcommand")
	}

	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("invite create", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		gatewayURL := fs.String("gateway", "", "gateway base URL; required so the invite points at a real control plane")
		mode := fs.String("mode", string(model.HostModeAttendedTemporary), "ticket mode: attended-temporary, managed, or break-glass")
		ttl := fs.Int("ttl-seconds", 7200, "ticket TTL in seconds")
		reason := fs.String("reason", "remote support", "ticket reason")
		capList := fs.String("capabilities", "", "comma-separated capabilities; defaults to temporary-mode capabilities")
		transport := fs.String("transport", "auto", "host job transport: auto, wss, long-poll, or poll")
		networkScope := fs.String("network-scope", "auto", "network scope hint: auto, internet, lan, relay, mesh, or ssh")
		authorityProfile := fs.String("authority-profile", "max-control", "agent authority profile: standard or max-control")
		operatorTokenFile := fs.String("operator-token-file", "", "file containing an operator auth bearer token")
		rdevCommand := fs.String("rdev-command", "rdev", "command name or absolute path to run on the target host")
		once := fs.Bool("once", false, "ask the target host process to exit after one job")
		autoApprove := fs.Bool("auto-approve", false, "auto-approve the first attended-temporary host created by this standard Connection Entry")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.inviteCreate(ctx, inviteCreateOptions{
			GatewayURL:        *gatewayURL,
			Mode:              model.HostMode(*mode),
			TTLSeconds:        *ttl,
			Reason:            *reason,
			Capabilities:      splitCapabilities(*capList),
			Transport:         *transport,
			NetworkScope:      *networkScope,
			AuthorityProfile:  *authorityProfile,
			OperatorTokenFile: *operatorTokenFile,
			RdevCommand:       *rdevCommand,
			Once:              *once,
			AutoApprove:       *autoApprove,
		})
	default:
		return fmt.Errorf("unknown invite subcommand %q", args[0])
	}
}

type inviteCreateOptions struct {
	GatewayURL        string
	Mode              model.HostMode
	TTLSeconds        int
	Reason            string
	Capabilities      []string
	Transport         string
	NetworkScope      string
	AuthorityProfile  string
	OperatorTokenFile string
	RdevCommand       string
	Once              bool
	AutoApprove       bool
}

func (a App) inviteCreate(ctx context.Context, opts inviteCreateOptions) error {
	if strings.TrimSpace(opts.GatewayURL) == "" {
		return fmt.Errorf("invite create requires --gateway; run rdev doctor and ask the operator which gateway to use if unclear")
	}
	if !opts.Mode.Valid() {
		return fmt.Errorf("unsupported ticket mode %q", opts.Mode)
	}
	if opts.TTLSeconds < 60 || opts.TTLSeconds > 86400 {
		return fmt.Errorf("ttl-seconds must be between 60 and 86400")
	}
	if opts.AutoApprove && opts.Mode != model.HostModeAttendedTemporary {
		return fmt.Errorf("--auto-approve is only supported for attended-temporary Connection Entries")
	}
	payload, err := createGatewayInviteTicket(ctx, http.DefaultClient, opts)
	if err != nil {
		return err
	}
	invite, err := agentinvite.New(agentinvite.Options{
		GatewayURL:            opts.GatewayURL,
		JoinURL:               payload.JoinURL,
		ManifestURL:           payload.ManifestURL,
		ManifestRootPublicKey: payload.ManifestRootPublicKey,
		Ticket:                payload.Ticket,
		Transport:             opts.Transport,
		NetworkScope:          opts.NetworkScope,
		AuthorityProfile:      opts.AuthorityProfile,
		Once:                  opts.Once,
		RequireHostApproval:   !opts.AutoApprove,
		RdevCommand:           opts.RdevCommand,
	})
	if err != nil {
		return err
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(invite)
}

type gatewayInviteTicketPayload struct {
	Ticket                model.Ticket `json:"ticket"`
	JoinURL               string       `json:"joinUrl"`
	ManifestURL           string       `json:"manifestUrl"`
	ManifestRootPublicKey string       `json:"manifestRootPublicKey"`
	Error                 string       `json:"error"`
}

func createGatewayInviteTicket(ctx context.Context, client *http.Client, opts inviteCreateOptions) (gatewayInviteTicketPayload, error) {
	body, err := json.Marshal(map[string]any{
		"mode":         opts.Mode,
		"ttl_seconds":  opts.TTLSeconds,
		"reason":       opts.Reason,
		"capabilities": opts.Capabilities,
		"auto_approve": opts.AutoApprove,
		"metadata":     inviteTicketMetadata(opts),
	})
	if err != nil {
		return gatewayInviteTicketPayload{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gatewayTicketsURL(opts.GatewayURL), bytes.NewReader(body))
	if err != nil {
		return gatewayInviteTicketPayload{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if opts.OperatorTokenFile != "" {
		token, err := readTokenFile(opts.OperatorTokenFile)
		if err != nil {
			return gatewayInviteTicketPayload{}, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := doGatewayRequest(client, req)
	if err != nil {
		return gatewayInviteTicketPayload{}, err
	}
	defer resp.Body.Close()
	var payload gatewayInviteTicketPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return gatewayInviteTicketPayload{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return gatewayInviteTicketPayload{}, fmt.Errorf("create invite ticket failed: %s", payload.Error)
	}
	return payload, nil
}

func inviteTicketMetadata(opts inviteCreateOptions) map[string]string {
	if !opts.AutoApprove || opts.Mode != model.HostModeAttendedTemporary {
		return nil
	}
	return map[string]string{
		"auto_approve":      "attended-temporary",
		"connection_entry":  "standard-visible",
		"approval_contract": "target-consent-scoped-ticket",
	}
}

func gatewayTicketsURL(gatewayURL string) string {
	base := strings.TrimRight(gatewayURL, "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/tickets"
	}
	return base + "/v1/tickets"
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
		"joinUrl":   exampleJoinURL(ticket.Code),
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

func exampleJoinURL(ticketCode string) string {
	return strings.TrimRight(exampleAgentJoinBaseURL, "/") + "/join/" + ticketCode
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
		statePath := fs.String("state", "", "optional development gateway JSON snapshot path; requires --signing-key")
		storageProvider := fs.String("storage-provider", "", "gateway state storage provider: file, postgres, redis-stream, or s3-compatible")
		storagePath := fs.String("storage-path", "", "gateway state storage path for file, libpq connection info/service name for postgres, redis:// URL for redis-stream, or s3://bucket/prefix for s3-compatible")
		signingKey := fs.String("signing-key", "", "optional persistent Ed25519 signing key file")
		signingKeyID := fs.String("signing-key-id", signing.DefaultKeyID, "signing key id for new or existing signing key file")
		manifestSigningKey := fs.String("manifest-signing-key", "", "optional Ed25519 key file for signing join manifests")
		manifestSigningKeyID := fs.String("manifest-signing-key-id", "manifest-dev", "signing key id for join manifests")
		enrollmentRootPublicKey := fs.String("enrollment-root-public-key", "", "optional enrollment root public key; when set, host registration requires rdev.host-enrollment-certificate.v1")
		enrollmentKey := fs.String("enrollment-key", "", "optional Ed25519 enrollment root signing key file for dev hosted certificate issuance")
		enrollmentKeyID := fs.String("enrollment-key-id", "enrollment-root", "enrollment root signing key id for new or existing enrollment key file")
		enrollmentRevocations := fs.String("enrollment-revocations", "", "optional signed enrollment revocation list JSON path")
		tlsCert := fs.String("tls-cert", "", "optional TLS server certificate PEM path for the development gateway")
		tlsKey := fs.String("tls-key", "", "optional TLS server private key PEM path for the development gateway")
		clientCA := fs.String("client-ca", "", "optional client CA PEM path; when set, require and verify client certificates")
		operatorAuth := fs.String("operator-auth", "", "optional operator auth JSON file requiring bearer tokens for control-plane APIs")
		hostedOperatorAuth := fs.String("hosted-operator-auth", "", "optional hosted operator auth JSON file for EdDSA JWT role tokens")
		oidcJWKSOperatorAuth := fs.String("oidc-jwks-operator-auth", "", "optional OIDC JWKS operator auth JSON file for RS256 JWT role tokens")
		samlOperatorAuth := fs.String("saml-operator-auth", "", "optional SAML operator auth JSON file for signed SAMLResponse bearer tokens")
		rdevAssetsDir := fs.String("rdev-assets-dir", "", "optional directory containing rdev-windows-amd64.exe, rdev-darwin-arm64, rdev-darwin-amd64, rdev-linux-amd64, and rdev-linux-arm64 helpers")
		autoBuildRdevAssets := fs.Bool("auto-build-rdev-assets", true, "auto-build missing platform rdev helpers for dev gateway Connection Entry bootstraps when a checkout and Go are available")
		rdevWindowsAMD64 := fs.String("rdev-windows-amd64", "", "optional rdev.exe helper served to Windows amd64 Connection Entry bootstraps")
		rdevDarwinARM64 := fs.String("rdev-darwin-arm64", "", "optional rdev helper served to macOS arm64 Connection Entry bootstraps")
		rdevDarwinAMD64 := fs.String("rdev-darwin-amd64", "", "optional rdev helper served to macOS amd64 Connection Entry bootstraps")
		rdevLinuxAMD64 := fs.String("rdev-linux-amd64", "", "optional rdev helper served to Linux amd64 Connection Entry bootstraps")
		rdevLinuxARM64 := fs.String("rdev-linux-arm64", "", "optional rdev helper served to Linux arm64 Connection Entry bootstraps")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if !*dev {
			return fmt.Errorf("gateway serve currently requires --dev")
		}
		return a.gatewayServeDev(gatewayServeOptions{
			Addr:                     *addr,
			AuditLog:                 *auditLog,
			StatePath:                *statePath,
			StorageProvider:          *storageProvider,
			StoragePath:              *storagePath,
			SigningKeyPath:           *signingKey,
			SigningKeyID:             *signingKeyID,
			ManifestSigningKeyPath:   *manifestSigningKey,
			ManifestSigningKeyID:     *manifestSigningKeyID,
			EnrollmentRootPublicKey:  *enrollmentRootPublicKey,
			EnrollmentKeyPath:        *enrollmentKey,
			EnrollmentKeyID:          *enrollmentKeyID,
			EnrollmentRevocations:    *enrollmentRevocations,
			TLSCertPath:              *tlsCert,
			TLSKeyPath:               *tlsKey,
			ClientCAPath:             *clientCA,
			OperatorAuthPath:         *operatorAuth,
			HostedOperatorAuthPath:   *hostedOperatorAuth,
			OIDCJWKSOperatorAuthPath: *oidcJWKSOperatorAuth,
			SAMLOperatorAuthPath:     *samlOperatorAuth,
			RdevAssetsDir:            *rdevAssetsDir,
			AutoBuildRdevAssets:      *autoBuildRdevAssets,
			RdevWindowsAMD64Path:     *rdevWindowsAMD64,
			RdevDarwinARM64Path:      *rdevDarwinARM64,
			RdevDarwinAMD64Path:      *rdevDarwinAMD64,
			RdevLinuxAMD64Path:       *rdevLinuxAMD64,
			RdevLinuxARM64Path:       *rdevLinuxARM64,
		})
	case "storage":
		if len(args) < 2 {
			return fmt.Errorf("missing gateway storage subcommand")
		}
		switch args[1] {
		case "verify":
			fs := flag.NewFlagSet("gateway storage verify", flag.ContinueOnError)
			fs.SetOutput(a.Stderr)
			provider := fs.String("provider", gateway.FileStateStoreProvider, "gateway state storage provider: file, postgres, redis-stream, or s3-compatible")
			path := fs.String("path", "", "gateway state storage path for file, libpq connection info/service name for postgres, redis:// URL for redis-stream, or s3://bucket/prefix for s3-compatible")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			return a.gatewayStorageVerify(*provider, *path)
		default:
			return fmt.Errorf("unknown gateway storage subcommand %q", args[1])
		}
	default:
		return fmt.Errorf("unknown gateway subcommand %q", args[0])
	}
}

func (a App) gatewayStorageVerify(provider, path string) error {
	store, err := newGatewayStateStore(provider, path)
	if err != nil {
		return err
	}
	checks := []map[string]any{}
	add := func(name string, ok bool, detail string) {
		checks = append(checks, map[string]any{"name": name, "ok": ok, "detail": detail})
	}
	add("store_constructed", true, store.Describe())
	runtimeOK := true
	if verifier, ok := store.(interface{ VerifyRuntime() error }); ok {
		if err := verifier.VerifyRuntime(); err != nil {
			runtimeOK = false
			add("runtime_probe", false, err.Error())
		} else {
			add("runtime_probe", true, "round-trip ok")
		}
	}
	payload := map[string]any{
		"ok":       runtimeOK,
		"provider": strings.TrimSpace(provider),
		"store":    store.Describe(),
		"checks":   checks,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !runtimeOK {
		return fmt.Errorf("gateway storage verification failed")
	}
	return nil
}

// job provides operator-side access to jobs through the gateway HTTP API so
// that the Agent can check, wait for, and cancel jobs without having to drop
// down to raw curl calls against the HTTP API.
//
// Sub-commands:
//
//	rdev job create --gateway-url <url> --host-id <id> --adapter <name> --intent <text> (--policy-json <json>|--policy-file <path>)
//	rdev job list   --gateway-url <url> [--host-id <id>]
//	rdev job get    --gateway-url <url> --job-id <id>
//	rdev job wait   --gateway-url <url> --job-id <id> [--timeout-seconds <n>]
//	rdev job artifacts --gateway-url <url> --job-id <id>
//	rdev job policy-template --capability <name> [--target-os <os>]
//	rdev job cancel --gateway-url <url> --job-id <id> [--reason <text>]
func (a App) job(ctx context.Context, args []string) error {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(a.Stderr, "usage: rdev job <create|list|get|wait|artifacts|policy-template|cancel> [flags]")
		return fmt.Errorf("missing job subcommand")
	}
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("job create", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		gatewayURL := fs.String("gateway-url", "", "gateway API base URL (required)")
		hostID := fs.String("host-id", "", "target host ID (required)")
		adapter := fs.String("adapter", "shell", "job adapter, for example shell or powershell")
		intent := fs.String("intent", "", "short human-readable job intent (required)")
		policyJSON := fs.String("policy-json", "", "inline JSON policy object")
		policyFile := fs.String("policy-file", "", "path to JSON policy object")
		operatorTokenFile := fs.String("operator-token-file", "", "file containing an operator bearer token")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *gatewayURL == "" {
			return fmt.Errorf("--gateway-url is required")
		}
		if *hostID == "" {
			return fmt.Errorf("--host-id is required")
		}
		if strings.TrimSpace(*intent) == "" {
			return fmt.Errorf("--intent is required")
		}
		policy, err := readJobPolicy(*policyJSON, *policyFile)
		if err != nil {
			return err
		}
		return a.jobCreate(ctx, *gatewayURL, *hostID, *adapter, *intent, policy, *operatorTokenFile)

	case "list":
		fs := flag.NewFlagSet("job list", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		gatewayURL := fs.String("gateway-url", "", "gateway API base URL (required)")
		hostID := fs.String("host-id", "", "filter by host ID (optional)")
		operatorTokenFile := fs.String("operator-token-file", "", "file containing an operator bearer token")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *gatewayURL == "" {
			return fmt.Errorf("--gateway-url is required")
		}
		return a.jobList(ctx, *gatewayURL, *hostID, *operatorTokenFile)

	case "get":
		fs := flag.NewFlagSet("job get", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		gatewayURL := fs.String("gateway-url", "", "gateway API base URL (required)")
		jobID := fs.String("job-id", "", "job ID (required)")
		operatorTokenFile := fs.String("operator-token-file", "", "file containing an operator bearer token")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *gatewayURL == "" {
			return fmt.Errorf("--gateway-url is required")
		}
		if *jobID == "" {
			return fmt.Errorf("--job-id is required")
		}
		return a.jobGet(ctx, *gatewayURL, *jobID, *operatorTokenFile)

	case "wait":
		fs := flag.NewFlagSet("job wait", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		gatewayURL := fs.String("gateway-url", "", "gateway API base URL (required)")
		jobID := fs.String("job-id", "", "job ID (required)")
		timeoutSeconds := fs.Int("timeout-seconds", 120, "max seconds to wait for job completion")
		timeout := fs.Duration("timeout", 0, "alias for --timeout-seconds, as a Go duration such as 90s or 2m")
		operatorTokenFile := fs.String("operator-token-file", "", "file containing an operator bearer token")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *timeout > 0 {
			*timeoutSeconds = int(timeout.Round(time.Second) / time.Second)
			if *timeoutSeconds <= 0 {
				*timeoutSeconds = 1
			}
		}
		if *gatewayURL == "" {
			return fmt.Errorf("--gateway-url is required")
		}
		if *jobID == "" {
			return fmt.Errorf("--job-id is required")
		}
		return a.jobWait(ctx, *gatewayURL, *jobID, *timeoutSeconds, *operatorTokenFile)

	case "artifacts":
		fs := flag.NewFlagSet("job artifacts", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		gatewayURL := fs.String("gateway-url", "", "gateway API base URL (required)")
		jobID := fs.String("job-id", "", "job ID (required)")
		operatorTokenFile := fs.String("operator-token-file", "", "file containing an operator bearer token")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *gatewayURL == "" {
			return fmt.Errorf("--gateway-url is required")
		}
		if *jobID == "" {
			return fmt.Errorf("--job-id is required")
		}
		return a.jobArtifacts(ctx, *gatewayURL, *jobID, *operatorTokenFile)

	case "policy-template":
		fs := flag.NewFlagSet("job policy-template", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		capability := fs.String("capability", "shell.user", "template capability: shell.user, fs.read, fs.write.scoped, process.inspect, tool.availability")
		targetOS := fs.String("target-os", "auto", "target OS hint: auto, windows, macos, linux")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return writeJSON(a.Stdout, jobtemplate.PolicyTemplate(*capability, *targetOS))

	case "cancel":
		fs := flag.NewFlagSet("job cancel", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		gatewayURL := fs.String("gateway-url", "", "gateway API base URL (required)")
		jobID := fs.String("job-id", "", "job ID (required)")
		reason := fs.String("reason", "canceled by operator via CLI", "cancellation reason")
		operatorTokenFile := fs.String("operator-token-file", "", "file containing an operator bearer token")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *gatewayURL == "" {
			return fmt.Errorf("--gateway-url is required")
		}
		if *jobID == "" {
			return fmt.Errorf("--job-id is required")
		}
		return a.jobCancel(ctx, *gatewayURL, *jobID, *reason, *operatorTokenFile)

	default:
		return fmt.Errorf("unknown job subcommand %q; available: create, list, get, wait, artifacts, policy-template, cancel", args[0])
	}
}

func readJobPolicy(policyJSON, policyFile string) (map[string]any, error) {
	if strings.TrimSpace(policyJSON) != "" && strings.TrimSpace(policyFile) != "" {
		return nil, fmt.Errorf("use only one of --policy-json or --policy-file")
	}
	var data []byte
	switch {
	case strings.TrimSpace(policyJSON) != "":
		data = []byte(policyJSON)
	case strings.TrimSpace(policyFile) != "":
		content, err := os.ReadFile(policyFile)
		if err != nil {
			return nil, fmt.Errorf("read --policy-file: %w", err)
		}
		data = content
	default:
		return nil, fmt.Errorf("--policy-json or --policy-file is required")
	}
	var policy map[string]any
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("decode job policy JSON: %w", err)
	}
	if policy == nil {
		policy = map[string]any{}
	}
	return policy, nil
}

func (a App) jobCreate(ctx context.Context, gatewayURL, hostID, adapter, intent string, policy map[string]any, operatorTokenFile string) error {
	u := strings.TrimRight(gatewayURL, "/") + "/v1/jobs"
	body, err := json.Marshal(map[string]any{
		"host_id": hostID,
		"adapter": adapter,
		"intent":  intent,
		"policy":  policy,
	})
	if err != nil {
		return fmt.Errorf("marshal job create request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := loadOperatorToken(operatorTokenFile); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", u, err)
	}
	defer resp.Body.Close()
	return writeHTTPResponseJSON(a.Stdout, resp)
}

func (a App) jobList(ctx context.Context, gatewayURL, hostID, operatorTokenFile string) error {
	u := strings.TrimRight(gatewayURL, "/") + "/v1/jobs"
	if hostID != "" {
		u += "?host_id=" + url.QueryEscape(hostID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	if tok := loadOperatorToken(operatorTokenFile); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	return writeHTTPResponseJSON(a.Stdout, resp)
}

func (a App) jobGet(ctx context.Context, gatewayURL, jobID, operatorTokenFile string) error {
	u := strings.TrimRight(gatewayURL, "/") + "/v1/jobs/" + url.PathEscape(jobID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	if tok := loadOperatorToken(operatorTokenFile); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	return writeHTTPResponseJSON(a.Stdout, resp)
}

// jobWait polls GET /v1/jobs/:id every 2 seconds until the job reaches a
// terminal state (succeeded, completed, failed, canceled) or the timeout expires.
//
// Note: the model layer uses "succeeded" as the success terminal state.
// "completed" is retained here for backward compatibility with older gateway
// versions that may still emit that value.
func (a App) jobWait(ctx context.Context, gatewayURL, jobID string, timeoutSeconds int, operatorTokenFile string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	tok := loadOperatorToken(operatorTokenFile)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	u := strings.TrimRight(gatewayURL, "/") + "/v1/jobs/" + url.PathEscape(jobID)
	for {
		payload, err := fetchJobJSON(ctx, u, tok)
		if err != nil {
			return fmt.Errorf("polling job: %w", err)
		}
		job := nestedMapOrSelf(payload, "job")
		jobStatus, _ := job["status"].(string)
		switch jobStatus {
		case "succeeded", "completed", "failed", "canceled":
			return writeJSON(a.Stdout, job)
		}
		_, _ = fmt.Fprintf(a.Stderr, "[rdev] job %s status=%s; waiting...\n", jobID, jobStatus)
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return fmt.Errorf("job %s did not reach a terminal state within %ds (last status: %s)", jobID, timeoutSeconds, jobStatus)
		}
	}
}

func (a App) jobArtifacts(ctx context.Context, gatewayURL, jobID, operatorTokenFile string) error {
	u := strings.TrimRight(gatewayURL, "/") + "/v1/jobs/" + url.PathEscape(jobID) + "/artifacts"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	if tok := loadOperatorToken(operatorTokenFile); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	return writeHTTPResponseJSON(a.Stdout, resp)
}

func (a App) jobCancel(ctx context.Context, gatewayURL, jobID, reason, operatorTokenFile string) error {
	u := strings.TrimRight(gatewayURL, "/") + "/v1/jobs/" + url.PathEscape(jobID) + "/cancel"
	body, _ := json.Marshal(map[string]any{"reason": reason})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := loadOperatorToken(operatorTokenFile); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", u, err)
	}
	defer resp.Body.Close()
	return writeHTTPResponseJSON(a.Stdout, resp)
}

// fetchJobJSON calls GET on the job URL and returns the parsed JSON object.
func fetchJobJSON(ctx context.Context, jobURL, bearerToken string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jobURL, nil)
	if err != nil {
		return nil, err
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return payload, fmt.Errorf("HTTP %d from gateway", resp.StatusCode)
	}
	return payload, nil
}

// writeHTTPResponseJSON forwards a gateway API response body to w as
// pretty-printed JSON, returning an error if the status is ≥ 400.
func writeHTTPResponseJSON(w io.Writer, resp *http.Response) error {
	var payload any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decoding response body: %w", err)
	}
	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling response: %w", err)
	}
	_, _ = fmt.Fprintf(w, "%s\n", out)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("gateway returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// loadOperatorToken reads a bearer token from a file, or falls back to the
// RDEV_OPERATOR_TOKEN environment variable. Returns "" if neither is set.
func loadOperatorToken(tokenFile string) string {
	if tokenFile != "" {
		if data, err := os.ReadFile(tokenFile); err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return strings.TrimSpace(os.Getenv("RDEV_OPERATOR_TOKEN"))
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
	case "plan-install":
		fs := flag.NewFlagSet("skillkit plan-install", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		bundle := fs.String("bundle", "", "verified skillkit bundle directory")
		out := fs.String("out", "", "empty output directory for install plan and scripts")
		frameworks := fs.String("frameworks", "", "comma-separated frameworks: codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent")
		rdevCommand := fs.String("rdev-command", "", "rdev command to embed in generated scripts; default auto-detects a stable rdev binary")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.skillkitPlanInstall(*bundle, *out, *frameworks, *rdevCommand)
	case "verify-install-plan":
		fs := flag.NewFlagSet("skillkit verify-install-plan", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		plan := fs.String("plan", "", "install-plan.json generated by rdev skillkit plan-install")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.skillkitVerifyInstallPlan(*plan)
	case "install":
		fs := flag.NewFlagSet("skillkit install", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		bundle := fs.String("bundle", "", "verified skillkit bundle directory")
		framework := fs.String("framework", "", "target framework: codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent")
		target := fs.String("target", "", "target skill directory override; required for generic-mcp-agent")
		execute := fs.Bool("execute", false, "actually copy files; default is dry-run")
		force := fs.Bool("force", false, "overwrite existing skill directories after review")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.skillkitInstall(*bundle, *framework, *target, *execute, *force)
	default:
		return fmt.Errorf("unknown skillkit subcommand %q", args[0])
	}
}

func (a App) adapter(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing adapter subcommand")
	}
	switch args[0] {
	case "scaffold":
		fs := flag.NewFlagSet("adapter scaffold", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		adapterName := fs.String("adapter", "", "adapter name, for example claude-code")
		out := fs.String("out", "", "output lifecycle manifest path")
		resultSchema := fs.String("result-schema", "", "adapter result artifact schema; defaults to rdev.<adapter>-result.v1")
		force := fs.Bool("force", false, "overwrite an existing output file")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.adapterScaffold(adapterScaffoldOptions{
			Adapter:      *adapterName,
			OutPath:      *out,
			ResultSchema: *resultSchema,
			Force:        *force,
		})
	case "verify-result":
		fs := flag.NewFlagSet("adapter verify-result", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		artifact := fs.String("artifact", "", "adapter result artifact JSON path, or - for stdin")
		adapterName := fs.String("adapter", "", "expected adapter name")
		schemaVersion := fs.String("schema", "", "expected adapter result schema version")
		commandFields := fs.String("command-fields", "", "comma-separated nested command evidence fields; defaults to top-level")
		requiredStringFields := fs.String("required-string-fields", "workspace_root", "comma-separated required top-level string fields")
		requireTiming := fs.Bool("require-timing", true, "require started_at, ended_at, and nonnegative duration_millis")
		requireRedaction := fs.Bool("require-redaction", true, "require redaction metadata")
		rejectSecrets := fs.Bool("reject-secret-patterns", true, "reject common unredacted secret patterns")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.adapterVerifyResult(adapterVerifyResultOptions{
			ArtifactPath:             *artifact,
			Adapter:                  *adapterName,
			SchemaVersion:            *schemaVersion,
			CommandFields:            splitCapabilities(*commandFields),
			RequiredStringFields:     splitCapabilities(*requiredStringFields),
			RequireTiming:            *requireTiming,
			RequireRedaction:         *requireRedaction,
			RejectUnredactedPatterns: *rejectSecrets,
		})
	case "verify-lifecycle":
		fs := flag.NewFlagSet("adapter verify-lifecycle", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		artifact := fs.String("artifact", "", "adapter lifecycle manifest JSON path, or - for stdin")
		adapterName := fs.String("adapter", "", "expected adapter name")
		schemaVersion := fs.String("schema", adapterkit.LifecycleManifestSchemaVersion, "expected adapter lifecycle manifest schema version")
		requiredPhases := fs.String("required-phases", "detect,plan,prepare,run,collect,cleanup", "comma-separated required lifecycle phases")
		requireSafety := fs.Bool("require-safety", true, "require safety boundary declarations")
		requireCancellation := fs.Bool("require-cancellation", true, "require cancellation support declarations")
		requireResultSchema := fs.Bool("require-result-schema", true, "require collect.result_schema")
		rejectSecrets := fs.Bool("reject-secret-patterns", true, "reject common unredacted secret patterns")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.adapterVerifyLifecycle(adapterVerifyLifecycleOptions{
			ArtifactPath:             *artifact,
			Adapter:                  *adapterName,
			SchemaVersion:            *schemaVersion,
			RequiredPhases:           splitCapabilities(*requiredPhases),
			RequireSafety:            *requireSafety,
			RequireCancellation:      *requireCancellation,
			RequireResultSchema:      *requireResultSchema,
			RejectUnredactedPatterns: *rejectSecrets,
		})
	case "verify-cancellation":
		fs := flag.NewFlagSet("adapter verify-cancellation", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		artifact := fs.String("artifact", "", "adapter cancellation result artifact JSON path, or - for stdin")
		adapterName := fs.String("adapter", "", "expected adapter name")
		schemaVersion := fs.String("schema", "", "expected adapter result schema version")
		commandFields := fs.String("command-fields", "", "comma-separated nested command evidence fields; defaults to top-level")
		requiredStringFields := fs.String("required-string-fields", "workspace_root", "comma-separated required top-level string fields")
		requireTiming := fs.Bool("require-timing", true, "require started_at, ended_at, and nonnegative duration_millis")
		requireRedaction := fs.Bool("require-redaction", true, "require redaction metadata")
		rejectSecrets := fs.Bool("reject-secret-patterns", true, "reject common unredacted secret patterns")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.adapterVerifyCancellation(adapterVerifyCancellationOptions{
			ArtifactPath:             *artifact,
			Adapter:                  *adapterName,
			SchemaVersion:            *schemaVersion,
			CommandFields:            splitCapabilities(*commandFields),
			RequiredStringFields:     splitCapabilities(*requiredStringFields),
			RequireTiming:            *requireTiming,
			RequireRedaction:         *requireRedaction,
			RejectUnredactedPatterns: *rejectSecrets,
		})
	case "verify-runtime":
		fs := flag.NewFlagSet("adapter verify-runtime", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		artifact := fs.String("artifact", "", "adapter runtime fixture JSON path, or - for stdin")
		adapterName := fs.String("adapter", "", "expected adapter name")
		requiredPhases := fs.String("required-phases", "detect,plan,prepare,run,collect,cleanup", "comma-separated required runtime phases")
		requireSuccessful := fs.Bool("require-successful", true, "require each required phase to have ok=true")
		requireCleanup := fs.Bool("require-cleanup", true, "require cleanup_attempted and cleanup_ok")
		requireResultArtifact := fs.Bool("require-result-artifact", false, "require result_artifact_schema and result_artifact")
		requireCancellation := fs.Bool("require-cancellation", false, "require canceled=true, timed_out=false, and cleanup after cancel")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.adapterVerifyRuntime(adapterVerifyRuntimeOptions{
			ArtifactPath:          *artifact,
			Adapter:               *adapterName,
			RequiredPhases:        splitCapabilities(*requiredPhases),
			RequireSuccessful:     *requireSuccessful,
			RequireCleanup:        *requireCleanup,
			RequireResultArtifact: *requireResultArtifact,
			RequireCancellation:   *requireCancellation,
		})
	default:
		return fmt.Errorf("unknown adapter subcommand %q", args[0])
	}
}

type adapterVerifyResultOptions struct {
	ArtifactPath             string
	Adapter                  string
	SchemaVersion            string
	CommandFields            []string
	RequiredStringFields     []string
	RequireTiming            bool
	RequireRedaction         bool
	RejectUnredactedPatterns bool
}

type adapterScaffoldOptions struct {
	Adapter      string
	OutPath      string
	ResultSchema string
	Force        bool
}

type adapterVerifyLifecycleOptions struct {
	ArtifactPath             string
	Adapter                  string
	SchemaVersion            string
	RequiredPhases           []string
	RequireSafety            bool
	RequireCancellation      bool
	RequireResultSchema      bool
	RejectUnredactedPatterns bool
}

type adapterVerifyCancellationOptions struct {
	ArtifactPath             string
	Adapter                  string
	SchemaVersion            string
	CommandFields            []string
	RequiredStringFields     []string
	RequireTiming            bool
	RequireRedaction         bool
	RejectUnredactedPatterns bool
}

type adapterVerifyRuntimeOptions struct {
	ArtifactPath          string
	Adapter               string
	RequiredPhases        []string
	RequireSuccessful     bool
	RequireCleanup        bool
	RequireResultArtifact bool
	RequireCancellation   bool
}

type adapterLifecycleManifest struct {
	SchemaVersion string                       `json:"schema_version"`
	Adapter       string                       `json:"adapter"`
	Phases        adapterLifecyclePhases       `json:"phases"`
	Safety        adapterLifecycleSafety       `json:"safety"`
	Cancellation  adapterLifecycleCancellation `json:"cancellation"`
}

type adapterLifecyclePhases struct {
	Detect  adapterPhase `json:"detect"`
	Plan    adapterPhase `json:"plan"`
	Prepare adapterPhase `json:"prepare"`
	Run     adapterPhase `json:"run"`
	Collect adapterPhase `json:"collect"`
	Cleanup adapterPhase `json:"cleanup"`
}

type adapterPhase struct {
	Implemented                  bool     `json:"implemented"`
	Evidence                     []string `json:"evidence"`
	DeclaresExternalConsequences bool     `json:"declares_external_consequences,omitempty"`
	DeclaresRequiredApprovals    bool     `json:"declares_required_approvals,omitempty"`
	EnforcesWorkspaceBoundary    bool     `json:"enforces_workspace_boundary,omitempty"`
	UsesWorkspaceLock            bool     `json:"uses_workspace_lock,omitempty"`
	SupportsTimeout              bool     `json:"supports_timeout,omitempty"`
	SupportsCancellation         bool     `json:"supports_cancellation,omitempty"`
	EmitsResultArtifact          bool     `json:"emits_result_artifact,omitempty"`
	ResultSchema                 string   `json:"result_schema,omitempty"`
	Idempotent                   bool     `json:"idempotent,omitempty"`
	ReleasesLocks                bool     `json:"releases_locks,omitempty"`
}

type adapterLifecycleSafety struct {
	AdapterAuthorizesJobs           bool `json:"adapter_authorizes_jobs"`
	AdapterApprovesDangerousActions bool `json:"adapter_approves_dangerous_actions"`
	AdapterInstallsPersistence      bool `json:"adapter_installs_persistence"`
	HostValidatesBeforeRun          bool `json:"host_validates_before_run"`
	RedactsOutputs                  bool `json:"redacts_outputs"`
}

type adapterLifecycleCancellation struct {
	Supported        bool   `json:"supported"`
	EvidenceField    string `json:"evidence_field"`
	TimeoutExclusive bool   `json:"timeout_exclusive"`
	CleanupOnCancel  bool   `json:"cleanup_on_cancel"`
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
	case "prepare-candidate":
		fs := flag.NewFlagSet("release prepare-candidate", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		sourceRoot := fs.String("source-root", ".", "repository source root containing skills/ and mcp/tools.json")
		out := fs.String("out", "", "empty output directory for the release candidate")
		version := fs.String("version", "", "release version, for example v0.1.0")
		gatewayURL := fs.String("gateway-url", "", "default gateway URL to include in Skillkit install docs")
		artifacts := fs.String("artifacts", "", "comma-separated built artifact paths to stage, sign, and include")
		requiredArtifacts := fs.String("require-artifacts", "", "comma-separated artifact ids that must be present in the release bundle")
		keyPath := fs.String("key", "", "Ed25519 release signing key file")
		keyID := fs.String("key-id", "release-root", "release signing key id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.releasePrepareCandidate(*sourceRoot, *out, *version, *gatewayURL, splitCapabilities(*artifacts), splitCapabilities(*requiredArtifacts), *keyPath, *keyID)
	case "verify-candidate":
		fs := flag.NewFlagSet("release verify-candidate", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		candidatePath := fs.String("candidate", "", "release candidate directory or release-candidate.json path")
		requiredArtifacts := fs.String("require-artifacts", "", "comma-separated artifact ids that must be present in the release bundle and candidate summary")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.releaseVerifyCandidate(*candidatePath, splitCapabilities(*requiredArtifacts))
	default:
		return fmt.Errorf("unknown release subcommand %q", args[0])
	}
}

func (a App) gatewayServeDev(opts gatewayServeOptions) error {
	store, err := gatewayStateStore(opts)
	if err != nil {
		return err
	}
	if store != nil && opts.SigningKeyPath == "" {
		return fmt.Errorf("gateway serve persistent storage requires --signing-key so restored job envelopes keep the same trust root")
	}
	key, created, err := signing.LoadOrCreate(opts.SigningKeyPath, opts.SigningKeyID)
	if err != nil {
		return err
	}
	gw := gateway.NewMemoryGatewayWithSigningKey(time.Now, key.ID, key.PublicKey, key.PrivateKey)
	if opts.EnrollmentRevocations != "" && opts.EnrollmentRootPublicKey == "" {
		if opts.EnrollmentKeyPath == "" {
			return fmt.Errorf("gateway serve --enrollment-revocations requires --enrollment-root-public-key or --enrollment-key")
		}
	}
	if opts.EnrollmentKeyPath != "" {
		enrollmentKey, enrollmentCreated, err := signing.LoadOrCreate(opts.EnrollmentKeyPath, opts.EnrollmentKeyID)
		if err != nil {
			return err
		}
		enrollmentRoot := model.NewTrustBundle(enrollmentKey.ID, enrollmentKey.PublicKey)
		if opts.EnrollmentRootPublicKey != "" {
			configuredRoot, err := parseRootPublicKey(opts.EnrollmentRootPublicKey)
			if err != nil {
				return err
			}
			if configuredRoot.SigningKeyID != enrollmentRoot.SigningKeyID || configuredRoot.PublicKey != enrollmentRoot.PublicKey {
				return fmt.Errorf("gateway serve --enrollment-key does not match --enrollment-root-public-key")
			}
		}
		gw.WithEnrollmentIssuer(enrollmentRoot, enrollmentKey.PrivateKey)
		action := "loaded"
		if enrollmentCreated {
			action = "created"
		}
		_, _ = fmt.Fprintf(a.Stderr, "rdev gateway enrollment signing key %s at %s\n", action, opts.EnrollmentKeyPath)
		_, _ = fmt.Fprintf(a.Stderr, "rdev gateway enrollment root id=%s public_key=%s\n", enrollmentKey.ID, encodeRootPublicKey(enrollmentKey.ID, enrollmentKey.PublicKey))
		opts.EnrollmentRootPublicKey = encodeRootPublicKey(enrollmentKey.ID, enrollmentKey.PublicKey)
	}
	if opts.EnrollmentRootPublicKey != "" {
		root, err := parseRootPublicKey(opts.EnrollmentRootPublicKey)
		if err != nil {
			return err
		}
		gw.WithEnrollmentRoot(root)
		if opts.EnrollmentRevocations != "" {
			revocations, err := readEnrollmentRevocationListFile(opts.EnrollmentRevocations)
			if err != nil {
				return err
			}
			if err := model.VerifyHostEnrollmentRevocationListSignature(revocations, root, time.Now()); err != nil {
				return err
			}
			gw.WithEnrollmentRevocations(revocations)
			_, _ = fmt.Fprintf(a.Stderr, "rdev gateway enrollment revocations loaded path=%s revoked=%d\n", opts.EnrollmentRevocations, len(revocations.RevokedCertificates))
		}
		fingerprint, err := root.Fingerprint()
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(a.Stderr, "rdev gateway enrollment root id=%s fingerprint=%s\n", root.SigningKeyID, fingerprint)
	}
	if store != nil {
		snapshot, loaded, err := store.LoadInto(gw)
		if err != nil {
			return err
		}
		if loaded {
			_, _ = fmt.Fprintf(a.Stderr, "rdev gateway state loaded from %s tickets=%d hosts=%d jobs=%d audit=%d\n", store.Describe(), len(snapshot.Tickets), len(snapshot.Hosts), len(snapshot.Jobs), len(snapshot.Audit))
		} else {
			_, _ = fmt.Fprintf(a.Stderr, "rdev gateway state will be created at %s\n", store.Describe())
		}
	}
	if opts.ManifestSigningKeyPath != "" {
		manifestKey, manifestCreated, err := signing.LoadOrCreate(opts.ManifestSigningKeyPath, opts.ManifestSigningKeyID)
		if err != nil {
			return err
		}
		gw.WithManifestSigningKey(manifestKey.ID, manifestKey.PublicKey, manifestKey.PrivateKey)
		action := "loaded"
		if manifestCreated {
			action = "created"
		}
		_, _ = fmt.Fprintf(a.Stderr, "rdev gateway manifest signing key %s at %s\n", action, opts.ManifestSigningKeyPath)
		_, _ = fmt.Fprintf(a.Stderr, "rdev gateway manifest root id=%s public_key=%s\n", manifestKey.ID, encodeRootPublicKey(manifestKey.ID, manifestKey.PublicKey))
	}
	if opts.AuditLog != "" {
		store := audit.NewJSONLStore(opts.AuditLog)
		gw.WithAuditSink(&store)
	}
	var auth *operatorauth.Authorizer
	var principals []operatorauth.Principal
	if opts.OperatorAuthPath != "" {
		loadedAuth, file, err := operatorauth.Load(opts.OperatorAuthPath)
		if err != nil {
			return err
		}
		auth = loadedAuth
		principals = file.Principals
		_, _ = fmt.Fprintf(a.Stderr, "rdev gateway operator auth loaded from %s principals=%d\n", opts.OperatorAuthPath, len(file.Principals))
	}
	var hostedSources []operatorauth.BearerAuthSource
	if opts.HostedOperatorAuthPath != "" {
		hosted, file, err := operatorauth.LoadHosted(opts.HostedOperatorAuthPath)
		if err != nil {
			return err
		}
		hostedSources = append(hostedSources, hosted)
		_, _ = fmt.Fprintf(a.Stderr, "rdev gateway hosted operator auth loaded from %s issuer=%s audience=%s keys=%d\n", opts.HostedOperatorAuthPath, file.Issuer, file.Audience, len(file.Keys))
	}
	if opts.OIDCJWKSOperatorAuthPath != "" {
		oidc, file, err := operatorauth.LoadOIDCJWKS(opts.OIDCJWKSOperatorAuthPath)
		if err != nil {
			return err
		}
		hostedSources = append(hostedSources, oidc)
		_, _ = fmt.Fprintf(a.Stderr, "rdev gateway OIDC JWKS operator auth loaded from %s issuer=%s audience=%s keys=%d\n", opts.OIDCJWKSOperatorAuthPath, file.Issuer, file.Audience, oidc.KeyCount())
	}
	if opts.SAMLOperatorAuthPath != "" {
		saml, file, err := operatorauth.LoadSAML(opts.SAMLOperatorAuthPath)
		if err != nil {
			return err
		}
		hostedSources = append(hostedSources, saml)
		_, _ = fmt.Fprintf(a.Stderr, "rdev gateway SAML operator auth loaded from %s issuer=%s audience=%s certificates=%d\n", opts.SAMLOperatorAuthPath, file.IDPIssuer, file.Audience, saml.CertificateCount())
	}
	if len(hostedSources) > 0 {
		combined, err := operatorauth.NewCombinedSources(principals, hostedSources...)
		if err != nil {
			return err
		}
		auth = combined
	}
	if opts.AutoBuildRdevAssets && !gatewayHasExplicitAssetConfig(opts) {
		assetsDir, ready, err := prepareGatewayAutoBuildRdevAssets(context.Background(), opts.Addr)
		if err != nil {
			return err
		}
		if ready && strings.TrimSpace(assetsDir) != "" {
			opts.RdevAssetsDir = assetsDir
			_, _ = fmt.Fprintf(a.Stderr, "rdev gateway dev auto-built rdev helper assets at %s\n", assetsDir)
		} else {
			_, _ = fmt.Fprintf(a.Stderr, "rdev gateway dev warning: target bootstrap self-repair assets are not all ready; use rdev support-session connect --start or --rdev-assets-dir for one-command target setup\n")
		}
	}
	server := httpapi.NewServerWithOperatorAuthAndStateStore(gw, store, auth)
	server.Assets = gatewayAssetConfig(opts)
	if opts.SigningKeyPath != "" {
		action := "loaded"
		if created {
			action = "created"
		}
		_, _ = fmt.Fprintf(a.Stderr, "rdev gateway signing key %s at %s\n", action, opts.SigningKeyPath)
	}
	_, _ = fmt.Fprintf(a.Stderr, "rdev gateway signing key id=%s fingerprint=%s\n", key.ID, signing.Fingerprint(key.PublicKey))
	tlsConfig, err := gatewayTLSConfig(opts)
	if err != nil {
		return err
	}
	scheme := "http"
	if tlsConfig != nil {
		scheme = "https"
		if opts.ClientCAPath != "" {
			_, _ = fmt.Fprintf(a.Stderr, "rdev gateway dev mTLS client CA loaded from %s\n", opts.ClientCAPath)
		}
	}
	_, _ = fmt.Fprintf(a.Stderr, "rdev gateway dev listening on %s://%s\n", scheme, opts.Addr)
	return listenAndServeGateway(opts.Addr, server.Handler(), tlsConfig)
}

func gatewayAssetConfig(opts gatewayServeOptions) httpapi.AssetConfig {
	assets := httpapi.AssetConfig{}
	if dir := strings.TrimSpace(opts.RdevAssetsDir); dir != "" {
		assets.RdevWindowsAMD64Path = filepath.Join(dir, "rdev-windows-amd64.exe")
		assets.RdevDarwinARM64Path = filepath.Join(dir, "rdev-darwin-arm64")
		assets.RdevDarwinAMD64Path = filepath.Join(dir, "rdev-darwin-amd64")
		assets.RdevLinuxAMD64Path = filepath.Join(dir, "rdev-linux-amd64")
		assets.RdevLinuxARM64Path = filepath.Join(dir, "rdev-linux-arm64")
	}
	if strings.TrimSpace(opts.RdevWindowsAMD64Path) != "" {
		assets.RdevWindowsAMD64Path = opts.RdevWindowsAMD64Path
	}
	if strings.TrimSpace(opts.RdevDarwinARM64Path) != "" {
		assets.RdevDarwinARM64Path = opts.RdevDarwinARM64Path
	}
	if strings.TrimSpace(opts.RdevDarwinAMD64Path) != "" {
		assets.RdevDarwinAMD64Path = opts.RdevDarwinAMD64Path
	}
	if strings.TrimSpace(opts.RdevLinuxAMD64Path) != "" {
		assets.RdevLinuxAMD64Path = opts.RdevLinuxAMD64Path
	}
	if strings.TrimSpace(opts.RdevLinuxARM64Path) != "" {
		assets.RdevLinuxARM64Path = opts.RdevLinuxARM64Path
	}
	return assets
}

func gatewayHasExplicitAssetConfig(opts gatewayServeOptions) bool {
	return strings.TrimSpace(opts.RdevAssetsDir) != "" ||
		strings.TrimSpace(opts.RdevWindowsAMD64Path) != "" ||
		strings.TrimSpace(opts.RdevDarwinARM64Path) != "" ||
		strings.TrimSpace(opts.RdevDarwinAMD64Path) != "" ||
		strings.TrimSpace(opts.RdevLinuxAMD64Path) != "" ||
		strings.TrimSpace(opts.RdevLinuxARM64Path) != ""
}

func prepareGatewayAutoBuildRdevAssets(ctx context.Context, addr string) (string, bool, error) {
	prepared, err := prepareSupportSessionEnvironment(ctx, supportSessionPrepareOptions{
		RepoRoot:    findSupportSessionRepoRoot("."),
		WorkDir:     "",
		Addr:        addr,
		BuildAssets: true,
	})
	if err != nil {
		return "", false, err
	}
	assetsDir, _ := prepared["bin_dir"].(string)
	ready := false
	if assetReport, ok := prepared["asset_report"].(map[string]any); ok {
		ready = assetReport["all_ready"] == true
	}
	return assetsDir, ready, nil
}

func gatewayTLSConfig(opts gatewayServeOptions) (*tls.Config, error) {
	if opts.ClientCAPath != "" && (opts.TLSCertPath == "" || opts.TLSKeyPath == "") {
		return nil, fmt.Errorf("gateway serve --client-ca requires --tls-cert and --tls-key")
	}
	if opts.TLSCertPath == "" && opts.TLSKeyPath == "" {
		return nil, nil
	}
	if opts.TLSCertPath == "" || opts.TLSKeyPath == "" {
		return nil, fmt.Errorf("gateway serve TLS requires both --tls-cert and --tls-key")
	}
	certificate, err := tls.LoadX509KeyPair(opts.TLSCertPath, opts.TLSKeyPath)
	if err != nil {
		return nil, err
	}
	config := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
	}
	if opts.ClientCAPath != "" {
		content, err := os.ReadFile(opts.ClientCAPath)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(content) {
			return nil, fmt.Errorf("gateway serve --client-ca does not contain a valid PEM certificate")
		}
		config.ClientAuth = tls.RequireAndVerifyClientCert
		config.ClientCAs = pool
	}
	return config, nil
}

func listenAndServeGateway(addr string, handler http.Handler, tlsConfig *tls.Config) error {
	return listenAndServeGatewayContext(context.Background(), addr, handler, tlsConfig)
}

func listenAndServeGatewayContext(ctx context.Context, addr string, handler http.Handler, tlsConfig *tls.Config) error {
	server := &http.Server{
		Addr:      addr,
		Handler:   handler,
		TLSConfig: tlsConfig,
	}
	errCh := make(chan error, 1)
	go func() {
		if tlsConfig != nil {
			errCh <- server.ListenAndServeTLS("", "")
			return
		}
		errCh <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		err := <-errCh
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return ctx.Err()
		}
		return err
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// findFreeAddr returns the first TCP address in [preferred, preferred+20) that
// is not already bound. If preferred is available it is returned unchanged.
// If no free address is found within the search range, preferred is returned
// and the caller will receive a bind error from the OS when it tries to listen.
//
// Only the port is incremented; the host part is preserved as-is so that
// interface binding (0.0.0.0, 127.0.0.1, etc.) is respected.
func findFreeAddr(preferred string) string {
	host, portStr, err := net.SplitHostPort(preferred)
	if err != nil {
		// Not a host:port – return unchanged and let the caller handle it.
		return preferred
	}
	basePort, err := strconv.Atoi(portStr)
	if err != nil {
		return preferred
	}
	for delta := 0; delta < 20; delta++ {
		candidate := net.JoinHostPort(host, strconv.Itoa(basePort+delta))
		ln, err := net.Listen("tcp", candidate)
		if err == nil {
			ln.Close()
			return candidate
		}
	}
	return preferred
}

func gatewayStateStore(opts gatewayServeOptions) (gateway.StateStore, error) {
	provider := strings.TrimSpace(opts.StorageProvider)
	path := strings.TrimSpace(opts.StoragePath)
	statePath := strings.TrimSpace(opts.StatePath)
	if provider == "" && path == "" && statePath == "" {
		return nil, nil
	}
	if statePath != "" && path != "" && statePath != path {
		return nil, fmt.Errorf("gateway serve accepts either --state or --storage-path, not two different paths")
	}
	if provider == "" {
		provider = gateway.FileStateStoreProvider
	}
	if path == "" {
		path = statePath
	}
	return newGatewayStateStore(provider, path)
}

func newGatewayStateStore(provider, path string) (gateway.StateStore, error) {
	switch strings.TrimSpace(provider) {
	case gateway.FileStateStoreProvider:
		return gateway.NewFileStateStore(path)
	case gateway.PostgresStateStoreProvider:
		return gateway.NewPostgresStateStore(path)
	case gateway.RedisStreamStateStoreProvider:
		return gateway.NewRedisStreamStateStore(path)
	case gateway.S3CompatibleStateStoreProvider:
		return gateway.NewS3CompatibleStateStore(path)
	default:
		return nil, fmt.Errorf("unsupported gateway storage provider %q", provider)
	}
}

func (a App) enrollmentSignCertificate(opts enrollmentSignCertificateOptions) error {
	if opts.KeyPath == "" {
		return fmt.Errorf("key is required")
	}
	if opts.TicketCode == "" {
		return fmt.Errorf("ticket-code is required")
	}
	if opts.Name == "" {
		return fmt.Errorf("name is required")
	}
	if opts.OS == "" {
		return fmt.Errorf("os is required")
	}
	if opts.Arch == "" {
		return fmt.Errorf("arch is required")
	}
	if opts.IdentityKeyID == "" || opts.IdentityPublicKey == "" || opts.IdentityFingerprint == "" {
		return fmt.Errorf("identity-key-id, identity-public-key, and identity-fingerprint are required")
	}
	if len(opts.Capabilities) == 0 {
		return fmt.Errorf("capabilities are required")
	}
	if opts.ValidMinutes <= 0 {
		return fmt.Errorf("valid-minutes must be positive")
	}
	mode, err := parseEnrollmentHostMode(opts.Mode)
	if err != nil {
		return err
	}
	key, _, err := signing.LoadOrCreate(opts.KeyPath, opts.KeyID)
	if err != nil {
		return err
	}
	registration := model.HostRegistration{
		TicketCode:          opts.TicketCode,
		Name:                opts.Name,
		OS:                  opts.OS,
		Arch:                opts.Arch,
		Capabilities:        opts.Capabilities,
		IdentityKeyID:       opts.IdentityKeyID,
		IdentityPublicKey:   opts.IdentityPublicKey,
		IdentityFingerprint: opts.IdentityFingerprint,
	}
	ticket := model.Ticket{
		Code:         opts.TicketCode,
		Mode:         mode,
		Capabilities: opts.Capabilities,
	}
	certificate, err := model.SignHostEnrollmentCertificate(registration, ticket, key.ID, key.PrivateKey, time.Now(), time.Duration(opts.ValidMinutes)*time.Minute)
	if err != nil {
		return err
	}
	if opts.OutPath != "" {
		if err := writeEnrollmentCertificateFile(opts.OutPath, certificate, opts.Force); err != nil {
			return err
		}
	}
	payload := map[string]any{
		"ok":                  true,
		"schema":              certificate.SchemaVersion,
		"certificate":         certificate,
		"root_public_key":     encodeRootPublicKey(key.ID, key.PublicKey),
		"valid_until":         certificate.NotAfter,
		"authorized_mode":     certificate.Mode,
		"authorized_host":     certificate.HostName,
		"authorized_identity": certificate.SubjectIdentityFingerprint,
	}
	if opts.OutPath != "" {
		payload["certificate_path"] = opts.OutPath
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

type issuedEnrollmentCertificatePayload struct {
	Certificate            model.HostEnrollmentCertificate `json:"certificate"`
	CertificateFingerprint string                          `json:"certificate_fingerprint"`
	EnrollmentRoot         model.TrustBundle               `json:"enrollment_root"`
	Error                  string                          `json:"error,omitempty"`
}

func (a App) enrollmentIssueCertificate(ctx context.Context, opts enrollmentIssueCertificateOptions) error {
	if opts.GatewayURL == "" {
		return fmt.Errorf("gateway is required")
	}
	if opts.OutPath == "" {
		return fmt.Errorf("out is required")
	}
	if opts.RootPublicKey == "" {
		return fmt.Errorf("root-public-key is required")
	}
	if opts.TicketCode == "" {
		return fmt.Errorf("ticket-code is required")
	}
	if opts.Name == "" {
		return fmt.Errorf("name is required")
	}
	if opts.OS == "" {
		return fmt.Errorf("os is required")
	}
	if opts.Arch == "" {
		return fmt.Errorf("arch is required")
	}
	if opts.IdentityKeyID == "" || opts.IdentityPublicKey == "" || opts.IdentityFingerprint == "" {
		return fmt.Errorf("identity-key-id, identity-public-key, and identity-fingerprint are required")
	}
	if opts.ValidMinutes <= 0 {
		return fmt.Errorf("valid-minutes must be positive")
	}
	expectedRoot, err := parseRootPublicKey(opts.RootPublicKey)
	if err != nil {
		return err
	}
	issued, err := issueEnrollmentCertificate(ctx, opts.GatewayURL, opts)
	if err != nil {
		return err
	}
	if issued.EnrollmentRoot.SigningKeyID != expectedRoot.SigningKeyID || issued.EnrollmentRoot.PublicKey != expectedRoot.PublicKey {
		return fmt.Errorf("issued enrollment root does not match pinned root-public-key")
	}
	if err := model.VerifyHostEnrollmentCertificateSignature(issued.Certificate, expectedRoot, time.Now()); err != nil {
		return err
	}
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(issued.Certificate)
	if err != nil {
		return err
	}
	if fingerprint != issued.CertificateFingerprint {
		return fmt.Errorf("issued enrollment certificate fingerprint mismatch")
	}
	if err := writeEnrollmentCertificateFile(opts.OutPath, issued.Certificate, opts.Force); err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                      true,
		"schema":                  issued.Certificate.SchemaVersion,
		"gateway":                 opts.GatewayURL,
		"certificate":             issued.Certificate,
		"certificate_path":        opts.OutPath,
		"certificate_fingerprint": fingerprint,
		"root_public_key":         opts.RootPublicKey,
		"issuer_key_id":           issued.Certificate.IssuerKeyID,
		"authorized_mode":         issued.Certificate.Mode,
		"authorized_host":         issued.Certificate.HostName,
		"authorized_identity":     issued.Certificate.SubjectIdentityFingerprint,
		"not_before":              issued.Certificate.NotBefore,
		"not_after":               issued.Certificate.NotAfter,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) enrollmentVerifyCertificate(certificatePath, rootPublicKey, revocationsPath string) error {
	certificate, err := readEnrollmentCertificateFile(certificatePath)
	if err != nil {
		return err
	}
	root, err := parseRootPublicKey(rootPublicKey)
	if err != nil {
		return err
	}
	if err := model.VerifyHostEnrollmentCertificateSignature(certificate, root, time.Now()); err != nil {
		return err
	}
	revocationCount := 0
	if revocationsPath != "" {
		revocations, err := readEnrollmentRevocationListFile(revocationsPath)
		if err != nil {
			return err
		}
		if err := model.VerifyHostEnrollmentRevocationListSignature(revocations, root, time.Now()); err != nil {
			return err
		}
		if err := model.VerifyHostEnrollmentCertificateNotRevoked(certificate, revocations); err != nil {
			return err
		}
		revocationCount = len(revocations.RevokedCertificates)
	}
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                       true,
		"schema":                   certificate.SchemaVersion,
		"certificate":              certificatePath,
		"certificate_fingerprint":  fingerprint,
		"issuer_key_id":            certificate.IssuerKeyID,
		"subject_identity":         certificate.SubjectIdentityFingerprint,
		"ticket_code":              certificate.TicketCode,
		"mode":                     certificate.Mode,
		"not_after":                certificate.NotAfter,
		"root_public_key_verified": root.SigningKeyID,
	}
	if revocationsPath != "" {
		payload["revocations"] = revocationsPath
		payload["revoked_certificate_count"] = revocationCount
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) enrollmentRenewCertificate(opts enrollmentRenewCertificateOptions) error {
	if opts.OutPath == "" {
		return fmt.Errorf("out is required")
	}
	if opts.CertificatePath == "" {
		return fmt.Errorf("certificate is required")
	}
	if opts.ValidMinutes <= 0 {
		return fmt.Errorf("valid-minutes must be positive")
	}
	if opts.GatewayURL != "" {
		return a.enrollmentRenewCertificateFromGateway(context.Background(), opts)
	}
	if opts.KeyPath == "" {
		return fmt.Errorf("key is required")
	}
	key, _, err := signing.LoadOrCreate(opts.KeyPath, opts.KeyID)
	if err != nil {
		return err
	}
	certificate, err := readEnrollmentCertificateFile(opts.CertificatePath)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	root := model.NewTrustBundle(key.ID, key.PublicKey)
	if opts.RevocationsPath != "" {
		revocations, err := readEnrollmentRevocationListFile(opts.RevocationsPath)
		if err != nil {
			return err
		}
		if err := model.VerifyHostEnrollmentRevocationListSignature(revocations, root, now); err != nil {
			return err
		}
		if err := model.VerifyHostEnrollmentCertificateNotRevoked(certificate, revocations); err != nil {
			return err
		}
	}
	previousFingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		return err
	}
	renewed, err := model.RenewHostEnrollmentCertificate(certificate, root, key.PrivateKey, now, time.Duration(opts.ValidMinutes)*time.Minute)
	if err != nil {
		return err
	}
	renewedFingerprint, err := model.HostEnrollmentCertificateFingerprint(renewed)
	if err != nil {
		return err
	}
	if err := writeEnrollmentCertificateFile(opts.OutPath, renewed, opts.Force); err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                               true,
		"schema":                           renewed.SchemaVersion,
		"certificate":                      renewed,
		"certificate_path":                 opts.OutPath,
		"previous_certificate":             opts.CertificatePath,
		"previous_certificate_fingerprint": previousFingerprint,
		"certificate_fingerprint":          renewedFingerprint,
		"root_public_key":                  encodeRootPublicKey(key.ID, key.PublicKey),
		"issuer_key_id":                    renewed.IssuerKeyID,
		"authorized_mode":                  renewed.Mode,
		"authorized_host":                  renewed.HostName,
		"authorized_identity":              renewed.SubjectIdentityFingerprint,
		"not_before":                       renewed.NotBefore,
		"not_after":                        renewed.NotAfter,
	}
	if opts.RevocationsPath != "" {
		payload["revocations"] = opts.RevocationsPath
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

type renewedEnrollmentCertificatePayload struct {
	Certificate                    model.HostEnrollmentCertificate `json:"certificate"`
	CertificateFingerprint         string                          `json:"certificate_fingerprint"`
	PreviousCertificateFingerprint string                          `json:"previous_certificate_fingerprint"`
	EnrollmentRoot                 model.TrustBundle               `json:"enrollment_root"`
	Error                          string                          `json:"error,omitempty"`
}

func (a App) enrollmentRenewCertificateFromGateway(ctx context.Context, opts enrollmentRenewCertificateOptions) error {
	if opts.RootPublicKey == "" {
		return fmt.Errorf("root-public-key is required for hosted renewal")
	}
	certificate, err := readEnrollmentCertificateFile(opts.CertificatePath)
	if err != nil {
		return err
	}
	renewed, previousFingerprint, fingerprint, err := renewEnrollmentCertificateFromGateway(ctx, http.DefaultClient, opts.GatewayURL, certificate, opts)
	if err != nil {
		return err
	}
	if err := writeEnrollmentCertificateFile(opts.OutPath, renewed, opts.Force); err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                               true,
		"schema":                           renewed.SchemaVersion,
		"gateway":                          opts.GatewayURL,
		"certificate":                      renewed,
		"certificate_path":                 opts.OutPath,
		"previous_certificate":             opts.CertificatePath,
		"previous_certificate_fingerprint": previousFingerprint,
		"certificate_fingerprint":          fingerprint,
		"root_public_key":                  opts.RootPublicKey,
		"issuer_key_id":                    renewed.IssuerKeyID,
		"authorized_mode":                  renewed.Mode,
		"authorized_host":                  renewed.HostName,
		"authorized_identity":              renewed.SubjectIdentityFingerprint,
		"not_before":                       renewed.NotBefore,
		"not_after":                        renewed.NotAfter,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func renewEnrollmentCertificateFromGateway(ctx context.Context, client *http.Client, gatewayURL string, certificate model.HostEnrollmentCertificate, opts enrollmentRenewCertificateOptions) (model.HostEnrollmentCertificate, string, string, error) {
	expectedRoot, err := parseRootPublicKey(opts.RootPublicKey)
	if err != nil {
		return model.HostEnrollmentCertificate{}, "", "", err
	}
	previousFingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		return model.HostEnrollmentCertificate{}, "", "", err
	}
	renewed, err := renewEnrollmentCertificate(ctx, client, gatewayURL, certificate, opts)
	if err != nil {
		return model.HostEnrollmentCertificate{}, "", "", err
	}
	if renewed.EnrollmentRoot.SigningKeyID != expectedRoot.SigningKeyID || renewed.EnrollmentRoot.PublicKey != expectedRoot.PublicKey {
		return model.HostEnrollmentCertificate{}, "", "", fmt.Errorf("renewed enrollment root does not match pinned root-public-key")
	}
	if renewed.PreviousCertificateFingerprint != previousFingerprint {
		return model.HostEnrollmentCertificate{}, "", "", fmt.Errorf("renewed enrollment certificate previous fingerprint mismatch")
	}
	if err := model.VerifyHostEnrollmentCertificateSignature(renewed.Certificate, expectedRoot, time.Now()); err != nil {
		return model.HostEnrollmentCertificate{}, "", "", err
	}
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(renewed.Certificate)
	if err != nil {
		return model.HostEnrollmentCertificate{}, "", "", err
	}
	if fingerprint != renewed.CertificateFingerprint {
		return model.HostEnrollmentCertificate{}, "", "", fmt.Errorf("renewed enrollment certificate fingerprint mismatch")
	}
	return renewed.Certificate, previousFingerprint, fingerprint, nil
}

func (a App) enrollmentInitRevocations(opts enrollmentInitRevocationsOptions) error {
	if opts.OutPath == "" {
		return fmt.Errorf("out is required")
	}
	if opts.KeyPath == "" {
		return fmt.Errorf("key is required")
	}
	if opts.ValidHours <= 0 {
		return fmt.Errorf("valid-hours must be positive")
	}
	key, _, err := signing.LoadOrCreate(opts.KeyPath, opts.KeyID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	list, err := model.SignHostEnrollmentRevocationList(nil, key.ID, key.PrivateKey, now, time.Duration(opts.ValidHours)*time.Hour)
	if err != nil {
		return err
	}
	if err := writeEnrollmentRevocationListFile(opts.OutPath, list, opts.Force); err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                        true,
		"schema":                    list.SchemaVersion,
		"revocations_path":          opts.OutPath,
		"revoked_certificate_count": len(list.RevokedCertificates),
		"issuer_key_id":             list.IssuerKeyID,
		"root_public_key":           encodeRootPublicKey(key.ID, key.PublicKey),
		"not_after":                 list.NotAfter,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) enrollmentRevokeCertificate(opts enrollmentRevokeCertificateOptions) error {
	if opts.OutPath == "" {
		return fmt.Errorf("out is required")
	}
	if opts.KeyPath == "" {
		return fmt.Errorf("key is required")
	}
	if opts.CertificatePath == "" {
		return fmt.Errorf("certificate is required")
	}
	if opts.ValidHours <= 0 {
		return fmt.Errorf("valid-hours must be positive")
	}
	key, _, err := signing.LoadOrCreate(opts.KeyPath, opts.KeyID)
	if err != nil {
		return err
	}
	certificate, err := readEnrollmentCertificateFile(opts.CertificatePath)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	root := model.NewTrustBundle(key.ID, key.PublicKey)
	if err := model.VerifyHostEnrollmentCertificateSignature(certificate, root, now); err != nil {
		return err
	}
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		return err
	}
	revocations := []model.HostEnrollmentCertificateRevocation{}
	if opts.CurrentPath != "" {
		current, err := readEnrollmentRevocationListFile(opts.CurrentPath)
		if err != nil {
			return err
		}
		if err := model.VerifyHostEnrollmentRevocationListSignature(current, root, now); err != nil {
			return err
		}
		revocations = append(revocations, current.RevokedCertificates...)
	}
	alreadyRevoked := false
	for _, revocation := range revocations {
		if revocation.CertificateFingerprint == fingerprint {
			alreadyRevoked = true
			break
		}
	}
	if !alreadyRevoked {
		revocations = append(revocations, model.HostEnrollmentCertificateRevocation{
			CertificateFingerprint: fingerprint,
			Reason:                 opts.Reason,
			RevokedAt:              now,
		})
	}
	list, err := model.SignHostEnrollmentRevocationList(revocations, key.ID, key.PrivateKey, now, time.Duration(opts.ValidHours)*time.Hour)
	if err != nil {
		return err
	}
	if err := writeEnrollmentRevocationListFile(opts.OutPath, list, opts.Force); err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                        true,
		"schema":                    list.SchemaVersion,
		"revocations_path":          opts.OutPath,
		"revoked_certificate":       fingerprint,
		"revoked_certificate_count": len(list.RevokedCertificates),
		"issuer_key_id":             list.IssuerKeyID,
		"root_public_key":           encodeRootPublicKey(key.ID, key.PublicKey),
		"not_after":                 list.NotAfter,
	}
	if alreadyRevoked {
		payload["already_revoked"] = true
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) enrollmentVerifyRevocations(revocationsPath, rootPublicKey string) error {
	revocations, err := readEnrollmentRevocationListFile(revocationsPath)
	if err != nil {
		return err
	}
	root, err := parseRootPublicKey(rootPublicKey)
	if err != nil {
		return err
	}
	if err := model.VerifyHostEnrollmentRevocationListSignature(revocations, root, time.Now()); err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                        true,
		"schema":                    revocations.SchemaVersion,
		"revocations":               revocationsPath,
		"issuer_key_id":             revocations.IssuerKeyID,
		"root_public_key_verified":  root.SigningKeyID,
		"revoked_certificate_count": len(revocations.RevokedCertificates),
		"not_after":                 revocations.NotAfter,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) enrollmentFetchRevocations(ctx context.Context, gatewayURL, rootPublicKey, operatorTokenFile, outPath string, force bool) error {
	if gatewayURL == "" {
		return fmt.Errorf("gateway is required")
	}
	if rootPublicKey == "" {
		return fmt.Errorf("root-public-key is required")
	}
	if outPath == "" {
		return fmt.Errorf("out is required")
	}
	revocations, root, err := fetchEnrollmentRevocations(ctx, gatewayURL, rootPublicKey, operatorTokenFile)
	if err != nil {
		return err
	}
	if err := writeEnrollmentRevocationListFile(outPath, revocations, force); err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                        true,
		"schema":                    revocations.SchemaVersion,
		"gateway":                   gatewayURL,
		"revocations_path":          outPath,
		"issuer_key_id":             revocations.IssuerKeyID,
		"root_public_key_verified":  root.SigningKeyID,
		"revoked_certificate_count": len(revocations.RevokedCertificates),
		"generated_at":              revocations.GeneratedAt,
		"not_after":                 revocations.NotAfter,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) enrollmentLifecycleKeyCustody(rootPublicKey, custodian, provider string, rotationDays int, dualControl, breakGlass bool, outPath string, force bool) error {
	root, err := parseRootPublicKey(rootPublicKey)
	if err != nil {
		return err
	}
	record, err := enrollmentlifecycle.NewKeyCustodyRecord(root, custodian, provider, rotationDays, dualControl, breakGlass, time.Now())
	if err != nil {
		return err
	}
	if outPath != "" {
		if err := enrollmentlifecycle.WriteJSON(outPath, record, force); err != nil {
			return err
		}
	}
	payload := map[string]any{
		"ok":       true,
		"schema":   record.SchemaVersion,
		"record":   record,
		"out_path": outPath,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) enrollmentLifecycleFleetRenewalPlan(certificatesPath, revocationsPath, rootPublicKey string, renewBefore, renewValidFor, maxSkew time.Duration, requireRevocations bool, outPath string, force bool) error {
	if strings.TrimSpace(certificatesPath) == "" {
		return fmt.Errorf("certificates is required")
	}
	root, err := parseRootPublicKey(rootPublicKey)
	if err != nil {
		return err
	}
	certificates, err := enrollmentlifecycle.ReadCertificateSet(certificatesPath)
	if err != nil {
		return err
	}
	for _, certificate := range certificates {
		if err := model.VerifyHostEnrollmentCertificateSignature(certificate, root, time.Now()); err != nil {
			return err
		}
	}
	var revocations *model.HostEnrollmentRevocationList
	if strings.TrimSpace(revocationsPath) != "" {
		list, err := readEnrollmentRevocationListFile(revocationsPath)
		if err != nil {
			return err
		}
		if err := model.VerifyHostEnrollmentRevocationListSignature(list, root, time.Now()); err != nil {
			return err
		}
		revocations = &list
	}
	plan, err := enrollmentlifecycle.BuildFleetRenewalPlan(certificates, revocations, enrollmentlifecycle.FleetRenewalPolicy{
		RootPublicKey:      rootPublicKey,
		RenewBefore:        renewBefore,
		RenewValidFor:      renewValidFor,
		MaximumSkew:        maxSkew,
		RequireRevocations: requireRevocations,
	}, time.Now())
	if err != nil {
		return err
	}
	if outPath != "" {
		if err := enrollmentlifecycle.WriteJSON(outPath, plan, force); err != nil {
			return err
		}
	}
	payload := map[string]any{
		"ok":       true,
		"schema":   plan.SchemaVersion,
		"plan":     plan,
		"out_path": outPath,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) enrollmentLifecycleEmergencyDrill(name, scenario, operatorRole, rootPublicKey, revocationsPath, outPath string, force bool) error {
	if rootPublicKey != "" {
		if _, err := parseRootPublicKey(rootPublicKey); err != nil {
			return err
		}
	}
	var revocations *model.HostEnrollmentRevocationList
	if strings.TrimSpace(revocationsPath) != "" {
		root, err := parseRootPublicKey(rootPublicKey)
		if err != nil {
			return err
		}
		list, err := readEnrollmentRevocationListFile(revocationsPath)
		if err != nil {
			return err
		}
		if err := model.VerifyHostEnrollmentRevocationListSignature(list, root, time.Now()); err != nil {
			return err
		}
		revocations = &list
	}
	drill := enrollmentlifecycle.NewEmergencyDrill(name, scenario, operatorRole, rootPublicKey, revocationsPath, revocations, time.Now())
	if outPath != "" {
		if err := enrollmentlifecycle.WriteJSON(outPath, drill, force); err != nil {
			return err
		}
	}
	payload := map[string]any{
		"ok":       drill.Passed,
		"schema":   drill.SchemaVersion,
		"drill":    drill,
		"out_path": outPath,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
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

func (a App) releasePrepareCandidate(sourceRoot, outPath, version, gatewayURL string, artifactPaths, requiredArtifacts []string, keyPath, keyID string) error {
	if outPath == "" {
		return fmt.Errorf("out is required")
	}
	if version == "" {
		return fmt.Errorf("version is required")
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
	candidate, err := release.PrepareCandidate(release.CandidateOptions{
		SourceRoot:        sourceRoot,
		OutDir:            outPath,
		Version:           version,
		GatewayURL:        gatewayURL,
		ArtifactPaths:     artifactPaths,
		RequiredArtifacts: requiredArtifacts,
		Key:               key,
		Now:               time.Now(),
	})
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                  candidate.OK(),
		"schema":              candidate.SchemaVersion,
		"version":             candidate.Version,
		"out":                 outPath,
		"release_candidate":   filepath.Join(outPath, "release-candidate.json"),
		"release_bundle":      filepath.Join(outPath, candidate.ReleaseBundlePath),
		"skillkit":            filepath.Join(outPath, candidate.SkillkitPath),
		"sbom":                filepath.Join(outPath, candidate.SBOMPath),
		"provenance":          filepath.Join(outPath, candidate.ProvenancePath),
		"checksums":           filepath.Join(outPath, candidate.ChecksumsPath),
		"artifact_count":      len(candidate.Artifacts),
		"file_count":          len(candidate.Files),
		"root_public_key":     candidate.RootPublicKey,
		"checks":              candidate.Checks,
		"recommended_actions": candidate.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !candidate.OK() {
		return fmt.Errorf("release candidate preparation failed")
	}
	return nil
}

func (a App) releaseVerifyCandidate(candidatePath string, requiredArtifacts []string) error {
	if candidatePath == "" {
		return fmt.Errorf("candidate is required")
	}
	verification, err := release.VerifyCandidate(release.CandidateVerifyOptions{
		CandidatePath:     candidatePath,
		RequiredArtifacts: requiredArtifacts,
		GeneratedAt:       time.Now(),
	})
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                    verification.OK(),
		"schema":                verification.SchemaVersion,
		"candidate":             verification.CandidatePath,
		"candidate_dir":         verification.CandidateDir,
		"version":               verification.Version,
		"root_public_key":       verification.RootPublicKey,
		"required_artifacts":    verification.RequiredArtifacts,
		"checks":                verification.Checks,
		"files":                 verification.Files,
		"bundle_verification":   verification.BundleVerification,
		"skillkit_verification": verification.SkillkitVerification,
		"recommended_actions":   verification.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !verification.OK() {
		return fmt.Errorf("release candidate verification failed")
	}
	return nil
}

func (a App) adapterScaffold(opts adapterScaffoldOptions) error {
	adapterName := strings.TrimSpace(opts.Adapter)
	if adapterName == "" {
		return fmt.Errorf("adapter is required")
	}
	outPath := strings.TrimSpace(opts.OutPath)
	if outPath == "" {
		return fmt.Errorf("out is required")
	}
	if !opts.Force {
		if _, err := os.Stat(outPath); err == nil {
			return fmt.Errorf("out already exists: %s", outPath)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	resultSchema := strings.TrimSpace(opts.ResultSchema)
	if resultSchema == "" {
		resultSchema = "rdev." + adapterName + "-result.v1"
	}
	manifest := adapterLifecycleTemplate(adapterName, resultSchema)
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outPath, content, 0o644); err != nil {
		return err
	}
	report := adapterkit.VerifyLifecycleManifestJSON(content, adapterkit.LifecycleContract{
		Adapter:                 adapterName,
		RequireSafety:           true,
		RequireCancellation:     true,
		RequireResultSchema:     true,
		RejectUnredactedSecrets: true,
	})
	payload := map[string]any{
		"schema":        "rdev.adapter-scaffold.v1",
		"ok":            report.OK,
		"adapter":       adapterName,
		"manifest":      outPath,
		"result_schema": resultSchema,
		"verification":  report,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !report.OK {
		return fmt.Errorf("generated adapter lifecycle manifest failed conformance")
	}
	return nil
}

func (a App) adapterVerifyResult(opts adapterVerifyResultOptions) error {
	if strings.TrimSpace(opts.ArtifactPath) == "" {
		return fmt.Errorf("artifact is required")
	}
	if strings.TrimSpace(opts.Adapter) == "" {
		return fmt.Errorf("adapter is required")
	}
	if strings.TrimSpace(opts.SchemaVersion) == "" {
		return fmt.Errorf("schema is required")
	}
	var content []byte
	var err error
	if opts.ArtifactPath == "-" {
		content, err = io.ReadAll(os.Stdin)
	} else {
		content, err = os.ReadFile(opts.ArtifactPath)
	}
	if err != nil {
		return err
	}
	report := adapterkit.VerifyResultArtifactJSON(content, adapterkit.ResultArtifactContract{
		Adapter:                 opts.Adapter,
		SchemaVersion:           opts.SchemaVersion,
		CommandFields:           opts.CommandFields,
		RequiredStringFields:    opts.RequiredStringFields,
		RequireTiming:           opts.RequireTiming,
		RequireRedaction:        opts.RequireRedaction,
		RejectUnredactedSecrets: opts.RejectUnredactedPatterns,
	})
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return err
	}
	if !report.OK {
		return fmt.Errorf("adapter result conformance failed")
	}
	return nil
}

func (a App) adapterVerifyLifecycle(opts adapterVerifyLifecycleOptions) error {
	if strings.TrimSpace(opts.ArtifactPath) == "" {
		return fmt.Errorf("artifact is required")
	}
	if strings.TrimSpace(opts.Adapter) == "" {
		return fmt.Errorf("adapter is required")
	}
	if strings.TrimSpace(opts.SchemaVersion) == "" {
		return fmt.Errorf("schema is required")
	}
	var content []byte
	var err error
	if opts.ArtifactPath == "-" {
		content, err = io.ReadAll(os.Stdin)
	} else {
		content, err = os.ReadFile(opts.ArtifactPath)
	}
	if err != nil {
		return err
	}
	report := adapterkit.VerifyLifecycleManifestJSON(content, adapterkit.LifecycleContract{
		Adapter:                 opts.Adapter,
		SchemaVersion:           opts.SchemaVersion,
		RequiredPhases:          opts.RequiredPhases,
		RequireSafety:           opts.RequireSafety,
		RequireCancellation:     opts.RequireCancellation,
		RequireResultSchema:     opts.RequireResultSchema,
		RejectUnredactedSecrets: opts.RejectUnredactedPatterns,
	})
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return err
	}
	if !report.OK {
		return fmt.Errorf("adapter lifecycle conformance failed")
	}
	return nil
}

func (a App) adapterVerifyCancellation(opts adapterVerifyCancellationOptions) error {
	if strings.TrimSpace(opts.ArtifactPath) == "" {
		return fmt.Errorf("artifact is required")
	}
	if strings.TrimSpace(opts.Adapter) == "" {
		return fmt.Errorf("adapter is required")
	}
	if strings.TrimSpace(opts.SchemaVersion) == "" {
		return fmt.Errorf("schema is required")
	}
	var content []byte
	var err error
	if opts.ArtifactPath == "-" {
		content, err = io.ReadAll(os.Stdin)
	} else {
		content, err = os.ReadFile(opts.ArtifactPath)
	}
	if err != nil {
		return err
	}
	report := adapterkit.VerifyCancellationArtifactJSON(content, adapterkit.CancellationContract{
		Adapter:                 opts.Adapter,
		SchemaVersion:           opts.SchemaVersion,
		CommandFields:           opts.CommandFields,
		RequiredStringFields:    opts.RequiredStringFields,
		RequireTiming:           opts.RequireTiming,
		RequireRedaction:        opts.RequireRedaction,
		RejectUnredactedSecrets: opts.RejectUnredactedPatterns,
	})
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return err
	}
	if !report.OK {
		return fmt.Errorf("adapter cancellation conformance failed")
	}
	return nil
}

func (a App) adapterVerifyRuntime(opts adapterVerifyRuntimeOptions) error {
	if strings.TrimSpace(opts.ArtifactPath) == "" {
		return fmt.Errorf("artifact is required")
	}
	if strings.TrimSpace(opts.Adapter) == "" {
		return fmt.Errorf("adapter is required")
	}
	var content []byte
	var err error
	if opts.ArtifactPath == "-" {
		content, err = io.ReadAll(os.Stdin)
	} else {
		content, err = os.ReadFile(opts.ArtifactPath)
	}
	if err != nil {
		return err
	}
	report := adapterkit.VerifyRuntimeFixtureJSON(content, adapterkit.RuntimeFixtureContract{
		Adapter:               opts.Adapter,
		RequiredPhases:        opts.RequiredPhases,
		RequireSuccessful:     opts.RequireSuccessful,
		RequireCleanup:        opts.RequireCleanup,
		RequireResultArtifact: opts.RequireResultArtifact,
		RequireCancellation:   opts.RequireCancellation,
	})
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return err
	}
	if !report.OK {
		return fmt.Errorf("adapter runtime conformance failed")
	}
	return nil
}

func adapterLifecycleTemplate(adapterName, resultSchema string) adapterLifecycleManifest {
	return adapterLifecycleManifest{
		SchemaVersion: adapterkit.LifecycleManifestSchemaVersion,
		Adapter:       adapterName,
		Phases: adapterLifecyclePhases{
			Detect: adapterPhase{
				Implemented: true,
				Evidence:    []string{"binary_path", "version", "capabilities"},
			},
			Plan: adapterPhase{
				Implemented:                  true,
				Evidence:                     []string{"prompt_summary", "planned_commands", "expected_artifacts"},
				DeclaresExternalConsequences: true,
				DeclaresRequiredApprovals:    true,
			},
			Prepare: adapterPhase{
				Implemented:               true,
				Evidence:                  []string{"workspace_root", "worktree_path", "lock_id"},
				EnforcesWorkspaceBoundary: true,
				UsesWorkspaceLock:         true,
			},
			Run: adapterPhase{
				Implemented:          true,
				Evidence:             []string{"argv", "started_at", "exit_code"},
				SupportsTimeout:      true,
				SupportsCancellation: true,
			},
			Collect: adapterPhase{
				Implemented:         true,
				Evidence:            []string{"stdout", "stderr", "git_status", "git_diff", "verification_commands"},
				EmitsResultArtifact: true,
				ResultSchema:        resultSchema,
			},
			Cleanup: adapterPhase{
				Implemented:   true,
				Evidence:      []string{"lock_released", "temp_files_removed", "processes_stopped"},
				Idempotent:    true,
				ReleasesLocks: true,
			},
		},
		Safety: adapterLifecycleSafety{
			AdapterAuthorizesJobs:           false,
			AdapterApprovesDangerousActions: false,
			AdapterInstallsPersistence:      false,
			HostValidatesBeforeRun:          true,
			RedactsOutputs:                  true,
		},
		Cancellation: adapterLifecycleCancellation{
			Supported:        true,
			EvidenceField:    "canceled",
			TimeoutExclusive: true,
			CleanupOnCancel:  true,
		},
	}
}

func (a App) trustInit(opts trustInitOptions) error {
	if opts.OutPath == "" {
		return fmt.Errorf("out is required")
	}
	if opts.RootKeyPath == "" {
		return fmt.Errorf("root-key is required")
	}
	if opts.GatewayPath == "" {
		return fmt.Errorf("gateway-key is required")
	}
	if opts.ValidHours <= 0 {
		return fmt.Errorf("valid-hours must be positive")
	}
	rootKey, _, err := signing.LoadOrCreate(opts.RootKeyPath, opts.RootKeyID)
	if err != nil {
		return err
	}
	gatewayKey, _, err := signing.LoadOrCreate(opts.GatewayPath, opts.GatewayKeyID)
	if err != nil {
		return err
	}
	if rootKey.ID == gatewayKey.ID {
		return fmt.Errorf("root key id and gateway key id must be different")
	}
	now := time.Now().UTC()
	bundle, err := model.NewSignedTrustBundle(model.SignedTrustBundleSpec{
		BundleID:     opts.BundleID,
		Sequence:     1,
		NotBefore:    now,
		NotAfter:     now.Add(time.Duration(opts.ValidHours) * time.Hour),
		SigningKeyID: rootKey.ID,
		Keys: []model.TrustKey{
			model.NewTrustKey(rootKey.ID, rootKey.PublicKey, model.TrustKeyStatusActive, now),
			model.NewTrustKey(gatewayKey.ID, gatewayKey.PublicKey, model.TrustKeyStatusActive, now),
		},
	}, now)
	if err != nil {
		return err
	}
	bundle, err = bundle.Sign(rootKey.PrivateKey)
	if err != nil {
		return err
	}
	if err := bundle.Verify(model.NewTrustBundle(rootKey.ID, rootKey.PublicKey), now); err != nil {
		return err
	}
	if err := writeTrustBundleFile(opts.OutPath, bundle, opts.Force); err != nil {
		return err
	}
	return a.printTrustBundleSummary(bundle, opts.OutPath, model.NewTrustBundle(rootKey.ID, rootKey.PublicKey), map[string]any{
		"action":             "init",
		"gateway_public_key": encodeRootPublicKey(gatewayKey.ID, gatewayKey.PublicKey),
	})
}

func (a App) trustRotate(opts trustRotateOptions) error {
	if opts.CurrentPath == "" {
		return fmt.Errorf("current is required")
	}
	if opts.OutPath == "" {
		return fmt.Errorf("out is required")
	}
	if opts.RootKeyPath == "" {
		return fmt.Errorf("root-key is required")
	}
	if opts.GatewayPath == "" {
		return fmt.Errorf("gateway-key is required")
	}
	if opts.ValidHours <= 0 {
		return fmt.Errorf("valid-hours must be positive")
	}
	current, err := readTrustBundleFile(opts.CurrentPath)
	if err != nil {
		return err
	}
	rootKey, _, err := signing.LoadOrCreate(opts.RootKeyPath, current.SigningKeyID)
	if err != nil {
		return err
	}
	root := model.NewTrustBundle(rootKey.ID, rootKey.PublicKey)
	now := time.Now().UTC()
	if err := current.Verify(root, now); err != nil {
		return err
	}
	if opts.GatewayKeyID == "" {
		opts.GatewayKeyID = "gateway-" + strconv.Itoa(current.Sequence+1)
	}
	gatewayKey, _, err := signing.LoadOrCreate(opts.GatewayPath, opts.GatewayKeyID)
	if err != nil {
		return err
	}
	if _, ok := current.Key(gatewayKey.ID); ok {
		return fmt.Errorf("gateway key id %q already exists in current trust bundle", gatewayKey.ID)
	}
	keys := copyTrustKeysForCLI(current.Keys)
	retired := map[string]bool{}
	for _, keyID := range opts.RetireKeyIDs {
		retired[keyID] = true
	}
	for i := range keys {
		if retired[keys[i].KeyID] {
			if keys[i].KeyID == current.SigningKeyID {
				return fmt.Errorf("cannot retire current signing key %q with trust rotate", keys[i].KeyID)
			}
			keys[i].Status = model.TrustKeyStatusRetired
			until := now
			keys[i].NotAfter = &until
		}
	}
	for keyID := range retired {
		if _, ok := current.Key(keyID); !ok {
			return fmt.Errorf("retire key %q not found in current trust bundle", keyID)
		}
	}
	keys = append(keys, model.NewTrustKey(gatewayKey.ID, gatewayKey.PublicKey, model.TrustKeyStatusActive, now))
	next, err := buildNextTrustBundle(current, keys, current.SigningKeyID, opts.ValidHours, now)
	if err != nil {
		return err
	}
	next, err = next.Sign(rootKey.PrivateKey)
	if err != nil {
		return err
	}
	if err := next.VerifyUpdate(current, root, now); err != nil {
		return err
	}
	if err := writeTrustBundleFile(opts.OutPath, next, opts.Force); err != nil {
		return err
	}
	return a.printTrustBundleSummary(next, opts.OutPath, root, map[string]any{
		"action":             "rotate",
		"previous":           opts.CurrentPath,
		"retired_keys":       opts.RetireKeyIDs,
		"gateway_public_key": encodeRootPublicKey(gatewayKey.ID, gatewayKey.PublicKey),
	})
}

func (a App) trustRevoke(opts trustRevokeOptions) error {
	if opts.CurrentPath == "" {
		return fmt.Errorf("current is required")
	}
	if opts.OutPath == "" {
		return fmt.Errorf("out is required")
	}
	if opts.RootKeyPath == "" {
		return fmt.Errorf("root-key is required")
	}
	if opts.KeyID == "" {
		return fmt.Errorf("key-id is required")
	}
	if opts.ValidHours <= 0 {
		return fmt.Errorf("valid-hours must be positive")
	}
	current, err := readTrustBundleFile(opts.CurrentPath)
	if err != nil {
		return err
	}
	if opts.KeyID == current.SigningKeyID {
		return fmt.Errorf("cannot revoke current signing key %q with trust revoke; publish a new pinned root out-of-band", opts.KeyID)
	}
	rootKey, _, err := signing.LoadOrCreate(opts.RootKeyPath, current.SigningKeyID)
	if err != nil {
		return err
	}
	root := model.NewTrustBundle(rootKey.ID, rootKey.PublicKey)
	now := time.Now().UTC()
	if err := current.Verify(root, now); err != nil {
		return err
	}
	keys := copyTrustKeysForCLI(current.Keys)
	found := false
	for i := range keys {
		if keys[i].KeyID == opts.KeyID {
			found = true
			keys[i].Status = model.TrustKeyStatusRevoked
			keys[i].RevokedReason = opts.Reason
			revokedAt := now
			keys[i].RevokedAt = &revokedAt
			keys[i].NotAfter = &revokedAt
		}
	}
	if !found {
		return fmt.Errorf("key id %q not found in current trust bundle", opts.KeyID)
	}
	next, err := buildNextTrustBundle(current, keys, current.SigningKeyID, opts.ValidHours, now)
	if err != nil {
		return err
	}
	next, err = next.Sign(rootKey.PrivateKey)
	if err != nil {
		return err
	}
	if err := next.VerifyUpdate(current, root, now); err != nil {
		return err
	}
	if err := writeTrustBundleFile(opts.OutPath, next, opts.Force); err != nil {
		return err
	}
	return a.printTrustBundleSummary(next, opts.OutPath, root, map[string]any{
		"action":      "revoke",
		"previous":    opts.CurrentPath,
		"revoked_key": opts.KeyID,
		"reason":      opts.Reason,
	})
}

func (a App) trustVerify(bundlePath, rootPublicKey string) error {
	if bundlePath == "" {
		return fmt.Errorf("bundle is required")
	}
	root, err := parseRootPublicKey(rootPublicKey)
	if err != nil {
		return err
	}
	bundle, err := readTrustBundleFile(bundlePath)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	verifyErr := bundle.Verify(root, now)
	hash, hashErr := bundle.Hash()
	if hashErr != nil {
		return hashErr
	}
	payload := map[string]any{
		"ok":              verifyErr == nil,
		"schema":          bundle.SchemaVersion,
		"bundle":          bundlePath,
		"bundle_id":       bundle.BundleID,
		"sequence":        bundle.Sequence,
		"hash":            hash,
		"signing_key_id":  bundle.SigningKeyID,
		"root_public_key": rootPublicKey,
		"keys":            trustKeySummary(bundle),
	}
	if verifyErr != nil {
		payload["error"] = verifyErr.Error()
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	return verifyErr
}

func buildNextTrustBundle(current model.SignedTrustBundle, keys []model.TrustKey, signingKeyID string, validHours int, now time.Time) (model.SignedTrustBundle, error) {
	hash, err := current.Hash()
	if err != nil {
		return model.SignedTrustBundle{}, err
	}
	return model.NewSignedTrustBundle(model.SignedTrustBundleSpec{
		BundleID:           current.BundleID,
		Sequence:           current.Sequence + 1,
		NotBefore:          now,
		NotAfter:           now.Add(time.Duration(validHours) * time.Hour),
		PreviousBundleHash: hash,
		SigningKeyID:       signingKeyID,
		Keys:               keys,
	}, now)
}

func readTrustBundleFile(path string) (model.SignedTrustBundle, error) {
	if path == "" {
		return model.SignedTrustBundle{}, fmt.Errorf("trust bundle path is required")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return model.SignedTrustBundle{}, err
	}
	var bundle model.SignedTrustBundle
	if err := json.Unmarshal(content, &bundle); err == nil && bundle.SchemaVersion == model.SignedTrustBundleSchemaVersion {
		return bundle, nil
	}
	var wrapped struct {
		TrustBundle model.SignedTrustBundle `json:"trust_bundle"`
	}
	if err := json.Unmarshal(content, &wrapped); err != nil {
		return model.SignedTrustBundle{}, err
	}
	if wrapped.TrustBundle.SchemaVersion != model.SignedTrustBundleSchemaVersion {
		return model.SignedTrustBundle{}, fmt.Errorf("unsupported trust bundle schema %q", wrapped.TrustBundle.SchemaVersion)
	}
	return wrapped.TrustBundle, nil
}

func readEnrollmentCertificateFile(path string) (model.HostEnrollmentCertificate, error) {
	if path == "" {
		return model.HostEnrollmentCertificate{}, fmt.Errorf("certificate is required")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return model.HostEnrollmentCertificate{}, err
	}
	var certificate model.HostEnrollmentCertificate
	if err := json.Unmarshal(content, &certificate); err == nil && certificate.SchemaVersion == model.HostEnrollmentCertificateSchemaVersion {
		return certificate, nil
	}
	var wrapped struct {
		Certificate           model.HostEnrollmentCertificate `json:"certificate"`
		EnrollmentCertificate model.HostEnrollmentCertificate `json:"enrollment_certificate"`
	}
	if err := json.Unmarshal(content, &wrapped); err != nil {
		return model.HostEnrollmentCertificate{}, err
	}
	if wrapped.Certificate.SchemaVersion == model.HostEnrollmentCertificateSchemaVersion {
		return wrapped.Certificate, nil
	}
	if wrapped.EnrollmentCertificate.SchemaVersion == model.HostEnrollmentCertificateSchemaVersion {
		return wrapped.EnrollmentCertificate, nil
	}
	return model.HostEnrollmentCertificate{}, fmt.Errorf("unsupported enrollment certificate schema")
}

func readEnrollmentRevocationListFile(path string) (model.HostEnrollmentRevocationList, error) {
	if path == "" {
		return model.HostEnrollmentRevocationList{}, fmt.Errorf("revocations is required")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return model.HostEnrollmentRevocationList{}, err
	}
	var list model.HostEnrollmentRevocationList
	if err := json.Unmarshal(content, &list); err != nil {
		return model.HostEnrollmentRevocationList{}, err
	}
	if list.SchemaVersion != model.HostEnrollmentRevocationListSchemaVersion {
		return model.HostEnrollmentRevocationList{}, fmt.Errorf("unsupported enrollment revocation list schema %q", list.SchemaVersion)
	}
	return list, nil
}

func writeEnrollmentCertificateFile(path string, certificate model.HostEnrollmentCertificate, force bool) error {
	if path == "" {
		return fmt.Errorf("out is required")
	}
	content, err := json.MarshalIndent(certificate, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
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

func writeEnrollmentRevocationListFile(path string, list model.HostEnrollmentRevocationList, force bool) error {
	if path == "" {
		return fmt.Errorf("out is required")
	}
	content, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
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

func parseEnrollmentHostMode(value string) (model.HostMode, error) {
	switch strings.TrimSpace(value) {
	case "", "managed":
		return model.HostModeManaged, nil
	case "temporary", "attended-temporary":
		return model.HostModeAttendedTemporary, nil
	case "break-glass":
		return model.HostModeBreakGlass, nil
	default:
		return "", fmt.Errorf("unsupported host mode %q", value)
	}
}

func writeTrustBundleFile(path string, bundle model.SignedTrustBundle, force bool) error {
	if path == "" {
		return fmt.Errorf("out is required")
	}
	content, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
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

func (a App) printTrustBundleSummary(bundle model.SignedTrustBundle, path string, root model.TrustBundle, extra map[string]any) error {
	hash, err := bundle.Hash()
	if err != nil {
		return err
	}
	if err := bundle.Verify(root, time.Now().UTC()); err != nil {
		return err
	}
	rootPublicKey, err := root.Ed25519PublicKey()
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":              true,
		"schema":          bundle.SchemaVersion,
		"bundle":          path,
		"bundle_id":       bundle.BundleID,
		"sequence":        bundle.Sequence,
		"hash":            hash,
		"signing_key_id":  bundle.SigningKeyID,
		"root_public_key": encodeRootPublicKey(root.SigningKeyID, rootPublicKey),
		"keys":            trustKeySummary(bundle),
	}
	for key, value := range extra {
		payload[key] = value
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func trustKeySummary(bundle model.SignedTrustBundle) []map[string]any {
	keys := make([]map[string]any, 0, len(bundle.Keys))
	for _, key := range bundle.Keys {
		keys = append(keys, map[string]any{
			"key_id":         key.KeyID,
			"status":         key.Status,
			"not_before":     key.NotBefore,
			"not_after":      key.NotAfter,
			"revoked_at":     key.RevokedAt,
			"revoked_reason": key.RevokedReason,
		})
	}
	return keys
}

func copyTrustKeysForCLI(keys []model.TrustKey) []model.TrustKey {
	copied := make([]model.TrustKey, len(keys))
	copy(copied, keys)
	for i := range copied {
		if copied[i].NotAfter != nil {
			value := copied[i].NotAfter.UTC()
			copied[i].NotAfter = &value
		}
		if copied[i].RevokedAt != nil {
			value := copied[i].RevokedAt.UTC()
			copied[i].RevokedAt = &value
		}
	}
	return copied
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
		"ok":                              true,
		"out":                             outPath,
		"schema":                          manifest.SchemaVersion,
		"skill_count":                     len(manifest.Skills),
		"file_count":                      len(manifest.Files),
		"frameworks":                      manifest.Frameworks,
		"manifest":                        filepath.Join(outPath, "manifest.json"),
		"gateway_url":                     manifest.GatewayURL,
		"adaptive_configuration_schema":   manifest.AdaptiveConfiguration.SchemaVersion,
		"adaptive_configuration_required": manifest.AdaptiveConfiguration.Required,
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
		"ok":                              report.OK(),
		"schema":                          report.SchemaVersion,
		"bundle":                          report.BundleDir,
		"manifest":                        report.ManifestPath,
		"manifest_schema":                 report.ManifestSchema,
		"checks":                          report.Checks,
		"files_verified":                  report.FilesVerified,
		"skills_verified":                 report.SkillsVerified,
		"frameworks_verified":             report.FrameworksVerified,
		"adaptive_configuration_verified": checkPassedByName(report.Checks, "adaptive_configuration_contract"),
		"recommended_actions":             report.RecommendedActions,
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

func (a App) skillkitPlanInstall(bundleDir, outPath, frameworks, rdevCommand string) error {
	if bundleDir == "" {
		return fmt.Errorf("bundle is required")
	}
	if outPath == "" {
		return fmt.Errorf("out is required")
	}
	var frameworkValues []string
	if strings.TrimSpace(frameworks) != "" {
		frameworkValues = []string{frameworks}
	}
	plan, err := skillkit.PlanInstall(skillkit.InstallPlanOptions{
		BundleDir:   bundleDir,
		OutDir:      outPath,
		Frameworks:  frameworkValues,
		RdevCommand: rdevCommand,
	})
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                            true,
		"schema":                        plan.SchemaVersion,
		"bundle":                        plan.BundleDir,
		"out":                           plan.OutDir,
		"plan":                          filepath.Join(plan.OutDir, "install-plan.json"),
		"external_mutation":             plan.ExternalMutation,
		"framework_count":               len(plan.Frameworks),
		"frameworks":                    installPlanFrameworkNames(plan.Frameworks),
		"file_count":                    len(plan.Files),
		"install_commands":              filepath.Join(plan.OutDir, "INSTALL_COMMANDS.md"),
		"recommended_steps":             plan.RecommendedNextSteps,
		"bundle_verify_ok":              plan.BundleVerification.OK(),
		"adaptive_configuration_schema": plan.AdaptiveConfiguration.SchemaVersion,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func (a App) skillkitVerifyInstallPlan(planPath string) error {
	if planPath == "" {
		return fmt.Errorf("plan is required")
	}
	report, err := skillkit.VerifyInstallPlan(planPath, time.Now())
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                              report.OK(),
		"schema":                          report.SchemaVersion,
		"plan":                            report.PlanPath,
		"plan_schema":                     report.PlanSchema,
		"checks":                          report.Checks,
		"files_verified":                  report.FilesVerified,
		"frameworks_verified":             report.FrameworksVerified,
		"bundle_verify_ok":                report.BundleVerification.OK(),
		"adaptive_configuration_verified": checkPassedByName(report.Checks, "adaptive_configuration_contract"),
		"recommended_actions":             report.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !report.OK() {
		return fmt.Errorf("skillkit install plan verification failed")
	}
	return nil
}

func (a App) skillkitInstall(bundleDir, framework, targetDir string, execute, force bool) error {
	if bundleDir == "" {
		return fmt.Errorf("bundle is required")
	}
	if framework == "" {
		return fmt.Errorf("framework is required")
	}
	report, err := skillkit.Install(skillkit.InstallOptions{
		BundleDir: bundleDir,
		Framework: framework,
		TargetDir: targetDir,
		Execute:   execute,
		Force:     force,
	})
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                  report.OK(),
		"schema":              report.SchemaVersion,
		"bundle":              report.BundleDir,
		"framework":           report.Framework,
		"display_name":        report.DisplayName,
		"target":              report.TargetDir,
		"execute":             report.Execute,
		"executed":            report.Executed,
		"force":               report.Force,
		"local_mutation":      report.LocalMutation,
		"external_mutation":   report.ExternalMutation,
		"bundle_verify_ok":    report.BundleVerification.OK(),
		"checks":              report.Checks,
		"actions":             report.Actions,
		"installed_skills":    report.InstalledSkills,
		"install_manifest":    report.InstallManifest,
		"mcp_command":         report.MCPCommand,
		"reference_files":     report.ReferenceFiles,
		"recommended_steps":   report.RecommendedNextSteps,
		"recommended_actions": report.RecommendedActions,
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !report.OK() {
		return fmt.Errorf("skillkit install verification failed")
	}
	return nil
}

func installPlanFrameworkNames(plans []skillkit.FrameworkInstallPlan) []string {
	names := make([]string, 0, len(plans))
	for _, plan := range plans {
		names = append(names, plan.Framework)
	}
	return names
}

func checkPassedByName(checks []skillkit.VerificationCheck, name string) bool {
	for _, check := range checks {
		if check.Name == name && check.Passed {
			return true
		}
	}
	return false
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
  rdev bootstrap agent-plan --repo-root . --remote-requested
  rdev support-session connect --start --target auto --locale auto
  rdev support-session connect --target auto --reason "visible temporary remote support"
  rdev support-session status --gateway-url http://127.0.0.1:8787 --ticket-code ABCD-1234 --wait --locale auto
  rdev support-session report --gateway-url http://127.0.0.1:8787 --ticket-code ABCD-1234
  rdev support-session --help
  rdev ticket create --mode attended-temporary --ttl-seconds 7200
  rdev policy explain --mode attended-temporary --capability shell.user
  rdev policy explain-shell --policy-json '{"workspace_root":".","capabilities":["shell.user"],"argv":["go","env","GOOS"],"allow_commands":["go"]}'
  rdev demo local
  rdev mcp tools
  rdev mcp serve
  rdev gateway serve --dev --addr 127.0.0.1:8787 --signing-key .rdev/keys/gateway-signing-key.json --state .rdev/gateway/state.json --enrollment-root-public-key enrollment-root:... --enrollment-revocations .rdev/enrollment/revocations.json
  rdev gateway serve --dev --addr 127.0.0.1:8787 --tls-cert gateway.pem --tls-key gateway-key.pem --client-ca client-ca.pem
  rdev audit export --input .rdev/audit/events.jsonl --out .rdev/audit/chain.json
  rdev audit verify --input .rdev/audit/chain.json
  rdev evidence export --job-json job.json --artifacts-json artifacts.json --audit-jsonl events.jsonl --out job_evidence
  rdev evidence export --gateway http://127.0.0.1:8787 --job-id job_... --out job_evidence
  rdev skillkit export --source-root . --out dist/remote-dev-skillkit --gateway-url https://api.example.com/v1
  rdev skillkit verify --bundle dist/remote-dev-skillkit
  rdev skillkit plan-install --bundle dist/remote-dev-skillkit --out dist/skillkit-install --frameworks codex,hermes,generic-mcp-agent
  rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
  rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills
  rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute
  rdev update check --repo EitanWong/remote-dev-skillkit
  rdev update plan --repo EitanWong/remote-dev-skillkit --platform darwin/arm64
  rdev deps install --tool chisel --scope user --platform linux/amd64 --url https://example.com/chisel.tar.gz --expected-sha256 <sha256>
  rdev deps install --tool tailscale --scope user --platform linux/amd64 --url https://example.com/tailscale.zip --expected-sha256 <sha256> --execute
  rdev job list --gateway-url http://127.0.0.1:8787
  rdev job get --gateway-url http://127.0.0.1:8787 --job-id job_...
  rdev job wait --gateway-url http://127.0.0.1:8787 --job-id job_... --timeout-seconds 120
  rdev job artifacts --gateway-url http://127.0.0.1:8787 --job-id job_...
  rdev job policy-template --capability process.inspect --target-os windows
  rdev job cancel --gateway-url http://127.0.0.1:8787 --job-id job_... --reason "stuck job"
  rdev invite create --gateway https://api.example.com/v1 --reason "repair target host" --transport auto
  rdev connection-entry plan --invite invite.json --out connection-entry --target-os windows --ownership third-party
  rdev connection-entry run --runner-manifest connection-entry/connection-entry-runner/connection-entry-runner.json --dry-run --evidence-dir relay-evidence
  RDEV_RELAY_GATEWAY_URL=http://127.0.0.1:8787 rdev connection-entry run --runner-manifest connection-entry/connection-entry-runner/connection-entry-runner.json --evidence-dir relay-evidence
  rdev connection-entry plan --invite invite.json --out managed-entry --target-os linux --ownership owned --managed-binary /opt/rdev/rdev --release-bundle /opt/rdev/release-bundle.json --release-root-public-key release-root:...
  rdev adapter scaffold --adapter claude-code --out examples/adapters/claude-code-lifecycle.json
  rdev adapter verify-result --artifact shell-result.json --adapter shell --schema rdev.shell-result.v1
  rdev adapter verify-lifecycle --artifact examples/adapters/claude-code-lifecycle.json --adapter claude-code
  rdev adapter verify-cancellation --artifact shell-result.json --adapter shell --schema rdev.shell-result.v1
  rdev adapter verify-runtime --artifact adapter-runtime-fixture.json --adapter claude-code --require-result-artifact
  rdev operator-auth init --out .rdev/operator-auth/operators.json --token-dir .rdev/operator-auth/tokens
  rdev operator-auth verify --auth .rdev/operator-auth/operators.json
  rdev operator-auth verify-oidc-jwks --auth .rdev/operator-auth/oidc-jwks.json --token-file .rdev/operator-auth/operator.jwt --role operator
  rdev operator-auth verify-saml --auth .rdev/operator-auth/saml.json --response-file .rdev/operator-auth/operator.samlresponse --role operator
  rdev gateway storage verify --provider postgres --path service=rdev_gateway
  rdev hosted-provider package --out dist/hosted-provider --storage-provider file --auth-provider hosted-ed25519-jwt
  rdev hosted-provider package --out dist/hosted-provider-postgres --storage-provider postgres --auth-provider hosted-ed25519-jwt
  rdev hosted-provider package --out dist/hosted-provider-postgres-oidc --storage-provider postgres --auth-provider oidc-jwks
  rdev hosted-provider verify --package dist/hosted-provider
  rdev enrollment issue-certificate --gateway http://127.0.0.1:8787 --out host-enrollment.json --root-public-key enrollment-root:... --ticket-code ABCD-1234 --name managed-mac --os darwin --arch arm64 --identity-key-id host --identity-public-key <base64url> --identity-fingerprint sha256:... --operator-token-file operator-token.txt
  rdev enrollment sign-certificate --out host-enrollment.json --key .rdev/keys/enrollment-root.json --ticket-code ABCD-1234 --mode managed --name managed-mac --os darwin --arch arm64 --identity-key-id host --identity-public-key <base64url> --identity-fingerprint sha256:... --capabilities codex.run,git.diff
  rdev enrollment verify-certificate --certificate host-enrollment.json --root-public-key enrollment-root:...
  rdev enrollment renew-certificate --certificate host-enrollment.json --out host-enrollment-renewed.json --key .rdev/keys/enrollment-root.json --revocations revocations.json
  rdev enrollment renew-certificate --certificate host-enrollment.json --out host-enrollment-hosted-renewed.json --gateway http://127.0.0.1:8787 --root-public-key enrollment-root:... --operator-token-file operator-token.txt
  rdev enrollment init-revocations --out revocations.json --key .rdev/keys/enrollment-root.json
  rdev enrollment revoke-certificate --out revocations.json --key .rdev/keys/enrollment-root.json --certificate host-enrollment.json --reason "host retired"
  rdev enrollment verify-revocations --revocations revocations.json --root-public-key enrollment-root:...
  rdev enrollment verify-certificate --certificate host-enrollment.json --root-public-key enrollment-root:... --revocations revocations.json
  rdev enrollment fetch-revocations --gateway http://127.0.0.1:8787 --root-public-key enrollment-root:... --out revocations.json
  rdev trust init --out .rdev/trust/trust-bundle.json --root-key .rdev/keys/trust-root.json --gateway-key .rdev/keys/gateway-prod.json
  rdev trust rotate --current .rdev/trust/trust-bundle.json --out .rdev/trust/trust-bundle-next.json --root-key .rdev/keys/trust-root.json --gateway-key .rdev/keys/gateway-next.json --gateway-key-id gateway-next --retire-key gateway-prod
  rdev trust revoke --current .rdev/trust/trust-bundle-next.json --out .rdev/trust/trust-bundle-revoked.json --root-key .rdev/keys/trust-root.json --key-id gateway-next --reason "key compromise drill"
  rdev trust verify --bundle .rdev/trust/trust-bundle-revoked.json --root-public-key trust-root:...
  rdev workspace lock --repo . --host-id hst_... --job-id job_... --adapter codex
  rdev workspace prepare-worktree --repo . --host-id hst_... --job-id job_... --adapter codex
  rdev acceptance fresh-agent-support-session --out fresh-agent-support-session
  rdev acceptance managed-mac --out acceptance-run --repo .
  rdev acceptance managed-mac-service --out service-plan --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --repo . --release-bundle /opt/rdev/release-bundle.json --release-root-public-key release-root:... --release-require-artifacts rdev,rdev-host,rdev-verify
  rdev acceptance windows-temporary --out windows-plan --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --download-url https://agent.example/rdev-host.exe --expected-sha256 <sha256> --release-bundle-url https://agent.example/release-bundle.json --release-root-public-key release-root:... --verifier-download-url https://agent.example/rdev-verify.exe --verifier-sha256 <sha256>
  rdev acceptance windows-managed-service --out windows-service-plan --binary 'C:\Program Files\rdev\rdev.exe' --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --release-bundle 'C:\Program Files\rdev\release-bundle.json' --release-root-public-key release-root:... --release-require-artifacts rdev.exe,rdev-host.exe,rdev-verify.exe
  rdev acceptance linux-managed-service --out linux-service-plan --binary /opt/rdev/rdev --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --release-bundle /opt/rdev/release-bundle.json --release-root-public-key release-root:... --release-require-artifacts rdev,rdev-host,rdev-verify
  rdev acceptance verify --report acceptance-run/report.json
  rdev acceptance verify-managed-mac-service --plan service-plan/service-plan.json
  rdev acceptance verify-windows-temporary --plan windows-plan/windows-temporary-plan.json
  rdev acceptance verify-windows-managed-service --plan windows-service-plan/windows-managed-service-plan.json
  rdev acceptance verify-linux-managed-service --plan linux-service-plan/linux-managed-service-plan.json
  rdev acceptance verify-relay-adapter-package --package relay-evidence/package.json
  rdev acceptance verify-hosted-provider-runtime-package --package hosted-runtime-evidence/package.json
  rdev acceptance verify-post-release-download-package --package post-release-download-evidence/package.json
  rdev acceptance scaffold-evidence --hosted-provider-package hosted-provider --out hosted-runtime-evidence-input
  rdev acceptance scaffold-evidence --relay-adapter-package relay-adapter --out relay-evidence-input
  rdev acceptance evidence-status --scaffold hosted-runtime-evidence-input
  rdev acceptance scaffold-post-release-download --post-release-install-dir post-release-install --out post-release-download-evidence-input
  rdev acceptance post-release-evidence-status --scaffold post-release-download-evidence-input
  rdev acceptance package-managed-mac-service --plan service-plan/service-plan.json --out mac-service-evidence --review-transcript review.txt --start-transcript start.txt --inspect-transcript inspect.txt --logs launchagent.log --release-gate release-gate.json --audit audit.jsonl --reconnect reconnect.txt --managed-report managed-mac/report.json --stop-transcript stop.txt --uninstall-transcript uninstall.txt
  rdev acceptance package-windows-temporary --plan windows-plan/windows-temporary-plan.json --out windows-evidence --transcript transcript.txt --release-verification rdev-verify.json --audit audit.jsonl --no-persistence-dir no-persistence --approval-probes-dir approval-probes
  rdev acceptance package-linux-managed-service --plan linux-service-plan/linux-managed-service-plan.json --out linux-evidence --start-transcript start.txt --status-transcript status.txt --logs journal.txt --release-gate release-gate.json --audit audit.jsonl --reconnect reconnect.txt --job-evidence-dir job-evidence --stop-transcript stop.txt --uninstall-transcript uninstall.txt
  rdev acceptance package-relay-adapter --relay-package relay-adapter --out relay-evidence --evidence-dir relay-evidence-input
  rdev acceptance package-hosted-provider-runtime --hosted-provider-package hosted-provider --out hosted-runtime-evidence --evidence-dir hosted-runtime-evidence-input
  rdev acceptance package-post-release-download --scaffold post-release-download-evidence-input --out post-release-download-evidence
  rdev acceptance release-evidence-index --out release-evidence-index --hosted-provider-runtime-package hosted-runtime-evidence --relay-adapter-package relay-evidence --post-release-download-package post-release-download-evidence
  rdev release sign --artifact ./rdev-host.exe --key .rdev/keys/release-root.json
  rdev release verify --artifact ./rdev-host.exe --manifest ./rdev-host.exe.rdev-release.json --root-public-key release-root:...
  rdev release create-bundle --dir dist --artifacts rdev,rdev-host.exe,rdev-verify.exe --key .rdev/keys/release-root.json
  rdev release verify-bundle --bundle dist/release-bundle.json --root-public-key release-root:...
  rdev release prepare-candidate --source-root . --out dist/release-candidate --version v0.1.0 --artifacts ./rdev,./rdev-host.exe,./rdev-verify.exe --key .rdev/keys/release-root.json
  rdev release verify-candidate --candidate dist/release-candidate --require-artifacts rdev-host.exe,rdev-verify.exe
  rdev host serve --mode temporary --gateway http://127.0.0.1:8787 --ticket-code ABCD-1234
  rdev host serve --mode managed --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --enrollment-certificate host-enrollment.json --renew-enrollment-certificate --fetch-enrollment-revocations --enrollment-root-public-key enrollment-root:... --release-bundle /opt/rdev/release-bundle.json --release-root-public-key release-root:... --release-require-artifacts rdev-host,rdev-verify
  rdev host install-service --platform macos --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --release-bundle /opt/rdev/release-bundle.json --release-root-public-key release-root:... --release-require-artifacts rdev-host,rdev-verify --plist-out ./com.remote-dev-skillkit.host.plist
  rdev host service-status --platform macos --plist ./com.remote-dev-skillkit.host.plist
  rdev host service-control --platform macos --action start --plist ./com.remote-dev-skillkit.host.plist
  rdev host uninstall-service --platform macos --plist ./com.remote-dev-skillkit.host.plist
  rdev host install-service --platform linux --label rdev-host.service --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --release-bundle /opt/rdev/release-bundle.json --release-root-public-key release-root:... --release-require-artifacts rdev-host,rdev-verify --unit-out ./rdev-host.service
  rdev host service-status --platform linux --label rdev-host.service --unit ./rdev-host.service
  rdev host service-control --platform linux --action start --label rdev-host.service --unit ./rdev-host.service
  rdev host uninstall-service --platform linux --label rdev-host.service --unit ./rdev-host.service
  rdev host install-service --platform windows --label RemoteDevSkillkitHost --binary 'C:\Program Files\rdev\rdev.exe' --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --release-bundle 'C:\Program Files\rdev\release-bundle.json' --release-root-public-key release-root:... --release-require-artifacts rdev-host.exe,rdev-verify.exe
  rdev host service-status --platform windows --label RemoteDevSkillkitHost
  rdev host service-control --platform windows --action start --label RemoteDevSkillkitHost
  rdev host uninstall-service --platform windows --label RemoteDevSkillkitHost
`))
}

func (a App) printSupportSessionUsage() {
	_, _ = fmt.Fprintln(a.Stdout, strings.TrimSpace(`rdev support-session - standard AI-native remote support entry

Usage:
  rdev support-session connect --start --target auto --locale auto
  rdev support-session connect --gateway-url https://gateway.example.com --target auto --locale auto
  rdev support-session prepare --target auto --build-assets
  rdev support-session status --gateway-url <active-gateway-url> --ticket-code ABCD-1234 --wait
  rdev support-session report --gateway-url <active-gateway-url> --ticket-code ABCD-1234
  rdev support-session recover --gateway-url <active-gateway-url> --ticket-code ABCD-1234
  rdev support-session audit-capabilities --gateway-url <active-gateway-url> --host-id hst_...

Fresh-Agent path:
  1. Run: rdev support-session connect --start
  2. Read handoff_text_file.path or target_handoff_envelope.full_text.
  3. Send that one complete handoff to the target-side human.
  4. Wait with support-session status, then use report.recommended_job_host_id.

Do not add --public-tunnel. Tunnel/provider selection is owned by connect --start.
Use "<subcommand> --help" for flags.`))
}

func (a App) printCommandGroupUsage(command string) bool {
	if command == "support-session" {
		a.printSupportSessionUsage()
		return true
	}
	subcommands := map[string]string{
		"acceptance":       "fresh-agent-support-session, managed-mac, managed-mac-service, windows-temporary, windows-managed-service, linux-managed-service, verify, verify-windows-temporary, verify-managed-mac-service, verify-windows-managed-service, verify-linux-managed-service, verify-relay-adapter-package, verify-hosted-provider-runtime-package, verify-post-release-download-package, scaffold-evidence, evidence-status, scaffold-post-release-download, post-release-evidence-status, package-windows-temporary, package-managed-mac-service, package-linux-managed-service, package-relay-adapter, package-hosted-provider-runtime, package-post-release-download, release-evidence-index",
		"adapter":          "scaffold, verify-result, verify-lifecycle, verify-cancellation, verify-runtime",
		"audit":            "export, verify",
		"bootstrap":        "agent-plan",
		"connection-entry": "plan, run",
		"demo":             "local",
		"deps":             "install",
		"enrollment":       "issue-certificate, sign-certificate, verify-certificate, renew-certificate, revoke-certificate, init-revocations, verify-revocations, fetch-revocations, lifecycle",
		"evidence":         "export",
		"gateway":          "serve, storage verify",
		"host":             "serve, install-service, service-status, service-control, uninstall-service",
		"hosted-provider":  "package, verify",
		"invite":           "create",
		"job":              "create, list, get, wait, artifacts, policy-template, cancel",
		"mcp":              "tools, serve",
		"operator-auth":    "init, verify, verify-hosted, verify-oidc-jwks, verify-saml",
		"policy":           "explain, explain-shell",
		"release":          "sign, verify, create-bundle, verify-bundle, prepare-candidate, verify-candidate",
		"relay-adapter":    "package, verify",
		"skillkit":         "export, verify, plan-install, verify-install-plan, install",
		"ticket":           "create",
		"trust":            "init, rotate, revoke, verify",
		"update":           "check, plan",
		"workspace":        "lock, status, unlock, prepare-worktree",
	}
	available, ok := subcommands[command]
	if !ok {
		return false
	}
	_, _ = fmt.Fprintf(a.Stdout, "rdev %s\n\nUsage:\n  rdev %s <subcommand> [flags]\n\nSubcommands:\n  %s\n\nUse `rdev %s <subcommand> --help` for flags.\n", command, command, available, command)
	return true
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

func doGatewayRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(req)
}

// retryingRoundTripper wraps an http.RoundTripper and automatically retries
// idempotent requests (GET, HEAD) on transient connection-level errors.
// Cloudflare Quick Tunnels and similar reverse-proxy layers occasionally close
// keepalive connections with a TLS EOF / "unexpected EOF" before the response
// is fully delivered; a single retry is usually enough to succeed.
type retryingRoundTripper struct {
	Base       http.RoundTripper
	MaxRetries int
}

func (r retryingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := r.Base
	if base == nil {
		base = http.DefaultTransport
	}
	maxRetries := r.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	// Only retry safe/idempotent methods — never blindly re-POST.
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return base.RoundTrip(req)
	}
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(time.Duration(attempt*attempt) * 100 * time.Millisecond):
			}
		}
		resp, err := base.RoundTrip(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isRetryableNetErr(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

// isRetryableNetErr returns true for transient low-level transport errors that
// are safe to retry (EOF, connection-reset, broken-pipe).
func isRetryableNetErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "eof") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "use of closed network connection")
}

// A missing heartbeat for > 90 s causes the gateway to mark the host stale.
func sendHeartbeat(ctx context.Context, client *http.Client, gatewayURL, hostID, hostSecret string) error {
	if strings.TrimSpace(hostSecret) == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(gatewayURL, "/")+"/v1/hosts/"+url.PathEscape(hostID)+"/heartbeat",
		nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+hostSecret)
	resp, err := doGatewayRequest(client, req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func startHostHeartbeat(ctx context.Context, client *http.Client, gatewayURL, hostID, hostSecret string) func() {
	if strings.TrimSpace(hostSecret) == "" {
		return func() {}
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	_ = sendHeartbeat(heartbeatCtx, client, gatewayURL, hostID, hostSecret)
	go func() {
		// Gateway marks a host stale after ~90 s of silence; ping every 30 s
		// and send one immediately before long waits through the call sites.
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				_ = sendHeartbeat(heartbeatCtx, client, gatewayURL, hostID, hostSecret)
			}
		}
	}()
	return stopHeartbeat
}

func registerHost(ctx context.Context, client *http.Client, gatewayURL string, registration model.HostRegistration) (model.Host, string, error) {
	body, err := json.Marshal(registration)
	if err != nil {
		return model.Host{}, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(gatewayURL, "/")+"/v1/hosts/register", bytes.NewReader(body))
	if err != nil {
		return model.Host{}, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := doGatewayRequest(client, req)
	if err != nil {
		return model.Host{}, "", err
	}
	defer resp.Body.Close()
	var payload struct {
		Host       model.Host `json:"host"`
		HostSecret string     `json:"host_secret"`
		Error      string     `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return model.Host{}, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return model.Host{}, "", fmt.Errorf("register host failed: %s", payload.Error)
	}
	if strings.TrimSpace(payload.HostSecret) == "" {
		return model.Host{}, "", fmt.Errorf("register host failed: missing host_secret")
	}
	return payload.Host, payload.HostSecret, nil
}

func (a App) pollAndRunDevJobs(ctx context.Context, opts hostServeOptions, client *http.Client, hostID, identityFingerprint, hostSecret string) (int, error) {
	return a.runHostJobs(ctx, opts, client, hostID, identityFingerprint, hostSecret)
}

func (a App) runHostJobs(ctx context.Context, opts hostServeOptions, client *http.Client, hostID, identityFingerprint, hostSecret string) (int, error) {
	switch strings.TrimSpace(opts.Transport) {
	case "auto":
		return a.runAutoHostJobs(ctx, opts, client, hostID, identityFingerprint, hostSecret)
	case "wss":
		return a.runWSSHostJobs(ctx, opts, client, hostID, identityFingerprint, hostSecret)
	default:
		return a.runPollingHostJobs(ctx, opts, client, hostID, identityFingerprint, hostSecret)
	}
}

func (a App) runAutoHostJobs(ctx context.Context, opts hostServeOptions, client *http.Client, hostID, identityFingerprint, hostSecret string) (int, error) {
	processed, err := a.runAutoHostJobsOnce(ctx, opts, client, hostID, identityFingerprint, hostSecret)
	if err == nil || processed > 0 || ctx.Err() != nil || len(opts.ManifestGatewayCandidates) == 0 {
		return processed, err
	}
	fallbacks := manifestGatewayFallbackURLs(opts.ManifestGatewayCandidates, opts.GatewayURL)
	for _, gatewayURL := range fallbacks {
		if !joinManifestGatewayReachable(ctx, client, gatewayURL, opts.TrustPin) {
			continue
		}
		next := opts
		next.GatewayURL = gatewayURL
		fallbackProcessed, fallbackErr := a.runAutoHostJobsOnce(ctx, next, client, hostID, identityFingerprint, hostSecret)
		if fallbackErr == nil || fallbackProcessed > 0 || ctx.Err() != nil {
			return fallbackProcessed, fallbackErr
		}
		err = fmt.Errorf("%v; fallback gateway %s: %w", err, gatewayURL, fallbackErr)
	}
	return processed, err
}

func (a App) runAutoHostJobsOnce(ctx context.Context, opts hostServeOptions, client *http.Client, hostID, identityFingerprint, hostSecret string) (int, error) {
	wssOpts := opts
	wssOpts.Transport = "wss"
	processed, err := a.runWSSHostJobs(ctx, wssOpts, client, hostID, identityFingerprint, hostSecret)
	if err == nil || processed > 0 || ctx.Err() != nil {
		return processed, err
	}
	longPollOpts := opts
	longPollOpts.Transport = "long-poll"
	longPollProcessed, longPollErr := a.runPollingHostJobs(ctx, longPollOpts, client, hostID, identityFingerprint, hostSecret)
	if longPollErr == nil || longPollProcessed > 0 || ctx.Err() != nil {
		return longPollProcessed, longPollErr
	}
	pollOpts := opts
	pollOpts.Transport = "poll"
	pollProcessed, pollErr := a.runPollingHostJobs(ctx, pollOpts, client, hostID, identityFingerprint, hostSecret)
	if pollErr != nil {
		return pollProcessed, fmt.Errorf("auto transport failed: wss: %v; long-poll: %v; poll: %w", err, longPollErr, pollErr)
	}
	return pollProcessed, nil
}

func manifestGatewayFallbackURLs(candidates []model.JoinManifestGatewayCandidate, currentGatewayURL string) []string {
	current := strings.TrimRight(strings.TrimSpace(currentGatewayURL), "/")
	out := []string{}
	seen := map[string]bool{current: true, "": true}
	for _, candidate := range candidates {
		gatewayURL := strings.TrimRight(strings.TrimSpace(candidate.URL), "/")
		if seen[gatewayURL] {
			continue
		}
		seen[gatewayURL] = true
		out = append(out, gatewayURL)
	}
	return out
}

func (a App) runPollingHostJobs(ctx context.Context, opts hostServeOptions, client *http.Client, hostID, identityFingerprint, hostSecret string) (int, error) {
	// maxJobs == 0 means unlimited; negative values fall back to 1.
	maxJobs := opts.MaxJobs
	switch {
	case maxJobs == 0:
		maxJobs = math.MaxInt
	case maxJobs < 0:
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
	trust, err := fetchHostTrust(ctx, client, opts.GatewayURL, opts.TrustPin, opts.TrustStorePath)
	if err != nil {
		return 0, err
	}
	trust.NonceStore = hostNonceStore(opts.NonceStorePath)
	trust.ApprovalStore = hostApprovalStore(opts.ApprovalStorePath)
	trust.WorkspaceLockStore = opts.WorkspaceLockStore
	trust.CaptureRuntimeFixture = opts.CaptureRuntimeFixture
	processed := 0
	stopHeartbeat := startHostHeartbeat(ctx, client, opts.GatewayURL, hostID, hostSecret)
	defer stopHeartbeat()
	for processed < maxJobs {
		wait := time.Duration(0)
		if transport == "long-poll" {
			wait = longPollTimeout
		}
		if opts.TrustStorePath != "" {
			trust, err = refreshHostTrustUpdate(ctx, client, opts.GatewayURL, hostID, opts.TrustStorePath, trust)
			if err != nil {
				return processed, err
			}
		}
		_ = sendHeartbeat(ctx, client, opts.GatewayURL, hostID, hostSecret)
		job, found, err := fetchNextJob(ctx, client, opts.GatewayURL, hostID, hostSecret, wait)
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
			monitorJobCancellation(monitorCtx, client, opts.GatewayURL, job.ID, cancellationPollInterval(interval), func() {
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
					if _, appendErr := appendJobArtifact(ctx, client, opts.GatewayURL, hostID, hostSecret, job.ID, result.ArtifactContent); appendErr != nil {
						return processed, appendErr
					}
				}
				if result.RuntimeFixtureContent != "" {
					if _, appendErr := appendJobArtifact(ctx, client, opts.GatewayURL, hostID, hostSecret, job.ID, result.RuntimeFixtureContent); appendErr != nil {
						return processed, appendErr
					}
				}
				processed++
				continue
			}
			if ctx.Err() != nil {
				return processed, ctx.Err()
			}
			if _, failErr := failJob(ctx, client, opts.GatewayURL, hostID, hostSecret, job.ID, err.Error(), result.ArtifactContent); failErr != nil {
				return processed, fmt.Errorf("%v; additionally failed to report job failure: %w", err, failErr)
			}
			if result.RuntimeFixtureContent != "" {
				if _, appendErr := appendJobArtifact(ctx, client, opts.GatewayURL, hostID, hostSecret, job.ID, result.RuntimeFixtureContent); appendErr != nil {
					return processed, fmt.Errorf("%v; additionally failed to append runtime fixture: %w", err, appendErr)
				}
			}
			processed++
			continue
		}
		if _, err := completeJob(ctx, client, opts.GatewayURL, hostID, hostSecret, job.ID, result.ArtifactContent); err != nil {
			return processed, err
		}
		if result.RuntimeFixtureContent != "" {
			if _, err := appendJobArtifact(ctx, client, opts.GatewayURL, hostID, hostSecret, job.ID, result.RuntimeFixtureContent); err != nil {
				return processed, err
			}
		}
		processed++
	}
	return processed, nil
}

func (a App) runWSSHostJobs(ctx context.Context, opts hostServeOptions, client *http.Client, hostID, identityFingerprint, hostSecret string) (int, error) {
	// maxJobs == 0 means unlimited; negative values fall back to 1.
	maxJobs := opts.MaxJobs
	switch {
	case maxJobs == 0:
		maxJobs = math.MaxInt
	case maxJobs < 0:
		maxJobs = 1
	}
	trust, err := fetchHostTrust(ctx, client, opts.GatewayURL, opts.TrustPin, opts.TrustStorePath)
	if err != nil {
		return 0, err
	}
	trust.NonceStore = hostNonceStore(opts.NonceStorePath)
	trust.ApprovalStore = hostApprovalStore(opts.ApprovalStorePath)
	trust.WorkspaceLockStore = opts.WorkspaceLockStore
	trust.CaptureRuntimeFixture = opts.CaptureRuntimeFixture
	wsURL, err := wsproto.HTTPToWebSocketURL(opts.GatewayURL, "/v1/ws/hosts/"+url.PathEscape(hostID))
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(hostSecret) != "" {
		wsURL += "?host_secret=" + url.QueryEscape(hostSecret)
	}
	tlsConfig, err := gatewayTLSClientConfig(opts)
	if err != nil {
		return 0, err
	}
	conn, err := wsproto.Dial(ctx, wsURL, tlsConfig)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	stopHeartbeat := startHostHeartbeat(ctx, client, opts.GatewayURL, hostID, hostSecret)
	defer stopHeartbeat()
	processed := 0
	for processed < maxJobs {
		var msg wsproto.Message
		if err := conn.ReadJSON(&msg); err != nil {
			return processed, err
		}
		switch msg.Type {
		case wsproto.MessageNoop:
			continue
		case wsproto.MessageError:
			return processed, fmt.Errorf("gateway websocket error: %s", msg.Error)
		case wsproto.MessageJob:
			if msg.Job == nil {
				return processed, fmt.Errorf("gateway websocket job message missing job")
			}
			result, err := trust.RunDevJob(ctx, hostID, identityFingerprint, *msg.Job, time.Now())
			response := wsproto.Message{
				Type:            wsproto.MessageComplete,
				HostID:          hostID,
				JobID:           msg.Job.ID,
				ArtifactContent: result.ArtifactContent,
			}
			if err != nil {
				response.Type = wsproto.MessageFail
				response.Reason = err.Error()
			}
			if result.RuntimeFixtureContent != "" {
				if err := conn.WriteJSON(wsproto.Message{
					Type:            wsproto.MessageArtifact,
					HostID:          hostID,
					JobID:           msg.Job.ID,
					ArtifactContent: result.RuntimeFixtureContent,
				}); err != nil {
					return processed, err
				}
				if err := readWSSAck(conn, wsproto.MessageArtifact, hostID, msg.Job.ID); err != nil {
					return processed, err
				}
			}
			if err := conn.WriteJSON(response); err != nil {
				return processed, err
			}
			if err := readWSSAck(conn, response.Type, hostID, msg.Job.ID); err != nil {
				return processed, err
			}
			processed++
		default:
			return processed, fmt.Errorf("unsupported websocket message type %q", msg.Type)
		}
	}
	return processed, nil
}

func readWSSAck(conn *wsproto.Conn, expectedType, hostID, jobID string) error {
	var ack wsproto.Message
	if err := conn.ReadJSON(&ack); err != nil {
		return err
	}
	if ack.Type == wsproto.MessageError {
		return fmt.Errorf("gateway websocket error: %s", ack.Error)
	}
	if ack.Type != expectedType || ack.HostID != hostID || ack.JobID != jobID {
		return fmt.Errorf("unexpected websocket ack type=%q host=%q job=%q", ack.Type, ack.HostID, ack.JobID)
	}
	return nil
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

func monitorJobCancellation(ctx context.Context, client *http.Client, gatewayURL, jobID string, interval time.Duration, cancel func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			job, err := fetchJob(ctx, client, gatewayURL, jobID)
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

func fetchJoinManifest(ctx context.Context, client *http.Client, manifestURL, trustPin, manifestRootPublicKey string) (model.JoinManifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return model.JoinManifest{}, err
	}
	resp, err := doGatewayRequest(client, req)
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

func selectJoinManifestGatewayURL(ctx context.Context, client *http.Client, manifest model.JoinManifest) string {
	candidates := manifest.GatewayCandidates
	if len(candidates) == 0 {
		return strings.TrimRight(strings.TrimSpace(manifest.GatewayURL), "/")
	}
	fallback := strings.TrimRight(strings.TrimSpace(manifest.GatewayURL), "/")
	for _, candidate := range candidates {
		gatewayURL := strings.TrimRight(strings.TrimSpace(candidate.URL), "/")
		if gatewayURL == "" {
			continue
		}
		if joinManifestGatewayReachable(ctx, client, gatewayURL, manifest.TrustFingerprint) {
			return gatewayURL
		}
	}
	return fallback
}

func joinManifestGatewayReachable(ctx context.Context, client *http.Client, gatewayURL, trustPin string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := fetchTrustBundle(probeCtx, client, gatewayURL, trustPin)
	return err == nil
}

type hostTrust struct {
	Legacy                *model.TrustBundle
	SignedBundle          *model.SignedTrustBundle
	NonceStore            hostnonce.Store
	ApprovalStore         hostapproval.Store
	WorkspaceLockStore    string
	CaptureRuntimeFixture bool
}

func (t hostTrust) RunDevJob(ctx context.Context, hostID, identityFingerprint string, job model.Job, now time.Time) (hostrunner.Result, error) {
	opts := hostrunner.Options{
		IdentityFingerprint:   identityFingerprint,
		NonceStore:            t.NonceStore,
		ApprovalStore:         t.ApprovalStore,
		WorkspaceLockStore:    t.WorkspaceLockStore,
		CaptureRuntimeFixture: t.CaptureRuntimeFixture,
	}
	if t.SignedBundle != nil {
		return hostrunner.RunDevJobWithTrustBundleOptionsContext(ctx, hostID, *t.SignedBundle, job, now, opts)
	}
	if t.Legacy != nil {
		return hostrunner.RunDevJobWithOptionsContext(ctx, hostID, *t.Legacy, job, now, opts)
	}
	return hostrunner.Result{}, fmt.Errorf("host trust is not configured")
}

func fetchHostTrust(ctx context.Context, client *http.Client, gatewayURL, trustPin, trustStorePath string) (hostTrust, error) {
	store, err := hosttrust.OpenStore(trustStorePath)
	if err != nil {
		return hostTrust{}, err
	}
	signed, err := fetchSignedTrustBundle(ctx, client, gatewayURL, trustPin)
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
	legacy, legacyErr := fetchTrustBundle(ctx, client, gatewayURL, trustPin)
	if legacyErr != nil {
		return hostTrust{}, fmt.Errorf("fetch signed trust bundle failed: %v; fallback legacy trust failed: %w", err, legacyErr)
	}
	return hostTrust{Legacy: &legacy}, nil
}

func refreshHostTrustUpdate(ctx context.Context, client *http.Client, gatewayURL, hostID, trustStorePath string, current hostTrust) (hostTrust, error) {
	store, err := hosttrust.OpenStore(trustStorePath)
	if err != nil {
		return hostTrust{}, err
	}
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
	update, err := fetchTrustBundleUpdate(ctx, client, gatewayURL, hostID, stored.Sequence, hash)
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

func fetchSignedTrustBundle(ctx context.Context, client *http.Client, gatewayURL, trustPin string) (model.SignedTrustBundle, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(gatewayURL, "/")+"/v1/trust-bundle", nil)
	if err != nil {
		return model.SignedTrustBundle{}, err
	}
	resp, err := doGatewayRequest(client, req)
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

func fetchEnrollmentRevocations(ctx context.Context, gatewayURL, rootPublicKey, operatorTokenFile string) (model.HostEnrollmentRevocationList, model.TrustBundle, error) {
	return fetchEnrollmentRevocationsWithClient(ctx, http.DefaultClient, gatewayURL, rootPublicKey, operatorTokenFile)
}

func fetchEnrollmentRevocationsWithClient(ctx context.Context, client *http.Client, gatewayURL, rootPublicKey, operatorTokenFile string) (model.HostEnrollmentRevocationList, model.TrustBundle, error) {
	root, err := parseRootPublicKey(rootPublicKey)
	if err != nil {
		return model.HostEnrollmentRevocationList{}, model.TrustBundle{}, err
	}
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(gatewayURL, "/")+"/v1/enrollment/revocations", nil)
	if err != nil {
		return model.HostEnrollmentRevocationList{}, model.TrustBundle{}, err
	}
	if operatorTokenFile != "" {
		token, err := readTokenFile(operatorTokenFile)
		if err != nil {
			return model.HostEnrollmentRevocationList{}, model.TrustBundle{}, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := doGatewayRequest(client, req)
	if err != nil {
		return model.HostEnrollmentRevocationList{}, model.TrustBundle{}, err
	}
	defer resp.Body.Close()
	var payload struct {
		Revocations model.HostEnrollmentRevocationList `json:"revocations"`
		Error       string                             `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return model.HostEnrollmentRevocationList{}, model.TrustBundle{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return model.HostEnrollmentRevocationList{}, model.TrustBundle{}, fmt.Errorf("fetch enrollment revocations failed: %s", payload.Error)
	}
	if err := model.VerifyHostEnrollmentRevocationListSignature(payload.Revocations, root, time.Now()); err != nil {
		return model.HostEnrollmentRevocationList{}, model.TrustBundle{}, err
	}
	return payload.Revocations, root, nil
}

func issueEnrollmentCertificate(ctx context.Context, gatewayURL string, opts enrollmentIssueCertificateOptions) (issuedEnrollmentCertificatePayload, error) {
	body, err := json.Marshal(map[string]any{
		"ticket_code":          opts.TicketCode,
		"name":                 opts.Name,
		"os":                   opts.OS,
		"arch":                 opts.Arch,
		"capabilities":         opts.Capabilities,
		"identity_key_id":      opts.IdentityKeyID,
		"identity_public_key":  opts.IdentityPublicKey,
		"identity_fingerprint": opts.IdentityFingerprint,
		"valid_minutes":        opts.ValidMinutes,
	})
	if err != nil {
		return issuedEnrollmentCertificatePayload{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(gatewayURL, "/")+"/v1/enrollment/certificates", bytes.NewReader(body))
	if err != nil {
		return issuedEnrollmentCertificatePayload{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if opts.OperatorTokenFile != "" {
		token, err := readTokenFile(opts.OperatorTokenFile)
		if err != nil {
			return issuedEnrollmentCertificatePayload{}, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return issuedEnrollmentCertificatePayload{}, err
	}
	defer resp.Body.Close()
	var payload issuedEnrollmentCertificatePayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return issuedEnrollmentCertificatePayload{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return issuedEnrollmentCertificatePayload{}, fmt.Errorf("issue enrollment certificate failed: %s", payload.Error)
	}
	return payload, nil
}

func renewEnrollmentCertificate(ctx context.Context, client *http.Client, gatewayURL string, certificate model.HostEnrollmentCertificate, opts enrollmentRenewCertificateOptions) (renewedEnrollmentCertificatePayload, error) {
	body, err := json.Marshal(map[string]any{
		"certificate":   certificate,
		"valid_minutes": opts.ValidMinutes,
	})
	if err != nil {
		return renewedEnrollmentCertificatePayload{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(gatewayURL, "/")+"/v1/enrollment/certificates/renew", bytes.NewReader(body))
	if err != nil {
		return renewedEnrollmentCertificatePayload{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if opts.OperatorTokenFile != "" {
		token, err := readTokenFile(opts.OperatorTokenFile)
		if err != nil {
			return renewedEnrollmentCertificatePayload{}, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := doGatewayRequest(client, req)
	if err != nil {
		return renewedEnrollmentCertificatePayload{}, err
	}
	defer resp.Body.Close()
	var payload renewedEnrollmentCertificatePayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return renewedEnrollmentCertificatePayload{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return renewedEnrollmentCertificatePayload{}, fmt.Errorf("renew enrollment certificate failed: %s", payload.Error)
	}
	return payload, nil
}

func readTokenFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(content))
	if token == "" {
		return "", fmt.Errorf("token file %s is empty", path)
	}
	if strings.ContainsAny(token, "\r\n\t ") {
		return "", fmt.Errorf("token file %s must contain a single bearer token", path)
	}
	return token, nil
}

func fetchTrustBundleUpdate(ctx context.Context, client *http.Client, gatewayURL, hostID string, currentSequence int, currentHash string) (model.TrustBundleUpdate, error) {
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
	resp, err := doGatewayRequest(client, req)
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

func fetchTrustBundle(ctx context.Context, client *http.Client, gatewayURL, trustPin string) (model.TrustBundle, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(gatewayURL, "/")+"/v1/trust", nil)
	if err != nil {
		return model.TrustBundle{}, err
	}
	resp, err := doGatewayRequest(client, req)
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

func fetchNextJob(ctx context.Context, client *http.Client, gatewayURL, hostID, hostSecret string, wait time.Duration) (model.Job, bool, error) {
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
	if hostSecret != "" {
		req.Header.Set("Authorization", "Bearer "+hostSecret)
	}
	resp, err := doGatewayRequest(client, req)
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

func waitForHostActive(ctx context.Context, client *http.Client, gatewayURL, hostID string, timeout, interval time.Duration) (model.Host, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if interval <= 0 {
		interval = time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		host, err := fetchHost(ctx, client, gatewayURL, hostID)
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

func fetchHost(ctx context.Context, client *http.Client, gatewayURL, hostID string) (model.Host, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(gatewayURL, "/")+"/v1/hosts/"+hostID, nil)
	if err != nil {
		return model.Host{}, err
	}
	resp, err := doGatewayRequest(client, req)
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
	job, err := fetchJob(ctx, nil, gatewayURL, jobID)
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

func fetchJob(ctx context.Context, client *http.Client, gatewayURL, jobID string) (model.Job, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(gatewayURL, "/")+"/v1/jobs/"+url.PathEscape(jobID), nil)
	if err != nil {
		return model.Job{}, err
	}
	resp, err := doGatewayRequest(client, req)
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

func completeJob(ctx context.Context, client *http.Client, gatewayURL, hostID, hostSecret, jobID, artifactContent string) (model.Job, error) {
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
	if hostSecret != "" {
		req.Header.Set("Authorization", "Bearer "+hostSecret)
	}
	resp, err := doGatewayRequest(client, req)
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

func failJob(ctx context.Context, client *http.Client, gatewayURL, hostID, hostSecret, jobID, reason, artifactContent string) (model.Job, error) {
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
	if hostSecret != "" {
		req.Header.Set("Authorization", "Bearer "+hostSecret)
	}
	resp, err := doGatewayRequest(client, req)
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

func appendJobArtifact(ctx context.Context, client *http.Client, gatewayURL, hostID, hostSecret, jobID, artifactContent string) (model.Job, error) {
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
	if hostSecret != "" {
		req.Header.Set("Authorization", "Bearer "+hostSecret)
	}
	resp, err := doGatewayRequest(client, req)
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

func splitCommaList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
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

func readOptionalTokenFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(content)), nil
}

func writeJSON(out io.Writer, value any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

// extractPort parses the port from an addr like "0.0.0.0:8787" or
// "[::]:8787".  Returns fallback when no port can be parsed.
func extractPort(addr, fallback string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil && port != "" {
		return port
	}
	return fallback
}

func shouldStartManagedPublicTunnel(needsPublicTunnel bool, explicitGatewayURL string) bool {
	return needsPublicTunnel && strings.TrimSpace(explicitGatewayURL) == ""
}

func firstStableGatewayURL(candidates []supportsession.GatewayURLCandidate) string {
	for _, candidate := range candidates {
		switch strings.TrimSpace(candidate.Kind) {
		case "hosted", "relay", "mesh", "vpn", "ssh", "cloudflared", "cloudflared-named":
			if url := strings.TrimRight(strings.TrimSpace(candidate.URL), "/"); url != "" {
				return url
			}
		}
	}
	return ""
}

func writeJSONFile0600(path string, value any) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("ready file path is required")
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return atomicWriteFile0600(path, data)
}

func writeTextFile0600(path, text string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("text file path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return atomicWriteFile0600(path, []byte(text))
}

func atomicWriteFile0600(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return os.Chmod(path, 0o600)
}

func writeSupportSessionHandoffTextFile0600(path string, started map[string]any) error {
	envelope, _ := started["target_handoff_envelope"].(map[string]any)
	text, _ := envelope["full_text"].(string)
	if strings.TrimSpace(text) == "" {
		if handoff, ok := started["user_handoff"].(map[string]any); ok {
			message, _ := handoff["message"].(string)
			copyPaste, _ := handoff["copy_paste"].(string)
			text = strings.TrimSpace(message)
			if strings.TrimSpace(copyPaste) != "" {
				if text != "" {
					text += "\n\n"
				}
				text += strings.TrimSpace(copyPaste)
			}
		}
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("support session handoff text is empty")
	}
	return writeTextFile0600(path, text)
}

func writeSupportSessionConnectedReportFile0600(path string, status map[string]any) error {
	next, _ := status["connected_next_steps"].(map[string]any)
	text, _ := next["user_report"].(string)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return writeTextFile0600(path, text)
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

func connectionEntryChecksPassed(checks []connectionentry.Check) bool {
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
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		_, _ = fmt.Fprintf(os.Stderr, "rdev: %v\n", err)
		os.Exit(1)
	}
}
