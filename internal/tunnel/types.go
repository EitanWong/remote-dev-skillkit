package tunnel

import (
	"context"
	"time"
)

const AvailabilitySchemaVersion = "rdev.tunnel-availability.v1"
const ReadinessSchemaVersion = "rdev.connection-readiness.v2"

type RegionProfile string

const (
	RegionGlobal     RegionProfile = "global"
	RegionCNMainland RegionProfile = "cn-mainland"
)

type EvidenceStatus string

const (
	EvidenceUnknown   EvidenceStatus = "unknown"
	EvidenceCandidate EvidenceStatus = "candidate"
	EvidenceVerified  EvidenceStatus = "verified"
	EvidenceDegraded  EvidenceStatus = "degraded"
	EvidenceBlocked   EvidenceStatus = "blocked"
)

type FailureDomains struct {
	AuthoritativeDNS      string `json:"authoritative_dns,omitempty"`
	EdgeNetwork           string `json:"edge_network,omitempty"`
	OriginNetwork         string `json:"origin_network,omitempty"`
	ControlPlane          string `json:"control_plane,omitempty"`
	CertificateDependency string `json:"certificate_dependency,omitempty"`
}

type ProviderMetadata struct {
	ID                    string         `json:"id"`
	DisplayName           string         `json:"display_name"`
	Protocols             []string       `json:"protocols"`
	Anonymous             bool           `json:"anonymous"`
	CredentialRequirement string         `json:"credential_requirement,omitempty"`
	Executable            string         `json:"executable"`
	DocumentationURL      string         `json:"documentation_url"`
	TermsURL              string         `json:"terms_url,omitempty"`
	DefaultAutomatic      bool           `json:"default_automatic"`
	AutomaticPriority     int            `json:"automatic_priority,omitempty"`
	RequiresSSHPin        bool           `json:"requires_ssh_pin"`
	FailureDomains        FailureDomains `json:"failure_domains"`
}

type StartRequest struct {
	LocalURL       string
	LocalPort      string
	KnownHostsFile string
}

type Candidate struct {
	ProviderID     string         `json:"provider_id"`
	URL            string         `json:"url"`
	FailureDomains FailureDomains `json:"failure_domains"`
}

type Handle interface {
	Candidate() Candidate
	Wait() <-chan error
	Stop(context.Context) error
}

type Provider interface {
	ID() string
	Metadata() ProviderMetadata
	// Start must return promptly after ctx cancellation. The context remains
	// valid for the returned handle's lifetime; Wait signals only after provider
	// resources are reaped. Manager cancels ctx on startup timeout, parent
	// cancellation, handle stop, or handle exit.
	Start(context.Context, StartRequest) (Handle, error)
}

type Policy struct {
	Region                RegionProfile
	Now                   time.Time
	AllowedProviderIDs    []string
	RestrictProviders     bool
	AllowNonDefault       bool
	AllowUnverifiedGlobal bool
}

type Eligibility struct {
	Eligible bool
	Reason   string
	Evidence *RegionalEvidence
}

func cloneMetadata(meta ProviderMetadata) ProviderMetadata {
	cloned := meta
	cloned.Protocols = cloneStrings(meta.Protocols)
	return cloned
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}
