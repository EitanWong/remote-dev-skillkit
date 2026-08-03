package operatorauth

import (
	"crypto/rsa"
	"strings"
	"testing"
	"time"
)

func newSAMLVerifierForTest(t *testing.T, now time.Time) (*SAMLVerifier, *rsa.PrivateKey, []byte) {
	t.Helper()
	privateKey, certDER, certPEM := testSAMLCertificate(t, now)
	verifier, err := NewSAMLVerifier(SAMLFile{
		SchemaVersion:        SAMLSchemaVersion,
		IDPIssuer:            "https://idp.example.test/saml",
		Audience:             "rdev-gateway",
		AssertionConsumerURL: "https://gateway.example.test/saml/acs",
		RoleAttribute:        "rdev_roles",
		CertificatePEM:       certPEM,
	}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return verifier, privateKey, certDER
}

func TestSAMLVerifierRejectsExpiredAssertion(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	verifier, privateKey, certDER := newSAMLVerifierForTest(t, now)

	response := signedSAMLResponse(t, privateKey, certDER, now, samlResponseSpec{
		audience:     "rdev-gateway",
		roles:        RoleOperator,
		notOnOrAfter: now.Add(-time.Hour).UTC().Format(time.RFC3339),
	})
	if _, err := verifier.VerifyResponse(response); err == nil {
		t.Fatal("expected expired assertion to fail")
	}
	if verifier.AuthorizeBearer("Bearer "+response, RoleOperator) {
		t.Fatal("expired assertion must not authorize")
	}
}

func TestSAMLVerifierRejectsWrongRecipient(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	verifier, privateKey, certDER := newSAMLVerifierForTest(t, now)

	response := signedSAMLResponse(t, privateKey, certDER, now, samlResponseSpec{
		audience:  "rdev-gateway",
		roles:     RoleOperator,
		recipient: "https://evil.example.test/saml/acs",
	})
	if _, err := verifier.VerifyResponse(response); err == nil {
		t.Fatal("expected wrong recipient to fail")
	}
}

func TestSAMLVerifierRejectsBadSignature(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	verifier, _, certDER := newSAMLVerifierForTest(t, now)
	wrongKey, _, _ := testSAMLCertificate(t, now)

	response := signedSAMLResponse(t, wrongKey, certDER, now, samlResponseSpec{
		audience: "rdev-gateway",
		roles:    RoleOperator,
	})
	if _, err := verifier.VerifyResponse(response); err == nil {
		t.Fatal("expected bad signature to fail")
	}
	if verifier.AuthorizeBearer("Bearer "+response, RoleOperator) {
		t.Fatal("bad signature must not authorize")
	}
}

func TestSAMLVerifierRejectsEmptyResponse(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	verifier, _, _ := newSAMLVerifierForTest(t, now)

	if _, err := verifier.VerifyResponse("   "); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("expected empty response rejection, got %v", err)
	}
}
