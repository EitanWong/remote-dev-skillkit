package cli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

func TestLiveTunn3lReadiness(t *testing.T) {
	if os.Getenv("RDEV_LIVE_TUNNEL_TEST") != "1" {
		t.Skip("set RDEV_LIVE_TUNNEL_TEST=1 to run the external tunnel readiness test")
	}
	const instance = "live-tunn3l-readiness"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/healthz" {
			http.NotFound(w, request)
			return
		}
		w.Header().Set("X-Rdev-Gateway-Instance", instance)
		_, _ = io.WriteString(w, "ok\n")
	}))
	defer origin.Close()

	parsedOrigin, err := url.Parse(origin.URL)
	if err != nil || parsedOrigin.Port() == "" {
		t.Fatal("local readiness origin setup failed")
	}
	providerRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal("attempts=0 stage=provider-root")
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(providerRoot, 0o700); err != nil {
			t.Fatal("attempts=0 stage=provider-root")
		}
	}
	providerCtx, cancelProvider := context.WithCancel(context.Background())
	provider := newTunn3lProvider(io.Discard, managedGzipInstaller{})
	handle, err := provider.Start(providerCtx, tunnel.StartRequest{
		LocalURL: origin.URL, LocalPort: parsedOrigin.Port(), ProviderRoot: providerRoot,
	})
	if err != nil {
		cancelProvider()
		t.Fatalf("attempts=0 stage=%s", liveTunn3lStartupStage(err))
	}
	defer stopLiveTunnelHandle(t, handle, cancelProvider)

	deadline := time.Now().Add(60 * time.Second)
	attempts := 0
	lastStage := "dns"
	for time.Now().Before(deadline) {
		attempts++
		remaining := time.Until(deadline)
		probeTimeout := 5 * time.Second
		if remaining < probeTimeout {
			probeTimeout = remaining
		}
		probeCtx, cancelProbe := context.WithTimeout(context.Background(), probeTimeout)
		evidence, probeErr := tunnel.ProbeGatewayHealth(probeCtx, nil, handle.Candidate(), instance)
		cancelProbe()
		if probeErr == nil {
			t.Logf("attempts=%d stage=ready", attempts)
			return
		}
		lastStage = liveProbeStage(evidence)
		t.Logf("attempts=%d stage=%s", attempts, lastStage)

		wait := time.Until(deadline)
		if wait > time.Second {
			wait = time.Second
		}
		if wait <= 0 {
			break
		}
		timer := time.NewTimer(wait)
		select {
		case <-handle.Wait():
			if !timer.Stop() {
				<-timer.C
			}
			t.Fatalf("attempts=%d stage=%s", attempts, lastStage)
		case <-timer.C:
		}
	}
	t.Fatalf("attempts=%d stage=%s", attempts, lastStage)
}

func liveTunn3lStartupStage(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "unsupported"):
		return "unsupported"
	case strings.Contains(message, "provider root"):
		return "provider-root"
	case strings.Contains(message, "installation"):
		return "install"
	case strings.Contains(message, "provider state"):
		return "state"
	case strings.Contains(message, "timed out"):
		return "startup-timeout"
	default:
		return "startup"
	}
}

type liveTunnelCleanupFakeHandle struct {
	wait    chan error
	stopErr error
	stops   int
}

func (h *liveTunnelCleanupFakeHandle) Candidate() tunnel.Candidate { return tunnel.Candidate{} }
func (h *liveTunnelCleanupFakeHandle) Wait() <-chan error          { return h.wait }
func (h *liveTunnelCleanupFakeHandle) Stop(context.Context) error {
	h.stops++
	return h.stopErr
}

func TestStopLiveTunnelHandleStopsAlreadyExitedHandle(t *testing.T) {
	exitErr := &exec.ExitError{}
	wait := make(chan error, 1)
	wait <- exitErr
	handle := &liveTunnelCleanupFakeHandle{wait: wait, stopErr: exitErr}
	canceled := false

	stopLiveTunnelHandle(t, handle, func() { canceled = true })

	if handle.stops != 1 {
		t.Fatalf("Stop calls = %d, want 1", handle.stops)
	}
	if !canceled {
		t.Fatal("provider context was not canceled")
	}
	select {
	case <-wait:
		t.Fatal("already-exited handle was not reaped")
	default:
	}
}

func stopLiveTunnelHandle(t *testing.T, handle tunnel.Handle, cancelProvider context.CancelFunc) {
	t.Helper()
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 10*time.Second)
	stopErr := handle.Stop(stopCtx)
	cancelStop()
	cancelProvider()
	cleanupFailed := !expectedLiveTunnelCleanupError(stopErr)
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case waitErr, ok := <-handle.Wait():
		if ok && !expectedLiveTunnelCleanupError(waitErr) {
			cleanupFailed = true
		}
	case <-timer.C:
		cleanupFailed = true
	}
	if cleanupFailed {
		t.Error("attempts=0 stage=cleanup")
	}
}

func expectedLiveTunnelCleanupError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return true
	}
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

func liveProbeStage(evidence tunnel.ProbeEvidence) string {
	switch {
	case !evidence.DNSOK:
		return "dns"
	case !evidence.TCPConnectOK:
		return "tcp"
	case !evidence.TLSOK:
		return "tls"
	case !evidence.HealthOK:
		return "http"
	default:
		return "ready"
	}
}
