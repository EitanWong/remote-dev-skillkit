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
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

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
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
	"github.com/EitanWong/remote-dev-skillkit/internal/skillkit"
	"github.com/EitanWong/remote-dev-skillkit/internal/trustref"
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
		return a.host(args[1:])
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
	case "help", "-h", "--help":
		a.printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
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

func (a App) host(args []string) error {
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
		manifestRootPublicKey := fs.String("manifest-root-public-key", "", "optional join manifest trust root, formatted key_id:base64url_public_key")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.hostServe(context.Background(), hostServeOptions{
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
			ManifestRootPublicKey: *manifestRootPublicKey,
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
	ManifestRootPublicKey string
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
	default:
		return fmt.Errorf("unknown skillkit subcommand %q", args[0])
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
  rdev release sign --artifact ./rdev-host.exe --key .rdev/keys/release-root.json
  rdev release verify --artifact ./rdev-host.exe --manifest ./rdev-host.exe.rdev-release.json --root-public-key release-root:...
  rdev host serve --mode temporary --gateway http://127.0.0.1:8787 --ticket-code ABCD-1234
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
		result, err := trust.RunDevJob(hostID, identityFingerprint, job, time.Now())
		if err != nil {
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
	Legacy        *model.TrustBundle
	SignedBundle  *model.SignedTrustBundle
	NonceStore    hostnonce.Store
	ApprovalStore hostapproval.Store
}

func (t hostTrust) RunDevJob(hostID, identityFingerprint string, job model.Job, now time.Time) (hostrunner.Result, error) {
	opts := hostrunner.Options{
		IdentityFingerprint: identityFingerprint,
		NonceStore:          t.NonceStore,
		ApprovalStore:       t.ApprovalStore,
	}
	if t.SignedBundle != nil {
		return hostrunner.RunDevJobWithTrustBundleOptions(hostID, *t.SignedBundle, job, now, opts)
	}
	if t.Legacy != nil {
		return hostrunner.RunDevJobWithOptions(hostID, *t.Legacy, job, now, opts)
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

func Main() {
	app := NewApp(os.Stdout, os.Stderr)
	if err := app.Run(context.Background(), os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "rdev: %v\n", err)
		os.Exit(1)
	}
}
