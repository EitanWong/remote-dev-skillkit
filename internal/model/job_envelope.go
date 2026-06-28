package model

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	JobEnvelopeSchemaVersion = "rdev.job.v1"
	JobEnvelopeSigningAlg    = "ed25519"
	DefaultJobTTLSeconds     = 1800
	DefaultMaxOutputBytes    = 1024 * 1024
)

var (
	ErrEnvelopeInvalid   = errors.New("job envelope invalid")
	ErrEnvelopeExpired   = errors.New("job envelope expired")
	ErrEnvelopeSignature = errors.New("job envelope signature invalid")
)

type JobWorkspace struct {
	Root       string   `json:"root,omitempty"`
	WriteScope []string `json:"write_scope,omitempty"`
	Branch     string   `json:"branch,omitempty"`
}

type JobLimits struct {
	MaxDurationSeconds int    `json:"max_duration_seconds"`
	MaxOutputBytes     int    `json:"max_output_bytes"`
	Network            string `json:"network"`
}

type JobEnvelopeSpec struct {
	OperatorID        string
	Workspace         JobWorkspace
	Capabilities      []string
	Limits            JobLimits
	ApprovalsRequired []string
	Payload           map[string]any
	TTLSeconds        int
	SigningKeyID      string
}

type JobEnvelope struct {
	SchemaVersion     string         `json:"schema_version"`
	JobID             string         `json:"job_id"`
	HostID            string         `json:"host_id"`
	TicketID          string         `json:"ticket_id,omitempty"`
	OperatorID        string         `json:"operator_id"`
	IssuedAt          time.Time      `json:"issued_at"`
	ExpiresAt         time.Time      `json:"expires_at"`
	Nonce             string         `json:"nonce"`
	Mode              HostMode       `json:"mode"`
	Adapter           string         `json:"adapter"`
	Intent            string         `json:"intent"`
	Workspace         JobWorkspace   `json:"workspace"`
	Capabilities      []string       `json:"capabilities"`
	Limits            JobLimits      `json:"limits"`
	ApprovalsRequired []string       `json:"approvals_required,omitempty"`
	Payload           map[string]any `json:"payload,omitempty"`
	SigningAlg        string         `json:"signing_alg"`
	SigningKeyID      string         `json:"signing_key_id"`
	Signature         string         `json:"signature,omitempty"`
}

func NewJobEnvelope(job Job, host Host, ticket Ticket, spec JobEnvelopeSpec, now time.Time) (JobEnvelope, error) {
	if job.ID == "" || host.ID == "" || job.HostID != host.ID {
		return JobEnvelope{}, fmt.Errorf("%w: job and host must be bound", ErrEnvelopeInvalid)
	}
	if job.Adapter == "" || job.Intent == "" {
		return JobEnvelope{}, fmt.Errorf("%w: adapter and intent are required", ErrEnvelopeInvalid)
	}
	if spec.OperatorID == "" {
		spec.OperatorID = "operator"
	}
	if spec.TTLSeconds <= 0 {
		spec.TTLSeconds = DefaultJobTTLSeconds
	}
	if spec.SigningKeyID == "" {
		spec.SigningKeyID = "gateway-dev"
	}
	if len(spec.Capabilities) == 0 {
		spec.Capabilities = append([]string(nil), host.Capabilities...)
	}
	spec.Limits = normalizeJobLimits(spec.Limits)

	issuedAt := now.UTC()
	expiresAt := issuedAt.Add(time.Duration(spec.TTLSeconds) * time.Second)
	if !ticket.ExpiresAt.IsZero() && ticket.ExpiresAt.Before(expiresAt) {
		expiresAt = ticket.ExpiresAt.UTC()
	}
	nonce, err := newNonce()
	if err != nil {
		return JobEnvelope{}, err
	}
	return JobEnvelope{
		SchemaVersion:     JobEnvelopeSchemaVersion,
		JobID:             job.ID,
		HostID:            host.ID,
		TicketID:          host.TicketID,
		OperatorID:        spec.OperatorID,
		IssuedAt:          issuedAt,
		ExpiresAt:         expiresAt,
		Nonce:             nonce,
		Mode:              host.Mode,
		Adapter:           job.Adapter,
		Intent:            job.Intent,
		Workspace:         spec.Workspace,
		Capabilities:      append([]string(nil), spec.Capabilities...),
		Limits:            spec.Limits,
		ApprovalsRequired: append([]string(nil), spec.ApprovalsRequired...),
		Payload:           cloneMap(spec.Payload),
		SigningAlg:        JobEnvelopeSigningAlg,
		SigningKeyID:      spec.SigningKeyID,
	}, nil
}

func (e JobEnvelope) Sign(privateKey ed25519.PrivateKey) (JobEnvelope, error) {
	if err := e.validateForSigning(); err != nil {
		return JobEnvelope{}, err
	}
	message, err := e.signingBytes()
	if err != nil {
		return JobEnvelope{}, err
	}
	signature := ed25519.Sign(privateKey, message)
	e.Signature = base64.RawURLEncoding.EncodeToString(signature)
	return e, nil
}

func (e JobEnvelope) VerifyForHost(publicKey ed25519.PublicKey, hostID string, now time.Time) error {
	if hostID == "" || e.HostID != hostID {
		return fmt.Errorf("%w: host binding mismatch", ErrEnvelopeInvalid)
	}
	return e.Verify(publicKey, now)
}

func (e JobEnvelope) Verify(publicKey ed25519.PublicKey, now time.Time) error {
	if err := e.validateForSigning(); err != nil {
		return err
	}
	if e.Signature == "" {
		return fmt.Errorf("%w: missing signature", ErrEnvelopeSignature)
	}
	if now.UTC().After(e.ExpiresAt) {
		return ErrEnvelopeExpired
	}
	signature, err := base64.RawURLEncoding.DecodeString(e.Signature)
	if err != nil {
		return fmt.Errorf("%w: malformed signature", ErrEnvelopeSignature)
	}
	unsigned := e
	unsigned.Signature = ""
	message, err := unsigned.signingBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, message, signature) {
		return ErrEnvelopeSignature
	}
	return nil
}

func (e JobEnvelope) validateForSigning() error {
	if e.SchemaVersion != JobEnvelopeSchemaVersion {
		return fmt.Errorf("%w: unsupported schema version", ErrEnvelopeInvalid)
	}
	if e.JobID == "" || e.HostID == "" || e.OperatorID == "" || e.Nonce == "" {
		return fmt.Errorf("%w: missing required identity fields", ErrEnvelopeInvalid)
	}
	if !e.Mode.Valid() {
		return fmt.Errorf("%w: invalid host mode", ErrEnvelopeInvalid)
	}
	if e.Adapter == "" || e.Intent == "" {
		return fmt.Errorf("%w: adapter and intent are required", ErrEnvelopeInvalid)
	}
	if e.SigningAlg != JobEnvelopeSigningAlg || e.SigningKeyID == "" {
		return fmt.Errorf("%w: unsupported signing metadata", ErrEnvelopeInvalid)
	}
	if e.IssuedAt.IsZero() || e.ExpiresAt.IsZero() || !e.IssuedAt.Before(e.ExpiresAt) {
		return fmt.Errorf("%w: invalid validity window", ErrEnvelopeInvalid)
	}
	if e.Limits.MaxDurationSeconds <= 0 || e.Limits.MaxOutputBytes <= 0 || e.Limits.Network == "" {
		return fmt.Errorf("%w: invalid limits", ErrEnvelopeInvalid)
	}
	return nil
}

func (e JobEnvelope) signingBytes() ([]byte, error) {
	unsigned := e
	unsigned.Signature = ""
	return json.Marshal(unsigned)
}

func normalizeJobLimits(limits JobLimits) JobLimits {
	if limits.MaxDurationSeconds <= 0 {
		limits.MaxDurationSeconds = DefaultJobTTLSeconds
	}
	if limits.MaxOutputBytes <= 0 {
		limits.MaxOutputBytes = DefaultMaxOutputBytes
	}
	if limits.Network == "" {
		limits.Network = "default-deny"
	}
	return limits
}

func cloneMap(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func newNonce() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
