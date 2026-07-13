package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/audit"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestMemoryGatewayCreatesSignedJoinManifest(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := gw.JoinManifest(ticket.Code, "http://127.0.0.1:8787", "http://127.0.0.1:8787/join/"+ticket.Code)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.TicketCode != ticket.Code {
		t.Fatalf("expected ticket code %q, got %q", ticket.Code, manifest.TicketCode)
	}
	if err := manifest.Verify(now); err != nil {
		t.Fatalf("expected manifest to verify: %v", err)
	}
}

func TestMemoryGatewayJoinManifestUsesTicketMetadataGatewayCandidates(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	candidates := []model.JoinManifestGatewayCandidate{
		{URL: "https://relay.example.test/rdev", Kind: "relay", Scope: "configured-relay", Recommended: true},
		{URL: "http://192.0.2.10:8787", Kind: "lan-private", Scope: "diagnostic"},
	}
	content, err := json.Marshal(candidates)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := gw.CreateTicketWithMetadata(model.HostModeAttendedTemporary, 600, nil, "repair", map[string]string{
		TicketMetadataGatewayCandidates: string(content),
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := gw.JoinManifest(ticket.Code, "https://relay.example.test/rdev", "https://relay.example.test/rdev/join/"+ticket.Code)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.GatewayCandidates) < 2 || manifest.GatewayCandidates[0].Kind != "relay" {
		t.Fatalf("expected ticket metadata gateway candidates in signed manifest, got %#v", manifest.GatewayCandidates)
	}
	if err := manifest.Verify(now); err != nil {
		t.Fatalf("expected manifest to verify: %v", err)
	}
}

func TestMemoryGatewayUpdatesSignedGatewayCandidatesAfterRotation(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	ticket, err := gw.CreateTicketWithMetadata(model.HostModeAttendedTemporary, 600, nil, "rotation", map[string]string{
		TicketMetadataGatewayCandidates: `[{"url":"https://old.example.test","kind":"tunn3l"}]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = gw.UpdateTicketGatewayCandidates(ticket.ID, []model.JoinManifestGatewayCandidate{
		{URL: "https://replacement.example.test", Kind: "tunn3l", Scope: "public-tunnel", Recommended: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := gw.JoinManifest(ticket.Code, "https://replacement.example.test", "https://replacement.example.test/join/"+ticket.Code)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.GatewayCandidates) != 1 || manifest.GatewayCandidates[0].URL != "https://replacement.example.test" {
		t.Fatalf("updated gateway candidates = %#v", manifest.GatewayCandidates)
	}
	if err := manifest.Verify(now); err != nil {
		t.Fatalf("updated manifest did not verify: %v", err)
	}
}

func TestMemoryGatewayPreservesEmptyGatewayCandidateMetadataForFailClosedValidation(t *testing.T) {
	gw := NewMemoryGateway()
	ticket, err := gw.CreateProbingTicketWithMetadata(model.HostModeAttendedTemporary, 600, nil, "invalid authority", map[string]string{
		TicketMetadataGatewayCandidates: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := ticket.Metadata[TicketMetadataGatewayCandidates]; !ok || value != "" {
		t.Fatalf("gateway candidate metadata presence was lost: %#v", ticket.Metadata)
	}
}

func TestMemoryGatewayTicketReturnsAreDetachedFromStoredAuthority(t *testing.T) {
	gw := NewMemoryGateway()
	const originalMetadata = `[{"url":"https://public.example.test","recommended":true}]`
	inputCapabilities := []string{"shell.user"}
	created, err := gw.CreateProbingTicketWithMetadata(model.HostModeAttendedTemporary, 600, inputCapabilities, "detached ticket", map[string]string{
		TicketMetadataGatewayCandidates: originalMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	created.Capabilities[0] = "unattended.access"
	created.Metadata[TicketMetadataGatewayCandidates] = `[{"url":"https://mutated.example.test"}]`
	inputCapabilities[0] = "unattended.access"
	assertStored := func(stage string, wantStatus model.TicketStatus) {
		t.Helper()
		stored, ok := gw.Ticket(created.ID)
		if !ok {
			t.Fatalf("%s: stored ticket missing", stage)
		}
		if stored.Status != wantStatus || len(stored.Capabilities) != 1 || stored.Capabilities[0] != "shell.user" || stored.Metadata[TicketMetadataGatewayCandidates] != originalMetadata {
			t.Fatalf("%s: stored ticket was mutated through returned value: %#v", stage, stored)
		}
	}
	assertStored("create", model.TicketStatusProbing)

	published, err := gw.PublishTicket(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	published.Metadata[TicketMetadataGatewayCandidates] = `[{"url":"https://published-mutation.example.test"}]`
	byCode, ok := gw.TicketForCode(created.Code)
	if !ok {
		t.Fatal("published ticket missing by code")
	}
	byCode.Capabilities[0] = "unattended.access"
	byCode.Metadata[TicketMetadataGatewayCandidates] = `[{"url":"https://lookup-mutation.example.test"}]`
	assertStored("publish and lookup", model.TicketStatusActive)

	revoked, err := gw.RevokeTicket(created.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	revoked.Metadata[TicketMetadataGatewayCandidates] = `[{"url":"https://revoke-mutation.example.test"}]`
	assertStored("revoke", model.TicketStatusRevoked)

	rollbackTicket, err := gw.CreateProbingTicketWithMetadata(model.HostModeAttendedTemporary, 600, []string{"shell.user"}, "rollback", map[string]string{
		TicketMetadataGatewayCandidates: originalMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	rolledBack, _, err := gw.RollbackTicket(rollbackTicket.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	rolledBack.Metadata[TicketMetadataGatewayCandidates] = `[{"url":"https://rollback-mutation.example.test"}]`
	storedRollback, ok := gw.Ticket(rollbackTicket.ID)
	if !ok || storedRollback.Metadata[TicketMetadataGatewayCandidates] != originalMetadata {
		t.Fatalf("rollback return mutated stored ticket: %#v", storedRollback)
	}
}

func TestMemoryGatewayJoinManifestFailsClosedForInvalidStoredGatewayCandidates(t *testing.T) {
	for _, tt := range []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "malformed", raw: "{"},
		{name: "empty array", raw: "[]"},
		{name: "empty URL", raw: `[{"url":""}]`},
		{name: "malformed URL", raw: `[{"url":"not a url"}]`},
		{name: "empty query marker", raw: `[{"url":"https://public.example.test?"}]`},
		{name: "empty fragment marker", raw: `[{"url":"https://public.example.test#"}]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gw := NewMemoryGateway()
			ticket, err := gw.CreateTicketWithMetadata(model.HostModeAttendedTemporary, 600, nil, "invalid authority", map[string]string{
				TicketMetadataGatewayCandidates: tt.raw,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := gw.JoinManifest(ticket.Code, "http://localhost", "http://localhost/join/"+ticket.Code); err == nil {
				t.Fatal("invalid stored gateway candidates silently fell back")
			}
		})
	}
}

func TestMemoryGatewayPreservesHostIdentity(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := hostIdentityFingerprint(publicKey)
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	registration := model.HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "win-temp-01",
		OS:                  "windows",
		Arch:                "amd64",
		IdentityKeyID:       "host-test",
		IdentityPublicKey:   encodeHostIdentityPublicKey(publicKey),
		IdentityFingerprint: fingerprint,
	}
	proof, err := model.SignHostRegistration(registration, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	registration.IdentityProof = &proof
	host, err := gw.RegisterHost(registration)
	if err != nil {
		t.Fatal(err)
	}
	if host.IdentityFingerprint != fingerprint {
		t.Fatalf("expected host identity fingerprint %q, got %q", fingerprint, host.IdentityFingerprint)
	}
	host, err = gw.ActivateHost(host.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host.IdentityFingerprint != fingerprint {
		t.Fatalf("expected activated host identity fingerprint %q, got %q", fingerprint, host.IdentityFingerprint)
	}
}

func TestMemoryGatewayRejectsHostIdentityFingerprintMismatch(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	_, err = gw.RegisterHost(model.HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "win-temp-01",
		OS:                  "windows",
		Arch:                "amd64",
		IdentityKeyID:       "host-test",
		IdentityPublicKey:   encodeHostIdentityPublicKey(publicKey),
		IdentityFingerprint: "sha256:wrong",
	})
	if err == nil {
		t.Fatal("expected identity fingerprint mismatch")
	}
}

func TestMemoryGatewayEnrollmentRootRequiresCertificate(t *testing.T) {
	issuerPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentRoot(model.NewTrustBundle("enrollment-root", issuerPublicKey))
	capabilities := []string{"shell.user"}
	ticket, err := gw.CreateTicket(model.HostModeManaged, 600, capabilities, "managed enrollment")
	if err != nil {
		t.Fatal(err)
	}
	registration := signedGatewayHostRegistration(t, ticket, capabilities)
	_, err = gw.RegisterHost(registration)
	if err == nil || !strings.Contains(err.Error(), "enrollment certificate is required") {
		t.Fatalf("expected enrollment certificate requirement, got %v", err)
	}
}

func TestMemoryGatewayEnrollmentRootAcceptsValidCertificate(t *testing.T) {
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentRoot(model.NewTrustBundle("enrollment-root", issuerPublicKey))
	capabilities := []string{"shell.user"}
	ticket, err := gw.CreateTicket(model.HostModeManaged, 600, capabilities, "managed enrollment")
	if err != nil {
		t.Fatal(err)
	}
	registration := signedGatewayHostRegistration(t, ticket, capabilities)
	certificate, err := model.SignHostEnrollmentCertificate(registration, ticket, "enrollment-root", issuerPrivateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registration.EnrollmentCertificate = &certificate
	host, err := gw.RegisterHost(registration)
	if err != nil {
		t.Fatal(err)
	}
	if host.Status != model.HostStatusPending {
		t.Fatalf("expected pending host, got %s", host.Status)
	}
	if host.IdentityFingerprint != registration.IdentityFingerprint {
		t.Fatalf("expected fingerprint %q, got %q", registration.IdentityFingerprint, host.IdentityFingerprint)
	}
}

func TestMemoryGatewayIssuesEnrollmentCertificate(t *testing.T) {
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentIssuer(root, issuerPrivateKey)
	capabilities := []string{"shell.user", "git.diff"}
	ticket, err := gw.CreateTicket(model.HostModeManaged, 600, capabilities, "managed enrollment")
	if err != nil {
		t.Fatal(err)
	}
	registration := signedGatewayHostRegistration(t, ticket, capabilities)
	certificate, err := gw.IssueEnrollmentCertificate(EnrollmentCertificateRequest{
		TicketCode:          ticket.Code,
		Name:                registration.Name,
		OS:                  registration.OS,
		Arch:                registration.Arch,
		Capabilities:        []string{"shell.user"},
		IdentityKeyID:       registration.IdentityKeyID,
		IdentityPublicKey:   registration.IdentityPublicKey,
		IdentityFingerprint: registration.IdentityFingerprint,
		ValidMinutes:        60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.VerifyHostEnrollmentCertificateSignature(certificate, root, now); err != nil {
		t.Fatalf("issued certificate should verify: %v", err)
	}
	registration.Capabilities = []string{"shell.user"}
	registration.EnrollmentCertificate = &certificate
	host, err := gw.RegisterHost(registration)
	if err != nil {
		t.Fatal(err)
	}
	if host.Status != model.HostStatusPending {
		t.Fatalf("expected pending host, got %s", host.Status)
	}
}

func TestMemoryGatewayIssueEnrollmentCertificateRejectsCapabilityEscalation(t *testing.T) {
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentIssuer(model.NewTrustBundle("enrollment-root", issuerPublicKey), issuerPrivateKey)
	capabilities := []string{"shell.user"}
	ticket, err := gw.CreateTicket(model.HostModeManaged, 600, capabilities, "managed enrollment")
	if err != nil {
		t.Fatal(err)
	}
	registration := signedGatewayHostRegistration(t, ticket, capabilities)
	_, err = gw.IssueEnrollmentCertificate(EnrollmentCertificateRequest{
		TicketCode:          ticket.Code,
		Name:                registration.Name,
		OS:                  registration.OS,
		Arch:                registration.Arch,
		Capabilities:        []string{"shell.user", "git.diff"},
		IdentityKeyID:       registration.IdentityKeyID,
		IdentityPublicKey:   registration.IdentityPublicKey,
		IdentityFingerprint: registration.IdentityFingerprint,
		ValidMinutes:        60,
	})
	if err == nil || !strings.Contains(err.Error(), "exceed ticket capabilities") {
		t.Fatalf("expected capability escalation rejection, got %v", err)
	}
}

func TestMemoryGatewayRenewsEnrollmentCertificate(t *testing.T) {
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	currentNow := now
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	gw := NewMemoryGatewayWithClock(func() time.Time { return currentNow }).
		WithEnrollmentIssuer(root, issuerPrivateKey)
	capabilities := []string{"shell.user"}
	ticket, err := gw.CreateTicket(model.HostModeManaged, 600, capabilities, "managed enrollment")
	if err != nil {
		t.Fatal(err)
	}
	registration := signedGatewayHostRegistration(t, ticket, capabilities)
	certificate, err := gw.IssueEnrollmentCertificate(EnrollmentCertificateRequest{
		TicketCode:          ticket.Code,
		Name:                registration.Name,
		OS:                  registration.OS,
		Arch:                registration.Arch,
		Capabilities:        capabilities,
		IdentityKeyID:       registration.IdentityKeyID,
		IdentityPublicKey:   registration.IdentityPublicKey,
		IdentityFingerprint: registration.IdentityFingerprint,
		ValidMinutes:        30,
	})
	if err != nil {
		t.Fatal(err)
	}
	previousFingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		t.Fatal(err)
	}
	currentNow = now.Add(5 * time.Minute)
	renewed, err := gw.RenewEnrollmentCertificate(EnrollmentCertificateRenewalRequest{
		Certificate:  certificate,
		ValidMinutes: 120,
	})
	if err != nil {
		t.Fatal(err)
	}
	renewedFingerprint, err := model.HostEnrollmentCertificateFingerprint(renewed)
	if err != nil {
		t.Fatal(err)
	}
	if renewedFingerprint == previousFingerprint {
		t.Fatalf("expected renewed fingerprint to change, got %q", renewedFingerprint)
	}
	if renewed.TicketCode != certificate.TicketCode || renewed.Mode != certificate.Mode || renewed.HostName != certificate.HostName || renewed.SubjectIdentityFingerprint != certificate.SubjectIdentityFingerprint {
		t.Fatalf("renewal changed certificate scope: before=%#v after=%#v", certificate, renewed)
	}
	if renewed.OS != certificate.OS || renewed.Arch != certificate.Arch || !slices.Equal(renewed.Capabilities, certificate.Capabilities) {
		t.Fatalf("renewal changed platform/capabilities: before=%#v after=%#v", certificate, renewed)
	}
	if !renewed.NotAfter.After(certificate.NotAfter) {
		t.Fatalf("expected renewed certificate to extend validity: before=%s after=%s", certificate.NotAfter, renewed.NotAfter)
	}
	if err := model.VerifyHostEnrollmentCertificateSignature(renewed, root, currentNow); err != nil {
		t.Fatalf("renewed certificate should verify: %v", err)
	}
}

func TestMemoryGatewayEnrollmentRevocationsRejectCertificate(t *testing.T) {
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	capabilities := []string{"shell.user"}
	ticket, registration := enrollmentGatewayRegistration(t, now, capabilities, issuerPrivateKey)
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(*registration.EnrollmentCertificate)
	if err != nil {
		t.Fatal(err)
	}
	revocations, err := model.SignHostEnrollmentRevocationList([]model.HostEnrollmentCertificateRevocation{
		{
			CertificateFingerprint: fingerprint,
			Reason:                 "compromised host identity",
			RevokedAt:              now,
		},
	}, "enrollment-root", issuerPrivateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	gw := NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentRoot(root).
		WithEnrollmentRevocations(revocations)
	gw.tickets[ticket.ID] = ticket
	gw.codeIndex[ticket.Code] = ticket.ID
	_, err = gw.RegisterHost(registration)
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("expected revoked enrollment certificate rejection, got %v", err)
	}
}

func TestMemoryGatewayEnrollmentRevocationsRejectRenewal(t *testing.T) {
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	capabilities := []string{"shell.user"}
	ticket, registration := enrollmentGatewayRegistration(t, now, capabilities, issuerPrivateKey)
	certificate := *registration.EnrollmentCertificate
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		t.Fatal(err)
	}
	revocations, err := model.SignHostEnrollmentRevocationList([]model.HostEnrollmentCertificateRevocation{
		{
			CertificateFingerprint: fingerprint,
			Reason:                 "compromised host identity",
			RevokedAt:              now,
		},
	}, root.SigningKeyID, issuerPrivateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	gw := NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentIssuer(root, issuerPrivateKey).
		WithEnrollmentRevocations(revocations)
	gw.tickets[ticket.ID] = ticket
	gw.codeIndex[ticket.Code] = ticket.ID
	_, err = gw.RenewEnrollmentCertificate(EnrollmentCertificateRenewalRequest{
		Certificate:  certificate,
		ValidMinutes: 120,
	})
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("expected revoked enrollment certificate renewal rejection, got %v", err)
	}
}

func TestMemoryGatewayCreatesJoinManifestWithSeparateRoot(t *testing.T) {
	gatewayPublicKey, gatewayPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifestPublicKey, manifestPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-tasks", gatewayPublicKey, gatewayPrivateKey).
		WithManifestSigningKey("manifest-root", manifestPublicKey, manifestPrivateKey)
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := gw.JoinManifest(ticket.Code, "http://127.0.0.1:8787", "http://127.0.0.1:8787/join/"+ticket.Code)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SigningKeyID != "manifest-root" {
		t.Fatalf("expected manifest signing root, got %q", manifest.SigningKeyID)
	}
	if manifest.Trust.SigningKeyID != "gateway-tasks" {
		t.Fatalf("expected embedded gateway task trust, got %q", manifest.Trust.SigningKeyID)
	}
	if err := manifest.VerifyWithRoot(model.NewTrustBundle("manifest-root", manifestPublicKey), now); err != nil {
		t.Fatalf("expected manifest to verify with separate root: %v", err)
	}
	if err := manifest.Verify(now); !errors.Is(err, model.ErrJoinManifestInvalid) {
		t.Fatalf("expected dev self-trust verify to reject separate root, got %v", err)
	}
}

func TestMemoryGatewayRevokeTicketPreventsRegistration(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })

	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.RevokeTicket(ticket.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.RegisterHost(model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "win-temp-01",
		OS:         "windows",
		Arch:       "amd64",
	}); err == nil {
		t.Fatal("expected revoked ticket registration to fail")
	}
}

func TestMemoryGatewayRollbackTicketRevokesRegisteredHostsAndAuthorization(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	ticket, err := gw.CreateTicketWithMetadata(model.HostModeAttendedTemporary, 600, nil, "transaction rollback", map[string]string{
		"auto_activate": "attended-temporary",
	})
	if err != nil {
		t.Fatal(err)
	}
	host, secret, err := gw.RegisterHostWithIdempotencyKey("register-once", "request-hash", model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "rollback-host",
		OS:         "windows",
		Arch:       "amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !gw.ValidateHostSecret(host.ID, secret) {
		t.Fatal("expected host authorization before rollback")
	}
	if _, err := gw.RecordSupportSessionPreconnect(model.SupportSessionPreconnect{TicketCode: ticket.Code, Phase: "started"}); err != nil {
		t.Fatal(err)
	}

	rolledBack, affectedHosts, err := gw.RollbackTicket(ticket.ID, "handoff publication failed")
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.Status != model.TicketStatusRevoked {
		t.Fatalf("ticket status = %q, want revoked", rolledBack.Status)
	}
	if len(affectedHosts) != 1 || affectedHosts[0].ID != host.ID || affectedHosts[0].Status != model.HostStatusRevoked {
		t.Fatalf("affected hosts = %#v, want revoked host %q", affectedHosts, host.ID)
	}
	if gw.ValidateHostSecret(host.ID, secret) {
		t.Fatal("rollback retained host authorization")
	}
	if events := gw.SupportSessionPreconnects(ticket.Code); len(events) != 0 {
		t.Fatalf("rollback retained ticket preconnect state: %#v", events)
	}
	if _, err := gw.GenerateHostSecret(host.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("generate secret for rolled-back host error = %v, want invalid state", err)
	}
	if err := gw.HeartbeatHost(host.ID, secret); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("heartbeat after rollback error = %v, want policy denied", err)
	}
	if _, _, err := gw.RegisterHostWithIdempotencyKey("register-once", "request-hash", model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "rollback-host",
		OS:         "windows",
		Arch:       "amd64",
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("idempotent registration after rollback error = %v, want invalid state", err)
	}
	snapshot := gw.Snapshot()
	if len(snapshot.Hosts) != 1 || snapshot.Hosts[0].Status != model.HostStatusRevoked {
		t.Fatalf("rollback snapshot hosts = %#v, want revoked host", snapshot.Hosts)
	}
}

func TestMemoryGatewayRollbackTicketIsIdempotent(t *testing.T) {
	gw := NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "transaction rollback")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := gw.RollbackTicket(ticket.ID, "first rollback"); err != nil {
		t.Fatal(err)
	}
	rolledBack, hosts, err := gw.RollbackTicket(ticket.ID, "repeated rollback")
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.Status != model.TicketStatusRevoked || len(hosts) != 0 {
		t.Fatalf("repeated rollback = %#v, %#v", rolledBack, hosts)
	}
}

func TestMemoryGatewayProbingTicketOnlyAllowsPowerShellBootstrapUntilPublished(t *testing.T) {
	gw := NewMemoryGateway()
	ticket, err := gw.CreateProbingTicketWithMetadata(model.HostModeAttendedTemporary, 600, nil, "final probe", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != model.TicketStatusProbing {
		t.Fatalf("ticket status = %q, want probing", ticket.Status)
	}
	if err := gw.ValidatePowerShellBootstrapTicket(ticket.Code); err != nil {
		t.Fatalf("probing bootstrap rejected: %v", err)
	}
	if _, err := gw.JoinManifest(ticket.Code, "https://gateway.example.test", "https://gateway.example.test/join/"+ticket.Code); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("probing ticket JoinManifest error = %v, want invalid state", err)
	}
	if _, err := gw.RegisterHost(model.HostRegistration{TicketCode: ticket.Code, Name: "blocked", OS: "windows", Arch: "amd64"}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("probing ticket registration error = %v, want invalid state", err)
	}
	if _, err := gw.RecordSupportSessionPreconnect(model.SupportSessionPreconnect{TicketCode: ticket.Code, Phase: "started"}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("probing ticket preconnect error = %v, want invalid state", err)
	}
	published, err := gw.PublishTicket(ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != model.TicketStatusActive {
		t.Fatalf("published status = %q", published.Status)
	}
	if _, err := gw.RegisterHost(model.HostRegistration{TicketCode: ticket.Code, Name: "allowed", OS: "windows", Arch: "amd64"}); err != nil {
		t.Fatalf("published ticket registration rejected: %v", err)
	}
}

func TestMemoryGatewayRollbackTicketIfNoConnectedHostPreservesActiveHost(t *testing.T) {
	gw := NewMemoryGateway()
	ticket, err := gw.CreateTicketWithMetadata(model.HostModeAttendedTemporary, 600, nil, "availability race", map[string]string{"auto_activate": "attended-temporary"})
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{TicketCode: ticket.Code, Name: "connected", OS: "windows", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	current, _, rolledBack, err := gw.RollbackTicketIfNoConnectedHost(ticket.ID, "route lost")
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack || current.Status != model.TicketStatusActive {
		t.Fatalf("connected ticket was rolled back: ticket=%#v rolledBack=%v", current, rolledBack)
	}
	currentHost, err := gw.Host(host.ID)
	if err != nil || currentHost.Status != model.HostStatusActive {
		t.Fatalf("connected host changed: host=%#v err=%v", currentHost, err)
	}
}

func TestMemoryGatewayRollbackTicketIfNoConnectedHostRevokesPendingHost(t *testing.T) {
	gw := NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "pending route loss")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{TicketCode: ticket.Code, Name: "pending", OS: "windows", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	current, affected, rolledBack, err := gw.RollbackTicketIfNoConnectedHost(ticket.ID, "route lost")
	if err != nil {
		t.Fatal(err)
	}
	if !rolledBack || current.Status != model.TicketStatusRevoked || len(affected) != 1 || affected[0].ID != host.ID {
		t.Fatalf("pending host blocked rollback: ticket=%#v affected=%#v rolledBack=%v", current, affected, rolledBack)
	}
}

func TestMemoryGatewayRollbackTicketIfNoConnectedHostRevokesStaleActiveHost(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	ticket, err := gw.CreateTicketWithMetadata(model.HostModeAttendedTemporary, 600, nil, "stale route loss", map[string]string{"auto_activate": "attended-temporary"})
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{TicketCode: ticket.Code, Name: "stale", OS: "windows", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(hostHeartbeatStaleAfter + time.Second)
	current, affected, rolledBack, err := gw.RollbackTicketIfNoConnectedHost(ticket.ID, "route lost")
	if err != nil {
		t.Fatal(err)
	}
	if !rolledBack || current.Status != model.TicketStatusRevoked || len(affected) != 1 || affected[0].ID != host.ID {
		t.Fatalf("stale active host blocked rollback: ticket=%#v affected=%#v rolledBack=%v", current, affected, rolledBack)
	}
}

func TestMemoryGatewayProjectsStaleHosts(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)

	secret, err := gw.GenerateHostSecret(host.ID)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	if err := gw.HeartbeatHost(host.ID, secret); err != nil {
		t.Fatal(err)
	}
	fresh, err := gw.Host(host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Status != model.HostStatusActive {
		t.Fatalf("expected fresh host active, got %s", fresh.Status)
	}

	now = now.Add(91 * time.Second)
	stale, err := gw.Host(host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Status != model.HostStatusStale {
		t.Fatalf("expected stale host projection, got %s", stale.Status)
	}
	if hosts := gw.Hosts(string(model.HostStatusActive)); len(hosts) != 0 {
		t.Fatalf("expected active filter to hide stale hosts, got %#v", hosts)
	}
	if hosts := gw.Hosts(string(model.HostStatusStale)); len(hosts) != 1 || hosts[0].ID != host.ID {
		t.Fatalf("expected stale filter to return host, got %#v", hosts)
	}
}

func TestMemoryGatewayHostsForUnknownTicketCodeReturnsEmpty(t *testing.T) {
	gw := NewMemoryGateway()
	wantHostIDs := map[string]bool{}
	for _, name := range []string{"first-ticket-host", "second-ticket-host"} {
		ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "ticket-scoped host list")
		if err != nil {
			t.Fatal(err)
		}
		host, err := gw.RegisterHost(model.HostRegistration{
			TicketCode: ticket.Code,
			Name:       name,
			OS:         "windows",
			Arch:       "amd64",
		})
		if err != nil {
			t.Fatal(err)
		}
		wantHostIDs[host.ID] = true
	}

	if hosts := gw.HostsForTicketCode("UNKNOWN-NONEMPTY-TICKET", ""); len(hosts) != 0 {
		t.Fatalf("unknown nonempty ticket returned %d hosts, want 0", len(hosts))
	}
	hosts := gw.HostsForTicketCode("", "")
	if len(hosts) != len(wantHostIDs) {
		t.Fatalf("empty ticket code returned %d hosts, want %d", len(hosts), len(wantHostIDs))
	}
	for _, host := range hosts {
		if !wantHostIDs[host.ID] {
			t.Fatal("empty ticket code returned an unexpected host")
		}
	}
}

func TestMemoryGatewayAutoActivateSupersedesMatchingStaleHost(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	ticket, err := gw.CreateTicketWithMetadata(model.HostModeAttendedTemporary, 600, nil, "repair", map[string]string{
		"auto_activate": "attended-temporary",
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := gw.RegisterHost(model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "win-support-target",
		OS:         "windows",
		Arch:       "amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != model.HostStatusActive {
		t.Fatalf("expected first host auto-activated, got %s", first.Status)
	}
	now = now.Add(91 * time.Second)
	second, err := gw.RegisterHost(model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "win-support-target",
		OS:         "windows",
		Arch:       "amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != model.HostStatusActive {
		t.Fatalf("expected stale matching re-registration to auto-activate, got %s", second.Status)
	}
	old, err := gw.Host(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.Status != model.HostStatusRevoked {
		t.Fatalf("expected old host superseded/revoked, got %s", old.Status)
	}
}

func TestMemoryGatewayWritesAuditSink(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	store := audit.NewJSONLStore(path)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now }).WithAuditSink(&store)

	if _, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) == 0 {
		t.Fatal("expected audit file to contain an event")
	}
}

func encodeHostIdentityPublicKey(publicKey ed25519.PublicKey) string {
	return base64.RawURLEncoding.EncodeToString(publicKey)
}

func hostIdentityFingerprint(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func activeHost(t *testing.T, gw *MemoryGateway) model.Host {
	t.Helper()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "win-temp-01",
		OS:         "windows",
		Arch:       "amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ActivateHost(host.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	return host
}

func signedGatewayHostRegistration(t *testing.T, ticket model.Ticket, capabilities []string) model.HostRegistration {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	registration := model.HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "managed-host",
		OS:                  "darwin",
		Arch:                "arm64",
		Capabilities:        capabilities,
		IdentityKeyID:       "host-test",
		IdentityPublicKey:   encodeHostIdentityPublicKey(publicKey),
		IdentityFingerprint: hostIdentityFingerprint(publicKey),
	}
	proof, err := model.SignHostRegistration(registration, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	registration.IdentityProof = &proof
	return registration
}

func enrollmentGatewayRegistration(t *testing.T, now time.Time, capabilities []string, issuerPrivateKey ed25519.PrivateKey) (model.Ticket, model.HostRegistration) {
	t.Helper()
	ticket, err := model.NewTicket(model.HostModeManaged, 600, capabilities, "managed enrollment", now)
	if err != nil {
		t.Fatal(err)
	}
	registration := signedGatewayHostRegistration(t, ticket, capabilities)
	certificate, err := model.SignHostEnrollmentCertificate(registration, ticket, "enrollment-root", issuerPrivateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registration.EnrollmentCertificate = &certificate
	return ticket, registration
}
