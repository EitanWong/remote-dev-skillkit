package model

import "time"

type AuditEvent struct {
	Sequence int       `json:"sequence"`
	Actor    string    `json:"actor"`
	Action   string    `json:"action"`
	TargetID string    `json:"target_id"`
	Message  string    `json:"message"`
	At       time.Time `json:"at"`
}
