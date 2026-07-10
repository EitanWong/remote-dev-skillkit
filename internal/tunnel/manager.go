package tunnel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"strings"
	"sync"
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
}

type Runtime struct {
	mu           sync.RWMutex
	snapshot     AvailabilitySet
	handles      []runtimeHandle
	candidates   []Candidate
	hasCandidate []bool
	done         chan struct{}
	stopOnce     sync.Once
	stopErr      error
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

	jobs := make(chan int)
	var workers sync.WaitGroup
	workerCount := min(maxActive, len(selections))
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				m.startOne(ctx, runtime, index, selections[index], request)
			}
		}()
	}
	for i := range selections {
		jobs <- i
	}
	close(jobs)
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

func (m Manager) startOne(ctx context.Context, runtime *Runtime, index int, selection Selection, request StartRequest) {
	if selection.Provider == nil {
		runtime.updateAttempt(index, func(attempt *Attempt) {
			attempt.Status = AttemptDegraded
			attempt.ErrorClass = "provider-invalid"
		})
		return
	}
	startCtx, cancelStart := withOptionalTimeout(ctx, m.StartTimeout)
	handle, err := selection.Provider.Start(startCtx, request)
	cancelStart()
	if err != nil || handle == nil {
		runtime.updateAttempt(index, func(attempt *Attempt) {
			attempt.Status = AttemptDegraded
			attempt.ErrorClass = classifyError(err, "start-failed")
		})
		return
	}

	candidate := handle.Candidate()
	if candidate.ProviderID == "" {
		candidate.ProviderID = selection.Provider.ID()
	}
	candidateID := candidateID(candidate.ProviderID, candidate.URL)
	runtime.mu.Lock()
	runtime.candidates[index] = candidate
	runtime.hasCandidate[index] = true
	runtime.snapshot.Attempts[index].CandidateID = candidateID
	runtime.handles = append(runtime.handles, runtimeHandle{handle: handle, attemptIndex: index})
	runtime.mu.Unlock()

	select {
	case waitErr := <-handle.Wait():
		runtime.markExited(index, waitErr)
		return
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
	go runtime.observeExit(index, handle)
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
	r.stopOnce.Do(func() {
		r.mu.RLock()
		handles := append([]runtimeHandle(nil), r.handles...)
		r.mu.RUnlock()
		errs := make([]error, 0, len(handles))
		for _, item := range handles {
			if err := item.handle.Stop(ctx); err != nil {
				errs = append(errs, err)
			}
			r.updateAttempt(item.attemptIndex, func(attempt *Attempt) {
				if attempt.Status != AttemptExited {
					attempt.Status = AttemptStopped
					attempt.ErrorClass = ""
				}
			})
		}
		close(r.done)
		r.stopErr = errors.Join(errs...)
	})
	return r.stopErr
}

func (r *Runtime) observeExit(index int, handle Handle) {
	select {
	case err := <-handle.Wait():
		r.markExited(index, err)
	case <-r.done:
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
	hostname := strings.ToLower(parsed.Hostname())
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
	parsed.Path = strings.TrimRight(parsed.Path, "/")
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
