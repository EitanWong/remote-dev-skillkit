package model

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

type HostStatus string

const (
	HostStatusPending HostStatus = "pending"
	HostStatusActive  HostStatus = "active"
	HostStatusRevoked HostStatus = "revoked"
)

type Host struct {
	ID                  string     `json:"id"`
	TicketID            string     `json:"ticket_id"`
	Mode                HostMode   `json:"mode"`
	Status              HostStatus `json:"status"`
	Name                string     `json:"name"`
	OS                  string     `json:"os"`
	Arch                string     `json:"arch"`
	Capabilities        []string   `json:"capabilities"`
	IdentityKeyID       string     `json:"identity_key_id,omitempty"`
	IdentityPublicKey   string     `json:"identity_public_key,omitempty"`
	IdentityFingerprint string     `json:"identity_fingerprint,omitempty"`
	ApprovedAt          *time.Time `json:"approved_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	LastSeenAt          time.Time  `json:"last_seen_at"`
}

type HostRegistration struct {
	TicketCode          string   `json:"ticket_code"`
	Name                string   `json:"name"`
	OS                  string   `json:"os"`
	Arch                string   `json:"arch"`
	Capabilities        []string `json:"capabilities"`
	IdentityKeyID       string   `json:"identity_key_id,omitempty"`
	IdentityPublicKey   string   `json:"identity_public_key,omitempty"`
	IdentityFingerprint string   `json:"identity_fingerprint,omitempty"`
}

func NewHost(ticket Ticket, registration HostRegistration, now time.Time) (Host, error) {
	if err := validateHostRegistrationIdentity(registration); err != nil {
		return Host{}, err
	}
	id, err := newID("hst")
	if err != nil {
		return Host{}, err
	}
	return Host{
		ID:                  id,
		TicketID:            ticket.ID,
		Mode:                ticket.Mode,
		Status:              HostStatusPending,
		Name:                registration.Name,
		OS:                  registration.OS,
		Arch:                registration.Arch,
		Capabilities:        registration.Capabilities,
		IdentityKeyID:       registration.IdentityKeyID,
		IdentityPublicKey:   registration.IdentityPublicKey,
		IdentityFingerprint: registration.IdentityFingerprint,
		CreatedAt:           now.UTC(),
		LastSeenAt:          now.UTC(),
	}, nil
}

func validateHostRegistrationIdentity(registration HostRegistration) error {
	if registration.IdentityKeyID == "" && registration.IdentityPublicKey == "" && registration.IdentityFingerprint == "" {
		return nil
	}
	if registration.IdentityKeyID == "" || registration.IdentityPublicKey == "" || registration.IdentityFingerprint == "" {
		return fmt.Errorf("host identity key id, public key, and fingerprint are required together")
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(registration.IdentityPublicKey)
	if err != nil {
		return fmt.Errorf("decode host identity public key: %w", err)
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid host identity public key length %d", len(publicKey))
	}
	sum := sha256.Sum256(publicKey)
	expected := "sha256:" + hex.EncodeToString(sum[:])
	if registration.IdentityFingerprint != expected {
		return fmt.Errorf("host identity fingerprint mismatch")
	}
	return nil
}
