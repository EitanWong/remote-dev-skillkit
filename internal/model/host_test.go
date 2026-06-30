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

func hostRegistrationTestTicket(t *testing.T) Ticket {
	t.Helper()
	ticket, err := NewTicket(HostModeManaged, 600, []string{"shell.user"}, "test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return ticket
}

func hostRegistrationPublicKey(publicKey ed25519.PublicKey) string {
	return base64.RawURLEncoding.EncodeToString(publicKey)
}

func hostRegistrationFingerprint(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}
