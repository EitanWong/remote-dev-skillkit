package model

import "time"

type HostStatus string

const (
	HostStatusPending HostStatus = "pending"
	HostStatusActive  HostStatus = "active"
	HostStatusRevoked HostStatus = "revoked"
)

type Host struct {
	ID           string     `json:"id"`
	TicketID     string     `json:"ticket_id"`
	Mode         HostMode   `json:"mode"`
	Status       HostStatus `json:"status"`
	Name         string     `json:"name"`
	OS           string     `json:"os"`
	Arch         string     `json:"arch"`
	Capabilities []string   `json:"capabilities"`
	ApprovedAt   *time.Time `json:"approved_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	LastSeenAt   time.Time  `json:"last_seen_at"`
}

type HostRegistration struct {
	TicketCode   string   `json:"ticket_code"`
	Name         string   `json:"name"`
	OS           string   `json:"os"`
	Arch         string   `json:"arch"`
	Capabilities []string `json:"capabilities"`
}

func NewHost(ticket Ticket, registration HostRegistration, now time.Time) (Host, error) {
	id, err := newID("hst")
	if err != nil {
		return Host{}, err
	}
	return Host{
		ID:           id,
		TicketID:     ticket.ID,
		Mode:         ticket.Mode,
		Status:       HostStatusPending,
		Name:         registration.Name,
		OS:           registration.OS,
		Arch:         registration.Arch,
		Capabilities: registration.Capabilities,
		CreatedAt:    now.UTC(),
		LastSeenAt:   now.UTC(),
	}, nil
}
