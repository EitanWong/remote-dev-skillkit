package tunnel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type AttemptStatus string

const (
	AttemptStarting AttemptStatus = "starting"
	AttemptHealthy  AttemptStatus = "healthy"
	AttemptDegraded AttemptStatus = "degraded"
	AttemptExited   AttemptStatus = "exited"
	AttemptStopped  AttemptStatus = "stopped"
)

type ProbeEvidence struct {
	DNSOK          bool          `json:"dns_ok"`
	TCPConnectOK   bool          `json:"tcp_connect_ok"`
	TLSOK          bool          `json:"tls_ok"`
	HealthOK       bool          `json:"health_ok"`
	BootstrapOK    bool          `json:"bootstrap_ok"`
	SmallAssetOK   bool          `json:"small_asset_ok"`
	Latency        time.Duration `json:"latency"`
	InstanceMarker string        `json:"instance_marker,omitempty"`
}

type Attempt struct {
	ProviderID  string        `json:"provider_id"`
	CandidateID string        `json:"candidate_id,omitempty"`
	Status      AttemptStatus `json:"status"`
	ErrorClass  string        `json:"error_class,omitempty"`
	Probe       ProbeEvidence `json:"probe"`
}

type AvailabilitySet struct {
	SchemaVersion string        `json:"schema_version"`
	Region        RegionProfile `json:"region"`
	Candidates    []Candidate   `json:"candidates"`
	Attempts      []Attempt     `json:"attempts"`
}

type ProbeFunc func(context.Context, Candidate) (ProbeEvidence, error)

type Manager struct {
	Region       RegionProfile
	MaxActive    int
	StartTimeout time.Duration
	ProbeTimeout time.Duration
	Probe        ProbeFunc
}

type runtimeHandle struct {
	handle       Handle
	attemptIndex int
	waitDone     chan struct{}
	waitErr      error
	stopping     atomic.Bool
	stopOnce     sync.Once
	stopErr      error
}

type Runtime struct {
	mu           sync.RWMutex
	snapshot     AvailabilitySet
	handles      []*runtimeHandle
	candidates   []Candidate
	hasCandidate []bool
	done         chan struct{}
	cleanupOnce  sync.Once
	cleanupDone  chan struct{}
	cleanupErr   error
}

func (m Manager) Start(ctx context.Context, selections []Selection, request StartRequest) (*Runtime, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	maxActive := m.MaxActive
	if maxActive <= 0 {
		maxActive = 2
	}
	runtime := &Runtime{
		snapshot: AvailabilitySet{
			SchemaVersion: AvailabilitySchemaVersion,
			Region:        normalizedManagerRegion(m.Region),
			Attempts:      make([]Attempt, len(selections)),
		},
		done:         make(chan struct{}),
		cleanupDone:  make(chan struct{}),
		candidates:   make([]Candidate, len(selections)),
		hasCandidate: make([]bool, len(selections)),
	}
	for i, selection := range selections {
		providerID := selection.Metadata.ID
		if providerID == "" && selection.Provider != nil {
			providerID = selection.Provider.ID()
		}
		runtime.snapshot.Attempts[i] = Attempt{ProviderID: providerID, Status: AttemptStarting}
	}

	jobs := make(chan int, len(selections))
	for i := range selections {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	workerCount := min(maxActive, len(selections))
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				if !m.startOne(ctx, runtime, index, selections[index], request) {
					return
				}
			}
		}()
	}
	workers.Wait()
	runtime.finalizeCandidates()

	go func() {
		select {
		case <-ctx.Done():
			_ = runtime.Stop(context.Background())
		case <-runtime.done:
		}
	}()
	return runtime, nil
}

// startOne returns whether its worker may start another provider without
// exceeding the live-handle bound.
func (m Manager) startOne(ctx context.Context, runtime *Runtime, index int, selection Selection, request StartRequest) bool {
	if selection.Provider == nil {
		runtime.updateAttempt(index, func(attempt *Attempt) {
			attempt.Status = AttemptDegraded
			attempt.ErrorClass = "provider-invalid"
		})
		return true
	}
	startCtx, cancelStart := withOptionalTimeout(ctx, m.StartTimeout)
	handle, err := selection.Provider.Start(startCtx, request)
	cancelStart()
	if err != nil || handle == nil {
		runtime.updateAttempt(index, func(attempt *Attempt) {
			attempt.Status = AttemptDegraded
			attempt.ErrorClass = classifyError(err, "start-failed")
		})
		return true
	}

	candidate := handle.Candidate()
	authoritativeID := selection.Provider.ID()
	if candidate.ProviderID != authoritativeID {
		item := newRuntimeHandle(handle, index)
		runtime.addHandle(item)
		runtime.updateAttempt(index, func(attempt *Attempt) {
			attempt.Status = AttemptDegraded
			attempt.ErrorClass = "provider-id-mismatch"
		})
		item.stopAndReap()
		return true
	}
	candidateID := candidateID(authoritativeID, candidate.URL)
	item := newRuntimeHandle(handle, index)
	runtime.mu.Lock()
	runtime.candidates[index] = candidate
	runtime.hasCandidate[index] = true
	runtime.snapshot.Attempts[index].CandidateID = candidateID
	runtime.handles = append(runtime.handles, item)
	runtime.mu.Unlock()
	go runtime.observeExit(item)

	select {
	case <-item.waitDone:
		runtime.markExited(index, item.waitErr)
		return true
	default:
	}

	probe := m.Probe
	if probe == nil {
		probe = func(probeCtx context.Context, candidate Candidate) (ProbeEvidence, error) {
			return ProbeGatewayHealth(probeCtx, nil, candidate, "")
		}
	}
	probeCtx, cancelProbe := withOptionalTimeout(ctx, m.ProbeTimeout)
	evidence, probeErr := probe(probeCtx, candidate)
	cancelProbe()
	runtime.updateAttempt(index, func(attempt *Attempt) {
		attempt.Probe = evidence
		if probeErr != nil {
			attempt.Status = AttemptDegraded
			attempt.ErrorClass = classifyError(probeErr, "probe-failed")
			return
		}
		attempt.Status = AttemptHealthy
		attempt.ErrorClass = ""
	})
	if probeErr != nil {
		item.stopAndReap()
		return true
	}
	return false
}

func (r *Runtime) Snapshot() AvailabilitySet {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneAvailabilitySet(r.snapshot)
}

func (r *Runtime) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.cleanupOnce.Do(func() { go r.cleanup() })
	select {
	case <-r.cleanupDone:
		return r.cleanupErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runtime) cleanup() {
	r.mu.RLock()
	handles := append([]*runtimeHandle(nil), r.handles...)
	r.mu.RUnlock()
	errs := make([]error, 0, len(handles))
	for _, item := range handles {
		item.stopAndReap()
		if item.stopErr != nil {
			errs = append(errs, item.stopErr)
		}
		r.updateAttempt(item.attemptIndex, func(attempt *Attempt) {
			if attempt.Status != AttemptExited {
				attempt.Status = AttemptStopped
				attempt.ErrorClass = ""
			}
		})
	}
	close(r.done)
	r.cleanupErr = errors.Join(errs...)
	close(r.cleanupDone)
}

func (r *Runtime) observeExit(item *runtimeHandle) {
	<-item.waitDone
	if !item.stopping.Load() {
		r.markExited(item.attemptIndex, item.waitErr)
	}
}

func (r *Runtime) markExited(index int, err error) {
	r.updateAttempt(index, func(attempt *Attempt) {
		if attempt.Status == AttemptStopped {
			return
		}
		attempt.Status = AttemptExited
		attempt.ErrorClass = classifyError(err, "process-exited")
	})
}

func (r *Runtime) updateAttempt(index int, update func(*Attempt)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	update(&r.snapshot.Attempts[index])
}

func (r *Runtime) finalizeCandidates() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshot.Candidates = make([]Candidate, 0, len(r.candidates))
	for i, candidate := range r.candidates {
		if r.hasCandidate[i] {
			r.snapshot.Candidates = append(r.snapshot.Candidates, candidate)
		}
	}
}

func (r *Runtime) addHandle(item *runtimeHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handles = append(r.handles, item)
}

func newRuntimeHandle(handle Handle, attemptIndex int) *runtimeHandle {
	item := &runtimeHandle{handle: handle, attemptIndex: attemptIndex, waitDone: make(chan struct{})}
	wait := handle.Wait()
	select {
	case item.waitErr = <-wait:
		close(item.waitDone)
		return item
	default:
	}
	go func() {
		item.waitErr = <-wait
		close(item.waitDone)
	}()
	return item
}

func (h *runtimeHandle) stopAndReap() {
	h.stopping.Store(true)
	h.stopOnce.Do(func() {
		h.stopErr = h.handle.Stop(context.Background())
	})
	<-h.waitDone
}

func normalizedManagerRegion(region RegionProfile) RegionProfile {
	if supportedRegion(region) {
		return region
	}
	return RegionGlobal
}

func withOptionalTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func candidateID(providerID, rawURL string) string {
	sum := sha256.Sum256([]byte(providerID + "\x00" + normalizeCandidateURL(rawURL)))
	return hex.EncodeToString(sum[:8])
}

func normalizeCandidateURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return strings.TrimSpace(rawURL)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	hostname := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	port := parsed.Port()
	if (parsed.Scheme == "https" && port == "443") || (parsed.Scheme == "http" && port == "80") {
		port = ""
	}
	parsed.Host = hostname
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		parsed.Host = "[" + hostname + "]"
	}
	parsed.Fragment = ""
	parsed.RawQuery = ""
	parsed.Path = path.Clean("/" + parsed.Path)
	if parsed.Path == "/" {
		parsed.Path = ""
	}
	parsed.RawPath = ""
	return parsed.String()
}

func classifyError(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	lower := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(lower, "timeout"):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case strings.Contains(lower, "nxdomain") || strings.Contains(lower, "no such host"):
		return "dns-failed"
	case strings.Contains(lower, "marker"):
		return "marker-mismatch"
	case strings.Contains(lower, "redirect"):
		return "redirect-rejected"
	default:
		return fallback
	}
}

func cloneAvailabilitySet(source AvailabilitySet) AvailabilitySet {
	cloned := source
	cloned.Candidates = append([]Candidate(nil), source.Candidates...)
	cloned.Attempts = append([]Attempt(nil), source.Attempts...)
	return cloned
}
