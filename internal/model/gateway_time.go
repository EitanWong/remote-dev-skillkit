package model

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

const GatewayTimeProofSchemaVersion = "rdev.gateway-time-proof.v1"
const GatewayTimeProofPurposeJoinManifest = "join-manifest"

type GatewayTimeProof struct {
	SchemaVersion string    `json:"schema_version"`
	Purpose       string    `json:"purpose"`
	SubjectSHA256 string    `json:"subject_sha256"`
	GatewayTime   time.Time `json:"gateway_time"`
	ValidUntil    time.Time `json:"valid_until"`
	SigningAlg    string    `json:"signing_alg"`
	SigningKeyID  string    `json:"signing_key_id"`
	Signature     string    `json:"signature,omitempty"`
}

func NewGatewayTimeProof(purpose string, subject any, signingKeyID string, privateKey ed25519.PrivateKey, now time.Time, ttl time.Duration) (GatewayTimeProof, error) {
	if purpose == "" {
		return GatewayTimeProof{}, fmt.Errorf("gateway time proof purpose is required")
	}
	if signingKeyID == "" {
		return GatewayTimeProof{}, fmt.Errorf("gateway time proof signing key id is required")
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	subjectSHA256, err := gatewayTimeSubjectSHA256(subject)
	if err != nil {
		return GatewayTimeProof{}, err
	}
	proof := GatewayTimeProof{
		SchemaVersion: GatewayTimeProofSchemaVersion,
		Purpose:       purpose,
		SubjectSHA256: subjectSHA256,
		GatewayTime:   now.UTC(),
		ValidUntil:    now.UTC().Add(ttl),
		SigningAlg:    SigningAlgEd25519,
		SigningKeyID:  signingKeyID,
	}
	message, err := proof.signingBytes()
	if err != nil {
		return GatewayTimeProof{}, err
	}
	proof.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, message))
	return proof, nil
}

func (p GatewayTimeProof) Verify(root TrustBundle, purpose string, subject any) (time.Time, error) {
	if p.SchemaVersion != GatewayTimeProofSchemaVersion {
		return time.Time{}, fmt.Errorf("unsupported gateway time proof schema %q", p.SchemaVersion)
	}
	if p.Purpose != purpose {
		return time.Time{}, fmt.Errorf("gateway time proof purpose mismatch: expected %q got %q", purpose, p.Purpose)
	}
	if p.SigningAlg != SigningAlgEd25519 || p.SigningKeyID == "" {
		return time.Time{}, fmt.Errorf("unsupported gateway time proof signing metadata")
	}
	if p.SigningKeyID != root.SigningKeyID {
		return time.Time{}, fmt.Errorf("gateway time proof signing key id mismatch")
	}
	if p.GatewayTime.IsZero() || p.ValidUntil.IsZero() || p.GatewayTime.After(p.ValidUntil) {
		return time.Time{}, fmt.Errorf("gateway time proof validity window is invalid")
	}
	subjectSHA256, err := gatewayTimeSubjectSHA256(subject)
	if err != nil {
		return time.Time{}, err
	}
	if p.SubjectSHA256 != subjectSHA256 {
		return time.Time{}, fmt.Errorf("gateway time proof subject hash mismatch")
	}
	signature, err := base64.RawURLEncoding.DecodeString(p.Signature)
	if err != nil {
		return time.Time{}, fmt.Errorf("decode gateway time proof signature: %w", err)
	}
	publicKey, err := root.Ed25519PublicKey()
	if err != nil {
		return time.Time{}, err
	}
	message, err := p.signingBytes()
	if err != nil {
		return time.Time{}, err
	}
	if !ed25519.Verify(publicKey, message, signature) {
		return time.Time{}, fmt.Errorf("gateway time proof signature invalid")
	}
	return p.GatewayTime.UTC(), nil
}

func (p GatewayTimeProof) signingBytes() ([]byte, error) {
	unsigned := p
	unsigned.Signature = ""
	return json.Marshal(unsigned)
}

func gatewayTimeSubjectSHA256(subject any) (string, error) {
	content, err := json.Marshal(subject)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
