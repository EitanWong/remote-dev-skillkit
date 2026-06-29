package model

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const ApprovalTokenSchemaVersion = "rdev.approval-token.v1"

var (
	ErrApprovalTokenInvalid   = errors.New("approval token invalid")
	ErrApprovalTokenExpired   = errors.New("approval token expired")
	ErrApprovalTokenSignature = errors.New("approval token signature invalid")
	ErrApprovalTokenConsumed  = errors.New("approval token consumed")
)

type ApprovalTokenSpec struct {
	TokenID      string
	JobID        string
	HostID       string
	ApprovalID   string
	Operation    string
	OperatorID   string
	Source       string
	IssuedAt     time.Time
	ExpiresAt    time.Time
	SigningKeyID string
}

type ApprovalToken struct {
	SchemaVersion string     `json:"schema_version"`
	TokenID       string     `json:"token_id"`
	JobID         string     `json:"job_id"`
	HostID        string     `json:"host_id"`
	ApprovalID    string     `json:"approval_id"`
	Operation     string     `json:"operation"`
	OperatorID    string     `json:"operator_id"`
	Source        string     `json:"source"`
	IssuedAt      time.Time  `json:"issued_at"`
	ExpiresAt     time.Time  `json:"expires_at"`
	ConsumedAt    *time.Time `json:"consumed_at,omitempty"`
	SigningAlg    string     `json:"signing_alg"`
	SigningKeyID  string     `json:"signing_key_id"`
	Signature     string     `json:"signature,omitempty"`
}

func NewApprovalToken(spec ApprovalTokenSpec, now time.Time) (ApprovalToken, error) {
	if spec.TokenID == "" {
		id, err := newID("apr")
		if err != nil {
			return ApprovalToken{}, err
		}
		spec.TokenID = id
	}
	if spec.OperatorID == "" {
		spec.OperatorID = "operator"
	}
	if spec.Source == "" {
		spec.Source = "operator"
	}
	if spec.SigningKeyID == "" {
		spec.SigningKeyID = "gateway-dev"
	}
	if spec.IssuedAt.IsZero() {
		spec.IssuedAt = now.UTC()
	}
	if spec.ExpiresAt.IsZero() {
		spec.ExpiresAt = spec.IssuedAt.Add(10 * time.Minute)
	}
	token := ApprovalToken{
		SchemaVersion: ApprovalTokenSchemaVersion,
		TokenID:       spec.TokenID,
		JobID:         spec.JobID,
		HostID:        spec.HostID,
		ApprovalID:    spec.ApprovalID,
		Operation:     spec.Operation,
		OperatorID:    spec.OperatorID,
		Source:        spec.Source,
		IssuedAt:      spec.IssuedAt.UTC(),
		ExpiresAt:     spec.ExpiresAt.UTC(),
		SigningAlg:    JobEnvelopeSigningAlg,
		SigningKeyID:  spec.SigningKeyID,
	}
	if err := token.validateForSigning(); err != nil {
		return ApprovalToken{}, err
	}
	return token, nil
}

func (t ApprovalToken) Sign(privateKey ed25519.PrivateKey) (ApprovalToken, error) {
	if err := t.validateForSigning(); err != nil {
		return ApprovalToken{}, err
	}
	message, err := t.signingBytes()
	if err != nil {
		return ApprovalToken{}, err
	}
	t.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, message))
	return t, nil
}

func (t ApprovalToken) Verify(root TrustBundle, jobID, hostID, operation string, now time.Time) error {
	if err := t.validateForSigning(); err != nil {
		return err
	}
	if t.Signature == "" {
		return fmt.Errorf("%w: missing signature", ErrApprovalTokenSignature)
	}
	if t.ConsumedAt != nil {
		return ErrApprovalTokenConsumed
	}
	if root.SigningKeyID != t.SigningKeyID {
		return fmt.Errorf("%w: trust root key id mismatch", ErrApprovalTokenInvalid)
	}
	if t.JobID != jobID || t.HostID != hostID || t.Operation != operation {
		return fmt.Errorf("%w: scope mismatch", ErrApprovalTokenInvalid)
	}
	now = now.UTC()
	if now.Before(t.IssuedAt.UTC()) || now.After(t.ExpiresAt.UTC()) {
		return ErrApprovalTokenExpired
	}
	publicKey, err := root.Ed25519PublicKey()
	if err != nil {
		return err
	}
	signature, err := base64.RawURLEncoding.DecodeString(t.Signature)
	if err != nil {
		return fmt.Errorf("%w: malformed signature", ErrApprovalTokenSignature)
	}
	message, err := t.signingBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, message, signature) {
		return ErrApprovalTokenSignature
	}
	return nil
}

func (t ApprovalToken) Consume(now time.Time) ApprovalToken {
	consumedAt := now.UTC()
	t.ConsumedAt = &consumedAt
	t.Signature = ""
	return t
}

func (t ApprovalToken) validateForSigning() error {
	if t.SchemaVersion != ApprovalTokenSchemaVersion {
		return fmt.Errorf("%w: unsupported schema version", ErrApprovalTokenInvalid)
	}
	if t.TokenID == "" || t.JobID == "" || t.HostID == "" || t.ApprovalID == "" || t.Operation == "" {
		return fmt.Errorf("%w: missing token scope", ErrApprovalTokenInvalid)
	}
	if t.OperatorID == "" || t.Source == "" {
		return fmt.Errorf("%w: missing approval authority", ErrApprovalTokenInvalid)
	}
	if t.IssuedAt.IsZero() || t.ExpiresAt.IsZero() || !t.IssuedAt.Before(t.ExpiresAt) {
		return fmt.Errorf("%w: invalid validity window", ErrApprovalTokenInvalid)
	}
	if t.SigningAlg != JobEnvelopeSigningAlg || t.SigningKeyID == "" {
		return fmt.Errorf("%w: unsupported signing metadata", ErrApprovalTokenInvalid)
	}
	return nil
}

func (t ApprovalToken) signingBytes() ([]byte, error) {
	unsigned := t
	unsigned.Signature = ""
	return json.Marshal(unsigned)
}
