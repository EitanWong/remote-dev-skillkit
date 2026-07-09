package connectionhealth

import (
	"strings"
	"time"
)

const PlanSchemaVersion = "rdev.connection-health-plan.v1"

type Candidate struct {
	URL      string `json:"url"`
	Kind     string `json:"kind,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

type Attempt struct {
	Phase     string    `json:"phase"`
	URL       string    `json:"url,omitempty"`
	Transport string    `json:"transport,omitempty"`
	OK        bool      `json:"ok"`
	Error     string    `json:"error,omitempty"`
	At        time.Time `json:"at,omitempty"`
}

type Plan struct {
	SchemaVersion      string      `json:"schema_version"`
	Candidates         []Candidate `json:"candidates,omitempty"`
	Attempts           []Attempt   `json:"attempts,omitempty"`
	SelectedGatewayURL string      `json:"selected_gateway_url,omitempty"`
	Status             string      `json:"status"`
	AgentNextAction    string      `json:"agent_next_action"`
}

func NewPlan(candidates []Candidate) Plan {
	plan := Plan{
		SchemaVersion:   PlanSchemaVersion,
		Candidates:      normalizeCandidates(candidates),
		Status:          "planned",
		AgentNextAction: "try the first reachable gateway candidate",
	}
	return plan
}

func (p Plan) WithAttempt(attempt Attempt) Plan {
	attempt.URL = strings.TrimRight(strings.TrimSpace(attempt.URL), "/")
	attempt.Phase = strings.TrimSpace(attempt.Phase)
	attempt.Transport = strings.TrimSpace(attempt.Transport)
	attempt.Error = strings.TrimSpace(attempt.Error)
	next := p.clone()
	next.Attempts = append(next.Attempts, attempt)
	if attempt.OK && attempt.URL != "" {
		next.SelectedGatewayURL = attempt.URL
	}
	next.deriveStatus()
	return next
}

func (p Plan) SelectGateway(rawURL string) Plan {
	next := p.clone()
	next.SelectedGatewayURL = strings.TrimRight(strings.TrimSpace(rawURL), "/")
	next.deriveStatus()
	return next
}

func (p Plan) clone() Plan {
	next := p
	next.Candidates = append([]Candidate(nil), p.Candidates...)
	next.Attempts = append([]Attempt(nil), p.Attempts...)
	return next
}

func (p *Plan) deriveStatus() {
	if len(p.Attempts) == 0 {
		if strings.TrimSpace(p.SelectedGatewayURL) != "" {
			p.Status = "gateway-selected"
			p.AgentNextAction = "continue host registration with the selected gateway"
			return
		}
		p.Status = "planned"
		p.AgentNextAction = "try the first reachable gateway candidate"
		return
	}
	seenFailure := false
	latest := p.Attempts[len(p.Attempts)-1]
	for _, attempt := range p.Attempts {
		if !attempt.OK {
			seenFailure = true
			break
		}
	}
	if latest.OK {
		if seenFailure {
			p.Status = "recovered"
			p.AgentNextAction = "connection recovered; continue normal session join or task transport"
			return
		}
		p.Status = "connected"
		p.AgentNextAction = "continue normal session join or task transport"
		return
	}
	if hasRemainingCandidate(p.Candidates, p.Attempts) {
		p.Status = "gateway-switching"
		p.AgentNextAction = "continue with the next signed gateway candidate"
		return
	}
	p.Status = "reconnecting"
	p.AgentNextAction = "retry the current connection path with bounded backoff"
}

func normalizeCandidates(candidates []Candidate) []Candidate {
	out := make([]Candidate, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate.URL = strings.TrimRight(strings.TrimSpace(candidate.URL), "/")
		candidate.Kind = strings.TrimSpace(candidate.Kind)
		if candidate.URL == "" || seen[candidate.URL] {
			continue
		}
		seen[candidate.URL] = true
		out = append(out, candidate)
	}
	return out
}

func hasRemainingCandidate(candidates []Candidate, attempts []Attempt) bool {
	if len(candidates) == 0 {
		return false
	}
	failed := map[string]bool{}
	for _, attempt := range attempts {
		if attempt.URL != "" && !attempt.OK {
			failed[attempt.URL] = true
		}
	}
	for _, candidate := range candidates {
		if !failed[candidate.URL] {
			return true
		}
	}
	return false
}
