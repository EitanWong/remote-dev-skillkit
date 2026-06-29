package model

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	SignedTrustBundleSchemaVersion = "rdev.trust-bundle.v1"
	TrustKeyStatusActive           = "active"
	TrustKeyStatusRetired          = "retired"
	TrustKeyStatusRevoked          = "revoked"
)

var (
	ErrTrustBundleInvalid   = errors.New("trust bundle invalid")
	ErrTrustBundleExpired   = errors.New("trust bundle expired")
	ErrTrustBundleSignature = errors.New("trust bundle signature invalid")
	ErrTrustKeyRevoked      = errors.New("trust key revoked")
)

type TrustKey struct {
	KeyID         string     `json:"key_id"`
	SigningAlg    string     `json:"signing_alg"`
	PublicKey     string     `json:"public_key"`
	Status        string     `json:"status"`
	NotBefore     time.Time  `json:"not_before"`
	NotAfter      *time.Time `json:"not_after,omitempty"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	RevokedReason string     `json:"revoked_reason,omitempty"`
}

type SignedTrustBundle struct {
	SchemaVersion      string     `json:"schema_version"`
	BundleID           string     `json:"bundle_id"`
	Sequence           int        `json:"sequence"`
	IssuedAt           time.Time  `json:"issued_at"`
	NotBefore          time.Time  `json:"not_before"`
	NotAfter           time.Time  `json:"not_after"`
	PreviousBundleHash string     `json:"previous_bundle_hash,omitempty"`
	Keys               []TrustKey `json:"keys"`
	SigningAlg         string     `json:"signing_alg"`
	SigningKeyID       string     `json:"signing_key_id"`
	Signature          string     `json:"signature,omitempty"`
}

type SignedTrustBundleSpec struct {
	BundleID           string
	Sequence           int
	NotBefore          time.Time
	NotAfter           time.Time
	PreviousBundleHash string
	Keys               []TrustKey
	SigningKeyID       string
}

func NewSignedTrustBundle(spec SignedTrustBundleSpec, now time.Time) (SignedTrustBundle, error) {
	if spec.BundleID == "" {
		return SignedTrustBundle{}, fmt.Errorf("%w: bundle id is required", ErrTrustBundleInvalid)
	}
	if spec.Sequence <= 0 {
		return SignedTrustBundle{}, fmt.Errorf("%w: sequence must be positive", ErrTrustBundleInvalid)
	}
	if spec.SigningKeyID == "" {
		return SignedTrustBundle{}, fmt.Errorf("%w: signing key id is required", ErrTrustBundleInvalid)
	}
	if spec.NotBefore.IsZero() {
		spec.NotBefore = now.UTC()
	}
	if spec.NotAfter.IsZero() {
		spec.NotAfter = spec.NotBefore.Add(24 * time.Hour)
	}
	bundle := SignedTrustBundle{
		SchemaVersion:      SignedTrustBundleSchemaVersion,
		BundleID:           spec.BundleID,
		Sequence:           spec.Sequence,
		IssuedAt:           now.UTC(),
		NotBefore:          spec.NotBefore.UTC(),
		NotAfter:           spec.NotAfter.UTC(),
		PreviousBundleHash: spec.PreviousBundleHash,
		Keys:               cloneTrustKeys(spec.Keys),
		SigningAlg:         JobEnvelopeSigningAlg,
		SigningKeyID:       spec.SigningKeyID,
	}
	if err := bundle.validateForSigning(); err != nil {
		return SignedTrustBundle{}, err
	}
	return bundle, nil
}

func NewTrustKey(keyID string, publicKey ed25519.PublicKey, status string, now time.Time) TrustKey {
	if status == "" {
		status = TrustKeyStatusActive
	}
	return TrustKey{
		KeyID:      keyID,
		SigningAlg: JobEnvelopeSigningAlg,
		PublicKey:  base64.RawURLEncoding.EncodeToString(publicKey),
		Status:     status,
		NotBefore:  now.UTC(),
	}
}

func (k TrustKey) TrustBundle() TrustBundle {
	return TrustBundle{
		SigningKeyID: k.KeyID,
		SigningAlg:   k.SigningAlg,
		PublicKey:    k.PublicKey,
	}
}

func (k TrustKey) Ed25519PublicKey() (ed25519.PublicKey, error) {
	return k.TrustBundle().Ed25519PublicKey()
}

func (k TrustKey) ActiveAt(now time.Time) bool {
	now = now.UTC()
	if k.Status != TrustKeyStatusActive {
		return false
	}
	if k.NotBefore.IsZero() || now.Before(k.NotBefore.UTC()) {
		return false
	}
	if k.NotAfter != nil && now.After(k.NotAfter.UTC()) {
		return false
	}
	return true
}

func (b SignedTrustBundle) Sign(privateKey ed25519.PrivateKey) (SignedTrustBundle, error) {
	if err := b.validateForSigning(); err != nil {
		return SignedTrustBundle{}, err
	}
	message, err := b.signingBytes()
	if err != nil {
		return SignedTrustBundle{}, err
	}
	b.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, message))
	return b, nil
}

func (b SignedTrustBundle) Verify(root TrustBundle, now time.Time) error {
	if err := b.validateForSigning(); err != nil {
		return err
	}
	if b.Signature == "" {
		return fmt.Errorf("%w: missing signature", ErrTrustBundleSignature)
	}
	now = now.UTC()
	if now.Before(b.NotBefore.UTC()) || now.After(b.NotAfter.UTC()) {
		return ErrTrustBundleExpired
	}
	if root.SigningKeyID != b.SigningKeyID {
		return fmt.Errorf("%w: trust root key id mismatch", ErrTrustBundleInvalid)
	}
	publicKey, err := root.Ed25519PublicKey()
	if err != nil {
		return err
	}
	signature, err := base64.RawURLEncoding.DecodeString(b.Signature)
	if err != nil {
		return fmt.Errorf("%w: malformed signature", ErrTrustBundleSignature)
	}
	unsigned := b
	unsigned.Signature = ""
	message, err := unsigned.signingBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, message, signature) {
		return ErrTrustBundleSignature
	}
	if key, ok := b.Key(root.SigningKeyID); ok && key.Status == TrustKeyStatusRevoked {
		return fmt.Errorf("%w: signing key %q", ErrTrustKeyRevoked, root.SigningKeyID)
	}
	return nil
}

func (b SignedTrustBundle) VerifyUpdate(previous SignedTrustBundle, root TrustBundle, now time.Time) error {
	if err := b.Verify(root, now); err != nil {
		return err
	}
	if b.Sequence <= previous.Sequence {
		return fmt.Errorf("%w: sequence must increase", ErrTrustBundleInvalid)
	}
	hash, err := previous.Hash()
	if err != nil {
		return err
	}
	if b.PreviousBundleHash != hash {
		return fmt.Errorf("%w: previous bundle hash mismatch", ErrTrustBundleInvalid)
	}
	return nil
}

func (b SignedTrustBundle) ActiveTrustBundle(keyID string, now time.Time) (TrustBundle, error) {
	key, ok := b.Key(keyID)
	if !ok {
		return TrustBundle{}, fmt.Errorf("%w: key %q not found", ErrTrustBundleInvalid, keyID)
	}
	if key.Status == TrustKeyStatusRevoked {
		return TrustBundle{}, fmt.Errorf("%w: key %q", ErrTrustKeyRevoked, keyID)
	}
	if !key.ActiveAt(now) {
		return TrustBundle{}, fmt.Errorf("%w: key %q is not active", ErrTrustBundleInvalid, keyID)
	}
	return key.TrustBundle(), nil
}

func (b SignedTrustBundle) Key(keyID string) (TrustKey, bool) {
	for _, key := range b.Keys {
		if key.KeyID == keyID {
			return key, true
		}
	}
	return TrustKey{}, false
}

func (b SignedTrustBundle) Hash() (string, error) {
	content, err := b.signingBytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (b SignedTrustBundle) validateForSigning() error {
	if b.SchemaVersion != SignedTrustBundleSchemaVersion {
		return fmt.Errorf("%w: unsupported schema version", ErrTrustBundleInvalid)
	}
	if b.BundleID == "" || b.Sequence <= 0 {
		return fmt.Errorf("%w: missing bundle identity", ErrTrustBundleInvalid)
	}
	if b.IssuedAt.IsZero() || b.NotBefore.IsZero() || b.NotAfter.IsZero() || !b.NotBefore.Before(b.NotAfter) {
		return fmt.Errorf("%w: invalid validity window", ErrTrustBundleInvalid)
	}
	if b.SigningAlg != JobEnvelopeSigningAlg || b.SigningKeyID == "" {
		return fmt.Errorf("%w: unsupported signing metadata", ErrTrustBundleInvalid)
	}
	if len(b.Keys) == 0 {
		return fmt.Errorf("%w: at least one key is required", ErrTrustBundleInvalid)
	}
	seen := map[string]bool{}
	for _, key := range b.Keys {
		if err := validateTrustKey(key); err != nil {
			return err
		}
		if seen[key.KeyID] {
			return fmt.Errorf("%w: duplicate key id %q", ErrTrustBundleInvalid, key.KeyID)
		}
		seen[key.KeyID] = true
	}
	return nil
}

func validateTrustKey(key TrustKey) error {
	if key.KeyID == "" {
		return fmt.Errorf("%w: key id is required", ErrTrustBundleInvalid)
	}
	if key.SigningAlg != JobEnvelopeSigningAlg {
		return fmt.Errorf("%w: unsupported key algorithm", ErrTrustBundleInvalid)
	}
	if key.Status != TrustKeyStatusActive && key.Status != TrustKeyStatusRetired && key.Status != TrustKeyStatusRevoked {
		return fmt.Errorf("%w: unsupported key status %q", ErrTrustBundleInvalid, key.Status)
	}
	if key.NotBefore.IsZero() {
		return fmt.Errorf("%w: key not_before is required", ErrTrustBundleInvalid)
	}
	if key.Status == TrustKeyStatusRevoked && key.RevokedAt == nil {
		return fmt.Errorf("%w: revoked key requires revoked_at", ErrTrustBundleInvalid)
	}
	if _, err := key.Ed25519PublicKey(); err != nil {
		return err
	}
	return nil
}

func (b SignedTrustBundle) signingBytes() ([]byte, error) {
	unsigned := b
	unsigned.Signature = ""
	return json.Marshal(unsigned)
}

func cloneTrustKeys(keys []TrustKey) []TrustKey {
	cloned := make([]TrustKey, 0, len(keys))
	for _, key := range keys {
		copy := key
		if key.NotAfter != nil {
			value := key.NotAfter.UTC()
			copy.NotAfter = &value
		}
		if key.RevokedAt != nil {
			value := key.RevokedAt.UTC()
			copy.RevokedAt = &value
		}
		cloned = append(cloned, copy)
	}
	return cloned
}
