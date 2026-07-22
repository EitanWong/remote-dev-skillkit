package hostcmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

var errNoHealthyRoutes = errors.New("no healthy gateway routes")

const (
	maxGatewayRoutes      = 16
	maxRouteProbeWorkers  = 8
	maxManifestCandidates = 15
)

// routeCandidate is the immutable identity of a gateway and one of the
// event transports maintained by the host helper.
type routeCandidate struct {
	URL       string
	Transport string
}

func (r routeCandidate) key() string {
	return r.URL + "\x00" + r.Transport
}

type routeProbeResult struct {
	Healthy bool
	Latency time.Duration
	Err     error
}

type routeSnapshot struct {
	Candidate  routeCandidate
	Generation uint64
	changed    <-chan struct{}
}

type routePoolConfig struct {
	MaxConcurrent      int
	ProbeTimeout       time.Duration
	Cooldown           time.Duration
	ReprobeInterval    time.Duration
	ShareGatewayHealth bool
	Now                func() time.Time
	Probe              func(context.Context, routeCandidate) routeProbeResult
}

type routeState struct {
	candidate     routeCandidate
	healthy       bool
	latency       time.Duration
	failures      int
	cooldownUntil time.Time
	fastStreak    int
	epoch         uint64
}

type routePool struct {
	mu          sync.Mutex
	opMu        sync.Mutex
	routes      []routeState
	active      int
	initialized bool
	generation  uint64
	lastProbe   time.Time
	changed     chan struct{}
	probeSlots  chan struct{}
	config      routePoolConfig
}

type routeProbeObservation struct {
	index  int
	epoch  uint64
	result routeProbeResult
}

func newRoutePool(candidates []routeCandidate, config routePoolConfig) *routePool {
	if config.MaxConcurrent <= 0 {
		config.MaxConcurrent = 4
	}
	if config.MaxConcurrent > maxRouteProbeWorkers {
		config.MaxConcurrent = maxRouteProbeWorkers
	}
	if config.ProbeTimeout <= 0 {
		config.ProbeTimeout = 2 * time.Second
	}
	if config.Cooldown <= 0 {
		config.Cooldown = 10 * time.Second
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.ReprobeInterval < 0 {
		config.ReprobeInterval = 0
	}
	capacity := len(candidates)
	if capacity > maxGatewayRoutes {
		capacity = maxGatewayRoutes
	}
	seen := make(map[string]struct{}, capacity)
	routes := make([]routeState, 0, capacity)
	for _, candidate := range candidates {
		if len(routes) >= maxGatewayRoutes {
			break
		}
		candidate.URL = strings.TrimRight(strings.TrimSpace(candidate.URL), "/")
		candidate.Transport = normalizeRouteTransport(candidate.Transport)
		if !safeRouteURL(candidate.URL) || candidate.Transport == "" {
			continue
		}
		if _, ok := seen[candidate.key()]; ok {
			continue
		}
		seen[candidate.key()] = struct{}{}
		routes = append(routes, routeState{candidate: candidate})
	}
	return &routePool{routes: routes, config: config, active: -1, changed: make(chan struct{}), probeSlots: make(chan struct{}, config.MaxConcurrent)}
}

func normalizeRouteTransport(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "long-poll":
		return "long-poll"
	case "poll", "":
		return "poll"
	default:
		return ""
	}
}

func (p *routePool) initialize(ctx context.Context) (routeCandidate, error) {
	if p == nil {
		return routeCandidate{}, errNoHealthyRoutes
	}
	p.opMu.Lock()
	defer p.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return routeCandidate{}, err
	}
	p.mu.Lock()
	if p.initialized {
		if p.active >= 0 && p.active < len(p.routes) {
			selected := p.routes[p.active].candidate
			p.mu.Unlock()
			return selected, nil
		}
		p.mu.Unlock()
		return routeCandidate{}, errNoHealthyRoutes
	}
	indices := make([]int, len(p.routes))
	for index := range p.routes {
		indices[index] = index
	}
	p.mu.Unlock()
	if len(indices) == 0 {
		return routeCandidate{}, errNoHealthyRoutes
	}
	observations := p.probeRoutes(ctx, indices)
	if err := ctx.Err(); err != nil {
		return routeCandidate{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, observation := range observations {
		if observation.index < 0 || observation.index >= len(p.routes) {
			continue
		}
		state := &p.routes[observation.index]
		if state.epoch != observation.epoch {
			continue
		}
		state.epoch++
		state.healthy = observation.result.Healthy && observation.result.Err == nil
		state.latency = observation.result.Latency
		if !state.healthy {
			state.failures++
			state.cooldownUntil = p.config.Now().Add(p.config.Cooldown)
		}
	}
	now := p.config.Now()
	p.initialized = true
	p.lastProbe = now
	selected := p.fastestHealthyLocked(now, -1)
	if selected < 0 {
		return routeCandidate{}, fmt.Errorf("%w: probes did not find a reachable candidate", errNoHealthyRoutes)
	}
	p.setActiveLocked(selected)
	return p.routes[selected].candidate, nil
}

func (p *routePool) current() (routeCandidate, bool) {
	snapshot, ok := p.currentSnapshot()
	return snapshot.Candidate, ok
}

func (p *routePool) currentSnapshot() (routeSnapshot, bool) {
	if p == nil {
		return routeSnapshot{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.initialized || p.active < 0 || p.active >= len(p.routes) {
		return routeSnapshot{}, false
	}
	if !p.routes[p.active].healthy || !p.routeEligibleLocked(p.active, p.config.Now()) {
		return routeSnapshot{}, false
	}
	return routeSnapshot{Candidate: p.routes[p.active].candidate, Generation: p.generation, changed: p.changed}, true
}

func (p *routePool) reportSnapshotSuccess(snapshot routeSnapshot) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if snapshot.Generation != p.generation || p.active < 0 || p.active >= len(p.routes) || p.routes[p.active].candidate != snapshot.Candidate {
		return false
	}
	state := &p.routes[p.active]
	state.healthy = true
	state.failures = 0
	state.cooldownUntil = time.Time{}
	state.fastStreak = 0
	return true
}

func (p *routePool) reportFailure(ctx context.Context, route routeCandidate) (routeCandidate, bool) {
	return p.reportSnapshotFailure(ctx, routeSnapshot{Candidate: route})
}

func (p *routePool) reportSnapshotFailure(ctx context.Context, snapshot routeSnapshot) (routeCandidate, bool) {
	if p == nil {
		return routeCandidate{}, false
	}
	if err := ctx.Err(); err != nil {
		return routeCandidate{}, false
	}
	now := p.config.Now()
	p.mu.Lock()
	index := p.indexOfLocked(snapshot.Candidate)
	if index < 0 {
		p.mu.Unlock()
		return routeCandidate{}, false
	}
	if snapshot.Generation != 0 && (snapshot.Generation != p.generation || p.active != index) {
		candidate, ok := p.currentLocked()
		p.mu.Unlock()
		return candidate, ok
	}
	state := &p.routes[index]
	state.healthy = false
	state.failures++
	state.fastStreak = 0
	state.epoch++
	state.cooldownUntil = now.Add(p.config.Cooldown)
	wasActive := p.initialized && p.active == index
	if !wasActive {
		p.mu.Unlock()
		return p.current()
	}
	selected := p.fastestHealthyLocked(now, index)
	if selected >= 0 {
		p.setActiveLocked(selected)
		p.resetFastStreaksLocked()
		candidate := p.routes[selected].candidate
		p.mu.Unlock()
		return candidate, true
	}
	p.setActiveLocked(-1)
	replacementGeneration := p.generation
	p.resetFastStreaksLocked()
	p.mu.Unlock()

	// Last-known health may be stale. A failure path bypasses the normal
	// latency probe interval so an active outage is handled immediately.
	replacement, ok := p.probeReplacement(ctx, index, replacementGeneration)
	return replacement, ok
}

func (p *routePool) probeReplacement(ctx context.Context, excluded int, expectedGeneration uint64) (routeCandidate, bool) {
	p.mu.Lock()
	indices := p.eligibleIndicesLocked(p.config.Now(), excluded)
	p.mu.Unlock()
	if len(indices) == 0 {
		return routeCandidate{}, false
	}
	observations := p.probeRoutes(ctx, indices)
	if err := ctx.Err(); err != nil {
		return routeCandidate{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, observation := range observations {
		if observation.index < 0 || observation.index >= len(p.routes) {
			continue
		}
		state := &p.routes[observation.index]
		if state.epoch != observation.epoch {
			continue
		}
		state.healthy = observation.result.Healthy && observation.result.Err == nil
		state.latency = observation.result.Latency
		state.epoch++
		if state.healthy {
			state.failures = 0
			state.cooldownUntil = time.Time{}
		} else {
			state.failures++
			state.cooldownUntil = p.config.Now().Add(p.config.Cooldown)
		}
	}
	selected := p.fastestHealthyLocked(p.config.Now(), excluded)
	if p.generation != expectedGeneration || p.active != -1 {
		return p.currentLocked()
	}
	if selected < 0 {
		return routeCandidate{}, false
	}
	p.setActiveLocked(selected)
	return p.routes[selected].candidate, true
}

func (p *routePool) routeEligible(route routeCandidate, now time.Time) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	index := p.indexOfLocked(route)
	return index >= 0 && p.routeEligibleLocked(index, now)
}

func (p *routePool) probe(ctx context.Context) (bool, error) {
	if p == nil {
		return false, errNoHealthyRoutes
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	p.mu.Lock()
	initialized := p.initialized
	p.mu.Unlock()
	if !initialized {
		if _, err := p.initialize(ctx); err != nil {
			return false, err
		}
		return true, nil
	}
	p.opMu.Lock()
	defer p.opMu.Unlock()
	now := p.config.Now()
	p.mu.Lock()
	if !p.initialized {
		p.mu.Unlock()
		return false, errNoHealthyRoutes
	}
	if p.config.ReprobeInterval > 0 && !p.lastProbe.IsZero() && now.Sub(p.lastProbe) < p.config.ReprobeInterval {
		p.mu.Unlock()
		return false, nil
	}
	indices := p.eligibleIndicesLocked(now, -1)
	active := p.active
	activeGeneration := p.generation
	p.lastProbe = now
	p.mu.Unlock()
	if len(indices) == 0 {
		return false, nil
	}
	observations := p.probeRoutes(ctx, indices)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	observedHealthy := make(map[int]bool, len(observations))
	for _, observation := range observations {
		if observation.index < 0 || observation.index >= len(p.routes) {
			continue
		}
		state := &p.routes[observation.index]
		if state.epoch != observation.epoch {
			continue
		}
		state.healthy = observation.result.Healthy && observation.result.Err == nil
		state.latency = observation.result.Latency
		state.epoch++
		if state.healthy {
			state.failures = 0
			state.cooldownUntil = time.Time{}
			observedHealthy[observation.index] = true
		} else {
			state.failures++
			state.fastStreak = 0
			state.cooldownUntil = now.Add(p.config.Cooldown)
		}
	}
	if p.generation != activeGeneration || p.active != active {
		return p.reconcileActiveHealthLocked(now), nil
	}
	if active < 0 || active >= len(p.routes) {
		selected := p.fastestHealthyLocked(now, -1)
		if selected < 0 {
			return false, nil
		}
		p.setActiveLocked(selected)
		return true, nil
	}
	if !p.routes[active].healthy {
		selected := p.fastestHealthyLocked(now, active)
		if selected >= 0 {
			p.setActiveLocked(selected)
			p.resetFastStreaksLocked()
			return true, nil
		}
		p.setActiveLocked(-1)
		p.resetFastStreaksLocked()
		return false, nil
	}
	if !observedHealthy[active] {
		p.resetFastStreaksLocked()
		return false, nil
	}
	activeLatency := p.routes[active].latency
	if activeLatency <= 0 {
		return false, nil
	}
	best := -1
	for index := range p.routes {
		if index == active || !observedHealthy[index] || !p.routes[index].healthy || !p.routeEligibleLocked(index, now) {
			continue
		}
		candidateLatency := p.routes[index].latency
		if candidateLatency <= 0 || candidateLatency > activeLatency-activeLatency/5 {
			continue
		}
		if best < 0 || candidateLatency < p.routes[best].latency || (candidateLatency == p.routes[best].latency && index < best) {
			best = index
		}
	}
	for index := range p.routes {
		if index != best {
			p.routes[index].fastStreak = 0
		}
	}
	if best < 0 {
		return false, nil
	}
	p.routes[best].fastStreak++
	if p.routes[best].fastStreak >= 2 {
		p.setActiveLocked(best)
		p.resetFastStreaksLocked()
		return true, nil
	}
	return false, nil
}

func (p *routePool) monitor(ctx context.Context, interval time.Duration) error {
	if p == nil {
		return errNoHealthyRoutes
	}
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := p.probe(ctx); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return err
			}
		}
	}
}

func (p *routePool) probeRoutes(ctx context.Context, indices []int) []routeProbeObservation {
	if len(indices) == 0 || p.config.Probe == nil {
		return nil
	}
	type probeJob struct {
		indices []int
	}
	jobsToRun := make([]probeJob, 0, len(indices))
	if p.config.ShareGatewayHealth {
		byURL := make(map[string]int, len(indices))
		for _, index := range indices {
			p.mu.Lock()
			if index < 0 || index >= len(p.routes) {
				p.mu.Unlock()
				continue
			}
			gatewayURL := p.routes[index].candidate.URL
			p.mu.Unlock()
			if jobIndex, ok := byURL[gatewayURL]; ok {
				jobsToRun[jobIndex].indices = append(jobsToRun[jobIndex].indices, index)
				continue
			}
			byURL[gatewayURL] = len(jobsToRun)
			jobsToRun = append(jobsToRun, probeJob{indices: []int{index}})
		}
	} else {
		for _, index := range indices {
			jobsToRun = append(jobsToRun, probeJob{indices: []int{index}})
		}
	}
	if len(jobsToRun) == 0 {
		return nil
	}
	workerCount := p.config.MaxConcurrent
	if workerCount > len(jobsToRun) {
		workerCount = len(jobsToRun)
	}
	jobs := make(chan probeJob)
	results := make(chan routeProbeObservation, len(indices))
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-jobs:
					if !ok {
						return
					}
					if len(job.indices) == 0 {
						continue
					}
					p.mu.Lock()
					representative := job.indices[0]
					if representative < 0 || representative >= len(p.routes) {
						p.mu.Unlock()
						continue
					}
					candidate := p.routes[representative].candidate
					epochs := make(map[int]uint64, len(job.indices))
					for _, index := range job.indices {
						if index >= 0 && index < len(p.routes) {
							epochs[index] = p.routes[index].epoch
						}
					}
					p.mu.Unlock()
					probeCtx := ctx
					cancel := func() {}
					if p.config.ProbeTimeout > 0 {
						probeCtx, cancel = context.WithTimeout(ctx, p.config.ProbeTimeout)
					}
					select {
					case p.probeSlots <- struct{}{}:
					case <-ctx.Done():
						cancel()
						return
					}
					startedAt := time.Now()
					result := p.config.Probe(probeCtx, candidate)
					<-p.probeSlots
					cancel()
					if result.Latency <= 0 {
						result.Latency = time.Since(startedAt)
					}
					for _, index := range job.indices {
						epoch, ok := epochs[index]
						if ok {
							results <- routeProbeObservation{index: index, epoch: epoch, result: result}
						}
					}
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, job := range jobsToRun {
			select {
			case jobs <- job:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()
	observations := make([]routeProbeObservation, 0, len(indices))
	for observation := range results {
		observations = append(observations, observation)
	}
	return observations
}

func (p *routePool) indexOfLocked(route routeCandidate) int {
	for index, state := range p.routes {
		if state.candidate == route {
			return index
		}
	}
	return -1
}

func (p *routePool) currentLocked() (routeCandidate, bool) {
	if !p.initialized || p.active < 0 || p.active >= len(p.routes) {
		return routeCandidate{}, false
	}
	if !p.routes[p.active].healthy || !p.routeEligibleLocked(p.active, p.config.Now()) {
		return routeCandidate{}, false
	}
	return p.routes[p.active].candidate, true
}

func (p *routePool) reconcileActiveHealthLocked(now time.Time) bool {
	if p.active < 0 || p.active >= len(p.routes) || (p.routes[p.active].healthy && p.routeEligibleLocked(p.active, now)) {
		return false
	}
	failed := p.active
	selected := p.fastestHealthyLocked(now, failed)
	if selected >= 0 {
		p.setActiveLocked(selected)
	} else {
		p.setActiveLocked(-1)
	}
	p.resetFastStreaksLocked()
	return true
}

func (p *routePool) setActiveLocked(index int) {
	if p.active == index {
		return
	}
	if p.changed == nil {
		p.changed = make(chan struct{})
	}
	previous := p.changed
	p.active = index
	p.generation++
	close(previous)
	p.changed = make(chan struct{})
}

func routeRequestContext(parent context.Context, snapshot routeSnapshot, timeout time.Duration) (context.Context, func()) {
	base, cancelBase := context.WithCancel(parent)
	requestCtx := base
	cancelTimeout := func() {}
	if timeout > 0 {
		requestCtx, cancelTimeout = context.WithTimeout(base, timeout)
	}
	go func() {
		select {
		case <-snapshot.changed:
			cancelBase()
		case <-requestCtx.Done():
		}
	}()
	return requestCtx, func() {
		cancelTimeout()
		cancelBase()
	}
}

func (p *routePool) routeEligibleLocked(index int, now time.Time) bool {
	return index >= 0 && index < len(p.routes) && (p.routes[index].cooldownUntil.IsZero() || !now.Before(p.routes[index].cooldownUntil))
}

func (p *routePool) eligibleIndicesLocked(now time.Time, excluded int) []int {
	indices := make([]int, 0, len(p.routes))
	for index, state := range p.routes {
		if index == excluded || !p.routeEligibleLocked(index, now) {
			continue
		}
		if state.healthy || !state.cooldownUntil.IsZero() {
			indices = append(indices, index)
		}
	}
	return indices
}

func (p *routePool) fastestHealthyLocked(now time.Time, excluded int) int {
	best := -1
	for index, state := range p.routes {
		if index == excluded || !state.healthy || !p.routeEligibleLocked(index, now) {
			continue
		}
		if best < 0 || state.latency < p.routes[best].latency || (state.latency == p.routes[best].latency && index < best) {
			best = index
		}
	}
	return best
}

func (p *routePool) resetFastStreaksLocked() {
	for index := range p.routes {
		p.routes[index].fastStreak = 0
	}
}

// routeCandidatesFromManifest expands only the maintained HTTP event
// adapters. Unsupported transports are intentionally left out.
func routeCandidatesFromManifest(current string, candidates []model.JoinManifestGatewayCandidate, transport string) []routeCandidate {
	transports := []string{}
	switch strings.TrimSpace(strings.ToLower(transport)) {
	case "auto":
		transports = []string{"long-poll", "poll"}
	case "long-poll":
		transports = []string{"long-poll"}
	case "poll":
		transports = []string{"poll"}
	default:
		return nil
	}
	maxURLs := maxGatewayRoutes / len(transports)
	capacity := len(candidates) + 1
	if capacity > maxURLs {
		capacity = maxURLs
	}
	urls := make([]string, 0, capacity)
	seen := make(map[string]struct{}, capacity)
	addURL := func(value string) {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if !safeRouteURL(value) {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		urls = append(urls, value)
	}
	addURL(current)
	for _, candidate := range candidates {
		if len(urls) >= maxURLs {
			break
		}
		addURL(candidate.URL)
	}
	routes := make([]routeCandidate, 0, len(urls)*len(transports))
	for _, gatewayURL := range urls {
		for _, routeTransport := range transports {
			if len(routes) >= maxGatewayRoutes {
				return routes
			}
			routes = append(routes, routeCandidate{URL: gatewayURL, Transport: routeTransport})
		}
	}
	return routes
}

func safeRouteURL(value string) bool {
	if value == "" || strings.ContainsAny(value, "\\\x00\r\n") {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Opaque != "" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.ForceQuery {
		return false
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return false
	}
	if strings.Contains(parsed.Path, "\\") {
		return false
	}
	for _, segment := range strings.Split(strings.Trim(parsed.Path, "/"), "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	if parsed.Scheme == "http" {
		return isLocalDevGatewayURL(value)
	}
	return true
}
