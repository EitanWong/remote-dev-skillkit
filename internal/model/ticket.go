package model

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
	"time"
)

type HostMode string
type TicketStatus string

const (
	HostModeAttendedTemporary HostMode = "attended-temporary"
	HostModeManaged           HostMode = "managed"
	HostModeBreakGlass        HostMode = "break-glass"

	TicketStatusActive  TicketStatus = "active"
	TicketStatusProbing TicketStatus = "probing"
	TicketStatusRevoked TicketStatus = "revoked"
)

type Ticket struct {
	ID           string            `json:"id"`
	Code         string            `json:"code"`
	Mode         HostMode          `json:"mode"`
	Status       TicketStatus      `json:"status"`
	TTLSeconds   int               `json:"ttl_seconds"`
	Capabilities []string          `json:"capabilities"`
	Reason       string            `json:"reason"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	ExpiresAt    time.Time         `json:"expires_at"`
}

func NewTicket(mode HostMode, ttlSeconds int, capabilities []string, reason string, now time.Time) (Ticket, error) {
	code, err := newJoinCode()
	if err != nil {
		return Ticket{}, err
	}
	id, err := newID("tkt")
	if err != nil {
		return Ticket{}, err
	}
	return Ticket{
		ID:           id,
		Code:         code,
		Mode:         mode,
		Status:       TicketStatusActive,
		TTLSeconds:   ttlSeconds,
		Capabilities: capabilities,
		Reason:       reason,
		CreatedAt:    now.UTC(),
		ExpiresAt:    now.Add(time.Duration(ttlSeconds) * time.Second).UTC(),
	}, nil
}

func (m HostMode) Valid() bool {
	switch m {
	case HostModeAttendedTemporary, HostModeManaged, HostModeBreakGlass:
		return true
	default:
		return false
	}
}

func newJoinCode() (string, error) {
	var raw [5]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:])
	if len(encoded) > 8 {
		encoded = encoded[:8]
	}
	return encoded[:4] + "-" + encoded[4:], nil
}

func newID(prefix string) (string, error) {
	var raw [10]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	encoded := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:]))
	return prefix + "_" + encoded, nil
}
