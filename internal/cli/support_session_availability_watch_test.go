package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

func TestForegroundAvailabilityLossRevokesTicketAndInvalidatesHandoff(t *testing.T) {
	runtime, handles := supportSessionAvailabilityRuntime(t, "only")
	gw, store, ticket := publishedSupportSessionForAvailabilityTest(t)
	root := t.TempDir()
	readyFile, handoffFile, statusFile := availabilityTestFiles(t, root, ticket.Code)
	published := runtime.Snapshot()
	var output bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchForegroundSupportSessionAvailability(context.Background(), foregroundSupportSessionOptions{
			Out: &output, StatusFile: statusFile, ReadyFile: readyFile, HandoffTextFile: handoffFile,
			ConnectedReportFile: filepath.Join(root, "connected.txt"), JournalPath: filepath.Join(root, "journal.json"),
			Gateway: gw, Store: store, TicketID: ticket.ID, TicketCode: ticket.Code, Locale: "en",
			GatewayURL: "https://only.example.test", Runtime: runtime, Published: published,
		})
	}()
	handles[0].wait <- errors.New("provider exited")
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("availability watcher did not stop after route loss")
	}
	assertAvailabilityInvalidated(t, gw, ticket, readyFile, handoffFile, statusFile)
	lossLog := ""
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.Contains(line, "[rdev] tunnel availability changed") {
			lossLog = line
		}
	}
	if lossLog == "" || strings.Contains(lossLog, "example.test") || strings.Contains(lossLog, ticket.Code) {
		t.Fatalf("availability loss log leaked URL/ticket or was missing: %q", lossLog)
	}
}

func TestAvailabilityInvalidationCallbackDoesNotExposeArtifactPaths(t *testing.T) {
	runtime, handles := supportSessionAvailabilityRuntime(t, "only")
	gw, store, ticket := publishedSupportSessionForAvailabilityTest(t)
	root := t.TempDir()
	readyFile, handoffFile, _ := availabilityTestFiles(t, root, ticket.Code)
	privateStatusPath := filepath.Join(root, "operator-secret", "status.json")
	callback := make(chan error, 1)
	go watchForegroundSupportSessionAvailability(context.Background(), foregroundSupportSessionOptions{
		Out: &bytes.Buffer{}, StatusFile: privateStatusPath, ReadyFile: readyFile, HandoffTextFile: handoffFile,
		JournalPath: filepath.Join(root, "journal.json"), Gateway: gw, Store: store,
		TicketID: ticket.ID, TicketCode: ticket.Code, Runtime: runtime, Published: runtime.Snapshot(),
		OnInvalidated: func(err error) { callback <- err },
	})
	handles[0].wait <- errors.New("provider exited")
	select {
	case err := <-callback:
		if err == nil || !strings.Contains(err.Error(), "tunnel availability lost before target connection") {
			t.Fatalf("unexpected invalidation callback error: %v", err)
		}
		for _, forbidden := range []string{privateStatusPath, "operator-secret", root} {
			if strings.Contains(err.Error(), forbidden) {
				t.Fatalf("invalidation callback leaked %q: %v", forbidden, err)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("availability invalidation callback did not run")
	}
}

func TestTunnelAvailabilityLogRejectsUnsafeAttemptFields(t *testing.T) {
	var out bytes.Buffer
	logTunnelAvailabilityLoss(&out, tunnel.AvailabilitySet{Attempts: []tunnel.Attempt{{
		ProviderID: "unsafe provider secret", CandidateID: "NOT-A-CANDIDATE", ErrorClass: "secret-attempt-error",
	}}}, "secret-log-error")
	logged := out.String()
	for _, forbidden := range []string{"unsafe provider secret", "NOT-A-CANDIDATE", "secret-attempt-error", "secret-log-error"} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("availability log leaked %q: %q", forbidden, logged)
		}
	}
	if !strings.Contains(logged, `"error_class":"availability-changed"`) {
		t.Fatalf("availability log did not fail closed: %q", logged)
	}
}

func TestTunnelAvailabilityLogIncludesOnlySafeCorrelationFields(t *testing.T) {
	var out bytes.Buffer
	logTunnelAvailabilityLoss(&out, tunnel.AvailabilitySet{Attempts: []tunnel.Attempt{{
		ProviderID: "safe-provider", CandidateID: "0123456789abcdef", ErrorClass: "probe-failed",
	}}}, "tunnel-availability-lost")
	if !strings.Contains(out.String(), `"provider_ids":["safe-provider"]`) ||
		!strings.Contains(out.String(), `"candidate_ids":["0123456789abcdef"]`) ||
		!strings.Contains(out.String(), `"error_class":"tunnel-availability-lost"`) {
		t.Fatalf("safe availability log missing fields: %q", out.String())
	}
	logTunnelAvailabilityLoss(nil, tunnel.AvailabilitySet{}, "ignored")
}

func TestPublicSupportSessionInvalidationErrorUsesFixedText(t *testing.T) {
	for _, detail := range []error{nil, errors.New("private /Users/example/status.json failure")} {
		err := publicSupportSessionInvalidationError("tunnel availability lost before target connection", detail)
		if err == nil || strings.Contains(err.Error(), "/Users/example") || strings.Contains(err.Error(), "private") {
			t.Fatalf("public invalidation error = %v", err)
		}
	}
}

func TestForegroundSecondaryRouteDeathKeepsPrimaryHandoffUntilLastRouteDies(t *testing.T) {
	runtime, handles := supportSessionAvailabilityRuntime(t, "first", "second")
	gw, store, ticket := publishedSupportSessionForAvailabilityTest(t)
	root := t.TempDir()
	readyFile, handoffFile, statusFile := availabilityTestFiles(t, root, ticket.Code)
	published := runtime.Snapshot()
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchForegroundSupportSessionAvailability(context.Background(), foregroundSupportSessionOptions{
			Out: &bytes.Buffer{}, StatusFile: statusFile, ReadyFile: readyFile, HandoffTextFile: handoffFile,
			JournalPath: filepath.Join(root, "journal.json"), Gateway: gw, Store: store,
			TicketID: ticket.ID, TicketCode: ticket.Code, Runtime: runtime, Published: published,
		})
	}()
	handles[1].wait <- errors.New("secondary exited")
	select {
	case <-done:
		t.Fatal("secondary route loss revoked a still-usable primary handoff")
	case <-time.After(150 * time.Millisecond):
	}
	current, ok := gw.TicketForCode(ticket.Code)
	if !ok || current.Status != model.TicketStatusActive {
		t.Fatalf("secondary route loss changed ticket state: %#v, found=%v", current, ok)
	}
	ready, err := os.ReadFile(readyFile)
	if err != nil || bytes.Contains(ready, []byte("tunnel_availability_lost")) {
		t.Fatalf("secondary route loss invalidated ready file: %s err=%v", ready, err)
	}

	handles[0].wait <- errors.New("primary exited")
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("last route loss did not invalidate the handoff")
	}
	assertAvailabilityInvalidated(t, gw, ticket, readyFile, handoffFile, statusFile)
}

func TestForegroundPrimaryRouteDeathInvalidatesWhileSecondaryRemainsLive(t *testing.T) {
	runtime, handles := supportSessionAvailabilityRuntime(t, "primary", "secondary")
	gw, store, ticket := publishedSupportSessionForAvailabilityTest(t)
	root := t.TempDir()
	readyFile, handoffFile, statusFile := availabilityTestFiles(t, root, ticket.Code)
	published := runtime.Snapshot()
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchForegroundSupportSessionAvailability(context.Background(), foregroundSupportSessionOptions{
			Out: &bytes.Buffer{}, StatusFile: statusFile, ReadyFile: readyFile, HandoffTextFile: handoffFile,
			JournalPath: filepath.Join(root, "journal.json"), Gateway: gw, Store: store,
			TicketID: ticket.ID, TicketCode: ticket.Code, Runtime: runtime, Published: published,
		})
	}()

	handles[0].wait <- errors.New("primary exited")
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("primary route loss did not invalidate the handoff while secondary remained live")
	}
	assertAvailabilityInvalidated(t, gw, ticket, readyFile, handoffFile, statusFile)
}

func TestForegroundExplicitGatewayLivenessFailureInvalidatesHandoff(t *testing.T) {
	gw, store, ticket := publishedSupportSessionForAvailabilityTest(t)
	root := t.TempDir()
	readyFile, handoffFile, statusFile := availabilityTestFiles(t, root, ticket.Code)
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchForegroundSupportSessionAvailability(context.Background(), foregroundSupportSessionOptions{
			Out: &bytes.Buffer{}, StatusFile: statusFile, ReadyFile: readyFile, HandoffTextFile: handoffFile,
			JournalPath: filepath.Join(root, "journal.json"), Gateway: gw, Store: store,
			TicketID: ticket.ID, TicketCode: ticket.Code,
			Published: tunnel.AvailabilitySet{
				SchemaVersion: tunnel.AvailabilitySchemaVersion,
				Region:        tunnel.RegionGlobal,
				Candidates:    []tunnel.Candidate{{ProviderID: "explicit", URL: "https://explicit.example.test"}},
			},
			LivenessInterval: 10 * time.Millisecond,
			LivenessProbe: func(context.Context) error {
				return errors.New("explicit gateway unavailable")
			},
		})
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("explicit gateway liveness failure did not invalidate handoff")
	}
	assertAvailabilityInvalidated(t, gw, ticket, readyFile, handoffFile, statusFile)
}

func TestForegroundConnectedHostStopsExplicitGatewayLivenessProbe(t *testing.T) {
	gw, store, ticket := publishedSupportSessionForAvailabilityTest(t)
	if _, err := gw.RegisterHost(model.HostRegistration{TicketCode: ticket.Code, Name: "connected-explicit", OS: "windows", Arch: "amd64"}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	readyFile, handoffFile, statusFile := availabilityTestFiles(t, root, ticket.Code)
	probeCalls := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchForegroundSupportSessionAvailability(context.Background(), foregroundSupportSessionOptions{
			Out: &bytes.Buffer{}, StatusFile: statusFile, ReadyFile: readyFile, HandoffTextFile: handoffFile,
			ConnectedReportFile: filepath.Join(root, "connected.txt"), JournalPath: filepath.Join(root, "journal.json"),
			Gateway: gw, Store: store, TicketID: ticket.ID, TicketCode: ticket.Code,
			Published:        tunnel.AvailabilitySet{SchemaVersion: tunnel.AvailabilitySchemaVersion, Region: tunnel.RegionGlobal},
			LivenessInterval: time.Millisecond,
			LivenessProbe: func(context.Context) error {
				probeCalls++
				return nil
			},
		})
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("connected explicit host did not stop liveness watcher")
	}
	if probeCalls != 0 {
		t.Fatalf("liveness probe ran %d times after host connected", probeCalls)
	}
}

func TestForegroundCancellationRevokesOnlyBeforeConnection(t *testing.T) {
	for _, connected := range []bool{false, true} {
		t.Run(fmt.Sprintf("connected=%t", connected), func(t *testing.T) {
			gw, store, ticket := publishedSupportSessionForAvailabilityTest(t)
			if connected {
				if _, err := gw.RegisterHost(model.HostRegistration{TicketCode: ticket.Code, Name: "connected-host", OS: "windows", Arch: "amd64"}); err != nil {
					t.Fatal(err)
				}
			}
			root := t.TempDir()
			readyFile, handoffFile, statusFile := availabilityTestFiles(t, root, ticket.Code)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				defer close(done)
				watchForegroundSupportSessionAvailability(ctx, foregroundSupportSessionOptions{
					Out: &bytes.Buffer{}, StatusFile: statusFile, ReadyFile: readyFile, HandoffTextFile: handoffFile,
					ConnectedReportFile: filepath.Join(root, "connected.txt"), JournalPath: filepath.Join(root, "journal.json"),
					Gateway: gw, Store: store, TicketID: ticket.ID, TicketCode: ticket.Code,
					Published: tunnel.AvailabilitySet{SchemaVersion: tunnel.AvailabilitySchemaVersion, Region: tunnel.RegionGlobal},
				})
			}()
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("availability watcher did not stop after cancellation")
			}
			current, _ := gw.TicketForCode(ticket.Code)
			if connected && current.Status != model.TicketStatusActive {
				t.Fatalf("cancellation after connection revoked ticket: %#v", current)
			}
			if !connected && current.Status != model.TicketStatusRevoked {
				t.Fatalf("cancellation before connection left ticket active: %#v", current)
			}
		})
	}
}

func TestForegroundConnectedHostStopsAvailabilityWatcher(t *testing.T) {
	runtime, handles := supportSessionAvailabilityRuntime(t, "connected")
	gw, store, ticket := publishedSupportSessionForAvailabilityTest(t)
	if _, err := gw.RegisterHost(model.HostRegistration{TicketCode: ticket.Code, Name: "connected-host", OS: "windows", Arch: "amd64"}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	readyFile, handoffFile, statusFile := availabilityTestFiles(t, root, ticket.Code)
	published := runtime.Snapshot()
	var output bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchForegroundSupportSessionAvailability(context.Background(), foregroundSupportSessionOptions{
			Out: &output, StatusFile: statusFile, ReadyFile: readyFile, HandoffTextFile: handoffFile,
			ConnectedReportFile: filepath.Join(root, "connected.txt"), JournalPath: filepath.Join(root, "journal.json"),
			Gateway: gw, Store: store, TicketID: ticket.ID, TicketCode: ticket.Code,
			Runtime: runtime, Published: published,
		})
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("connected host did not stop availability watcher")
	}
	if !strings.Contains(output.String(), `"event":"connected"`) || strings.Contains(output.String(), `"event":"waiting"`) {
		t.Fatalf("already-connected watcher emitted contradictory lifecycle event: %q", output.String())
	}
	handles[0].wait <- errors.New("exit after connection")
	time.Sleep(50 * time.Millisecond)
	current, _ := gw.TicketForCode(ticket.Code)
	if current.Status != model.TicketStatusActive {
		t.Fatalf("route exit after connection revoked ticket: %#v", current)
	}
}

func TestForegroundSupportStatusUsesBoundTargetEndpoint(t *testing.T) {
	gw, _, ticket := publishedSupportSessionForAvailabilityTest(t)
	_, endpoint, _, _, err := gw.JoinSessionByCode(ticket.Code, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Name:                "endpoint-only-target",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-foreground-endpoint",
		Transport:           controlplane.TransportLongPoll,
	})
	if err != nil {
		t.Fatal(err)
	}
	status := foregroundSupportStatus(foregroundSupportSessionOptions{
		Gateway: gw, TicketID: ticket.ID, TicketCode: ticket.Code, GatewayURL: "https://gateway.example.test",
	})
	if status["connected"] != true || status["session_id"] != ticket.SessionID || status["recommended_target_endpoint_id"] != endpoint.ID {
		t.Fatal("foreground status did not consume the bound target endpoint")
	}
}

func TestForegroundRegistrationBetweenStatusCheckAndRollbackWinsAtomically(t *testing.T) {
	runtime, handles := supportSessionAvailabilityRuntime(t, "racing-connect")
	gw, store, ticket := publishedSupportSessionForAvailabilityTest(t)
	root := t.TempDir()
	readyFile, handoffFile, statusFile := availabilityTestFiles(t, root, ticket.Code)
	published := runtime.Snapshot()
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchForegroundSupportSessionAvailability(context.Background(), foregroundSupportSessionOptions{
			Out: &bytes.Buffer{}, StatusFile: statusFile, ReadyFile: readyFile, HandoffTextFile: handoffFile,
			ConnectedReportFile: filepath.Join(root, "connected.txt"), JournalPath: filepath.Join(root, "journal.json"),
			Gateway: gw, Store: store, TicketID: ticket.ID, TicketCode: ticket.Code, Runtime: runtime, Published: published,
			BeforeInvalidation: func() {
				if _, err := gw.RegisterHost(model.HostRegistration{TicketCode: ticket.Code, Name: "race-winner", OS: "windows", Arch: "amd64"}); err != nil {
					t.Errorf("register between status and rollback: %v", err)
				}
			},
		})
	}()
	handles[0].wait <- errors.New("route exited during registration")
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("race watcher did not terminate")
	}
	current, _ := gw.TicketForCode(ticket.Code)
	if current.Status != model.TicketStatusActive || len(gw.HostsForTicketCode(ticket.Code, string(model.HostStatusActive))) != 1 {
		t.Fatalf("atomic connected check lost registration: ticket=%#v hosts=%#v", current, gw.HostsForTicketCode(ticket.Code, ""))
	}
	ready, _ := os.ReadFile(readyFile)
	if bytes.Contains(ready, []byte("tunnel_availability_lost")) {
		t.Fatalf("connected race invalidated ready file: %s", ready)
	}
}

func TestSupportSessionStartReturnsAndCleansUpWhenPublishedRouteDies(t *testing.T) {
	for _, name := range []string{"RDEV_HOSTED_GATEWAY_URL", "RDEV_CLOUDFLARED_GATEWAY_URL", "RDEV_RELAY_GATEWAY_URL", "RDEV_MESH_GATEWAY_URL", "RDEV_VPN_GATEWAY_URL", "RDEV_SSH_GATEWAY_URL"} {
		t.Setenv(name, "")
	}
	handle := newSupportSessionTestTunnelHandle(tunnel.Candidate{ProviderID: "foreground-loss", URL: "https://foreground-loss.example.test"})
	provider := fixedSupportSessionProvider{id: "foreground-loss", handle: handle}
	registry, err := tunnel.NewRegistry(provider)
	if err != nil {
		t.Fatal(err)
	}
	addr := supportSessionTestAddr(t)
	workDir := filepath.Join(t.TempDir(), "support")
	handoffWritten := make(chan struct{})
	app := NewApp(io.Discard, io.Discard)
	app.supportSessionStartDeps = &supportSessionStartDeps{
		Registry: registry,
		Manager: tunnel.Manager{Probe: func(context.Context, tunnel.Candidate) (tunnel.ProbeEvidence, error) {
			return tunnel.ProbeEvidence{DNSOK: true, TCPConnectOK: true, TLSOK: true, HealthOK: true}, nil
		}},
		BootstrapProbe: func(context.Context, tunnel.Candidate, string) error { return nil },
		FinalProbe:     func(context.Context, tunnel.Candidate, string, string) error { return nil },
		RecordEvent: func(event string) {
			if event == "handoff_written" {
				select {
				case <-handoffWritten:
				default:
					close(handoffWritten)
				}
			}
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- app.supportSessionStart(ctx, supportSessionStartOptions{
			RepoRoot: ".", Addr: addr, WorkDir: workDir, Target: "windows", Reason: "published route loss",
			TTLSeconds: 60, Locale: "en", AllowDegradedDirectHandoff: true,
		})
	}()
	select {
	case <-handoffWritten:
	case <-ctx.Done():
		t.Fatal("support session did not publish handoff")
	}
	handle.wait <- errors.New("published route exited")
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "tunnel availability lost before target connection") {
			t.Fatalf("foreground route loss error = %v", err)
		}
	case <-ctx.Done():
		t.Fatal("foreground start remained blocked after route loss")
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("gateway listener not cleaned after route loss: %v", err)
	}
	_ = listener.Close()
}

func TestSupportSessionStartInvalidatesManagedRouteWhenPublicBootstrapDisappears(t *testing.T) {
	for _, name := range []string{"RDEV_HOSTED_GATEWAY_URL", "RDEV_CLOUDFLARED_GATEWAY_URL", "RDEV_RELAY_GATEWAY_URL", "RDEV_MESH_GATEWAY_URL", "RDEV_VPN_GATEWAY_URL", "RDEV_SSH_GATEWAY_URL"} {
		t.Setenv(name, "")
	}
	handle := newSupportSessionTestTunnelHandle(tunnel.Candidate{ProviderID: "managed-stale", URL: "https://managed-stale.example.test"})
	registry, err := tunnel.NewRegistry(fixedSupportSessionProvider{id: "managed-stale", handle: handle})
	if err != nil {
		t.Fatal(err)
	}
	addr := supportSessionTestAddr(t)
	workDir := filepath.Join(t.TempDir(), "support")
	handoffWritten := make(chan struct{})
	var bootstrapGone atomic.Bool
	app := NewApp(io.Discard, io.Discard)
	app.supportSessionStartDeps = &supportSessionStartDeps{
		Registry: registry,
		Manager: tunnel.Manager{Probe: func(context.Context, tunnel.Candidate) (tunnel.ProbeEvidence, error) {
			return tunnel.ProbeEvidence{DNSOK: true, TCPConnectOK: true, TLSOK: true, HealthOK: true}, nil
		}},
		BootstrapProbe: func(context.Context, tunnel.Candidate, string) error { return nil },
		FinalProbe: func(context.Context, tunnel.Candidate, string, string) error {
			if bootstrapGone.Load() {
				return errors.New("public bootstrap returned 404")
			}
			return nil
		},
		LivenessInterval: time.Millisecond,
		LivenessFailures: 1,
		RecordEvent: func(event string) {
			if event == "handoff_written" {
				select {
				case <-handoffWritten:
				default:
					close(handoffWritten)
				}
			}
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- app.supportSessionStart(ctx, supportSessionStartOptions{
			RepoRoot: ".", Addr: addr, WorkDir: workDir, Target: "windows", Reason: "managed public bootstrap loss",
			TTLSeconds: 60, Locale: "en", AllowDegradedDirectHandoff: true,
		})
	}()
	select {
	case <-handoffWritten:
	case <-ctx.Done():
		t.Fatal("support session did not publish handoff")
	}
	bootstrapGone.Store(true)
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "public gateway liveness lost before target connection") {
			t.Fatalf("managed public bootstrap loss error = %v", err)
		}
	case <-ctx.Done():
		t.Fatal("support session remained published after managed public bootstrap disappeared")
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("gateway listener not cleaned after managed public bootstrap loss: %v", err)
	}
	_ = listener.Close()
}

func TestCompleteSupportSessionInvalidationTreatsMissingTicketAsInvalid(t *testing.T) {
	root := t.TempDir()
	readyFile, handoffFile, statusFile := availabilityTestFiles(t, root, "MISSING")
	connected, err := completeSupportSessionInvalidation(
		gateway.NewMemoryGateway(), &recordingStateStore{}, "missing-ticket",
		readyFile, handoffFile, statusFile,
		tunnel.AvailabilitySet{SchemaVersion: tunnel.AvailabilitySchemaVersion, Region: tunnel.RegionGlobal},
	)
	if err != nil {
		t.Fatal(err)
	}
	if connected {
		t.Fatal("missing ticket was treated as a connected host")
	}
	content, err := os.ReadFile(readyFile)
	if err != nil || !bytes.Contains(content, []byte(`"ready_to_send": false`)) {
		t.Fatalf("missing ticket handoff was not invalidated: %s err=%v", content, err)
	}
}

func supportSessionAvailabilityRuntime(t *testing.T, providerIDs ...string) (*tunnel.Runtime, []*supportSessionTestTunnelHandle) {
	t.Helper()
	providers := make([]tunnel.Provider, 0, len(providerIDs))
	handles := make([]*supportSessionTestTunnelHandle, 0, len(providerIDs))
	for _, id := range providerIDs {
		handle := newSupportSessionTestTunnelHandle(tunnel.Candidate{ProviderID: id, URL: "https://" + id + ".example.test"})
		handles = append(handles, handle)
		providers = append(providers, fixedSupportSessionProvider{id: id, handle: handle})
	}
	selections := make([]tunnel.Selection, 0, len(providers))
	for _, provider := range providers {
		selections = append(selections, tunnel.Selection{Provider: provider, Metadata: provider.Metadata()})
	}
	runtime, err := (tunnel.Manager{MaxActive: len(providers), Probe: func(context.Context, tunnel.Candidate) (tunnel.ProbeEvidence, error) {
		return tunnel.ProbeEvidence{DNSOK: true, TCPConnectOK: true, TLSOK: true, HealthOK: true}, nil
	}}).Start(context.Background(), selections, tunnel.StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	return runtime, handles
}

type fixedSupportSessionProvider struct {
	id     string
	handle tunnel.Handle
}

func (p fixedSupportSessionProvider) ID() string { return p.id }
func (p fixedSupportSessionProvider) Metadata() tunnel.ProviderMetadata {
	return tunnel.ProviderMetadata{ID: p.id, DefaultAutomatic: true}
}
func (p fixedSupportSessionProvider) Start(context.Context, tunnel.StartRequest) (tunnel.Handle, error) {
	return p.handle, nil
}

func publishedSupportSessionForAvailabilityTest(t *testing.T) (*gateway.MemoryGateway, *recordingStateStore, model.Ticket) {
	t.Helper()
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicketWithMetadata(model.HostModeAttendedTemporary, 600, nil, "availability watcher", map[string]string{"auto_activate": "attended-temporary"})
	if err != nil {
		t.Fatal(err)
	}
	store := &recordingStateStore{}
	if _, err := store.SaveFrom(gw); err != nil {
		t.Fatal(err)
	}
	return gw, store, ticket
}

func availabilityTestFiles(t *testing.T, root, ticketCode string) (string, string, string) {
	t.Helper()
	readyFile := filepath.Join(root, "ready.json")
	handoffFile := filepath.Join(root, "handoff.txt")
	statusFile := filepath.Join(root, "status.json")
	if err := os.WriteFile(readyFile, []byte(`{"ready_to_send":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(handoffFile, []byte("handoff "+ticketCode), 0o600); err != nil {
		t.Fatal(err)
	}
	return readyFile, handoffFile, statusFile
}

func assertAvailabilityInvalidated(t *testing.T, gw *gateway.MemoryGateway, ticket model.Ticket, readyFile, handoffFile, statusFile string) {
	t.Helper()
	current, _ := gw.TicketForCode(ticket.Code)
	if current.Status != model.TicketStatusRevoked {
		t.Fatalf("availability loss left ticket active: %#v", current)
	}
	var diagnostic map[string]any
	content, err := os.ReadFile(readyFile)
	if err == nil {
		err = json.Unmarshal(content, &diagnostic)
	}
	reason, _ := diagnostic["reason"].(string)
	if err != nil || diagnostic["ready_to_send"] != false ||
		(reason != "tunnel_availability_lost" && reason != "explicit_gateway_liveness_lost") {
		t.Fatalf("ready file was not invalidated: content=%s err=%v", content, err)
	}
	handoff, err := os.ReadFile(handoffFile)
	if err != nil || strings.Contains(string(handoff), ticket.Code) {
		t.Fatalf("handoff remains sendable: %q err=%v", handoff, err)
	}
	status, err := os.ReadFile(statusFile)
	if err != nil || !bytes.Contains(status, []byte(`"ready_to_send": false`)) {
		t.Fatalf("status is not fail-closed: %s err=%v", status, err)
	}
}
