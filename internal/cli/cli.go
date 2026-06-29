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
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/audit"
	"github.com/EitanWong/remote-dev-skillkit/internal/buildinfo"
	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostcap"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostrunner"
	"github.com/EitanWong/remote-dev-skillkit/internal/httpapi"
	"github.com/EitanWong/remote-dev-skillkit/internal/mcpstdio"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
	"github.com/EitanWong/remote-dev-skillkit/internal/release"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
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
		pollInterval := fs.Duration("poll-interval", time.Second, "job polling interval when --once=false")
		maxJobs := fs.Int("max-jobs", 1, "maximum jobs to process when --once=false")
		approvalTimeout := fs.Duration("approval-timeout", 30*time.Second, "maximum time to wait for host approval when --once=false")
		trustPin := fs.String("trust-pin", "", "optional gateway signing public key pin, formatted sha256:<hex>")
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
			PollInterval:          *pollInterval,
			MaxJobs:               *maxJobs,
			ApprovalTimeout:       *approvalTimeout,
			TrustPin:              *trustPin,
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
	PollInterval          time.Duration
	MaxJobs               int
	ApprovalTimeout       time.Duration
	TrustPin              string
	ManifestRootPublicKey string
}

func (a App) hostServe(ctx context.Context, opts hostServeOptions) error {
	switch opts.Mode {
	case "temporary", "managed", "break-glass":
	default:
		return fmt.Errorf("unsupported host mode %q", opts.Mode)
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
	inventory := hostcap.Detect(ctx)
	if opts.Name != "" {
		inventory.Name = opts.Name
	}
	host, err := registerHost(ctx, opts.GatewayURL, model.HostRegistration{
		TicketCode:   opts.TicketCode,
		Name:         inventory.Name,
		OS:           inventory.OS,
		Arch:         inventory.Arch,
		Capabilities: inventory.TemporaryCapabilities,
	})
	if err != nil {
		return err
	}

	payload := map[string]any{
		"mode":      opts.Mode,
		"gateway":   opts.GatewayURL,
		"host":      host,
		"inventory": inventory,
		"status":    "registered-pending-approval",
		"note":      "local development registration only; WSS transport is not implemented yet",
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if opts.Once {
		return enc.Encode(payload)
	}
	if _, err := waitForHostActive(ctx, opts.GatewayURL, host.ID, opts.ApprovalTimeout, opts.PollInterval); err != nil {
		return err
	}
	processed, err := a.pollAndRunDevJobs(ctx, opts, host.ID)
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

func (a App) printUsage() {
	_, _ = fmt.Fprintln(a.Stdout, strings.TrimSpace(`rdev - remote development skillkit

Usage:
  rdev version
  rdev doctor
  rdev ticket create --mode attended-temporary --ttl-seconds 7200
  rdev policy explain --mode attended-temporary --capability shell.user
  rdev demo local
  rdev mcp tools
  rdev mcp serve
  rdev gateway serve --dev --addr 127.0.0.1:8787
  rdev release sign --artifact ./rdev-host.exe --key .rdev/keys/release-root.json
  rdev release verify --artifact ./rdev-host.exe --manifest ./rdev-host.exe.rdev-release.json --root-public-key release-root:...
  rdev host serve --mode temporary --gateway http://127.0.0.1:8787 --ticket-code ABCD-1234
`))
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

func (a App) pollAndRunDevJobs(ctx context.Context, opts hostServeOptions, hostID string) (int, error) {
	maxJobs := opts.MaxJobs
	if maxJobs <= 0 {
		maxJobs = 1
	}
	interval := opts.PollInterval
	if interval <= 0 {
		interval = time.Second
	}
	trust, err := fetchTrustBundle(ctx, opts.GatewayURL, opts.TrustPin)
	if err != nil {
		return 0, err
	}
	processed := 0
	for processed < maxJobs {
		job, found, err := fetchNextJob(ctx, opts.GatewayURL, hostID)
		if err != nil {
			return processed, err
		}
		if !found {
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return processed, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		result, err := hostrunner.RunDevJob(hostID, trust, job, time.Now())
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

func encodeRootPublicKey(keyID string, publicKey ed25519.PublicKey) string {
	return trustref.Encode(keyID, publicKey)
}

func parseRootPublicKey(value string) (model.TrustBundle, error) {
	return trustref.Parse(value)
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

func fetchNextJob(ctx context.Context, gatewayURL, hostID string) (model.Job, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(gatewayURL, "/")+"/v1/hosts/"+hostID+"/jobs/next", nil)
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
