package hostcmd

import (
	"bytes"
	"context"
	"crypto/rand"
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
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/buildinfo"
	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostawake"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostcap"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostidentity"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostrunner"
	"github.com/EitanWong/remote-dev-skillkit/internal/hosttrust"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/release"
	"github.com/EitanWong/remote-dev-skillkit/internal/trustref"
)

type App struct {
	Stdout io.Writer
	Stderr io.Writer
}

func New(stdout, stderr io.Writer) App {
	return App{Stdout: stdout, Stderr: stderr}
}

func (a App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		a.printUsage()
		return nil
	}
	if args[0] == "host" {
		return a.Run(ctx, args[1:])
	}
	if args[0] == "version" {
		_, err := fmt.Fprintf(a.Stdout, "rdev-host %s\n", buildinfo.Version)
		return err
	}
	if args[0] == "serve" {
		return a.serve(ctx, args[1:])
	}
	if strings.HasPrefix(args[0], "-") {
		return a.serve(ctx, args)
	}
	return fmt.Errorf("unknown rdev-host subcommand %q; this host helper supports serve only", args[0])
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "-h" || arg == "--help"
}

func (a App) printUsage() {
	_, _ = fmt.Fprintln(a.Stdout, strings.TrimSpace(`rdev-host - lightweight target-side Remote Dev Skillkit host helper

Usage:
  rdev-host serve --gateway http://127.0.0.1:8787 --ticket-code ABCD-1234
  rdev-host --gateway http://127.0.0.1:8787 --ticket-code ABCD-1234

This binary intentionally exposes only the host connector path. Use full rdev
for operator CLI, MCP, gateway, acceptance, and managed-service authoring.`))
}

type serveOptions struct {
	Mode                       string
	GatewayURL                 string
	TicketCode                 string
	ManifestURL                string
	Name                       string
	Once                       bool
	Transport                  string
	PollInterval               time.Duration
	LongPollTimeout            time.Duration
	MaxTasks                   int
	TrustPin                   string
	GatewayCACertPath          string
	GatewayClientCertPath      string
	GatewayClientKeyPath       string
	TrustStorePath             string
	IdentityStorePath          string
	IdentityKeyID              string
	EnrollmentCertificatePath  string
	FetchEnrollmentRevocations bool
	RenewEnrollmentCertificate bool
	OperatorTokenFile          string
	WorkspaceLockStore         string
	CaptureRuntimeFixture      bool
	KeepAwake                  bool
	ManifestRootPublicKey      string
	ReleaseBundlePath          string
	ReleaseRootPublicKey       string
	ReleaseRequiredArtifacts   []string
	ManifestGatewayCandidates  []model.JoinManifestGatewayCandidate
	CapabilityCeiling          []string
	CapabilityCeilingSet       bool
}

func (a App) serve(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rdev-host serve", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	mode := fs.String("mode", "temporary", "host mode: temporary, managed, or break-glass")
	gateway := fs.String("gateway", "", "gateway URL; required with --ticket-code unless --manifest-url is used")
	ticketCode := fs.String("ticket-code", "", "one-time ticket code for local dev registration")
	manifestURL := fs.String("manifest-url", "", "signed join manifest URL")
	name := fs.String("name", "", "host display name; defaults to detected hostname")
	once := fs.Bool("once", true, "join once and exit after printing status")
	transport := fs.String("transport", "poll", "session event transport: auto, poll, or long-poll")
	pollInterval := fs.Duration("poll-interval", time.Second, "session event polling interval when --once=false")
	longPollTimeout := fs.Duration("long-poll-timeout", 25*time.Second, "long-poll wait duration when --transport=long-poll")
	maxTasks := fs.Int("max-tasks", 1, "maximum session tasks to process when --once=false; 0 = unlimited")
	trustPin := fs.String("trust-pin", "", "optional gateway signing public key pin, formatted sha256:<hex>")
	gatewayCA := fs.String("gateway-ca", "", "optional PEM CA bundle for the gateway HTTPS certificate")
	gatewayClientCert := fs.String("gateway-client-cert", "", "optional PEM client certificate for gateway mTLS")
	gatewayClientKey := fs.String("gateway-client-key", "", "optional PEM client private key for gateway mTLS")
	trustStore := fs.String("trust-store", "", "optional local signed trust bundle store path for managed hosts")
	identityStore := fs.String("identity-store", "", "optional local host identity key store path")
	identityKeyID := fs.String("identity-key-id", hostidentity.DefaultKeyID, "host identity key id")
	enrollmentCertificate := fs.String("enrollment-certificate", "", "optional host enrollment certificate JSON path")
	fetchEnrollmentRevocations := fs.Bool("fetch-enrollment-revocations", false, "fetch and verify signed enrollment revocations before registration")
	renewEnrollmentCertificate := fs.Bool("renew-enrollment-certificate", false, "renew the enrollment certificate before registration")
	operatorTokenFile := fs.String("operator-token-file", "", "file containing an operator auth bearer token")
	_ = fs.Duration("enrollment-renew-before", 24*time.Hour, "accepted for full rdev parity; renewal is handled by full rdev")
	_ = fs.Int("enrollment-renew-valid-minutes", 60, "accepted for full rdev parity; renewal is handled by full rdev")
	workspaceLockStore := fs.String("workspace-lock-store", "", "optional local workspace lock store directory")
	captureRuntimeFixture := fs.Bool("capture-runtime-fixture", false, "append an adapter runtime fixture artifact")
	keepAwake := fs.Bool("keep-awake", true, "best-effort prevention of idle sleep/display sleep while host serve is running")
	manifestRootPublicKey := fs.String("manifest-root-public-key", "", "optional join manifest trust root, formatted key_id:base64url_public_key")
	releaseBundle := fs.String("release-bundle", "", "optional signed release bundle index to verify before host registration")
	releaseRootPublicKey := fs.String("release-root-public-key", "", "required release root public key for --release-bundle")
	releaseRequiredArtifacts := fs.String("release-require-artifacts", "", "comma-separated artifact ids required in --release-bundle")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return a.runServe(ctx, serveOptions{
		Mode:                       *mode,
		GatewayURL:                 *gateway,
		TicketCode:                 *ticketCode,
		ManifestURL:                *manifestURL,
		Name:                       *name,
		Once:                       *once,
		Transport:                  *transport,
		PollInterval:               *pollInterval,
		LongPollTimeout:            *longPollTimeout,
		MaxTasks:                   *maxTasks,
		TrustPin:                   *trustPin,
		GatewayCACertPath:          *gatewayCA,
		GatewayClientCertPath:      *gatewayClientCert,
		GatewayClientKeyPath:       *gatewayClientKey,
		TrustStorePath:             *trustStore,
		IdentityStorePath:          *identityStore,
		IdentityKeyID:              *identityKeyID,
		EnrollmentCertificatePath:  *enrollmentCertificate,
		FetchEnrollmentRevocations: *fetchEnrollmentRevocations,
		RenewEnrollmentCertificate: *renewEnrollmentCertificate,
		OperatorTokenFile:          *operatorTokenFile,
		WorkspaceLockStore:         *workspaceLockStore,
		CaptureRuntimeFixture:      *captureRuntimeFixture,
		KeepAwake:                  *keepAwake,
		ManifestRootPublicKey:      *manifestRootPublicKey,
		ReleaseBundlePath:          *releaseBundle,
		ReleaseRootPublicKey:       *releaseRootPublicKey,
		ReleaseRequiredArtifacts:   splitCommaList(*releaseRequiredArtifacts),
	})
}

func (a App) runServe(ctx context.Context, opts serveOptions) error {
	switch opts.Mode {
	case "temporary", "managed", "break-glass":
	default:
		return fmt.Errorf("unsupported host mode %q", opts.Mode)
	}
	if opts.FetchEnrollmentRevocations || opts.RenewEnrollmentCertificate || strings.TrimSpace(opts.OperatorTokenFile) != "" {
		return fmt.Errorf("rdev-host lightweight helper does not perform enrollment renewal/revocation operations; use full rdev for managed enrollment maintenance")
	}
	if opts.Transport == "" {
		opts.Transport = "poll"
	}
	switch opts.Transport {
	case "auto", "poll", "long-poll":
	default:
		return fmt.Errorf("unsupported host transport %q", opts.Transport)
	}
	gatewayClient, err := gatewayHTTPClient(opts)
	if err != nil {
		return err
	}
	releaseGate, err := verifyReleaseGate(opts)
	if err != nil {
		return err
	}
	if opts.ManifestURL != "" {
		if !safeRouteURL(strings.TrimSpace(opts.ManifestURL)) {
			return fmt.Errorf("join manifest URL must use HTTPS or an explicit loopback development endpoint")
		}
		configuredGateway := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
		manifestCtx, cancelManifest := context.WithTimeout(ctx, 30*time.Second)
		manifest, err := fetchJoinManifest(manifestCtx, gatewayClient, opts.ManifestURL, opts.TrustPin, opts.ManifestRootPublicKey)
		cancelManifest()
		if err != nil {
			return fmt.Errorf("join manifest verification failed")
		}
		if configuredGateway != "" && !joinManifestContainsGateway(manifest, configuredGateway) {
			return fmt.Errorf("configured gateway is not present in the verified join manifest")
		}
		opts.ManifestGatewayCandidates = manifest.GatewayCandidates
		if strings.TrimSpace(opts.GatewayURL) == "" {
			opts.GatewayURL = manifest.GatewayURL
		}
		opts.TicketCode = manifest.TicketCode
		opts.TrustPin = manifest.TrustFingerprint
		opts.CapabilityCeiling = append([]string(nil), manifest.Capabilities...)
		opts.CapabilityCeilingSet = true
	}
	if opts.TicketCode == "" {
		_, err := fmt.Fprintf(a.Stdout, "rdev-host foreground placeholder\nmode=%s\ngateway=%s\nstatus=not-connected\nnote=provide --gateway and --ticket-code, or --manifest-url for a signed join manifest\n", opts.Mode, opts.GatewayURL)
		return err
	}
	if strings.TrimSpace(opts.GatewayURL) == "" {
		return fmt.Errorf("gateway is required when --ticket-code is provided")
	}
	manifestRootVerified := opts.ManifestURL != "" && strings.TrimSpace(opts.ManifestRootPublicKey) != ""
	if !isLocalDevGatewayURL(opts.GatewayURL) && !isSignedManifestGatewayURL(opts.GatewayURL, manifestRootVerified) {
		return fmt.Errorf("non-local gateway registration requires --manifest-url with --manifest-root-public-key and an HTTPS or private/LAN gateway URL")
	}
	routes := newGatewayCandidateSet(opts.GatewayURL, opts.ManifestGatewayCandidates, opts.Transport)
	if err := validateGatewayCandidateSet(routes, manifestRootVerified); err != nil {
		return err
	}
	selectedRoute, err := routes.initialize(ctx, gatewayClient, opts.TrustPin)
	if err != nil {
		return err
	}
	opts.GatewayURL = selectedRoute.URL
	opts.Transport = selectedRoute.Transport
	identity, identityCreated, err := hostidentity.LoadOrCreate(opts.IdentityStorePath, opts.IdentityKeyID)
	if err != nil {
		return err
	}
	inventory := hostcap.Detect(ctx)
	if opts.Name != "" {
		inventory.Name = opts.Name
	}
	endpointSpec := controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Name:                inventory.Name,
		Platform:            inventory.OS + "/" + inventory.Arch,
		IdentityFingerprint: identity.Fingerprint(),
		Capabilities:        ConstrainCapabilities(RegistrationCapabilities(inventory), opts.CapabilityCeiling, opts.CapabilityCeilingSet),
		Transport:           controlplane.TransportLongPoll,
		LeaseTTLMS:          60_000,
		RenewAfterMS:        20_000,
		RetryAfterMS:        1_000,
	}
	if opts.Transport == "poll" {
		endpointSpec.Transport = controlplane.TransportPoll
	}
	var enrollmentCertificateSummary *model.HostEnrollmentCertificate
	if opts.EnrollmentCertificatePath != "" {
		certificate, err := readEnrollmentCertificateFile(opts.EnrollmentCertificatePath)
		if err != nil {
			return err
		}
		enrollmentCertificateSummary = &certificate
	}
	joinCtx, cancelJoin := context.WithTimeout(ctx, 30*time.Second)
	session, endpoint, lease, initialEvents, err := joinSessionByCode(joinCtx, gatewayClient, opts.GatewayURL, opts.TicketCode, endpointSpec)
	cancelJoin()
	if err != nil {
		return err
	}
	remoteControlEntry := buildRemoteControlEntry(opts.GatewayURL, opts.TicketCode, endpoint)
	payload := map[string]any{
		"mode":      opts.Mode,
		"gateway":   opts.GatewayURL,
		"session":   session,
		"endpoint":  endpoint,
		"lease":     lease,
		"inventory": inventory,
		"identity": map[string]any{
			"key_id":             identity.KeyID,
			"fingerprint":        identity.Fingerprint(),
			"created":            identityCreated,
			"stored":             opts.IdentityStorePath != "",
			"registration_proof": false,
		},
		"remote_control_entry": remoteControlEntry,
		"status":               "session-joined",
		"transport":            opts.Transport,
		"note":                 "joined Control Plane v1 session; task transport starts when --once=false",
	}
	if opts.ManifestURL != "" {
		payload["manifest_url"] = opts.ManifestURL
		payload["manifest_gateway_selection"] = map[string]any{"selected_gateway_url": opts.GatewayURL, "source": "signed-join-manifest-candidates"}
	}
	if enrollmentCertificateSummary != nil {
		payload["enrollment_certificate"] = map[string]any{
			"schema":        enrollmentCertificateSummary.SchemaVersion,
			"issuer_key_id": enrollmentCertificateSummary.IssuerKeyID,
			"not_after":     enrollmentCertificateSummary.NotAfter,
		}
	}
	if releaseGate != nil {
		payload["release_gate"] = releaseGate
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if opts.Once {
		return enc.Encode(payload)
	}
	writeRemoteControlCard(a.Stderr, remoteControlEntry)
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
	processed, err := a.runSessionTasksWithEvents(ctx, opts, gatewayClient, session.ID, endpoint.ID, identity.Fingerprint(), lease.Secret, lease, routes, initialEvents)
	if err != nil {
		return err
	}
	payload["processed_tasks"] = processed
	payload["status"] = "polling-complete"
	return enc.Encode(payload)
}

type releaseGateResult struct {
	OK                bool      `json:"ok"`
	Schema            string    `json:"schema"`
	Bundle            string    `json:"bundle"`
	RootKeyID         string    `json:"root_key_id"`
	RequiredArtifacts []string  `json:"required_artifacts,omitempty"`
	VerifiedAt        time.Time `json:"verified_at"`
	ArtifactCount     int       `json:"artifact_count"`
}

func verifyReleaseGate(opts serveOptions) (*releaseGateResult, error) {
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
	return &releaseGateResult{
		OK:                true,
		Schema:            verification.SchemaVersion,
		Bundle:            verification.BundlePath,
		RootKeyID:         verification.RootKeyID,
		RequiredArtifacts: append([]string(nil), opts.ReleaseRequiredArtifacts...),
		VerifiedAt:        verification.GeneratedAt,
		ArtifactCount:     len(verification.Artifacts),
	}, nil
}

func writeRemoteControlCard(out io.Writer, entry map[string]any) {
	deviceID, _ := entry["support_device_id"].(string)
	passcode, _ := entry["session_passcode"].(string)
	_, _ = fmt.Fprintln(out, "[rdev] Remote control connector is ready.")
	if strings.TrimSpace(deviceID) != "" {
		_, _ = fmt.Fprintf(out, "[rdev] Device ID: %s\n", deviceID)
	}
	if strings.TrimSpace(passcode) != "" {
		_, _ = fmt.Fprintf(out, "[rdev] Session Password: %s\n", passcode)
	}
	_, _ = fmt.Fprintln(out, "[rdev] Keep this visible connector open. The Agent must not disconnect it unless the operator explicitly asks.")
}

func buildRemoteControlEntry(gatewayURL, ticketCode string, endpoint controlplane.Endpoint) map[string]any {
	ticketCode = strings.TrimSpace(ticketCode)
	gatewayURL = strings.TrimRight(strings.TrimSpace(gatewayURL), "/")
	deviceID, deviceIDSource := remoteControlDeviceID(ticketCode, endpoint)
	passcode := strings.ToUpper(ticketCode)
	endpointCounts := map[string]int{
		"online": 0,
		"total":  0,
	}
	if strings.TrimSpace(endpoint.ID) != "" {
		endpointCounts["total"] = 1
		if endpoint.State == controlplane.EndpointStateOnline {
			endpointCounts["online"] = 1
		}
	}
	entry := map[string]any{
		"schema_version":               "rdev.support-session-remote-control-entry.v1",
		"product_model":                "remote-control-style support device entry for AI Agents",
		"entry_name":                   "Support Device Entry",
		"support_device_id":            deviceID,
		"support_device_id_source":     deviceIDSource,
		"session_passcode":             passcode,
		"session_passcode_kind":        "ticket-scoped session passcode",
		"session_passcode_rotates":     true,
		"gateway_url":                  gatewayURL,
		"ephemeral_gateway":            strings.Contains(strings.ToLower(gatewayURL), ".trycloudflare.com"),
		"stable_gateway_required_for":  []string{"same address across Agent sessions", "managed service reconnect", "owned recurring host"},
		"connector_persistence":        "visible-attended-connector-with-persistent-host-identity",
		"explicit_disconnect_required": true,
		"agent_rule":                   "Treat this like a remote-control app entry: use support_device_id plus session_passcode/status/report fields, keep the connector online after work, and disconnect/revoke/stop only after an explicit operator request.",
		"human_rule":                   "The target-side person opens the visible connector and keeps it open; closing the connector or revoking the ticket ends access.",
		"temporary_support_policy":     "Temporary customer support remains visible and attended but is not auto-disconnected when the Agent finishes a task.",
		"managed_upgrade_policy":       "For an operator-owned recurring machine, ask for explicit managed-service authorization and require a stable gateway before installing service persistence.",
		"forbidden": []string{
			"Agent-initiated disconnect after task completion",
			"hidden install",
			"unauthorized service persistence",
			"long-lived shared host password",
		},
		"endpoint_count": endpointCounts,
	}
	if ticketCode != "" {
		entry["ticket_code"] = ticketCode
	}
	if endpoint.State == controlplane.EndpointStateOnline && strings.TrimSpace(endpoint.ID) != "" {
		entry["recommended_session_endpoint_id"] = endpoint.ID
	}
	return entry
}

func remoteControlDeviceID(ticketCode string, endpoint controlplane.Endpoint) (string, string) {
	if value := strings.TrimSpace(endpoint.IdentityFingerprint); value != "" {
		return remoteControlHumanCode("RDEV", value), "endpoint_identity_fingerprint"
	}
	if value := strings.TrimSpace(endpoint.ID); value != "" {
		return remoteControlHumanCode("RDEV", value), "endpoint_id"
	}
	if ticketCode = strings.TrimSpace(ticketCode); ticketCode != "" {
		return "RDEV-" + strings.ToUpper(strings.ReplaceAll(ticketCode, "_", "-")), "connection_entry_ticket"
	}
	return "RDEV-PENDING", "pending-target-connector"
}

func remoteControlHumanCode(prefix, seed string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(seed)))
	value := strings.ToUpper(hex.EncodeToString(sum[:]))[:12]
	return prefix + "-" + value[0:4] + "-" + value[4:8] + "-" + value[8:12]
}

func gatewayHTTPClient(opts serveOptions) (*http.Client, error) {
	tlsConfig, err := gatewayTLSClientConfig(opts)
	if err != nil {
		return nil, err
	}
	var base http.RoundTripper = http.DefaultTransport
	if tlsConfig != nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = tlsConfig
		base = transport
	}
	return &http.Client{Transport: retryingRoundTripper{Base: base, MaxRetries: 3}, CheckRedirect: rejectGatewayRedirect}, nil
}

func gatewayTLSClientConfig(opts serveOptions) (*tls.Config, error) {
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
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") &&
		(parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "::1" || parsed.Hostname() == "localhost") && parsed.Port() != ""
}

func isSignedManifestGatewayURL(value string, manifestRootVerified bool) bool {
	if !manifestRootVerified {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	return isLocalDevGatewayURL(value)
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
	if !requestCanBeRetried(req) {
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
		attemptReq, err := requestForAttempt(req, attempt)
		if err != nil {
			return nil, err
		}
		resp, err := base.RoundTrip(attemptReq)
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

func requestCanBeRetried(req *http.Request) bool {
	if req.Method == http.MethodGet || req.Method == http.MethodHead {
		return true
	}
	return req.Method == http.MethodPost &&
		strings.TrimSpace(req.Header.Get("Idempotency-Key")) != "" &&
		req.GetBody != nil
}

func requestForAttempt(req *http.Request, attempt int) (*http.Request, error) {
	if attempt == 0 || req.GetBody == nil {
		return req, nil
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	next := req.Clone(req.Context())
	next.Body = body
	return next, nil
}

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

func doGatewayRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	if client == nil {
		client = &http.Client{Transport: retryingRoundTripper{Base: http.DefaultTransport, MaxRetries: 3}, CheckRedirect: rejectGatewayRedirect}
	} else {
		guarded := *client
		guarded.CheckRedirect = rejectGatewayRedirect
		client = &guarded
	}
	return client.Do(req)
}

func rejectGatewayRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

func opaqueGatewayRouteError(operation string) error {
	return fmt.Errorf("gateway route %s", operation)
}

func newIdempotencyKey(prefix string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return prefix + "-" + hex.EncodeToString(raw[:])
}

func joinSessionByCode(ctx context.Context, client *http.Client, gatewayURL, joinCode string, endpoint controlplane.EndpointSpec) (controlplane.Session, controlplane.Endpoint, controlplane.Lease, []controlplane.Event, error) {
	body, err := json.Marshal(map[string]any{
		"join_code": joinCode,
		"endpoint":  endpoint,
	})
	if err != nil {
		return controlplane.Session{}, controlplane.Endpoint{}, controlplane.Lease{}, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(gatewayURL, "/")+"/v1/session-joins", bytes.NewReader(body))
	if err != nil {
		return controlplane.Session{}, controlplane.Endpoint{}, controlplane.Lease{}, nil, opaqueGatewayRouteError("registration request is invalid")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", newIdempotencyKey("session-join"))
	resp, err := doGatewayRequest(client, req)
	if err != nil {
		return controlplane.Session{}, controlplane.Endpoint{}, controlplane.Lease{}, nil, opaqueGatewayRouteError("registration failed")
	}
	defer resp.Body.Close()
	body, err = readBoundedGatewayBody(resp.Body, 256*1024)
	if err != nil {
		return controlplane.Session{}, controlplane.Endpoint{}, controlplane.Lease{}, nil, opaqueGatewayRouteError("registration response failed")
	}
	var payload struct {
		Session  controlplane.Session  `json:"session"`
		Endpoint controlplane.Endpoint `json:"endpoint"`
		Lease    controlplane.Lease    `json:"lease"`
		Events   []controlplane.Event  `json:"events"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return controlplane.Session{}, controlplane.Endpoint{}, controlplane.Lease{}, nil, NewJoinSessionResponseError(resp.StatusCode, resp.Status, body, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return controlplane.Session{}, controlplane.Endpoint{}, controlplane.Lease{}, nil, NewJoinSessionResponseError(resp.StatusCode, resp.Status, body, nil)
	}
	if payload.Session.ID == "" || payload.Endpoint.ID == "" || payload.Lease.Secret == "" {
		return controlplane.Session{}, controlplane.Endpoint{}, controlplane.Lease{}, nil, fmt.Errorf("join session failed: incomplete session join response")
	}
	if err := validateLeaseBinding(controlplane.Lease{}, payload.Lease, payload.Session.ID, payload.Endpoint.ID); err != nil {
		return controlplane.Session{}, controlplane.Endpoint{}, controlplane.Lease{}, nil, err
	}
	return payload.Session, payload.Endpoint, payload.Lease, payload.Events, nil
}

func (a App) runSessionTasks(ctx context.Context, opts serveOptions, client *http.Client, sessionID, endpointID, identityFingerprint, leaseSecret string, lease controlplane.Lease, providedRoutes ...*gatewayCandidateSet) (int, error) {
	var routes *gatewayCandidateSet
	if len(providedRoutes) > 0 {
		routes = providedRoutes[0]
	}
	return a.runSessionTasksWithEvents(ctx, opts, client, sessionID, endpointID, identityFingerprint, leaseSecret, lease, routes, nil)
}

func (a App) runSessionTasksWithEvents(ctx context.Context, opts serveOptions, client *http.Client, sessionID, endpointID, identityFingerprint, leaseSecret string, lease controlplane.Lease, providedRoutes *gatewayCandidateSet, initialEvents []controlplane.Event) (int, error) {
	maxTasks := opts.MaxTasks
	switch {
	case maxTasks == 0:
		maxTasks = math.MaxInt
	case maxTasks < 0:
		maxTasks = 1
	}
	interval := opts.PollInterval
	if interval <= 0 {
		interval = time.Second
	}
	routes := newGatewayCandidateSet(opts.GatewayURL, opts.ManifestGatewayCandidates, opts.Transport)
	if providedRoutes != nil {
		routes = providedRoutes
	}
	selectedRoute, err := routes.initialize(ctx, client, opts.TrustPin)
	if err != nil {
		return 0, err
	}
	opts.GatewayURL = selectedRoute.URL
	opts.Transport = selectedRoute.Transport
	for {
		snapshot, ok := routes.currentSnapshot()
		if !ok {
			return 0, errNoHealthyRoutes
		}
		opts.GatewayURL = snapshot.Candidate.URL
		opts.Transport = snapshot.Candidate.Transport
		trustCtx, cancelTrust := routeRequestContext(ctx, snapshot, 2*time.Second)
		_, trustErr := fetchHostTrust(trustCtx, client, opts.GatewayURL, opts.TrustPin, opts.TrustStorePath)
		cancelTrust()
		if trustErr == nil {
			if routes.reportSuccess(snapshot) {
				break
			}
			continue
		} else if !routes.reportFailure(ctx, snapshot) {
			return 0, opaqueGatewayRouteError("trust verification failed")
		}
	}
	monitorCtx, stopMonitor := context.WithCancel(ctx)
	monitorErrors := make(chan error, 1)
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		monitorErrors <- routes.monitor(monitorCtx, time.Second)
	}()
	defer func() {
		stopMonitor()
		<-monitorDone
	}()
	processed := 0
	afterSeq := uint64(0)
	currentLease := lease
	pendingInitialEvents := append([]controlplane.Event(nil), initialEvents...)
	if lease.RetryAfterMS > 0 {
		interval = time.Duration(lease.RetryAfterMS) * time.Millisecond
	}
pollLoop:
	for processed < maxTasks {
		select {
		case monitorErr := <-monitorErrors:
			if monitorErr != nil && !errors.Is(monitorErr, context.Canceled) {
				return processed, monitorErr
			}
		default:
		}
		snapshot, ok := routes.currentSnapshot()
		if !ok {
			if _, err := routes.probe(ctx); err != nil {
				return processed, err
			}
			snapshot, ok = routes.currentSnapshot()
			if !ok {
				if err := sleepOrDone(ctx, interval); err != nil {
					return processed, err
				}
				continue
			}
		}
		opts.GatewayURL = snapshot.Candidate.URL
		opts.Transport = snapshot.Candidate.Transport
		var events []controlplane.Event
		var nextLease controlplane.Lease
		var replay controlplane.EventReplayState
		var err error
		eventLimit := sessionEventLimit(opts.Transport)
		eventsFromInitial := len(pendingInitialEvents) > 0
		if eventsFromInitial {
			events = append([]controlplane.Event(nil), pendingInitialEvents...)
			pendingInitialEvents = nil
		} else {
			requestCtx, cancelRequest := routeRequestContext(ctx, snapshot, sessionRequestTimeout(opts))
			events, nextLease, replay, err = fetchSessionEvents(requestCtx, client, opts.GatewayURL, sessionID, endpointID, leaseSecret, afterSeq, eventLimit, sessionLongPollWait(opts))
			cancelRequest()
		}
		if err != nil {
			if isTransientGatewayResponseError(err) {
				if routes.reportFailure(ctx, snapshot) {
					continue pollLoop
				}
				_, _ = fmt.Fprintln(a.Stderr, "rdev-host: transient gateway response while polling session events; retrying after backoff")
				if err := sleepOrDone(ctx, interval); err != nil {
					return processed, err
				}
				continue
			}
			return processed, err
		}
		if !eventsFromInitial {
			if nextLease.Secret == "" {
				if routes.reportFailure(ctx, snapshot) {
					continue pollLoop
				}
				return processed, opaqueGatewayRouteError("returned an incomplete lease")
			}
			if err := validateLeaseBinding(currentLease, nextLease, sessionID, endpointID); err != nil {
				if routes.reportFailure(ctx, snapshot) {
					continue pollLoop
				}
				return processed, err
			}
		}
		if !routes.reportSuccess(snapshot) {
			if eventsFromInitial {
				pendingInitialEvents = append([]controlplane.Event(nil), events...)
			}
			continue pollLoop
		}
		if nextLease.Secret != "" {
			leaseSecret = nextLease.Secret
			currentLease = nextLease
			if nextLease.RetryAfterMS > 0 {
				interval = time.Duration(nextLease.RetryAfterMS) * time.Millisecond
			}
		}
		if replay.SnapshotRequired {
			return processed, fmt.Errorf("session event cursor is stale; restart host session to refresh snapshot")
		}
		foundTask := false
		for eventIndex, event := range events {
			if event.Seq <= afterSeq {
				continue
			}
			if event.Type != controlplane.EventTypeTask || event.TaskID == "" {
				afterSeq = event.Seq
				continue
			}
			action, _ := event.Payload["action"].(string)
			if action != "offer" {
				afterSeq = event.Seq
				continue
			}
			taskSnapshot, ok := routes.currentSnapshot()
			if !ok {
				pendingInitialEvents = append([]controlplane.Event(nil), events[eventIndex:]...)
				continue pollLoop
			}
			opts.GatewayURL = taskSnapshot.Candidate.URL
			opts.Transport = taskSnapshot.Candidate.Transport
			requestCtx, cancelRequest := routeRequestContext(ctx, taskSnapshot, sessionRequestTimeout(opts))
			task, err := fetchSessionTask(requestCtx, client, opts.GatewayURL, sessionID, event.TaskID)
			cancelRequest()
			if err != nil {
				if isTransientGatewayResponseError(err) && routes.reportFailure(ctx, taskSnapshot) {
					pendingInitialEvents = append([]controlplane.Event(nil), events[eventIndex:]...)
					continue pollLoop
				}
				return processed, err
			}
			if !routes.reportSuccess(taskSnapshot) {
				pendingInitialEvents = append([]controlplane.Event(nil), events[eventIndex:]...)
				continue pollLoop
			}
			if task.TargetEndpointID != endpointID || task.Terminal() {
				afterSeq = event.Seq
				continue
			}
			foundTask = true
			if err := a.runSessionTaskWithRoutes(ctx, opts, client, sessionID, endpointID, identityFingerprint, leaseSecret, task, routes); err != nil {
				return processed, err
			}
			afterSeq = event.Seq
			processed++
			if processed >= maxTasks {
				return processed, nil
			}
		}
		if len(events) < eventLimit && replay.LastSeq > afterSeq {
			afterSeq = replay.LastSeq
		}
		if !foundTask {
			if err := sleepOrDone(ctx, interval); err != nil {
				return processed, err
			}
		}
	}
	return processed, nil
}

func (a App) runSessionTask(ctx context.Context, opts serveOptions, client *http.Client, sessionID, endpointID, identityFingerprint, leaseSecret string, task controlplane.Task) error {
	return a.runSessionTaskWithRoutes(ctx, opts, client, sessionID, endpointID, identityFingerprint, leaseSecret, task, newGatewayCandidateSet(opts.GatewayURL, opts.ManifestGatewayCandidates))
}

func (a App) runSessionTaskWithRoutes(ctx context.Context, opts serveOptions, client *http.Client, sessionID, endpointID, identityFingerprint, leaseSecret string, task controlplane.Task, routes *gatewayCandidateSet) error {
	result := hostrunner.Result{}
	var err error
	if !CapabilitiesAllowed(task.Capabilities, opts.CapabilityCeiling, opts.CapabilityCeilingSet) {
		err = fmt.Errorf("task capabilities exceed the signed join manifest ceiling")
	} else {
		result, err = hostrunner.RunSessionTaskWithOptionsContext(ctx, sessionTaskSpec(task, endpointID, identityFingerprint), time.Now(), hostrunner.Options{
			IdentityFingerprint:   identityFingerprint,
			WorkspaceLockStore:    opts.WorkspaceLockStore,
			CaptureRuntimeFixture: opts.CaptureRuntimeFixture,
		})
	}
	status := string(controlplane.TaskStatusSucceeded)
	reason := ""
	if err != nil {
		status = string(controlplane.TaskStatusFailed)
		reason = err.Error()
	}
	payload := map[string]any{
		"status":           status,
		"attempt_id":       task.AttemptID,
		"idempotency_key":  newIdempotencyKey("task-result"),
		"artifact_content": result.ArtifactContent,
	}
	if reason != "" {
		payload["reason"] = reason
	}
	if result.RuntimeFixtureContent != "" {
		payload["runtime_fixture_content"] = result.RuntimeFixtureContent
	}
	for {
		snapshot, pooled := routes.currentSnapshot()
		if pooled {
			opts.GatewayURL = snapshot.Candidate.URL
			opts.Transport = snapshot.Candidate.Transport
		} else {
			if routes.pool != nil {
				return errNoHealthyRoutes
			}
			opts.GatewayURL = routes.current()
			opts.Transport = routes.currentTransport()
		}
		requestCtx := ctx
		cancelRequest := func() {}
		if pooled {
			requestCtx, cancelRequest = routeRequestContext(ctx, snapshot, sessionRequestTimeout(opts))
		}
		_, _, completeErr := completeSessionTask(requestCtx, client, opts.GatewayURL, sessionID, task.ID, leaseSecret, payload)
		cancelRequest()
		if completeErr == nil {
			if pooled {
				_ = routes.reportSuccess(snapshot)
			}
			return nil
		} else if !isTransientGatewayResponseError(completeErr) || (pooled && !routes.reportFailure(ctx, snapshot)) || (!pooled && !routes.rotate(ctx, client, opts.TrustPin)) {
			if err != nil {
				return fmt.Errorf("%v; additionally failed to report session task failure: %w", err, completeErr)
			}
			return completeErr
		}
	}
}

func sessionTaskSpec(task controlplane.Task, endpointID, identityFingerprint string) hostrunner.SessionTaskSpec {
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
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
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

func intValueFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

func fetchSessionEvents(ctx context.Context, client *http.Client, gatewayURL, sessionID, endpointID, leaseSecret string, afterSeq uint64, limit int, wait ...time.Duration) ([]controlplane.Event, controlplane.Lease, controlplane.EventReplayState, error) {
	values := url.Values{}
	values.Set("endpoint_id", endpointID)
	values.Set("after_seq", strconv.FormatUint(afterSeq, 10))
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	if len(wait) > 0 && wait[0] > 0 {
		waitMS := wait[0].Milliseconds()
		if waitMS > 60_000 {
			waitMS = 60_000
		}
		values.Set("wait_ms", strconv.FormatInt(waitMS, 10))
	}
	endpoint := strings.TrimRight(gatewayURL, "/") + "/v1/sessions/" + url.PathEscape(sessionID) + "/events?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, controlplane.Lease{}, controlplane.EventReplayState{}, err
	}
	if leaseSecret != "" {
		req.Header.Set("Authorization", "Bearer "+leaseSecret)
	}
	resp, err := doGatewayRequest(client, req)
	if err != nil {
		return nil, controlplane.Lease{}, controlplane.EventReplayState{}, transientGatewayResponseError{Endpoint: endpoint, Cause: err}
	}
	defer resp.Body.Close()
	body, readErr := readBoundedGatewayBody(resp.Body, 256*1024)
	if readErr != nil {
		return nil, controlplane.Lease{}, controlplane.EventReplayState{}, transientGatewayResponseError{Endpoint: endpoint, Status: resp.Status, Cause: readErr}
	}
	var payload struct {
		Events           []controlplane.Event `json:"events"`
		Lease            controlplane.Lease   `json:"lease"`
		SnapshotRequired bool                 `json:"snapshot_required"`
		SnapshotSeq      uint64               `json:"snapshot_seq"`
		LastSeq          uint64               `json:"last_seq"`
		RetryAfterMS     int                  `json:"retry_after_ms"`
		Reconnecting     bool                 `json:"reconnecting"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil, controlplane.Lease{}, controlplane.EventReplayState{}, transientGatewayResponseError{Endpoint: endpoint, Status: resp.Status, Body: bodyPreview(body), Cause: err}
		}
		return nil, controlplane.Lease{}, controlplane.EventReplayState{}, gatewayResponseError("fetch session events failed", endpoint, resp, body, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, controlplane.Lease{}, controlplane.EventReplayState{}, gatewayResponseError("fetch session events failed", endpoint, resp, body, nil)
	}
	replay := controlplane.EventReplayState{
		SnapshotRequired: payload.SnapshotRequired,
		SnapshotSeq:      payload.SnapshotSeq,
		LastSeq:          payload.LastSeq,
		RetryAfterMS:     payload.RetryAfterMS,
		Reconnecting:     payload.Reconnecting,
	}
	return payload.Events, payload.Lease, replay, nil
}

func fetchSessionTask(ctx context.Context, client *http.Client, gatewayURL, sessionID, taskID string) (controlplane.Task, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(gatewayURL, "/")+"/v1/sessions/"+url.PathEscape(sessionID), nil)
	if err != nil {
		return controlplane.Task{}, err
	}
	resp, err := doGatewayRequest(client, req)
	if err != nil {
		return controlplane.Task{}, transientGatewayResponseError{Endpoint: req.URL.String(), Cause: err}
	}
	defer resp.Body.Close()
	body, err := readBoundedGatewayBody(resp.Body, 256*1024)
	if err != nil {
		return controlplane.Task{}, transientGatewayResponseError{Endpoint: req.URL.String(), Status: resp.Status, Cause: err}
	}
	var payload struct {
		Snapshot controlplane.SessionSnapshot `json:"snapshot"`
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return controlplane.Task{}, gatewayResponseError("fetch session task failed", req.URL.String(), resp, body, nil)
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return controlplane.Task{}, gatewayResponseError("fetch session task failed", req.URL.String(), resp, body, err)
	}
	for _, task := range payload.Snapshot.Tasks {
		if task.ID == taskID {
			return task, nil
		}
	}
	return controlplane.Task{}, fmt.Errorf("fetch session task failed: task %s not found", taskID)
}

func completeSessionTask(ctx context.Context, client *http.Client, gatewayURL, sessionID, taskID, leaseSecret string, result map[string]any) (controlplane.Task, controlplane.Event, error) {
	body, err := json.Marshal(result)
	if err != nil {
		return controlplane.Task{}, controlplane.Event{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(gatewayURL, "/")+"/v1/sessions/"+url.PathEscape(sessionID)+"/tasks/"+url.PathEscape(taskID)+"/result", bytes.NewReader(body))
	if err != nil {
		return controlplane.Task{}, controlplane.Event{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	idempotencyKey, _ := result["idempotency_key"].(string)
	if strings.TrimSpace(idempotencyKey) == "" {
		idempotencyKey = newIdempotencyKey("task-result")
	}
	req.Header.Set("Idempotency-Key", idempotencyKey)
	if leaseSecret != "" {
		req.Header.Set("Authorization", "Bearer "+leaseSecret)
	}
	resp, err := doGatewayRequest(client, req)
	if err != nil {
		return controlplane.Task{}, controlplane.Event{}, transientGatewayResponseError{Endpoint: req.URL.String(), Cause: err}
	}
	defer resp.Body.Close()
	responseBody, err := readBoundedGatewayBody(resp.Body, 256*1024)
	if err != nil {
		return controlplane.Task{}, controlplane.Event{}, transientGatewayResponseError{Endpoint: req.URL.String(), Status: resp.Status, Cause: err}
	}
	var payload struct {
		Task  controlplane.Task  `json:"task"`
		Event controlplane.Event `json:"event"`
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return controlplane.Task{}, controlplane.Event{}, gatewayResponseError("complete session task failed", req.URL.String(), resp, responseBody, nil)
	}
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return controlplane.Task{}, controlplane.Event{}, gatewayResponseError("complete session task failed", req.URL.String(), resp, responseBody, err)
	}
	return payload.Task, payload.Event, nil
}

func sessionEventLimit(transport string) int {
	if transport == "long-poll" {
		return 16
	}
	return 64
}

func validateLeaseBinding(current, next controlplane.Lease, sessionID, endpointID string) error {
	if next.Secret == "" {
		return nil
	}
	if next.SessionID != sessionID || next.EndpointID != endpointID {
		return fmt.Errorf("gateway lease binding does not match the registered session")
	}
	if next.Generation <= 0 || (current.Generation > 0 && next.Generation <= current.Generation) {
		return fmt.Errorf("gateway lease generation did not advance")
	}
	return nil
}

func sessionRequestTimeout(opts serveOptions) time.Duration {
	if opts.Transport == "long-poll" {
		return sessionLongPollWait(opts) + 5*time.Second
	}
	return 30 * time.Second
}

func sessionLongPollWait(opts serveOptions) time.Duration {
	if opts.Transport != "long-poll" {
		return 0
	}
	wait := opts.LongPollTimeout
	if wait <= 0 {
		wait = 25 * time.Second
	}
	if wait > 60*time.Second {
		wait = 60 * time.Second
	}
	return wait
}

func sleepOrDone(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Second
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func fetchJoinManifest(ctx context.Context, client *http.Client, manifestURL, trustPin, manifestRootPublicKey string) (model.JoinManifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return model.JoinManifest{}, fmt.Errorf("join manifest request is invalid")
	}
	resp, err := doGatewayRequest(client, req)
	if err != nil {
		return model.JoinManifest{}, fmt.Errorf("join manifest request failed")
	}
	defer resp.Body.Close()
	var payload struct {
		Manifest         model.JoinManifest     `json:"manifest"`
		GatewayTimeProof model.GatewayTimeProof `json:"gateway_time_proof"`
		Error            string                 `json:"error"`
	}
	if err := decodeBoundedGatewayJSON(resp.Body, 256*1024, &payload); err != nil {
		return model.JoinManifest{}, err
	}
	if len(payload.Manifest.GatewayCandidates) > maxManifestCandidates {
		return model.JoinManifest{}, fmt.Errorf("join manifest contains too many gateway candidates")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return model.JoinManifest{}, fmt.Errorf("fetch join manifest failed: %s", payload.Error)
	}
	verifyTime := responseGatewayTime(resp)
	if verifyTime.IsZero() {
		verifyTime = time.Now()
	}
	if manifestRootPublicKey != "" {
		root, err := parseRootPublicKey(manifestRootPublicKey)
		if err != nil {
			return model.JoinManifest{}, err
		}
		if payload.GatewayTimeProof.SchemaVersion != "" {
			verifyTime, err = payload.GatewayTimeProof.Verify(root, model.GatewayTimeProofPurposeJoinManifest, payload.Manifest)
			if err != nil {
				return model.JoinManifest{}, fmt.Errorf("verify gateway time proof: %w", err)
			}
		}
		if err := payload.Manifest.VerifyWithRoot(root, verifyTime); err != nil {
			return model.JoinManifest{}, err
		}
	} else {
		if payload.GatewayTimeProof.SchemaVersion != "" {
			var err error
			verifyTime, err = payload.GatewayTimeProof.Verify(payload.Manifest.Trust, model.GatewayTimeProofPurposeJoinManifest, payload.Manifest)
			if err != nil {
				return model.JoinManifest{}, fmt.Errorf("verify gateway time proof: %w", err)
			}
		}
		if err := payload.Manifest.Verify(verifyTime); err != nil {
			return model.JoinManifest{}, err
		}
	}
	if err := payload.Manifest.Trust.VerifyPin(trustPin); err != nil {
		return model.JoinManifest{}, err
	}
	if !safeRouteURL(payload.Manifest.GatewayURL) {
		return model.JoinManifest{}, fmt.Errorf("join manifest contains an invalid gateway URL")
	}
	for _, candidate := range payload.Manifest.GatewayCandidates {
		if !safeRouteURL(candidate.URL) {
			return model.JoinManifest{}, fmt.Errorf("join manifest contains an invalid gateway candidate")
		}
	}
	return payload.Manifest, nil
}

func responseGatewayTime(resp *http.Response) time.Time {
	if resp == nil {
		return time.Time{}
	}
	value := strings.TrimSpace(resp.Header.Get("Date"))
	if value == "" {
		return time.Time{}
	}
	parsed, err := http.ParseTime(value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func selectJoinManifestGatewayURL(ctx context.Context, client *http.Client, manifest model.JoinManifest) string {
	fallback := strings.TrimRight(strings.TrimSpace(manifest.GatewayURL), "/")
	for _, candidate := range manifest.GatewayCandidates {
		gatewayURL := strings.TrimRight(strings.TrimSpace(candidate.URL), "/")
		if gatewayURL != "" && joinManifestGatewayReachable(ctx, client, gatewayURL, manifest.TrustFingerprint) {
			return gatewayURL
		}
	}
	return fallback
}

func joinManifestContainsGateway(manifest model.JoinManifest, value string) bool {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" || !safeRouteURL(value) {
		return false
	}
	if strings.TrimRight(strings.TrimSpace(manifest.GatewayURL), "/") == value {
		return true
	}
	for _, candidate := range manifest.GatewayCandidates {
		if strings.TrimRight(strings.TrimSpace(candidate.URL), "/") == value {
			return true
		}
	}
	return false
}

type gatewayCandidateSet struct {
	routes    []routeCandidate
	pool      *routePool
	transport string
}

func newGatewayCandidateSet(current string, candidates []model.JoinManifestGatewayCandidate, transport ...string) *gatewayCandidateSet {
	selectedTransport := "poll"
	if len(transport) > 0 && strings.TrimSpace(transport[0]) != "" {
		selectedTransport = transport[0]
	}
	return &gatewayCandidateSet{
		routes:    routeCandidatesFromManifest(current, candidates, selectedTransport),
		transport: selectedTransport,
	}
}

func newGatewayCandidateSetWithPool(pool *routePool) *gatewayCandidateSet {
	return &gatewayCandidateSet{pool: pool}
}

func validateGatewayCandidateSet(routes *gatewayCandidateSet, manifestRootVerified bool) error {
	if routes == nil || len(routes.routes) == 0 {
		return errNoHealthyRoutes
	}
	for _, route := range routes.routes {
		if !isLocalDevGatewayURL(route.URL) && !isSignedManifestGatewayURL(route.URL, manifestRootVerified) {
			return fmt.Errorf("gateway route is outside the verified manifest trust boundary")
		}
	}
	return nil
}

func (s *gatewayCandidateSet) ensurePool(client *http.Client, trustPin string) *routePool {
	if s == nil {
		return nil
	}
	if s.pool != nil {
		return s.pool
	}
	probe := func(ctx context.Context, route routeCandidate) routeProbeResult {
		started := time.Now()
		_, err := fetchHostTrust(ctx, client, route.URL, trustPin, "")
		return routeProbeResult{Healthy: err == nil, Latency: time.Since(started), Err: err}
	}
	s.pool = newRoutePool(s.routes, routePoolConfig{Probe: probe, ReprobeInterval: 5 * time.Second, ShareGatewayHealth: true})
	return s.pool
}

func (s *gatewayCandidateSet) current() string {
	if s == nil {
		return ""
	}
	if route, ok := s.poolCurrent(); ok {
		return route.URL
	}
	if len(s.routes) > 0 {
		return s.routes[0].URL
	}
	return ""
}

func (s *gatewayCandidateSet) currentTransport() string {
	if s == nil {
		return "poll"
	}
	if route, ok := s.poolCurrent(); ok {
		return route.Transport
	}
	if len(s.routes) > 0 {
		return s.routes[0].Transport
	}
	return normalizeRouteTransport(s.transport)
}

func (s *gatewayCandidateSet) poolCurrent() (routeCandidate, bool) {
	if s == nil || s.pool == nil {
		return routeCandidate{}, false
	}
	return s.pool.current()
}

func (s *gatewayCandidateSet) currentSnapshot() (routeSnapshot, bool) {
	if s == nil || s.pool == nil {
		return routeSnapshot{}, false
	}
	return s.pool.currentSnapshot()
}

func (s *gatewayCandidateSet) reportSuccess(snapshot routeSnapshot) bool {
	if s == nil || s.pool == nil {
		return false
	}
	return s.pool.reportSnapshotSuccess(snapshot)
}

func (s *gatewayCandidateSet) reportFailure(ctx context.Context, snapshot routeSnapshot) bool {
	if s == nil || s.pool == nil {
		return false
	}
	_, ok := s.pool.reportSnapshotFailure(ctx, snapshot)
	return ok
}

func (s *gatewayCandidateSet) initialize(ctx context.Context, client *http.Client, trustPin string) (routeCandidate, error) {
	pool := s.ensurePool(client, trustPin)
	if pool == nil {
		return routeCandidate{}, errNoHealthyRoutes
	}
	return pool.initialize(ctx)
}

func (s *gatewayCandidateSet) rotate(ctx context.Context, client *http.Client, trustPin string) bool {
	pool := s.ensurePool(client, trustPin)
	if pool == nil {
		return false
	}
	before, beforeOK := pool.current()
	if !beforeOK {
		_, err := pool.initialize(ctx)
		return err == nil
	}
	_, afterOK := pool.reportFailure(ctx, before)
	if !afterOK {
		return false
	}
	after, currentOK := pool.current()
	return currentOK && after != before
}

func (s *gatewayCandidateSet) probe(ctx context.Context) (bool, error) {
	if s == nil || s.pool == nil {
		return false, nil
	}
	return s.pool.probe(ctx)
}

func (s *gatewayCandidateSet) monitor(ctx context.Context, interval time.Duration) error {
	if s == nil || s.pool == nil {
		return errNoHealthyRoutes
	}
	return s.pool.monitor(ctx, interval)
}

func joinManifestGatewayReachable(ctx context.Context, client *http.Client, gatewayURL, trustPin string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := fetchTrustBundle(probeCtx, client, gatewayURL, trustPin)
	return err == nil
}

type hostTrust struct {
	Legacy       *model.TrustBundle
	SignedBundle *model.SignedTrustBundle
}

type signedTrustHTTPError struct {
	status int
}

func (e signedTrustHTTPError) Error() string {
	return fmt.Sprintf("signed trust endpoint returned status class %d", e.status/100)
}

func signedTrustEndpointUnsupported(err error) bool {
	var statusErr signedTrustHTTPError
	return errors.As(err, &statusErr) && statusErr.status == http.StatusNotFound
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
	if !signedTrustEndpointUnsupported(err) {
		return hostTrust{}, fmt.Errorf("signed trust verification failed")
	}
	if stored, ok, storeErr := store.Load(); storeErr != nil {
		return hostTrust{}, storeErr
	} else if ok {
		root, rootErr := activeSigningRoot(stored)
		if rootErr != nil || stored.Verify(root, time.Now()) != nil || root.VerifyPin(trustPin) != nil {
			return hostTrust{}, fmt.Errorf("stored signed trust verification failed")
		}
		return hostTrust{SignedBundle: &stored}, nil
	}
	legacy, legacyErr := fetchTrustBundle(ctx, client, gatewayURL, trustPin)
	if legacyErr != nil {
		return hostTrust{}, fmt.Errorf("legacy trust verification failed")
	}
	return hostTrust{Legacy: &legacy}, nil
}

func activeSigningRoot(bundle model.SignedTrustBundle) (model.TrustBundle, error) {
	key, ok := bundle.Key(bundle.SigningKeyID)
	if !ok {
		return model.TrustBundle{}, fmt.Errorf("signed trust bundle missing signing key %q", bundle.SigningKeyID)
	}
	return key.TrustBundle(), nil
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
		return model.SignedTrustBundle{}, fmt.Errorf("signed trust request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.ReadAll(io.LimitReader(resp.Body, 256*1024))
		return model.SignedTrustBundle{}, signedTrustHTTPError{status: resp.StatusCode}
	}
	var payload struct {
		TrustBundle model.SignedTrustBundle `json:"trust_bundle"`
		Error       string                  `json:"error"`
	}
	if err := decodeBoundedGatewayJSON(resp.Body, 256*1024, &payload); err != nil {
		return model.SignedTrustBundle{}, err
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

func fetchTrustBundle(ctx context.Context, client *http.Client, gatewayURL, trustPin string) (model.TrustBundle, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(gatewayURL, "/")+"/v1/trust", nil)
	if err != nil {
		return model.TrustBundle{}, err
	}
	resp, err := doGatewayRequest(client, req)
	if err != nil {
		return model.TrustBundle{}, fmt.Errorf("legacy trust request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return model.TrustBundle{}, fmt.Errorf("legacy trust endpoint returned status class %d", resp.StatusCode/100)
	}
	var payload struct {
		Trust model.TrustBundle `json:"trust"`
		Error string            `json:"error"`
	}
	if err := decodeBoundedGatewayJSON(resp.Body, 64*1024, &payload); err != nil {
		return model.TrustBundle{}, err
	}
	if _, err := payload.Trust.Ed25519PublicKey(); err != nil {
		return model.TrustBundle{}, err
	}
	if err := payload.Trust.VerifyPin(trustPin); err != nil {
		return model.TrustBundle{}, err
	}
	return payload.Trust, nil
}

type transientGatewayResponseError struct {
	Endpoint string
	Status   string
	Body     string
	Cause    error
}

func gatewayResponseError(operation, endpoint string, resp *http.Response, body []byte, cause error) error {
	if resp != nil && gatewayRouteFailure(resp.StatusCode, body) {
		return transientGatewayResponseError{
			Endpoint: endpoint,
			Status:   resp.Status,
			Cause:    cause,
		}
	}
	return fmt.Errorf("%s: gateway response rejected", operation)
}

func gatewayRouteFailure(statusCode int, body []byte) bool {
	if statusCode == http.StatusRequestTimeout || statusCode == http.StatusTooEarly || statusCode == http.StatusTooManyRequests || statusCode == http.StatusGone {
		return true
	}
	if statusCode >= http.StatusInternalServerError && statusCode <= 599 {
		return true
	}
	if statusCode != http.StatusNotFound {
		return false
	}
	message := strings.ToLower(string(body))
	return strings.Contains(message, "tunnel") || strings.Contains(message, "gateway")
}

func (e transientGatewayResponseError) Error() string {
	return "transient gateway response"
}

func isTransientGatewayResponseError(err error) bool {
	var transient transientGatewayResponseError
	return errors.As(err, &transient)
}

func gatewayErrorMessage(status string, body []byte, cause error) string {
	_ = body
	_ = cause
	message := strings.TrimSpace(status)
	if message == "" {
		message = "gateway response rejected"
	}
	return message
}

func bodyPreview(body []byte) string {
	value := strings.TrimSpace(string(body))
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 240 {
		value = value[:240] + "..."
	}
	return value
}

func decodeBoundedGatewayJSON(body io.Reader, maxBytes int64, destination any) error {
	content, err := readBoundedGatewayBody(body, maxBytes)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(content, destination); err != nil {
		return fmt.Errorf("decode gateway response: %w", err)
	}
	return nil
}

func readBoundedGatewayBody(body io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("gateway response size limit is invalid")
	}
	content, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read gateway response: %w", err)
	}
	if int64(len(content)) > maxBytes {
		return nil, fmt.Errorf("gateway response exceeds the size limit")
	}
	return content, nil
}

func splitCommaList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			values = append(values, part)
		}
	}
	return values
}
