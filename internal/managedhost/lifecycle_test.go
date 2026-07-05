package managedhost

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/operatorauth"
)

// --- test helpers ---

func generateKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func makeAuthorizer(t *testing.T, role string) (*operatorauth.Authorizer, string) {
	t.Helper()
	token, err := operatorauth.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	auth, err := operatorauth.New([]operatorauth.Principal{{
		ID:        "test-principal",
		Roles:     []string{role},
		TokenHash: operatorauth.HashToken(token),
	}})
	if err != nil {
		t.Fatal(err)
	}
	return auth, "Bearer " + token
}

func makeEnrollmentCertificate(t *testing.T, subjectPub ed25519.PublicKey, issuerKeyID string, issuerPriv ed25519.PrivateKey, now time.Time) model.HostEnrollmentCertificate {
	t.Helper()
	subjectTB := model.NewTrustBundle("subject", subjectPub)
	subjectFP, err := subjectTB.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := model.NewTicket(model.HostModeManaged, 3600, []string{"shell.user"}, "test", now)
	if err != nil {
		t.Fatal(err)
	}
	reg := model.HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "host-1",
		OS:                  "linux",
		Arch:                "amd64",
		Capabilities:        []string{"shell.user"},
		IdentityKeyID:       "subject",
		IdentityPublicKey:   base64.RawURLEncoding.EncodeToString(subjectPub),
		IdentityFingerprint: subjectFP,
	}
	cert, err := model.SignHostEnrollmentCertificate(reg, ticket, issuerKeyID, issuerPriv, now, time.Hour)
	if err != nil {
		t.Fatalf("SignHostEnrollmentCertificate: %v", err)
	}
	return cert
}

// --- EnrollmentRequest ---

func TestNewEnrollmentRequestRequiresHostName(t *testing.T) {
	pub, priv := generateKeyPair(t)
	_, err := NewEnrollmentRequest("", "darwin", "arm64", "key-1", "rdev_tok", pub, priv)
	if err == nil {
		t.Fatal("expected error for missing host name")
	}
}

func TestNewEnrollmentRequestRequiresToken(t *testing.T) {
	pub, priv := generateKeyPair(t)
	_, err := NewEnrollmentRequest("host-1", "darwin", "arm64", "key-1", "", pub, priv)
	if err == nil {
		t.Fatal("expected error for missing enrollment token")
	}
}

func TestNewEnrollmentRequestSetsSchemaAndNonce(t *testing.T) {
	pub, priv := generateKeyPair(t)
	req, err := NewEnrollmentRequest("host-1", "linux", "amd64", "key-1", "rdev_tok123", pub, priv)
	if err != nil {
		t.Fatalf("NewEnrollmentRequest: %v", err)
	}
	if req.SchemaVersion != EnrollmentRequestSchemaVersion {
		t.Fatalf("schema version = %q", req.SchemaVersion)
	}
	if req.Nonce == "" {
		t.Fatal("nonce must be set")
	}
	if req.IdentityProof == "" {
		t.Fatal("identity proof must be set")
	}
}

func TestValidateEnrollmentRequestAcceptsIssuerToken(t *testing.T) {
	pub, priv := generateKeyPair(t)
	req, err := NewEnrollmentRequest("host-1", "linux", "amd64", "key-1", "rdev_tok123", pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	auth, bearer := makeAuthorizer(t, operatorauth.RoleIssuer)
	if err := ValidateEnrollmentRequest(req, auth, bearer); err != nil {
		t.Fatalf("ValidateEnrollmentRequest: %v", err)
	}
}

func TestValidateEnrollmentRequestAcceptsAdminToken(t *testing.T) {
	pub, priv := generateKeyPair(t)
	req, err := NewEnrollmentRequest("host-1", "linux", "amd64", "key-1", "rdev_tok123", pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	auth, bearer := makeAuthorizer(t, operatorauth.RoleAdmin)
	if err := ValidateEnrollmentRequest(req, auth, bearer); err != nil {
		t.Fatalf("ValidateEnrollmentRequest admin: %v", err)
	}
}

func TestValidateEnrollmentRequestRejectsWrongBearerToken(t *testing.T) {
	pub, priv := generateKeyPair(t)
	req, err := NewEnrollmentRequest("host-1", "linux", "amd64", "key-1", "rdev_tok123", pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	auth, _ := makeAuthorizer(t, operatorauth.RoleIssuer)
	if err := ValidateEnrollmentRequest(req, auth, "Bearer wrong-token"); err == nil {
		t.Fatal("expected error for wrong bearer token")
	}
}

func TestValidateEnrollmentRequestRejectsTamperedHostName(t *testing.T) {
	pub, priv := generateKeyPair(t)
	req, err := NewEnrollmentRequest("host-1", "linux", "amd64", "key-1", "rdev_tok123", pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	req.HostName = "tampered"
	auth, bearer := makeAuthorizer(t, operatorauth.RoleIssuer)
	if err := ValidateEnrollmentRequest(req, auth, bearer); err == nil {
		t.Fatal("expected signature mismatch for tampered host name")
	}
}

func TestValidateEnrollmentRequestRejectsAuditorRole(t *testing.T) {
	pub, priv := generateKeyPair(t)
	req, err := NewEnrollmentRequest("host-1", "linux", "amd64", "key-1", "rdev_tok123", pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	auth, bearer := makeAuthorizer(t, operatorauth.RoleAuditor)
	if err := ValidateEnrollmentRequest(req, auth, bearer); err == nil {
		t.Fatal("expected error: auditor role must not be allowed to enroll")
	}
}

func TestValidateEnrollmentRequestWithNoAuthAlwaysPasses(t *testing.T) {
	pub, priv := generateKeyPair(t)
	req, err := NewEnrollmentRequest("host-1", "linux", "amd64", "key-1", "rdev_tok123", pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	// nil auth = no operator auth configured; identity proof still checked
	if err := ValidateEnrollmentRequest(req, nil, ""); err != nil {
		t.Fatalf("ValidateEnrollmentRequest nil auth: %v", err)
	}
}

// --- TrustFetchRequest ---

func TestNewTrustFetchRequestSetsSchemaAndProof(t *testing.T) {
	_, priv := generateKeyPair(t)
	now := time.Now().UTC()
	req, err := NewTrustFetchRequest("hst_abc", "sha256:aabb", "sha256:ccdd", 3, priv, now)
	if err != nil {
		t.Fatalf("NewTrustFetchRequest: %v", err)
	}
	if req.SchemaVersion != TrustFetchRequestSchemaVersion {
		t.Fatalf("schema version = %q", req.SchemaVersion)
	}
	if req.RequestProof == "" {
		t.Fatal("request proof must be set")
	}
}

func TestValidateTrustFetchRequestAcceptsValidRequest(t *testing.T) {
	pub, priv := generateKeyPair(t)
	now := time.Now().UTC()
	req, err := NewTrustFetchRequest("hst_abc", "sha256:aabb", "sha256:ccdd", 3, priv, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateTrustFetchRequest(req, pub, now); err != nil {
		t.Fatalf("ValidateTrustFetchRequest: %v", err)
	}
}

func TestValidateTrustFetchRequestRejectsTamperedHostID(t *testing.T) {
	pub, priv := generateKeyPair(t)
	now := time.Now().UTC()
	req, err := NewTrustFetchRequest("hst_abc", "sha256:aabb", "sha256:ccdd", 3, priv, now)
	if err != nil {
		t.Fatal(err)
	}
	req.HostID = "hst_other"
	if err := ValidateTrustFetchRequest(req, pub, now); err == nil {
		t.Fatal("expected signature mismatch for tampered host id")
	}
}

func TestValidateTrustFetchRequestRejectsWrongPublicKey(t *testing.T) {
	_, priv := generateKeyPair(t)
	wrongPub, _ := generateKeyPair(t)
	now := time.Now().UTC()
	req, err := NewTrustFetchRequest("hst_abc", "sha256:aabb", "sha256:ccdd", 3, priv, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateTrustFetchRequest(req, wrongPub, now); err == nil {
		t.Fatal("expected error for wrong public key")
	}
}

func TestValidateTrustFetchRequestRejectsMissingHostID(t *testing.T) {
	_, priv := generateKeyPair(t)
	now := time.Now().UTC()
	_, err := NewTrustFetchRequest("", "sha256:aabb", "sha256:ccdd", 3, priv, now)
	if err == nil {
		t.Fatal("expected error for missing host id")
	}
}

// --- ReEnrollmentRequest ---

func TestNewReEnrollmentRequestSetsSchemaAndProof(t *testing.T) {
	_, issuerPriv := generateKeyPair(t)
	now := time.Now().UTC()
	priorPub, priorPriv := generateKeyPair(t)
	cert := makeEnrollmentCertificate(t, priorPub, "issuer-key", issuerPriv, now)
	newPub, _ := generateKeyPair(t)
	req, err := NewReEnrollmentRequest("hst_abc", "key-v2", newPub, cert, priorPriv)
	if err != nil {
		t.Fatalf("NewReEnrollmentRequest: %v", err)
	}
	if req.SchemaVersion != ReEnrollRequestSchemaVersion {
		t.Fatalf("schema = %q", req.SchemaVersion)
	}
	if req.PriorCertProof == "" {
		t.Fatal("prior cert proof must be set")
	}
	if req.Nonce == "" {
		t.Fatal("nonce must be set")
	}
}

func TestValidateReEnrollmentRequestAcceptsValidRequest(t *testing.T) {
	issuerPub, issuerPriv := generateKeyPair(t)
	now := time.Now().UTC()
	priorPub, priorPriv := generateKeyPair(t)
	cert := makeEnrollmentCertificate(t, priorPub, "issuer-key", issuerPriv, now)
	enrollmentRoot := model.NewTrustBundle("issuer-key", issuerPub)
	newPub, _ := generateKeyPair(t)
	req, err := NewReEnrollmentRequest("hst_abc", "key-v2", newPub, cert, priorPriv)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReEnrollmentRequest(req, enrollmentRoot, priorPub, now); err != nil {
		t.Fatalf("ValidateReEnrollmentRequest: %v", err)
	}
}

func TestValidateReEnrollmentRequestRejectsMismatchedEnrollmentRoot(t *testing.T) {
	_, issuerPriv := generateKeyPair(t)
	wrongIssuerPub, _ := generateKeyPair(t)
	now := time.Now().UTC()
	priorPub, priorPriv := generateKeyPair(t)
	cert := makeEnrollmentCertificate(t, priorPub, "issuer-key", issuerPriv, now)
	// Root key ID matches but public key differs → signature verification fails.
	enrollmentRoot := model.NewTrustBundle("issuer-key", wrongIssuerPub)
	newPub, _ := generateKeyPair(t)
	req, err := NewReEnrollmentRequest("hst_abc", "key-v2", newPub, cert, priorPriv)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReEnrollmentRequest(req, enrollmentRoot, priorPub, now); err == nil {
		t.Fatal("expected error for mismatched enrollment root public key")
	}
}

func TestValidateReEnrollmentRequestRejectsTamperedPriorCert(t *testing.T) {
	issuerPub, issuerPriv := generateKeyPair(t)
	now := time.Now().UTC()
	priorPub, priorPriv := generateKeyPair(t)
	cert := makeEnrollmentCertificate(t, priorPub, "issuer-key", issuerPriv, now)
	enrollmentRoot := model.NewTrustBundle("issuer-key", issuerPub)
	newPub, _ := generateKeyPair(t)
	req, err := NewReEnrollmentRequest("hst_abc", "key-v2", newPub, cert, priorPriv)
	if err != nil {
		t.Fatal(err)
	}
	req.PriorCertificate.HostName = "tampered"
	if err := ValidateReEnrollmentRequest(req, enrollmentRoot, priorPub, now); err == nil {
		t.Fatal("expected error for tampered prior certificate")
	}
}

func TestValidateReEnrollmentRequestRejectsWrongPriorPublicKey(t *testing.T) {
	issuerPub, issuerPriv := generateKeyPair(t)
	now := time.Now().UTC()
	priorPub, priorPriv := generateKeyPair(t)
	cert := makeEnrollmentCertificate(t, priorPub, "issuer-key", issuerPriv, now)
	enrollmentRoot := model.NewTrustBundle("issuer-key", issuerPub)
	newPub, _ := generateKeyPair(t)
	req, err := NewReEnrollmentRequest("hst_abc", "key-v2", newPub, cert, priorPriv)
	if err != nil {
		t.Fatal(err)
	}
	wrongPub, _ := generateKeyPair(t)
	if err := ValidateReEnrollmentRequest(req, enrollmentRoot, wrongPub, now); err == nil {
		t.Fatal("expected error for wrong prior public key")
	}
}
