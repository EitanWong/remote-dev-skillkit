package model

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const JoinManifestSchemaVersion = "rdev.join-manifest.v1"

var (
	ErrJoinManifestInvalid   = errors.New("join manifest invalid")
	ErrJoinManifestExpired   = errors.New("join manifest expired")
	ErrJoinManifestSignature = errors.New("join manifest signature invalid")
)

type JoinManifestBootstrap struct {
	DownloadURL    string `json:"download_url,omitempty"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
}

type JoinManifest struct {
	SchemaVersion    string                `json:"schema_version"`
	TicketID         string                `json:"ticket_id"`
	TicketCode       string                `json:"ticket_code"`
	Mode             HostMode              `json:"mode"`
	Reason           string                `json:"reason"`
	Capabilities     []string              `json:"capabilities"`
	IssuedAt         time.Time             `json:"issued_at"`
	ExpiresAt        time.Time             `json:"expires_at"`
	GatewayURL       string                `json:"gateway_url"`
	JoinURL          string                `json:"join_url"`
	Trust            TrustBundle           `json:"trust"`
	TrustFingerprint string                `json:"trust_fingerprint"`
	Bootstrap        JoinManifestBootstrap `json:"bootstrap,omitempty"`
	SigningAlg       string                `json:"signing_alg"`
	SigningKeyID     string                `json:"signing_key_id"`
	Signature        string                `json:"signature,omitempty"`
}

type JoinManifestSpec struct {
	GatewayURL   string
	JoinURL      string
	Trust        TrustBundle
	Bootstrap    JoinManifestBootstrap
	SigningKeyID string
}

func NewJoinManifest(ticket Ticket, spec JoinManifestSpec, now time.Time) (JoinManifest, error) {
	if ticket.ID == "" || ticket.Code == "" {
		return JoinManifest{}, fmt.Errorf("%w: ticket identity is required", ErrJoinManifestInvalid)
	}
	if spec.GatewayURL == "" {
		return JoinManifest{}, fmt.Errorf("%w: gateway url is required", ErrJoinManifestInvalid)
	}
	if spec.JoinURL == "" {
		spec.JoinURL = spec.GatewayURL + "/join/" + ticket.Code
	}
	if spec.SigningKeyID == "" {
		spec.SigningKeyID = spec.Trust.SigningKeyID
	}
	fingerprint, err := spec.Trust.Fingerprint()
	if err != nil {
		return JoinManifest{}, err
	}
	return JoinManifest{
		SchemaVersion:    JoinManifestSchemaVersion,
		TicketID:         ticket.ID,
		TicketCode:       ticket.Code,
		Mode:             ticket.Mode,
		Reason:           ticket.Reason,
		Capabilities:     append([]string(nil), ticket.Capabilities...),
		IssuedAt:         now.UTC(),
		ExpiresAt:        ticket.ExpiresAt.UTC(),
		GatewayURL:       spec.GatewayURL,
		JoinURL:          spec.JoinURL,
		Trust:            spec.Trust,
		TrustFingerprint: fingerprint,
		Bootstrap:        spec.Bootstrap,
		SigningAlg:       JobEnvelopeSigningAlg,
		SigningKeyID:     spec.SigningKeyID,
	}, nil
}

func (m JoinManifest) Sign(privateKey ed25519.PrivateKey) (JoinManifest, error) {
	if err := m.validateForSigning(); err != nil {
		return JoinManifest{}, err
	}
	message, err := m.signingBytes()
	if err != nil {
		return JoinManifest{}, err
	}
	signature := ed25519.Sign(privateKey, message)
	m.Signature = base64.RawURLEncoding.EncodeToString(signature)
	return m, nil
}

func (m JoinManifest) Verify(now time.Time) error {
	if err := m.validateForSigning(); err != nil {
		return err
	}
	if m.Signature == "" {
		return fmt.Errorf("%w: missing signature", ErrJoinManifestSignature)
	}
	if now.UTC().After(m.ExpiresAt) {
		return ErrJoinManifestExpired
	}
	publicKey, err := m.Trust.Ed25519PublicKey()
	if err != nil {
		return err
	}
	signature, err := base64.RawURLEncoding.DecodeString(m.Signature)
	if err != nil {
		return fmt.Errorf("%w: malformed signature", ErrJoinManifestSignature)
	}
	unsigned := m
	unsigned.Signature = ""
	message, err := unsigned.signingBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, message, signature) {
		return ErrJoinManifestSignature
	}
	return nil
}

func (m JoinManifest) validateForSigning() error {
	if m.SchemaVersion != JoinManifestSchemaVersion {
		return fmt.Errorf("%w: unsupported schema version", ErrJoinManifestInvalid)
	}
	if m.TicketID == "" || m.TicketCode == "" || m.GatewayURL == "" || m.JoinURL == "" {
		return fmt.Errorf("%w: missing required fields", ErrJoinManifestInvalid)
	}
	if !m.Mode.Valid() {
		return fmt.Errorf("%w: invalid host mode", ErrJoinManifestInvalid)
	}
	if m.SigningAlg != JobEnvelopeSigningAlg || m.SigningKeyID == "" {
		return fmt.Errorf("%w: unsupported signing metadata", ErrJoinManifestInvalid)
	}
	if m.Trust.SigningKeyID != m.SigningKeyID {
		return fmt.Errorf("%w: trust key id mismatch", ErrJoinManifestInvalid)
	}
	fingerprint, err := m.Trust.Fingerprint()
	if err != nil {
		return err
	}
	if m.TrustFingerprint != fingerprint {
		return fmt.Errorf("%w: trust fingerprint mismatch", ErrJoinManifestInvalid)
	}
	if m.IssuedAt.IsZero() || m.ExpiresAt.IsZero() || !m.IssuedAt.Before(m.ExpiresAt) {
		return fmt.Errorf("%w: invalid validity window", ErrJoinManifestInvalid)
	}
	return nil
}

func (m JoinManifest) signingBytes() ([]byte, error) {
	unsigned := m
	unsigned.Signature = ""
	return json.Marshal(unsigned)
}
