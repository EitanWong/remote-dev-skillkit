package controlplane

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const HostSchemaVersion = "rdev.host.v1"

// Host is the durable directory record for a target identity. The registry is
// keyed by the host's identity fingerprint so reconnects and new sessions
// keep one stable directory entry. DisplayName is operator-controlled and is
// never overwritten by a rejoin; the connector-provided name only seeds a new
// record.
type Host struct {
	SchemaVersion       string        `json:"schema_version"`
	HostID              string        `json:"host_id"`
	IdentityFingerprint string        `json:"identity_fingerprint"`
	DisplayName         string        `json:"display_name"`
	Platform            string        `json:"platform"`
	Capabilities        []string      `json:"capabilities,omitempty"`
	State               EndpointState `json:"state"`
	FirstSeenAt         time.Time     `json:"first_seen_at"`
	LastSeenAt          time.Time     `json:"last_seen_at"`
	LastSessionID       string        `json:"last_session_id,omitempty"`
	LastEndpointID      string        `json:"last_endpoint_id,omitempty"`
}

const (
	MaxHostDisplayNameLength = 64
)

// UpsertHostLocked records or refreshes the directory entry for a joining
// target. Callers must hold the store mutex. The operator-set DisplayName is
// preserved across rejoins; only a fresh record adopts the connector name.
func (s *MemoryStore) UpsertHostLocked(session Session, endpoint Endpoint, now time.Time) {
	if endpoint.Role != EndpointRoleTarget || strings.TrimSpace(endpoint.IdentityFingerprint) == "" {
		return
	}
	fingerprint := endpoint.IdentityFingerprint
	existing, ok := s.hosts[fingerprint]
	if !ok {
		hostID, err := newID("hst")
		if err != nil {
			return
		}
		existing = Host{
			SchemaVersion:       HostSchemaVersion,
			HostID:              hostID,
			IdentityFingerprint: fingerprint,
			DisplayName:         endpoint.Name,
			Platform:            endpoint.Platform,
			FirstSeenAt:         now.UTC(),
		}
	}
	existing.Capabilities = append([]string(nil), endpoint.Capabilities...)
	existing.State = endpoint.State
	existing.LastSeenAt = now.UTC()
	existing.LastSessionID = session.ID
	existing.LastEndpointID = endpoint.ID
	if existing.Platform == "" {
		existing.Platform = endpoint.Platform
	}
	if existing.DisplayName == "" {
		existing.DisplayName = endpoint.Name
	}
	s.hosts[fingerprint] = existing
}

// Hosts returns the durable directory, newest last-seen first.
func (s *MemoryStore) Hosts() []Host {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Host, 0, len(s.hosts))
	for _, host := range s.hosts {
		out = append(out, host.clone())
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastSeenAt.After(out[j].LastSeenAt)
	})
	return out
}

// RenameHost sets the operator-controlled display name for one host record.
func (s *MemoryStore) RenameHost(hostID, displayName string) (Host, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return Host{}, fmt.Errorf("display_name is required")
	}
	if len(displayName) > MaxHostDisplayNameLength {
		return Host{}, fmt.Errorf("display_name exceeds %d characters", MaxHostDisplayNameLength)
	}
	if strings.ContainsAny(displayName, "\r\n\t") {
		return Host{}, fmt.Errorf("display_name must not contain control characters")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for fingerprint, host := range s.hosts {
		if host.HostID == hostID {
			host.DisplayName = displayName
			s.hosts[fingerprint] = host
			return host.clone(), nil
		}
	}
	return Host{}, fmt.Errorf("host %q not found", hostID)
}

func (h Host) clone() Host {
	next := h
	next.Capabilities = append([]string(nil), h.Capabilities...)
	return next
}
