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
	AttemptSkipped  AttemptStatus = "skipped"
)

type ProbeEvidence struct {
	DNSOK                  bool          `json:"dns_ok"`
	TCPConnectOK           bool          `json:"tcp_connect_ok"`
	TLSOK                  bool          `json:"tls_ok"`
	HealthOK               bool          `json:"health_ok"`
	BootstrapOK            bool          `json:"bootstrap_ok"`
	StaticBootstrapOK      bool          `json:"static_bootstrap_ok"`
	TicketBoundBootstrapOK bool          `json:"ticket_bound_bootstrap_ok"`
	SmallAssetOK           bool          `json:"small_asset_ok"`
	Latency                time.Duration `json:"latency"`
	InstanceMarker         string        `json:"instance_marker,omitempty"`
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
	wait         <-chan error
	waitDone     chan struct{}
	waitStarted  chan struct{}
	waitChecks   chan chan struct{}
	waitErr      error
	waitComplete atomic.Bool
	waitStart    sync.Once
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
	updates      chan struct{}
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
		updates:      make(chan struct{}, 1),
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
	runtime.markUnattemptedSkipped()
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
		item.startWaiter()
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
	item.startWaiter()
	runtime.mu.Lock()
	runtime.candidates[index] = candidate
	runtime.hasCandidate[index] = true
	runtime.snapshot.Attempts[index].CandidateID = candidateID
	runtime.handles = append(runtime.handles, item)
	runtime.mu.Unlock()

	if item.syncWaitCompleted() {
		runtime.markExited(index, item.waitErr)
		return true
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
	if runtime.completeProbe(index, item, evidence, probeErr) {
		return true
	}
	if probeErr != nil {
		item.stopAndReap()
		return true
	}
	go runtime.observeExit(item)
	return false
}

func (r *Runtime) Snapshot() AvailabilitySet {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneAvailabilitySet(r.snapshot)
}

func (r *Runtime) Changes() <-chan struct{} { return r.updates }

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
	errResults := make(chan error, len(handles))
	var cleanupWorkers sync.WaitGroup
	for _, item := range handles {
		cleanupWorkers.Add(1)
		go func(item *runtimeHandle) {
			defer cleanupWorkers.Done()
			item.stopAndReap()
			errResults <- item.stopErr
			r.updateAttemptAndCandidate(item.attemptIndex, false, func(attempt *Attempt) {
				if attempt.Status != AttemptExited {
					attempt.Status = AttemptStopped
					attempt.ErrorClass = ""
				}
			})
		}(item)
	}
	cleanupWorkers.Wait()
	close(errResults)
	errs := make([]error, 0, len(handles))
	for err := range errResults {
		if err != nil {
			errs = append(errs, err)
		}
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
	r.updateAttemptAndCandidate(index, false, func(attempt *Attempt) {
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
	r.refreshCandidatesLocked()
	r.notifyLocked()
}

func (r *Runtime) updateAttemptAndCandidate(index int, live bool, update func(*Attempt)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	update(&r.snapshot.Attempts[index])
	r.hasCandidate[index] = live
	r.refreshCandidatesLocked()
	r.notifyLocked()
}

func (r *Runtime) completeProbe(index int, item *runtimeHandle, evidence ProbeEvidence, probeErr error) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	attempt := &r.snapshot.Attempts[index]
	attempt.Probe = evidence
	if item.syncWaitCompleted() {
		attempt.Status = AttemptExited
		attempt.ErrorClass = classifyError(item.waitErr, "process-exited")
		r.hasCandidate[index] = false
		r.refreshCandidatesLocked()
		r.notifyLocked()
		return true
	}
	if probeErr != nil {
		attempt.Status = AttemptDegraded
		attempt.ErrorClass = classifyError(probeErr, "probe-failed")
		r.hasCandidate[index] = false
		r.refreshCandidatesLocked()
		r.notifyLocked()
		return false
	}
	attempt.Status = AttemptHealthy
	attempt.ErrorClass = ""
	r.hasCandidate[index] = true
	r.refreshCandidatesLocked()
	r.notifyLocked()
	return false
}

func (r *Runtime) markUnattemptedSkipped() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.snapshot.Attempts {
		if r.snapshot.Attempts[i].Status == AttemptStarting {
			r.snapshot.Attempts[i].Status = AttemptSkipped
			r.snapshot.Attempts[i].ErrorClass = "max-active"
		}
	}
}

func (r *Runtime) finalizeCandidates() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refreshCandidatesLocked()
	r.notifyLocked()
}

func (r *Runtime) refreshCandidatesLocked() {
	r.snapshot.Candidates = make([]Candidate, 0, len(r.candidates))
	for i, candidate := range r.candidates {
		if r.hasCandidate[i] && r.snapshot.Attempts[i].Status == AttemptHealthy {
			r.snapshot.Candidates = append(r.snapshot.Candidates, candidate)
		}
	}
}

func (r *Runtime) notifyLocked() {
	select {
	case r.updates <- struct{}{}:
	default:
	}
}

func (r *Runtime) addHandle(item *runtimeHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handles = append(r.handles, item)
}

func newRuntimeHandle(handle Handle, attemptIndex int) *runtimeHandle {
	return &runtimeHandle{
		handle: handle, attemptIndex: attemptIndex, wait: handle.Wait(),
		waitDone: make(chan struct{}), waitStarted: make(chan struct{}), waitChecks: make(chan chan struct{}),
	}
}

func (h *runtimeHandle) waitCompleted() bool {
	select {
	case <-h.waitDone:
		return true
	default:
		return false
	}
}

// syncWaitCompleted asks the sole Wait receiver to check the provider channel
// before acknowledging, avoiding a second receiver and the probe-end race.
func (h *runtimeHandle) syncWaitCompleted() bool {
	if h.waitCompleted() {
		return true
	}
	checked := make(chan struct{})
	select {
	case h.waitChecks <- checked:
		<-checked
	case <-h.waitDone:
	}
	return h.waitCompleted()
}

func (h *runtimeHandle) startWaiter() {
	if h.waitComplete.Load() {
		return
	}
	h.waitStart.Do(func() {
		go func() {
			close(h.waitStarted)
			for {
				select {
				case h.waitErr = <-h.wait:
					h.waitComplete.Store(true)
					close(h.waitDone)
					return
				case checked := <-h.waitChecks:
					select {
					case h.waitErr = <-h.wait:
						h.waitComplete.Store(true)
						close(h.waitDone)
					default:
					}
					close(checked)
					if h.waitComplete.Load() {
						return
					}
				}
			}
		}()
		<-h.waitStarted
	})
}

func (h *runtimeHandle) stopAndReap() {
	h.stopping.Store(true)
	h.startWaiter()
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
	return CandidateID(providerID, rawURL)
}

// CandidateID returns the deterministic, redaction-safe correlation ID used
// for a provider candidate throughout availability diagnostics.
func CandidateID(providerID, rawURL string) string {
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
	escapedPath := canonicalEscapedPath(parsed.EscapedPath())
	parsed.Path, _ = url.PathUnescape(escapedPath)
	parsed.RawPath = escapedPath
	if escapedPath == "" {
		parsed.Path = ""
		parsed.RawPath = ""
	}
	return parsed.String()
}

func canonicalEscapedPath(escaped string) string {
	normalized := normalizePercentEscapes(escaped)
	cleaned := path.Clean("/" + normalized)
	if cleaned == "/" {
		return ""
	}
	return cleaned
}

func normalizePercentEscapes(value string) string {
	var normalized strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '%' || i+2 >= len(value) {
			normalized.WriteByte(value[i])
			continue
		}
		high, highOK := hexNibble(value[i+1])
		low, lowOK := hexNibble(value[i+2])
		if !highOK || !lowOK {
			normalized.WriteByte(value[i])
			continue
		}
		decoded := high<<4 | low
		if isURLUnreserved(decoded) {
			normalized.WriteByte(decoded)
		} else {
			normalized.WriteByte('%')
			normalized.WriteByte("0123456789ABCDEF"[high])
			normalized.WriteByte("0123456789ABCDEF"[low])
		}
		i += 2
	}
	return normalized.String()
}

func hexNibble(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

func isURLUnreserved(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || strings.ContainsRune("-._~", rune(value))
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
