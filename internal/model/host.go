package model

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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
	TicketCode            string                     `json:"ticket_code"`
	Name                  string                     `json:"name"`
	OS                    string                     `json:"os"`
	Arch                  string                     `json:"arch"`
	Capabilities          []string                   `json:"capabilities"`
	IdentityKeyID         string                     `json:"identity_key_id,omitempty"`
	IdentityPublicKey     string                     `json:"identity_public_key,omitempty"`
	IdentityFingerprint   string                     `json:"identity_fingerprint,omitempty"`
	IdentityProof         *HostRegistrationProof     `json:"identity_proof,omitempty"`
	EnrollmentCertificate *HostEnrollmentCertificate `json:"enrollment_certificate,omitempty"`
}

const (
	HostRegistrationProofSchemaVersion         = "rdev.host-registration-proof.v1"
	HostEnrollmentCertificateSchemaVersion     = "rdev.host-enrollment-certificate.v1"
	HostEnrollmentRevocationListSchemaVersion  = "rdev.host-enrollment-revocations.v1"
	hostRegistrationProofPayloadVersion        = "rdev.host-registration-proof-payload.v1"
	hostEnrollmentCertificatePayloadSchema     = "rdev.host-enrollment-certificate-payload.v1"
	hostEnrollmentRevocationListPayloadSchema  = "rdev.host-enrollment-revocations-payload.v1"
	defaultHostEnrollmentCertificateMaxBackoff = 30 * time.Second
)

type HostRegistrationProof struct {
	SchemaVersion string `json:"schema_version"`
	SigningAlg    string `json:"signing_alg"`
	KeyID         string `json:"key_id"`
	Signature     string `json:"signature"`
}

type HostEnrollmentCertificate struct {
	SchemaVersion              string    `json:"schema_version"`
	SigningAlg                 string    `json:"signing_alg"`
	IssuerKeyID                string    `json:"issuer_key_id"`
	SubjectIdentityFingerprint string    `json:"subject_identity_fingerprint"`
	TicketCode                 string    `json:"ticket_code"`
	HostName                   string    `json:"host_name"`
	OS                         string    `json:"os"`
	Arch                       string    `json:"arch"`
	Mode                       HostMode  `json:"mode"`
	Capabilities               []string  `json:"capabilities"`
	NotBefore                  time.Time `json:"not_before"`
	NotAfter                   time.Time `json:"not_after"`
	Signature                  string    `json:"signature"`
}

type HostEnrollmentCertificateRevocation struct {
	CertificateFingerprint string    `json:"certificate_fingerprint"`
	Reason                 string    `json:"reason,omitempty"`
	RevokedAt              time.Time `json:"revoked_at"`
}

type HostEnrollmentRevocationList struct {
	SchemaVersion       string                                `json:"schema_version"`
	SigningAlg          string                                `json:"signing_alg"`
	IssuerKeyID         string                                `json:"issuer_key_id"`
	GeneratedAt         time.Time                             `json:"generated_at"`
	NotAfter            time.Time                             `json:"not_after"`
	RevokedCertificates []HostEnrollmentCertificateRevocation `json:"revoked_certificates"`
	Signature           string                                `json:"signature"`
}

type hostRegistrationProofPayload struct {
	SchemaVersion       string `json:"schema_version"`
	TicketCode          string `json:"ticket_code"`
	Name                string `json:"name"`
	OS                  string `json:"os"`
	Arch                string `json:"arch"`
	IdentityKeyID       string `json:"identity_key_id"`
	IdentityPublicKey   string `json:"identity_public_key"`
	IdentityFingerprint string `json:"identity_fingerprint"`
}

type hostEnrollmentCertificatePayload struct {
	SchemaVersion              string    `json:"schema_version"`
	IssuerKeyID                string    `json:"issuer_key_id"`
	SubjectIdentityFingerprint string    `json:"subject_identity_fingerprint"`
	TicketCode                 string    `json:"ticket_code"`
	HostName                   string    `json:"host_name"`
	OS                         string    `json:"os"`
	Arch                       string    `json:"arch"`
	Mode                       HostMode  `json:"mode"`
	Capabilities               []string  `json:"capabilities"`
	NotBefore                  time.Time `json:"not_before"`
	NotAfter                   time.Time `json:"not_after"`
}

type hostEnrollmentCertificateFingerprintPayload struct {
	SchemaVersion string `json:"schema_version"`
	Payload       []byte `json:"payload"`
	Signature     string `json:"signature"`
}

type hostEnrollmentRevocationListPayload struct {
	SchemaVersion       string                                `json:"schema_version"`
	IssuerKeyID         string                                `json:"issuer_key_id"`
	GeneratedAt         time.Time                             `json:"generated_at"`
	NotAfter            time.Time                             `json:"not_after"`
	RevokedCertificates []HostEnrollmentCertificateRevocation `json:"revoked_certificates"`
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
		if registration.IdentityProof != nil {
			return fmt.Errorf("host identity proof requires identity fields")
		}
		return nil
	}
	publicKey, err := validateHostRegistrationIdentityFields(registration)
	if err != nil {
		return err
	}
	if registration.IdentityProof == nil {
		return fmt.Errorf("host identity proof is required")
	}
	if registration.IdentityProof.SchemaVersion != HostRegistrationProofSchemaVersion {
		return fmt.Errorf("unsupported host identity proof schema %q", registration.IdentityProof.SchemaVersion)
	}
	if registration.IdentityProof.SigningAlg != JobEnvelopeSigningAlg {
		return fmt.Errorf("unsupported host identity proof signing algorithm %q", registration.IdentityProof.SigningAlg)
	}
	if registration.IdentityProof.KeyID != registration.IdentityKeyID {
		return fmt.Errorf("host identity proof key id mismatch")
	}
	signature, err := base64.RawURLEncoding.DecodeString(registration.IdentityProof.Signature)
	if err != nil {
		return fmt.Errorf("decode host identity proof signature: %w", err)
	}
	payload, err := hostRegistrationProofPayloadBytes(registration)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, payload, signature) {
		return fmt.Errorf("host identity proof signature mismatch")
	}
	return nil
}

func validateHostRegistrationIdentityFields(registration HostRegistration) (ed25519.PublicKey, error) {
	if registration.IdentityKeyID == "" || registration.IdentityPublicKey == "" || registration.IdentityFingerprint == "" {
		return nil, fmt.Errorf("host identity key id, public key, and fingerprint are required together")
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(registration.IdentityPublicKey)
	if err != nil {
		return nil, fmt.Errorf("decode host identity public key: %w", err)
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid host identity public key length %d", len(publicKey))
	}
	sum := sha256.Sum256(publicKey)
	expected := "sha256:" + hex.EncodeToString(sum[:])
	if registration.IdentityFingerprint != expected {
		return nil, fmt.Errorf("host identity fingerprint mismatch")
	}
	return ed25519.PublicKey(publicKey), nil
}

func SignHostRegistration(registration HostRegistration, privateKey ed25519.PrivateKey) (HostRegistrationProof, error) {
	publicKey, err := validateHostRegistrationIdentityFields(registration)
	if err != nil {
		return HostRegistrationProof{}, err
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return HostRegistrationProof{}, fmt.Errorf("invalid host identity private key length %d", len(privateKey))
	}
	derived, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok || !derived.Equal(publicKey) {
		return HostRegistrationProof{}, fmt.Errorf("host identity private key does not match registration public key")
	}
	payload, err := hostRegistrationProofPayloadBytes(registration)
	if err != nil {
		return HostRegistrationProof{}, err
	}
	signature := ed25519.Sign(privateKey, payload)
	return HostRegistrationProof{
		SchemaVersion: HostRegistrationProofSchemaVersion,
		SigningAlg:    JobEnvelopeSigningAlg,
		KeyID:         registration.IdentityKeyID,
		Signature:     base64.RawURLEncoding.EncodeToString(signature),
	}, nil
}

func hostRegistrationProofPayloadBytes(registration HostRegistration) ([]byte, error) {
	payload := hostRegistrationProofPayload{
		SchemaVersion:       hostRegistrationProofPayloadVersion,
		TicketCode:          registration.TicketCode,
		Name:                registration.Name,
		OS:                  registration.OS,
		Arch:                registration.Arch,
		IdentityKeyID:       registration.IdentityKeyID,
		IdentityPublicKey:   registration.IdentityPublicKey,
		IdentityFingerprint: registration.IdentityFingerprint,
	}
	return json.Marshal(payload)
}

func SignHostEnrollmentCertificate(registration HostRegistration, ticket Ticket, issuerKeyID string, issuerPrivateKey ed25519.PrivateKey, now time.Time, ttl time.Duration) (HostEnrollmentCertificate, error) {
	if _, err := validateHostRegistrationIdentityFields(registration); err != nil {
		return HostEnrollmentCertificate{}, err
	}
	if issuerKeyID == "" {
		return HostEnrollmentCertificate{}, fmt.Errorf("enrollment issuer key id is required")
	}
	if len(issuerPrivateKey) != ed25519.PrivateKeySize {
		return HostEnrollmentCertificate{}, fmt.Errorf("invalid enrollment issuer private key length %d", len(issuerPrivateKey))
	}
	if ticket.Code == "" {
		return HostEnrollmentCertificate{}, fmt.Errorf("ticket code is required")
	}
	if registration.TicketCode != ticket.Code {
		return HostEnrollmentCertificate{}, fmt.Errorf("registration ticket code does not match ticket")
	}
	if !ticket.Mode.Valid() {
		return HostEnrollmentCertificate{}, fmt.Errorf("unsupported host mode %q", ticket.Mode)
	}
	if registration.Name == "" || registration.OS == "" || registration.Arch == "" {
		return HostEnrollmentCertificate{}, fmt.Errorf("host name, os, and arch are required")
	}
	if ttl <= 0 {
		return HostEnrollmentCertificate{}, fmt.Errorf("enrollment certificate ttl must be positive")
	}
	capabilities := normalizedCapabilities(registration.Capabilities)
	if len(capabilities) == 0 {
		capabilities = normalizedCapabilities(ticket.Capabilities)
	}
	if len(capabilities) == 0 {
		return HostEnrollmentCertificate{}, fmt.Errorf("host enrollment certificate capabilities are required")
	}
	now = now.UTC()
	certificate := HostEnrollmentCertificate{
		SchemaVersion:              HostEnrollmentCertificateSchemaVersion,
		SigningAlg:                 JobEnvelopeSigningAlg,
		IssuerKeyID:                issuerKeyID,
		SubjectIdentityFingerprint: registration.IdentityFingerprint,
		TicketCode:                 registration.TicketCode,
		HostName:                   registration.Name,
		OS:                         registration.OS,
		Arch:                       registration.Arch,
		Mode:                       ticket.Mode,
		Capabilities:               capabilities,
		NotBefore:                  now,
		NotAfter:                   now.Add(ttl).UTC(),
	}
	payload, err := hostEnrollmentCertificatePayloadBytes(certificate)
	if err != nil {
		return HostEnrollmentCertificate{}, err
	}
	certificate.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(issuerPrivateKey, payload))
	return certificate, nil
}

func VerifyHostEnrollmentCertificate(registration HostRegistration, ticket Ticket, root TrustBundle, now time.Time) error {
	if registration.EnrollmentCertificate == nil {
		return fmt.Errorf("host enrollment certificate is required")
	}
	if _, err := validateHostRegistrationIdentityFields(registration); err != nil {
		return err
	}
	certificate := *registration.EnrollmentCertificate
	if err := VerifyHostEnrollmentCertificateSignature(certificate, root, now); err != nil {
		return err
	}
	if certificate.TicketCode != ticket.Code || certificate.TicketCode != registration.TicketCode {
		return fmt.Errorf("host enrollment certificate ticket code mismatch")
	}
	if certificate.Mode != ticket.Mode {
		return fmt.Errorf("host enrollment certificate mode mismatch")
	}
	if certificate.SubjectIdentityFingerprint != registration.IdentityFingerprint {
		return fmt.Errorf("host enrollment certificate identity fingerprint mismatch")
	}
	if certificate.HostName != registration.Name {
		return fmt.Errorf("host enrollment certificate host name mismatch")
	}
	if certificate.OS != registration.OS {
		return fmt.Errorf("host enrollment certificate os mismatch")
	}
	if certificate.Arch != registration.Arch {
		return fmt.Errorf("host enrollment certificate arch mismatch")
	}
	if !sameCapabilities(certificate.Capabilities, registration.Capabilities) {
		return fmt.Errorf("host enrollment certificate capabilities mismatch")
	}
	return nil
}

func VerifyHostEnrollmentCertificateSignature(certificate HostEnrollmentCertificate, root TrustBundle, now time.Time) error {
	if certificate.SchemaVersion != HostEnrollmentCertificateSchemaVersion {
		return fmt.Errorf("unsupported host enrollment certificate schema %q", certificate.SchemaVersion)
	}
	if certificate.SigningAlg != JobEnvelopeSigningAlg {
		return fmt.Errorf("unsupported host enrollment certificate signing algorithm %q", certificate.SigningAlg)
	}
	if certificate.IssuerKeyID == "" {
		return fmt.Errorf("host enrollment certificate issuer key id is required")
	}
	if root.SigningKeyID != certificate.IssuerKeyID {
		return fmt.Errorf("host enrollment certificate issuer key id mismatch")
	}
	if certificate.SubjectIdentityFingerprint == "" {
		return fmt.Errorf("host enrollment certificate subject identity fingerprint is required")
	}
	if certificate.TicketCode == "" || certificate.HostName == "" || certificate.OS == "" || certificate.Arch == "" {
		return fmt.Errorf("host enrollment certificate ticket, host name, os, and arch are required")
	}
	if !certificate.Mode.Valid() {
		return fmt.Errorf("unsupported host enrollment certificate mode %q", certificate.Mode)
	}
	if len(normalizedCapabilities(certificate.Capabilities)) == 0 {
		return fmt.Errorf("host enrollment certificate capabilities are required")
	}
	now = now.UTC()
	if certificate.NotBefore.IsZero() || certificate.NotAfter.IsZero() || !certificate.NotBefore.Before(certificate.NotAfter) {
		return fmt.Errorf("host enrollment certificate validity window is invalid")
	}
	if now.Add(defaultHostEnrollmentCertificateMaxBackoff).Before(certificate.NotBefore.UTC()) {
		return fmt.Errorf("host enrollment certificate is not valid yet")
	}
	if now.After(certificate.NotAfter.UTC()) {
		return fmt.Errorf("host enrollment certificate expired")
	}
	signature, err := base64.RawURLEncoding.DecodeString(certificate.Signature)
	if err != nil {
		return fmt.Errorf("decode host enrollment certificate signature: %w", err)
	}
	publicKey, err := root.Ed25519PublicKey()
	if err != nil {
		return err
	}
	payload, err := hostEnrollmentCertificatePayloadBytes(certificate)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, payload, signature) {
		return fmt.Errorf("host enrollment certificate signature mismatch")
	}
	return nil
}

func HostEnrollmentCertificateFingerprint(certificate HostEnrollmentCertificate) (string, error) {
	payload, err := hostEnrollmentCertificatePayloadBytes(certificate)
	if err != nil {
		return "", err
	}
	fingerprintPayload := hostEnrollmentCertificateFingerprintPayload{
		SchemaVersion: "rdev.host-enrollment-certificate-fingerprint.v1",
		Payload:       payload,
		Signature:     certificate.Signature,
	}
	content, err := json.Marshal(fingerprintPayload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func SignHostEnrollmentRevocationList(revocations []HostEnrollmentCertificateRevocation, issuerKeyID string, issuerPrivateKey ed25519.PrivateKey, now time.Time, ttl time.Duration) (HostEnrollmentRevocationList, error) {
	if issuerKeyID == "" {
		return HostEnrollmentRevocationList{}, fmt.Errorf("enrollment issuer key id is required")
	}
	if len(issuerPrivateKey) != ed25519.PrivateKeySize {
		return HostEnrollmentRevocationList{}, fmt.Errorf("invalid enrollment issuer private key length %d", len(issuerPrivateKey))
	}
	if ttl <= 0 {
		return HostEnrollmentRevocationList{}, fmt.Errorf("host enrollment revocation list ttl must be positive")
	}
	normalized, err := normalizeHostEnrollmentRevocations(revocations)
	if err != nil {
		return HostEnrollmentRevocationList{}, err
	}
	if len(normalized) == 0 {
		return HostEnrollmentRevocationList{}, fmt.Errorf("host enrollment revocation list requires at least one revoked certificate")
	}
	now = now.UTC()
	list := HostEnrollmentRevocationList{
		SchemaVersion:       HostEnrollmentRevocationListSchemaVersion,
		SigningAlg:          JobEnvelopeSigningAlg,
		IssuerKeyID:         issuerKeyID,
		GeneratedAt:         now,
		NotAfter:            now.Add(ttl).UTC(),
		RevokedCertificates: normalized,
	}
	payload, err := hostEnrollmentRevocationListPayloadBytes(list)
	if err != nil {
		return HostEnrollmentRevocationList{}, err
	}
	list.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(issuerPrivateKey, payload))
	return list, nil
}

func VerifyHostEnrollmentRevocationListSignature(list HostEnrollmentRevocationList, root TrustBundle, now time.Time) error {
	if list.SchemaVersion != HostEnrollmentRevocationListSchemaVersion {
		return fmt.Errorf("unsupported host enrollment revocation list schema %q", list.SchemaVersion)
	}
	if list.SigningAlg != JobEnvelopeSigningAlg {
		return fmt.Errorf("unsupported host enrollment revocation list signing algorithm %q", list.SigningAlg)
	}
	if list.IssuerKeyID == "" {
		return fmt.Errorf("host enrollment revocation list issuer key id is required")
	}
	if root.SigningKeyID != list.IssuerKeyID {
		return fmt.Errorf("host enrollment revocation list issuer key id mismatch")
	}
	if list.GeneratedAt.IsZero() || list.NotAfter.IsZero() || !list.GeneratedAt.Before(list.NotAfter) {
		return fmt.Errorf("host enrollment revocation list validity window is invalid")
	}
	now = now.UTC()
	if now.Add(defaultHostEnrollmentCertificateMaxBackoff).Before(list.GeneratedAt.UTC()) {
		return fmt.Errorf("host enrollment revocation list is not valid yet")
	}
	if now.After(list.NotAfter.UTC()) {
		return fmt.Errorf("host enrollment revocation list expired")
	}
	normalized, err := normalizeHostEnrollmentRevocations(list.RevokedCertificates)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		return fmt.Errorf("host enrollment revocation list requires at least one revoked certificate")
	}
	list.RevokedCertificates = normalized
	signature, err := base64.RawURLEncoding.DecodeString(list.Signature)
	if err != nil {
		return fmt.Errorf("decode host enrollment revocation list signature: %w", err)
	}
	publicKey, err := root.Ed25519PublicKey()
	if err != nil {
		return err
	}
	payload, err := hostEnrollmentRevocationListPayloadBytes(list)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, payload, signature) {
		return fmt.Errorf("host enrollment revocation list signature mismatch")
	}
	return nil
}

func VerifyHostEnrollmentCertificateNotRevoked(certificate HostEnrollmentCertificate, list HostEnrollmentRevocationList) error {
	fingerprint, err := HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		return err
	}
	for _, revoked := range list.RevokedCertificates {
		if revoked.CertificateFingerprint == fingerprint {
			if revoked.Reason != "" {
				return fmt.Errorf("host enrollment certificate revoked: %s", revoked.Reason)
			}
			return fmt.Errorf("host enrollment certificate revoked")
		}
	}
	return nil
}

func hostEnrollmentCertificatePayloadBytes(certificate HostEnrollmentCertificate) ([]byte, error) {
	payload := hostEnrollmentCertificatePayload{
		SchemaVersion:              hostEnrollmentCertificatePayloadSchema,
		IssuerKeyID:                certificate.IssuerKeyID,
		SubjectIdentityFingerprint: certificate.SubjectIdentityFingerprint,
		TicketCode:                 certificate.TicketCode,
		HostName:                   certificate.HostName,
		OS:                         certificate.OS,
		Arch:                       certificate.Arch,
		Mode:                       certificate.Mode,
		Capabilities:               normalizedCapabilities(certificate.Capabilities),
		NotBefore:                  certificate.NotBefore.UTC(),
		NotAfter:                   certificate.NotAfter.UTC(),
	}
	return json.Marshal(payload)
}

func hostEnrollmentRevocationListPayloadBytes(list HostEnrollmentRevocationList) ([]byte, error) {
	revocations, err := normalizeHostEnrollmentRevocations(list.RevokedCertificates)
	if err != nil {
		return nil, err
	}
	payload := hostEnrollmentRevocationListPayload{
		SchemaVersion:       hostEnrollmentRevocationListPayloadSchema,
		IssuerKeyID:         list.IssuerKeyID,
		GeneratedAt:         list.GeneratedAt.UTC(),
		NotAfter:            list.NotAfter.UTC(),
		RevokedCertificates: revocations,
	}
	return json.Marshal(payload)
}

func normalizeHostEnrollmentRevocations(revocations []HostEnrollmentCertificateRevocation) ([]HostEnrollmentCertificateRevocation, error) {
	if len(revocations) == 0 {
		return nil, nil
	}
	normalized := make([]HostEnrollmentCertificateRevocation, 0, len(revocations))
	seen := map[string]struct{}{}
	for _, revocation := range revocations {
		fingerprint := strings.TrimSpace(revocation.CertificateFingerprint)
		if fingerprint == "" {
			return nil, fmt.Errorf("host enrollment revocation certificate fingerprint is required")
		}
		if !strings.HasPrefix(fingerprint, "sha256:") {
			return nil, fmt.Errorf("host enrollment revocation certificate fingerprint must start with sha256:")
		}
		if _, ok := seen[fingerprint]; ok {
			return nil, fmt.Errorf("duplicate host enrollment revocation fingerprint %q", fingerprint)
		}
		seen[fingerprint] = struct{}{}
		revocation.CertificateFingerprint = fingerprint
		revocation.Reason = strings.TrimSpace(revocation.Reason)
		revocation.RevokedAt = revocation.RevokedAt.UTC()
		if revocation.RevokedAt.IsZero() {
			return nil, fmt.Errorf("host enrollment revocation revoked_at is required")
		}
		normalized = append(normalized, revocation)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].CertificateFingerprint < normalized[j].CertificateFingerprint
	})
	return normalized, nil
}

func normalizedCapabilities(capabilities []string) []string {
	if len(capabilities) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(capabilities))
	seen := map[string]struct{}{}
	for _, capability := range capabilities {
		capability = strings.TrimSpace(capability)
		if capability == "" {
			continue
		}
		if _, ok := seen[capability]; ok {
			continue
		}
		seen[capability] = struct{}{}
		normalized = append(normalized, capability)
	}
	sort.Strings(normalized)
	return normalized
}

func sameCapabilities(a, b []string) bool {
	left := normalizedCapabilities(a)
	right := normalizedCapabilities(b)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
