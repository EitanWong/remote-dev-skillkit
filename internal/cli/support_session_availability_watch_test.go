package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestForegroundOneOfTwoRouteDeathFailsClosedWithoutDeadCandidate(t *testing.T) {
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
	handles[0].wait <- errors.New("first exited")
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("watcher retained stale multi-route handoff")
	}
	content, err := os.ReadFile(statusFile)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(content, []byte("first.example.test")) || !bytes.Contains(content, []byte("second.example.test")) {
		t.Fatalf("invalidation status retained dead route: %s", content)
	}
	assertAvailabilityInvalidated(t, gw, ticket, readyFile, handoffFile, statusFile)
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
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchForegroundSupportSessionAvailability(context.Background(), foregroundSupportSessionOptions{
			Out: &bytes.Buffer{}, StatusFile: statusFile, ReadyFile: readyFile, HandoffTextFile: handoffFile,
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
	handles[0].wait <- errors.New("exit after connection")
	time.Sleep(50 * time.Millisecond)
	current, _ := gw.TicketForCode(ticket.Code)
	if current.Status != model.TicketStatusActive {
		t.Fatalf("route exit after connection revoked ticket: %#v", current)
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
	if err != nil || json.Unmarshal(content, &diagnostic) != nil || diagnostic["ready_to_send"] != false || diagnostic["reason"] != "tunnel_availability_lost" {
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
