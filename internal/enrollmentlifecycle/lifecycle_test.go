package enrollmentlifecycle

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestBuildFleetRenewalPlanClassifiesDueExpiredAndRevoked(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certDue := lifecycleCertificate(t, privateKey, now, 30*time.Minute, "due-host")
	certFresh := lifecycleCertificate(t, privateKey, now, 48*time.Hour, "fresh-host")
	dueFingerprint, err := model.HostEnrollmentCertificateFingerprint(certDue)
	if err != nil {
		t.Fatal(err)
	}
	revocations, err := model.SignHostEnrollmentRevocationList([]model.HostEnrollmentCertificateRevocation{{
		CertificateFingerprint: dueFingerprint,
		Reason:                 "drill",
		RevokedAt:              now,
	}}, "enrollment-root", privateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildFleetRenewalPlan([]model.HostEnrollmentCertificate{certFresh, certDue}, &revocations, FleetRenewalPolicy{
		RootPublicKey:      "enrollment-root:" + base64.RawURLEncoding.EncodeToString(publicKey),
		RenewBefore:        time.Hour,
		RenewValidFor:      24 * time.Hour,
		MaximumSkew:        30 * time.Second,
		RequireRevocations: true,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CertificateCount != 2 || plan.RenewalDueCount != 1 || plan.RevokedCount != 1 {
		t.Fatalf("unexpected plan counts: %#v", plan)
	}
	if !plan.Items[0].RenewalDue || !plan.Items[0].Revoked {
		t.Fatalf("expected due revoked certificate first, got %#v", plan.Items)
	}
}

func TestBuildFleetRenewalPlanRequiresRevocationsWhenPolicyRequiresThem(t *testing.T) {
	_, err := BuildFleetRenewalPlan(nil, nil, FleetRenewalPolicy{
		RootPublicKey:      "enrollment-root:abc",
		RenewBefore:        time.Hour,
		RenewValidFor:      24 * time.Hour,
		RequireRevocations: true,
	}, time.Now())
	if err == nil {
		t.Fatal("expected missing revocations to fail")
	}
}

func lifecycleCertificate(t *testing.T, privateKey ed25519.PrivateKey, now time.Time, ttl time.Duration, hostName string) model.HostEnrollmentCertificate {
	t.Helper()
	publicKey := privateKey.Public().(ed25519.PublicKey)
	publicKeyText := base64.RawURLEncoding.EncodeToString(publicKey)
	registration := model.HostRegistration{
		TicketCode:          "ABCD-1234",
		Name:                hostName,
		OS:                  "darwin",
		Arch:                "arm64",
		Capabilities:        []string{"codex.run"},
		IdentityKeyID:       "host",
		IdentityPublicKey:   publicKeyText,
		IdentityFingerprint: lifecyclePublicKeyFingerprint(publicKey),
	}
	ticket := model.Ticket{Code: "ABCD-1234", Mode: model.HostModeManaged, Capabilities: []string{"codex.run"}}
	certificate, err := model.SignHostEnrollmentCertificate(registration, ticket, "enrollment-root", privateKey, now, ttl)
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}

func lifecyclePublicKeyFingerprint(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}
