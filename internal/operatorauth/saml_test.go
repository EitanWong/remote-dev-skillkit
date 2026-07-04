package operatorauth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

func TestSAMLVerifierAuthorizesSignedResponseByRole(t *testing.T) {
	now := time.Date(2026, 7, 4, 23, 0, 0, 0, time.UTC)
	privateKey, certDER, certPEM := testSAMLCertificate(t, now)
	verifier, err := NewSAMLVerifier(SAMLFile{
		SchemaVersion:        SAMLSchemaVersion,
		IDPIssuer:            "https://idp.example.test/saml",
		Audience:             "rdev-gateway",
		AssertionConsumerURL: "https://gateway.example.test/saml/acs",
		RoleAttribute:        "rdev_roles",
		SubjectAttribute:     "email",
		CertificatePEM:       certPEM,
	}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	response := signedSAMLResponse(t, privateKey, certDER, now, "rdev-gateway", "operator auditor")
	if !verifier.AuthorizeBearer("Bearer "+response, RoleOperator) {
		t.Fatal("expected operator role to authorize")
	}
	if verifier.AuthorizeBearer("Bearer "+response, RoleIssuer) {
		t.Fatal("operator response should not authorize issuer-only action")
	}
	claims, err := verifier.VerifyResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "operator@example.test" || !containsRole(claims.Roles, RoleOperator) || !claims.ResponseSignatureValidated {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestSAMLVerifierRejectsWrongAudience(t *testing.T) {
	now := time.Date(2026, 7, 4, 23, 0, 0, 0, time.UTC)
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
	response := signedSAMLResponse(t, privateKey, certDER, now, "other-audience", RoleOperator)
	if verifier.AuthorizeBearer("Bearer "+response, RoleOperator) {
		t.Fatal("wrong audience should fail")
	}
}

func TestSAMLVerifierRejectsPrivateKeyMaterial(t *testing.T) {
	_, err := NewSAMLVerifier(SAMLFile{
		SchemaVersion:        SAMLSchemaVersion,
		IDPIssuer:            "https://idp.example.test/saml",
		Audience:             "rdev-gateway",
		AssertionConsumerURL: "https://gateway.example.test/saml/acs",
		RoleAttribute:        "rdev_roles",
		CertificatePEM:       "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n",
	}, time.Now)
	if err == nil || !strings.Contains(err.Error(), "certificates only") {
		t.Fatalf("expected private key material rejection, got %v", err)
	}
}

func testSAMLCertificate(t *testing.T, now time.Time) (*rsa.PrivateKey, []byte, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "rdev test idp"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
	return privateKey, certDER, certPEM
}

func signedSAMLResponse(t *testing.T, privateKey *rsa.PrivateKey, certDER []byte, now time.Time, audience, roles string) string {
	t.Helper()
	notBefore := now.Add(-time.Minute).UTC().Format(time.RFC3339)
	notOnOrAfter := now.Add(time.Hour).UTC().Format(time.RFC3339)
	issueInstant := now.UTC().Format(time.RFC3339)
	raw := fmt.Sprintf(`<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="response-id" Version="2.0" IssueInstant="%s" Destination="https://gateway.example.test/saml/acs">
  <saml:Issuer>https://idp.example.test/saml</saml:Issuer>
  <samlp:Status><samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></samlp:Status>
  <saml:Assertion ID="assertion-id" Version="2.0" IssueInstant="%s">
    <saml:Issuer>https://idp.example.test/saml</saml:Issuer>
    <saml:Subject>
      <saml:NameID>operator@example.test</saml:NameID>
      <saml:SubjectConfirmation Method="urn:oasis:names:tc:SAML:2.0:cm:bearer">
        <saml:SubjectConfirmationData NotOnOrAfter="%s" Recipient="https://gateway.example.test/saml/acs"/>
      </saml:SubjectConfirmation>
    </saml:Subject>
    <saml:Conditions NotBefore="%s" NotOnOrAfter="%s">
      <saml:AudienceRestriction><saml:Audience>%s</saml:Audience></saml:AudienceRestriction>
    </saml:Conditions>
    <saml:AttributeStatement>
      <saml:Attribute Name="email"><saml:AttributeValue>operator@example.test</saml:AttributeValue></saml:Attribute>
      <saml:Attribute Name="rdev_roles"><saml:AttributeValue>%s</saml:AttributeValue></saml:Attribute>
    </saml:AttributeStatement>
  </saml:Assertion>
</samlp:Response>`, issueInstant, issueInstant, notOnOrAfter, notBefore, notOnOrAfter, audience, roles)
	doc := etree.NewDocument()
	if err := doc.ReadFromString(raw); err != nil {
		t.Fatal(err)
	}
	ctx, err := dsig.NewSigningContext(privateKey, [][]byte{certDER})
	if err != nil {
		t.Fatal(err)
	}
	if err := ctx.SetSignatureMethod(dsig.RSASHA256SignatureMethod); err != nil {
		t.Fatal(err)
	}
	signed, err := ctx.SignEnveloped(doc.Root())
	if err != nil {
		t.Fatal(err)
	}
	out := etree.NewDocument()
	out.SetRoot(signed)
	xml, err := out.WriteToString()
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString([]byte(xml))
}

func containsRole(roles []string, role string) bool {
	for _, candidate := range roles {
		if candidate == role {
			return true
		}
	}
	return false
}
