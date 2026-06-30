package model

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestNewHostRequiresIdentityProofWhenIdentityIsPresent(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ticket := hostRegistrationTestTicket(t)
	_, err = NewHost(ticket, HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "managed-mac",
		OS:                  "darwin",
		Arch:                "arm64",
		IdentityKeyID:       "host-test",
		IdentityPublicKey:   hostRegistrationPublicKey(publicKey),
		IdentityFingerprint: hostRegistrationFingerprint(publicKey),
	}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "identity proof is required") {
		t.Fatalf("expected identity proof error, got %v", err)
	}
}

func TestNewHostVerifiesSignedIdentityProof(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ticket := hostRegistrationTestTicket(t)
	registration := HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "managed-mac",
		OS:                  "darwin",
		Arch:                "arm64",
		IdentityKeyID:       "host-test",
		IdentityPublicKey:   hostRegistrationPublicKey(publicKey),
		IdentityFingerprint: hostRegistrationFingerprint(publicKey),
	}
	proof, err := SignHostRegistration(registration, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	registration.IdentityProof = &proof
	host, err := NewHost(ticket, registration, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if host.IdentityFingerprint != registration.IdentityFingerprint {
		t.Fatalf("expected fingerprint %q, got %q", registration.IdentityFingerprint, host.IdentityFingerprint)
	}
}

func TestNewHostRejectsTamperedIdentityProofPayload(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ticket := hostRegistrationTestTicket(t)
	registration := HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "managed-mac",
		OS:                  "darwin",
		Arch:                "arm64",
		IdentityKeyID:       "host-test",
		IdentityPublicKey:   hostRegistrationPublicKey(publicKey),
		IdentityFingerprint: hostRegistrationFingerprint(publicKey),
	}
	proof, err := SignHostRegistration(registration, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	registration.Name = "tampered-name"
	registration.IdentityProof = &proof
	_, err = NewHost(ticket, registration, time.Now())
	if err == nil || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("expected signature mismatch, got %v", err)
	}
}

func TestSignHostRegistrationRejectsWrongPrivateKey(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, wrongPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ticket := hostRegistrationTestTicket(t)
	_, err = SignHostRegistration(HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "managed-mac",
		OS:                  "darwin",
		Arch:                "arm64",
		IdentityKeyID:       "host-test",
		IdentityPublicKey:   hostRegistrationPublicKey(publicKey),
		IdentityFingerprint: hostRegistrationFingerprint(publicKey),
	}, wrongPrivateKey)
	if err == nil || !strings.Contains(err.Error(), "private key does not match") {
		t.Fatalf("expected private key mismatch, got %v", err)
	}
}

func TestHostEnrollmentCertificateVerifiesRegistrationAuthorization(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	registration, ticket, _ := signedHostRegistrationForEnrollmentTest(t)
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := SignHostEnrollmentCertificate(registration, ticket, "enrollment-root", issuerPrivateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registration.EnrollmentCertificate = &certificate
	if err := VerifyHostEnrollmentCertificate(registration, ticket, NewTrustBundle("enrollment-root", issuerPublicKey), now.Add(time.Minute)); err != nil {
		t.Fatalf("expected enrollment certificate to verify: %v", err)
	}
}

func TestHostEnrollmentCertificateRejectsWrongRoot(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	registration, ticket, _ := signedHostRegistrationForEnrollmentTest(t)
	_, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrongPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := SignHostEnrollmentCertificate(registration, ticket, "enrollment-root", issuerPrivateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registration.EnrollmentCertificate = &certificate
	err = VerifyHostEnrollmentCertificate(registration, ticket, NewTrustBundle("enrollment-root", wrongPublicKey), now)
	if err == nil || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("expected signature mismatch, got %v", err)
	}
}

func TestHostEnrollmentCertificateRejectsTamperedRegistration(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	registration, ticket, _ := signedHostRegistrationForEnrollmentTest(t)
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := SignHostEnrollmentCertificate(registration, ticket, "enrollment-root", issuerPrivateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registration.Name = "tampered-host"
	registration.EnrollmentCertificate = &certificate
	err = VerifyHostEnrollmentCertificate(registration, ticket, NewTrustBundle("enrollment-root", issuerPublicKey), now)
	if err == nil || !strings.Contains(err.Error(), "host name mismatch") {
		t.Fatalf("expected host name mismatch, got %v", err)
	}
}

func TestHostEnrollmentCertificateRejectsExpiredCertificate(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	registration, ticket, _ := signedHostRegistrationForEnrollmentTest(t)
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := SignHostEnrollmentCertificate(registration, ticket, "enrollment-root", issuerPrivateKey, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	registration.EnrollmentCertificate = &certificate
	err = VerifyHostEnrollmentCertificate(registration, ticket, NewTrustBundle("enrollment-root", issuerPublicKey), now.Add(2*time.Minute))
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired certificate, got %v", err)
	}
}

func TestHostEnrollmentRevocationListRejectsRevokedCertificate(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	registration, ticket, _ := signedHostRegistrationForEnrollmentTest(t)
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := SignHostEnrollmentCertificate(registration, ticket, "enrollment-root", issuerPrivateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		t.Fatal(err)
	}
	list, err := SignHostEnrollmentRevocationList([]HostEnrollmentCertificateRevocation{
		{
			CertificateFingerprint: fingerprint,
			Reason:                 "host retired",
			RevokedAt:              now,
		},
	}, "enrollment-root", issuerPrivateKey, now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyHostEnrollmentRevocationListSignature(list, NewTrustBundle("enrollment-root", issuerPublicKey), now.Add(time.Minute)); err != nil {
		t.Fatalf("expected revocation list to verify: %v", err)
	}
	err = VerifyHostEnrollmentCertificateNotRevoked(certificate, list)
	if err == nil || !strings.Contains(err.Error(), "host retired") {
		t.Fatalf("expected revoked certificate rejection, got %v", err)
	}
}

func TestHostEnrollmentRevocationListRejectsWrongRoot(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	_, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrongPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	list, err := SignHostEnrollmentRevocationList([]HostEnrollmentCertificateRevocation{
		{
			CertificateFingerprint: "sha256:" + strings.Repeat("a", 64),
			Reason:                 "test",
			RevokedAt:              now,
		},
	}, "enrollment-root", issuerPrivateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	err = VerifyHostEnrollmentRevocationListSignature(list, NewTrustBundle("enrollment-root", wrongPublicKey), now)
	if err == nil || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("expected signature mismatch, got %v", err)
	}
}

func hostRegistrationTestTicket(t *testing.T) Ticket {
	t.Helper()
	ticket, err := NewTicket(HostModeManaged, 600, []string{"shell.user"}, "test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return ticket
}

func signedHostRegistrationForEnrollmentTest(t *testing.T) (HostRegistration, Ticket, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ticket := hostRegistrationTestTicket(t)
	registration := HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "managed-mac",
		OS:                  "darwin",
		Arch:                "arm64",
		Capabilities:        []string{"git.diff", "codex.run"},
		IdentityKeyID:       "host-test",
		IdentityPublicKey:   hostRegistrationPublicKey(publicKey),
		IdentityFingerprint: hostRegistrationFingerprint(publicKey),
	}
	proof, err := SignHostRegistration(registration, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	registration.IdentityProof = &proof
	return registration, ticket, privateKey
}

func hostRegistrationPublicKey(publicKey ed25519.PublicKey) string {
	return base64.RawURLEncoding.EncodeToString(publicKey)
}

func hostRegistrationFingerprint(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}
