package hostcmd

import (
	"bytes"
	"context"
	"crypto/rand"

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
	if args[0] == "service" {
		return a.service(ctx, args[1:])
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
  rdev-host serve --gateway https://gateway.example --join-code JOIN-CODE
  rdev-host --gateway https://gateway.example --join-code JOIN-CODE
  rdev-host service install --gateway https://gateway.example --join-code JOIN-CODE --state-root PATH

This binary intentionally exposes only the host connector path. Use full rdev
for operator CLI, MCP, gateway, acceptance, and managed-service authoring.`))
}

type serveOptions struct {
	Mode                  string
	GatewayURL            string
	JoinCode              string
	Name                  string
	Once                  bool
	Transport             string
	PollInterval          time.Duration
	LongPollTimeout       time.Duration
	MaxTasks              int
	TrustPin              string
	GatewayCACertPath     string
	GatewayClientCertPath string
	GatewayClientKeyPath  string
	TrustStorePath        string
	IdentityStorePath     string
	IdentityKeyID         string
	WorkspaceLockStore    string
	CaptureRuntimeFixture bool
	KeepAwake             bool
	CapabilityCeiling     []string
	CapabilityCeilingSet  bool
}

func (a App) serve(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rdev-host serve", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	mode := fs.String("mode", "temporary", "host mode: temporary, managed, or break-glass")
	gateway := fs.String("gateway", "", "Control Plane session gateway URL")
	joinCode := fs.String("join-code", "", "Control Plane session join code")
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
	workspaceLockStore := fs.String("workspace-lock-store", "", "optional local workspace lock store directory")
	captureRuntimeFixture := fs.Bool("capture-runtime-fixture", false, "append an adapter runtime fixture artifact")
	keepAwake := fs.Bool("keep-awake", true, "best-effort prevention of idle sleep/display sleep while host serve is running")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return a.runServe(ctx, serveOptions{
		Mode:                  *mode,
		GatewayURL:            *gateway,
		JoinCode:              *joinCode,
		Name:                  *name,
		Once:                  *once,
		Transport:             *transport,
		PollInterval:          *pollInterval,
		LongPollTimeout:       *longPollTimeout,
		MaxTasks:              *maxTasks,
		TrustPin:              *trustPin,
		GatewayCACertPath:     *gatewayCA,
		GatewayClientCertPath: *gatewayClientCert,
		GatewayClientKeyPath:  *gatewayClientKey,
		TrustStorePath:        *trustStore,
		IdentityStorePath:     *identityStore,
		IdentityKeyID:         *identityKeyID,
		WorkspaceLockStore:    *workspaceLockStore,
		CaptureRuntimeFixture: *captureRuntimeFixture,
		KeepAwake:             *keepAwake,
	})
}

func (a App) runServe(ctx context.Context, opts serveOptions) error {
	switch opts.Mode {
	case "temporary", "managed", "break-glass":
	default:
		return fmt.Errorf("unsupported host mode %q", opts.Mode)
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
	if opts.JoinCode == "" {
		_, err := fmt.Fprintf(a.Stdout, "rdev-host foreground placeholder\nmode=%s\ngateway=%s\nstatus=not-connected\nnote=provide --gateway and --join-code for a Control Plane session\n", opts.Mode, opts.GatewayURL)
		return err
	}
	if strings.TrimSpace(opts.GatewayURL) == "" {
		return fmt.Errorf("gateway is required when --join-code is provided")
	}
	if !isSessionGatewayURL(opts.GatewayURL) {
		return fmt.Errorf("session gateway must use HTTPS or an explicit loopback development endpoint")
	}
	routes := newGatewayCandidateSet(opts.GatewayURL, nil, opts.Transport)
	if err := validateSessionGatewayCandidateSet(routes); err != nil {
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
		Capabilities:        RegistrationCapabilities(inventory),
		Transport:           controlplane.TransportLongPoll,
		LeaseTTLMS:          60_000,
		RenewAfterMS:        20_000,
		RetryAfterMS:        1_000,
	}
	if opts.Transport == "poll" {
		endpointSpec.Transport = controlplane.TransportPoll
	}
	joinCtx, cancelJoin := context.WithTimeout(ctx, 30*time.Second)
	session, endpoint, lease, initialEvents, err := joinSessionByCode(joinCtx, gatewayClient, opts.GatewayURL, opts.JoinCode, endpointSpec)
	cancelJoin()
	if err != nil {
		return err
	}
	opts.CapabilityCeiling = append([]string(nil), session.Capabilities...)
	// An empty session ceiling means the operator did not constrain the
	// session; enforcing an empty set would reject every task. Match the
	// gateway-side semantics (limited only when the ceiling is non-empty).
	opts.CapabilityCeilingSet = len(opts.CapabilityCeiling) > 0
	sessionControlEntry := buildSessionControlEntry(session, endpoint)
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
		"session_control_entry": sessionControlEntry,
		"status":                "session-joined",
		"transport":             opts.Transport,
		"note":                  "joined Control Plane v1 session; task transport starts when --once=false",
	}

	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	writeSessionControlCard(a.Stderr, sessionControlEntry)
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
	processed, err := a.runSessionTasksWithEvents(ctx, opts, gatewayClient, session.ID, endpoint.ID, identity.Fingerprint(), lease.Secret, lease, routes, initialEvents)
	if err != nil {
		return err
	}
	payload["processed_tasks"] = processed
	payload["status"] = "polling-complete"
	return enc.Encode(payload)
}

func writeSessionControlCard(out io.Writer, entry map[string]any) {
	_, _ = fmt.Fprintln(out, "[rdev] Control Plane session connector is ready.")
	if endpointID, _ := entry["endpoint_id"].(string); strings.TrimSpace(endpointID) != "" {
		_, _ = fmt.Fprintf(out, "[rdev] Endpoint: %s\n", endpointID)
	}
	_, _ = fmt.Fprintln(out, "[rdev] Keep this visible connector open until the operator closes the session.")
}

func buildSessionControlEntry(session controlplane.Session, endpoint controlplane.Endpoint) map[string]any {
	return map[string]any{
		"schema_version": "rdev.session-control-entry.v1",
		"session_id":     session.ID,
		"profile":        session.Profile,
		"endpoint_id":    endpoint.ID,
		"endpoint_state": endpoint.State,
		"capabilities":   append([]string(nil), session.Capabilities...),
	}
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

func isSessionGatewayURL(value string) bool {
	return safeRouteURL(value)
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
	routes := newGatewayCandidateSet(opts.GatewayURL, nil, opts.Transport)
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
		trustErr := fetchHostTrust(trustCtx, client, opts.GatewayURL, opts.TrustPin, opts.TrustStorePath)
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
			task, err := fetchSessionTask(requestCtx, client, opts.GatewayURL, sessionID, endpointID, leaseSecret, event.TaskID)
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
	return a.runSessionTaskWithRoutes(ctx, opts, client, sessionID, endpointID, identityFingerprint, leaseSecret, task, newGatewayCandidateSet(opts.GatewayURL, nil))
}

func (a App) runSessionTaskWithRoutes(ctx context.Context, opts serveOptions, client *http.Client, sessionID, endpointID, identityFingerprint, leaseSecret string, task controlplane.Task, routes *gatewayCandidateSet) error {
	result := hostrunner.Result{}
	var err error
	if !CapabilitiesAllowed(task.Capabilities, opts.CapabilityCeiling, opts.CapabilityCeilingSet) {
		err = fmt.Errorf("task capabilities exceed the joined session ceiling")
	} else {
		progressReporter := a.engineeringProgressReporter(opts, client, sessionID, endpointID, leaseSecret, task, routes)
		result, err = hostrunner.RunSessionTaskWithOptionsContext(ctx, sessionTaskSpec(task, endpointID, identityFingerprint), time.Now(), hostrunner.Options{
			IdentityFingerprint:           identityFingerprint,
			WorkspaceLockStore:            opts.WorkspaceLockStore,
			CaptureRuntimeFixture:         opts.CaptureRuntimeFixture,
			EngineeringProgressReporter:   progressReporter,
			EngineeringResumeCheckpointID: stringValueFromAny(task.Payload["engineering_resume_checkpoint_id"]),
			EngineeringResumeSourceTaskID: stringValueFromAny(task.Payload["engineering_resume_task_id"]),
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
		_, _, completeErr := completeSessionTask(requestCtx, client, opts.GatewayURL, sessionID, endpointID, leaseSecret, task.ID, payload)
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

func (a App) engineeringProgressReporter(opts serveOptions, client *http.Client, sessionID, endpointID, leaseSecret string, task controlplane.Task, routes *gatewayCandidateSet) hostrunner.EngineeringProgressReporter {
	return func(ctx context.Context, progress hostrunner.EngineeringProgress) error {
		if progress.TaskID != task.ID {
			return fmt.Errorf("engineering progress task id does not match the assigned task")
		}
		event := controlplane.Event{
			Type:           controlplane.EventTypeTaskProgress,
			FromEndpointID: endpointID,
			TaskID:         task.ID,
			IdempotencyKey: "engineering-progress:" + task.AttemptID + ":" + progress.CheckpointID,
			Payload: map[string]any{
				"schema_version": progress.SchemaVersion,
				"phase":          progress.Phase,
				"attempt":        progress.Attempt,
				"summary":        progress.Summary,
				"artifact_refs":  progress.ArtifactRefs,
				"checkpoint_id":  progress.CheckpointID,
				"recoverable":    progress.Recoverable,
				"failure_class":  progress.FailureClass,
				"attempt_id":     progress.AttemptID,
			},
		}
		return a.reportEngineeringProgress(ctx, opts, client, sessionID, leaseSecret, event, routes)
	}
}

func (a App) reportEngineeringProgress(ctx context.Context, opts serveOptions, client *http.Client, sessionID, leaseSecret string, event controlplane.Event, routes *gatewayCandidateSet) error {
	for {
		snapshot, pooled := routes.currentSnapshot()
		gatewayURL := ""
		if pooled {
			gatewayURL = snapshot.Candidate.URL
		} else {
			if routes.pool != nil {
				return errNoHealthyRoutes
			}
			gatewayURL = routes.current()
		}
		requestCtx, cancel := context.WithTimeout(ctx, min(sessionRequestTimeout(opts), 5*time.Second))
		_, err := appendSessionEvent(requestCtx, client, gatewayURL, sessionID, leaseSecret, event)
		cancel()
		if err == nil {
			if pooled {
				_ = routes.reportSuccess(snapshot)
			}
			return nil
		}
		if !isTransientGatewayResponseError(err) || (pooled && !routes.reportFailure(ctx, snapshot)) || (!pooled && !routes.rotate(ctx, client, opts.TrustPin)) {
			return err
		}
	}
}

func sessionTaskSpec(task controlplane.Task, endpointID, identityFingerprint string) hostrunner.SessionTaskSpec {
	payload := cloneStringAnyMap(task.Payload)
	workspaceRoot := stringValueFromAny(payload["workspace_root"])
	writeScope := stringSliceFromAny(payload["write_scope"])
	return hostrunner.SessionTaskSpec{
		TaskID:              task.ID,
		AttemptID:           task.AttemptID,
		EndpointID:          endpointID,
		IdentityFingerprint: identityFingerprint,
		Adapter:             task.Adapter,
		Intent:              task.Intent,
		Workspace: model.TaskWorkspace{
			Root:        workspaceRoot,
			WriteScope:  writeScope,
			Branch:      stringValueFromAny(payload["branch"]),
			BaseSHA:     stringValueFromAny(payload["base_sha"]),
			Isolation:   stringValueFromAny(payload["isolation"]),
			DirtyPolicy: stringValueFromAny(payload["dirty_policy"]),
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

func fetchSessionTask(ctx context.Context, client *http.Client, gatewayURL, sessionID, endpointID, leaseSecret, taskID string) (controlplane.Task, error) {
	values := url.Values{}
	values.Set("endpoint_id", endpointID)
	endpoint := strings.TrimRight(gatewayURL, "/") + "/v1/sessions/" + url.PathEscape(sessionID) + "/tasks/" + url.PathEscape(taskID) + "?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return controlplane.Task{}, err
	}
	if leaseSecret != "" {
		req.Header.Set("Authorization", "Bearer "+leaseSecret)
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
		Task controlplane.Task `json:"task"`
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return controlplane.Task{}, gatewayResponseError("fetch session task failed", req.URL.String(), resp, body, nil)
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return controlplane.Task{}, gatewayResponseError("fetch session task failed", req.URL.String(), resp, body, err)
	}
	if payload.Task.ID != taskID || payload.Task.TargetEndpointID != endpointID {
		return controlplane.Task{}, fmt.Errorf("fetch session task failed: task %s not found", taskID)
	}
	return payload.Task, nil
}

func completeSessionTask(ctx context.Context, client *http.Client, gatewayURL, sessionID, endpointID, leaseSecret, taskID string, result map[string]any) (controlplane.Task, controlplane.Event, error) {
	body, err := json.Marshal(result)
	if err != nil {
		return controlplane.Task{}, controlplane.Event{}, err
	}
	values := url.Values{}
	values.Set("endpoint_id", endpointID)
	endpoint := strings.TrimRight(gatewayURL, "/") + "/v1/sessions/" + url.PathEscape(sessionID) + "/tasks/" + url.PathEscape(taskID) + "/result?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
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

func appendSessionEvent(ctx context.Context, client *http.Client, gatewayURL, sessionID, leaseSecret string, event controlplane.Event) (controlplane.Event, error) {
	body, err := json.Marshal(event)
	if err != nil {
		return controlplane.Event{}, err
	}
	endpoint := strings.TrimRight(gatewayURL, "/") + "/v1/sessions/" + url.PathEscape(sessionID) + "/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return controlplane.Event{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if event.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", event.IdempotencyKey)
	}
	if leaseSecret != "" {
		req.Header.Set("Authorization", "Bearer "+leaseSecret)
	}
	resp, err := doGatewayRequest(client, req)
	if err != nil {
		return controlplane.Event{}, transientGatewayResponseError{Endpoint: endpoint, Cause: err}
	}
	defer resp.Body.Close()
	responseBody, err := readBoundedGatewayBody(resp.Body, 256*1024)
	if err != nil {
		return controlplane.Event{}, transientGatewayResponseError{Endpoint: endpoint, Status: resp.Status, Cause: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return controlplane.Event{}, gatewayResponseError("append session event failed", endpoint, resp, responseBody, nil)
	}
	var payload struct {
		Event controlplane.Event `json:"event"`
	}
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return controlplane.Event{}, gatewayResponseError("append session event failed", endpoint, resp, responseBody, err)
	}
	return payload.Event, nil
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

type gatewayCandidateSet struct {
	routes    []routeCandidate
	pool      *routePool
	transport string
}

func newGatewayCandidateSet(current string, candidates []controlplane.GatewayCandidate, transport ...string) *gatewayCandidateSet {
	selectedTransport := "poll"
	if len(transport) > 0 && strings.TrimSpace(transport[0]) != "" {
		selectedTransport = transport[0]
	}
	return &gatewayCandidateSet{
		routes:    routeCandidatesFromSession(current, candidates, selectedTransport),
		transport: selectedTransport,
	}
}

func newGatewayCandidateSetWithPool(pool *routePool) *gatewayCandidateSet {
	return &gatewayCandidateSet{pool: pool}
}

func validateSessionGatewayCandidateSet(routes *gatewayCandidateSet) error {
	if routes == nil || len(routes.routes) == 0 {
		return errNoHealthyRoutes
	}
	for _, route := range routes.routes {
		if !isSessionGatewayURL(route.URL) {
			return fmt.Errorf("session gateway route is not HTTPS or an explicit loopback development endpoint")
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
		err := fetchHostTrust(ctx, client, route.URL, trustPin, "")
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

func fetchHostTrust(ctx context.Context, client *http.Client, gatewayURL, trustPin, trustStorePath string) error {
	store, err := hosttrust.OpenStore(trustStorePath)
	if err != nil {
		return err
	}
	signed, err := fetchSignedTrustBundle(ctx, client, gatewayURL, trustPin)
	if err != nil {
		return fmt.Errorf("signed trust verification failed")
	}
	if trustStorePath == "" {
		return nil
	}
	root, err := activeSigningRoot(signed)
	if err != nil {
		return err
	}
	if err := store.VerifyAndSaveUpdate(signed, root, time.Now()); err != nil {
		return err
	}
	return nil
}

func activeSigningRoot(bundle model.SignedTrustBundle) (model.TrustBundle, error) {
	key, ok := bundle.Key(bundle.SigningKeyID)
	if !ok {
		return model.TrustBundle{}, fmt.Errorf("signed trust bundle missing signing key %q", bundle.SigningKeyID)
	}
	return key.TrustBundle(), nil
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
		return model.SignedTrustBundle{}, fmt.Errorf("signed trust endpoint returned status class %d", resp.StatusCode/100)
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
