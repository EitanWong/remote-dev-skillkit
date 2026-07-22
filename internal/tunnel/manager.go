package tunnel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/rand"
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
	Region                RegionProfile
	MaxActive             int
	StartTimeout          time.Duration
	ProbeTimeout          time.Duration
	Probe                 ProbeFunc
	PoolTarget            int
	LivenessInterval      time.Duration
	LivenessFailures      int
	ReplacementBackoff    time.Duration
	ReplacementMaxBackoff time.Duration
}

type lifetimeBoundHandle struct {
	inner  Handle
	cancel context.CancelFunc
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
	release      func()
}

type Runtime struct {
	mu                  sync.RWMutex
	snapshot            AvailabilitySet
	handles             []*runtimeHandle
	currentHandles      []*runtimeHandle
	candidates          []Candidate
	hasCandidate        []bool
	replacementInFlight []bool
	livenessFailures    []int
	selections          []Selection
	request             StartRequest
	manager             Manager
	ctx                 context.Context
	cancel              context.CancelFunc
	stopRequested       atomic.Bool
	replacementWG       sync.WaitGroup
	done                chan struct{}
	cleanupOnce         sync.Once
	cleanupDone         chan struct{}
	cleanupErr          error
	updates             chan struct{}
}

func (m Manager) Start(ctx context.Context, selections []Selection, request StartRequest) (*Runtime, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runtimeCtx, cancel := context.WithCancel(ctx)
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
		done:                make(chan struct{}),
		cleanupDone:         make(chan struct{}),
		updates:             make(chan struct{}, 1),
		candidates:          make([]Candidate, len(selections)),
		hasCandidate:        make([]bool, len(selections)),
		currentHandles:      make([]*runtimeHandle, len(selections)),
		replacementInFlight: make([]bool, len(selections)),
		livenessFailures:    make([]int, len(selections)),
		selections:          append([]Selection(nil), selections...),
		request:             request,
		manager:             m,
		ctx:                 runtimeCtx,
		cancel:              cancel,
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
				if !m.startOne(runtimeCtx, runtime, index, selections[index], request) {
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
		case <-runtimeCtx.Done():
			_ = runtime.Stop(context.Background())
		case <-runtime.done:
		}
	}()
	if m.PoolTarget > 0 {
		go runtime.supervise()
	}
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
	handle, err := startProviderWithin(ctx, m.StartTimeout, selection.Provider, request)
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
	if len(runtime.currentHandles) < len(runtime.candidates) {
		runtime.currentHandles = append(runtime.currentHandles, make([]*runtimeHandle, len(runtime.candidates)-len(runtime.currentHandles))...)
	}
	runtime.candidates[index] = candidate
	runtime.hasCandidate[index] = true
	runtime.snapshot.Attempts[index].CandidateID = candidateID
	runtime.handles = append(runtime.handles, item)
	runtime.currentHandles[index] = item
	runtime.mu.Unlock()

	if item.syncWaitCompleted() {
		runtime.markExited(index, item.waitErr)
		return true
	}

	probeCtx, cancelProbe := withOptionalTimeout(ctx, m.ProbeTimeout)
	evidence, probeErr := m.probeCandidate(probeCtx, candidate)
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

func startProviderWithin(ctx context.Context, timeout time.Duration, provider Provider, request StartRequest) (Handle, error) {
	providerCtx, cancelProviderCause := context.WithCancelCause(ctx)
	cancelProvider := func() { cancelProviderCause(context.Canceled) }
	var timeoutDone chan struct{}
	var timer *time.Timer
	if timeout > 0 {
		timeoutDone = make(chan struct{})
		timer = time.AfterFunc(timeout, func() {
			cancelProviderCause(context.DeadlineExceeded)
			close(timeoutDone)
		})
	}
	handle, err := provider.Start(providerCtx, request)
	if timer != nil && !timer.Stop() {
		<-timeoutDone
	}
	if cause := context.Cause(providerCtx); cause != nil {
		stopRejectedProviderHandle(handle)
		return nil, cause
	}
	if err != nil || handle == nil {
		cancelProvider()
		stopRejectedProviderHandle(handle)
		return nil, err
	}
	return newLifetimeBoundHandle(handle, cancelProvider), nil
}

func newLifetimeBoundHandle(inner Handle, cancel context.CancelFunc) Handle {
	return &lifetimeBoundHandle{inner: inner, cancel: cancel}
}

func (h *lifetimeBoundHandle) Candidate() Candidate { return h.inner.Candidate() }

func (h *lifetimeBoundHandle) Wait() <-chan error { return h.inner.Wait() }

func (h *lifetimeBoundHandle) Stop(ctx context.Context) error {
	h.cancel()
	return h.inner.Stop(ctx)
}

func (h *lifetimeBoundHandle) releaseProviderContext() { h.cancel() }

func stopRejectedProviderHandle(handle Handle) {
	if handle == nil {
		return
	}
	waitDone := make(chan struct{})
	go func() {
		if wait := handle.Wait(); wait != nil {
			<-wait
		}
		close(waitDone)
	}()
	_ = handle.Stop(context.Background())
	<-waitDone
}

func (r *Runtime) Snapshot() AvailabilitySet {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneAvailabilitySet(r.snapshot)
}

func (r *Runtime) Changes() <-chan struct{} { return r.updates }

// RecoveryPending reports whether a pool slot is between a failed handle and
// its replacement. Callers can keep the published handoff alive during this
// short transition instead of treating the intermediate empty snapshot as a
// terminal outage.
func (r *Runtime) RecoveryPending() bool {
	if r == nil || r.manager.PoolTarget <= 0 || r.stopRequested.Load() {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for index, attempt := range r.snapshot.Attempts {
		if index < len(r.replacementInFlight) && r.replacementInFlight[index] {
			return true
		}
		if attempt.Status == AttemptHealthy || index >= len(r.currentHandles) || r.currentHandles[index] == nil {
			continue
		}
		return true
	}
	return false
}

func (r *Runtime) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.stopRequested.Store(true)
	if r.cancel != nil {
		r.cancel()
	}
	// scheduleReplacement adds workers while holding mu. Cross the same lock
	// after publishing stopRequested so cleanup cannot Wait alongside a late Add.
	r.mu.Lock()
	r.mu.Unlock()
	r.cleanupOnce.Do(func() { go r.cleanup() })
	select {
	case <-r.cleanupDone:
		return r.cleanupErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runtime) cleanup() {
	r.replacementWG.Wait()
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
	if item.stopping.Load() || r.ctx.Err() != nil || !r.isCurrentHandle(item) {
		return
	}
	r.markExited(item.attemptIndex, item.waitErr)
	r.scheduleReplacement(item.attemptIndex)
}

func (m Manager) probeCandidate(ctx context.Context, candidate Candidate) (ProbeEvidence, error) {
	if m.Probe != nil {
		return m.Probe(ctx, candidate)
	}
	return ProbeGatewayHealth(ctx, nil, candidate, "")
}

func (r *Runtime) supervise() {
	interval := r.manager.LivenessInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.probeHealthyCandidates()
		}
	}
}

func (r *Runtime) probeHealthyCandidates() {
	type probeTarget struct {
		index     int
		candidate Candidate
	}
	r.mu.RLock()
	targets := make([]probeTarget, 0, len(r.snapshot.Candidates))
	for index, attempt := range r.snapshot.Attempts {
		if attempt.Status == AttemptHealthy && index < len(r.hasCandidate) && r.hasCandidate[index] {
			targets = append(targets, probeTarget{index: index, candidate: r.candidates[index]})
		}
	}
	r.mu.RUnlock()
	for _, target := range targets {
		probeCtx, cancel := withOptionalTimeout(r.ctx, r.manager.ProbeTimeout)
		evidence, err := r.manager.probeCandidate(probeCtx, target.candidate)
		cancel()
		if err == nil {
			r.recordLivenessSuccess(target.index, evidence)
			continue
		}
		if r.recordLivenessFailure(target.index, evidence) {
			r.retireForReplacement(target.index, "liveness-probe-failed", evidence)
		}
	}
}

func (r *Runtime) recordLivenessSuccess(index int, evidence ProbeEvidence) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if index >= len(r.snapshot.Attempts) || r.snapshot.Attempts[index].Status != AttemptHealthy {
		return
	}
	r.livenessFailures[index] = 0
	r.snapshot.Attempts[index].Probe = evidence
}

func (r *Runtime) recordLivenessFailure(index int, evidence ProbeEvidence) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if index >= len(r.snapshot.Attempts) || r.snapshot.Attempts[index].Status != AttemptHealthy {
		return false
	}
	r.livenessFailures[index]++
	r.snapshot.Attempts[index].Probe = evidence
	threshold := r.manager.LivenessFailures
	if threshold <= 0 {
		threshold = 3
	}
	return r.livenessFailures[index] >= threshold
}

func (r *Runtime) retireForReplacement(index int, errorClass string, evidence ProbeEvidence) {
	r.mu.Lock()
	if index >= len(r.snapshot.Attempts) || r.snapshot.Attempts[index].Status != AttemptHealthy || r.replacementInFlight[index] {
		r.mu.Unlock()
		return
	}
	item := r.currentHandles[index]
	r.snapshot.Attempts[index].Status = AttemptDegraded
	r.snapshot.Attempts[index].ErrorClass = errorClass
	r.snapshot.Attempts[index].Probe = evidence
	r.hasCandidate[index] = false
	r.refreshCandidatesLocked()
	r.notifyLocked()
	r.mu.Unlock()
	if item == nil {
		r.scheduleReplacement(index)
		return
	}
	go func() {
		item.stopAndReap()
		r.scheduleReplacement(index)
	}()
}

func (r *Runtime) isCurrentHandle(item *runtimeHandle) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return item.attemptIndex >= 0 && item.attemptIndex < len(r.currentHandles) && r.currentHandles[item.attemptIndex] == item
}

func (r *Runtime) scheduleReplacement(index int) {
	if r.stopRequested.Load() || r.ctx.Err() != nil || index < 0 || index >= len(r.selections) {
		return
	}
	r.mu.Lock()
	if r.stopRequested.Load() || r.replacementInFlight[index] {
		r.mu.Unlock()
		return
	}
	r.replacementInFlight[index] = true
	r.replacementWG.Add(1)
	r.mu.Unlock()
	go func() {
		defer r.replacementWG.Done()
		defer func() {
			r.mu.Lock()
			r.replacementInFlight[index] = false
			r.mu.Unlock()
		}()
		delay := r.replacementDelay()
		for {
			if err := sleepContext(r.ctx, delay); err != nil {
				return
			}
			r.prepareReplacement(index)
			r.manager.startOne(r.ctx, r, index, r.selections[index], r.request)
			if r.isHealthy(index) {
				return
			}
			delay = nextReplacementDelay(delay, r.manager.ReplacementMaxBackoff)
		}
	}()
}

func (r *Runtime) prepareReplacement(index int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	providerID := r.selections[index].Metadata.ID
	if providerID == "" && r.selections[index].Provider != nil {
		providerID = r.selections[index].Provider.ID()
	}
	r.currentHandles[index] = nil
	r.hasCandidate[index] = false
	r.snapshot.Attempts[index] = Attempt{ProviderID: providerID, Status: AttemptStarting}
	r.livenessFailures[index] = 0
	r.refreshCandidatesLocked()
	r.notifyLocked()
}

func (r *Runtime) isHealthy(index int) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return index >= 0 && index < len(r.snapshot.Attempts) && r.snapshot.Attempts[index].Status == AttemptHealthy && r.hasCandidate[index]
}

func (r *Runtime) replacementDelay() time.Duration {
	base := r.manager.ReplacementBackoff
	if base <= 0 {
		base = time.Second
	}
	max := r.manager.ReplacementMaxBackoff
	if max <= 0 {
		max = time.Minute
	}
	if base > max {
		base = max
	}
	// A bounded jitter prevents multiple providers from retrying in lockstep.
	jitter := time.Duration(rand.Int63n(int64(base/4 + 1)))
	if base+jitter > max {
		return max
	}
	return base + jitter
}

func nextReplacementDelay(current, maximum time.Duration) time.Duration {
	if maximum <= 0 {
		maximum = time.Minute
	}
	if current >= maximum/2 {
		return maximum
	}
	return current * 2
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
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
	item := &runtimeHandle{
		handle: handle, attemptIndex: attemptIndex, wait: handle.Wait(),
		waitDone: make(chan struct{}), waitStarted: make(chan struct{}), waitChecks: make(chan chan struct{}),
		release: func() {},
	}
	if releaser, ok := handle.(interface{ releaseProviderContext() }); ok {
		item.release = releaser.releaseProviderContext
	}
	return item
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
					h.release()
					h.waitComplete.Store(true)
					close(h.waitDone)
					return
				case checked := <-h.waitChecks:
					select {
					case h.waitErr = <-h.wait:
						h.release()
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
	case strings.Contains(lower, "nxdomain") || strings.Contains(lower, "no such host") || strings.Contains(lower, "resolved a non-public address"):
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
