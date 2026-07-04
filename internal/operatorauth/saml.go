package operatorauth

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"

	saml2 "github.com/russellhaering/gosaml2"
	dsig "github.com/russellhaering/goxmldsig"
)

const SAMLSchemaVersion = "rdev.saml-operator-auth.v1"

type SAMLFile struct {
	SchemaVersion        string `json:"schema_version"`
	IDPIssuer            string `json:"idp_issuer"`
	Audience             string `json:"audience"`
	AssertionConsumerURL string `json:"assertion_consumer_url"`
	RoleAttribute        string `json:"role_attribute"`
	SubjectAttribute     string `json:"subject_attribute,omitempty"`
	CertificatePEM       string `json:"certificate_pem,omitempty"`
	CertificateFile      string `json:"certificate_file,omitempty"`
}

type SAMLVerifier struct {
	idpIssuer            string
	audience             string
	assertionConsumerURL string
	roleAttribute        string
	subjectAttribute     string
	certificateCount     int
	sp                   *saml2.SAMLServiceProvider
	now                  func() time.Time
}

type SAMLClaims struct {
	Issuer                     string   `json:"issuer"`
	Subject                    string   `json:"subject"`
	Audience                   string   `json:"audience"`
	AssertionConsumerURL       string   `json:"assertion_consumer_url"`
	Roles                      []string `json:"roles"`
	ResponseSignatureValidated bool     `json:"response_signature_validated"`
}

func LoadSAML(path string) (*SAMLVerifier, SAMLFile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, SAMLFile{}, err
	}
	var file SAMLFile
	if err := json.Unmarshal(content, &file); err != nil {
		return nil, SAMLFile{}, err
	}
	verifier, err := NewSAMLVerifier(file, time.Now)
	if err != nil {
		return nil, SAMLFile{}, err
	}
	return verifier, file, nil
}

func NewSAMLVerifier(file SAMLFile, now func() time.Time) (*SAMLVerifier, error) {
	file.IDPIssuer = strings.TrimSpace(file.IDPIssuer)
	file.Audience = strings.TrimSpace(file.Audience)
	file.AssertionConsumerURL = strings.TrimSpace(file.AssertionConsumerURL)
	file.RoleAttribute = strings.TrimSpace(file.RoleAttribute)
	file.SubjectAttribute = strings.TrimSpace(file.SubjectAttribute)
	file.CertificateFile = strings.TrimSpace(file.CertificateFile)
	if file.SchemaVersion != SAMLSchemaVersion {
		return nil, fmt.Errorf("unsupported SAML operator auth schema %q", file.SchemaVersion)
	}
	if file.IDPIssuer == "" {
		return nil, fmt.Errorf("SAML idp_issuer is required")
	}
	if file.Audience == "" {
		return nil, fmt.Errorf("SAML audience is required")
	}
	if file.AssertionConsumerURL == "" {
		return nil, fmt.Errorf("SAML assertion_consumer_url is required")
	}
	if file.RoleAttribute == "" {
		return nil, fmt.Errorf("SAML role_attribute is required")
	}
	certs, err := samlCertificates(file)
	if err != nil {
		return nil, err
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("SAML operator auth requires at least one IdP certificate")
	}
	if now == nil {
		now = time.Now
	}
	store := &dsig.MemoryX509CertificateStore{Roots: certs}
	return &SAMLVerifier{
		idpIssuer:            file.IDPIssuer,
		audience:             file.Audience,
		assertionConsumerURL: file.AssertionConsumerURL,
		roleAttribute:        file.RoleAttribute,
		subjectAttribute:     file.SubjectAttribute,
		certificateCount:     len(certs),
		sp: &saml2.SAMLServiceProvider{
			IdentityProviderIssuer:      file.IDPIssuer,
			AssertionConsumerServiceURL: file.AssertionConsumerURL,
			AudienceURI:                 file.Audience,
			IDPCertificateStore:         store,
			AllowMissingAttributes:      false,
			MaximumDecompressedBodySize: 1 << 20,
		},
		now: now,
	}, nil
}

func (v *SAMLVerifier) Enabled() bool {
	return v != nil && v.sp != nil && v.certificateCount > 0
}

func (v *SAMLVerifier) CertificateCount() int {
	if v == nil {
		return 0
	}
	return v.certificateCount
}

func (v *SAMLVerifier) AuthorizeBearer(header string, allowedRoles ...string) bool {
	if !v.Enabled() {
		return true
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	response := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if response == "" {
		return false
	}
	claims, err := v.VerifyResponse(response)
	if err != nil {
		return false
	}
	return principalHasRole(Principal{ID: claims.Subject, Roles: claims.Roles}, allowedRoles)
}

func (v *SAMLVerifier) VerifyResponse(encodedResponse string) (SAMLClaims, error) {
	if !v.Enabled() {
		return SAMLClaims{}, fmt.Errorf("SAML verifier is disabled")
	}
	encodedResponse = strings.TrimSpace(encodedResponse)
	if encodedResponse == "" {
		return SAMLClaims{}, fmt.Errorf("SAML response is required")
	}
	if err := rejectWeakSAMLAlgorithms(encodedResponse); err != nil {
		return SAMLClaims{}, err
	}
	v.sp.Clock = dsig.NewFakeClockAt(v.now())
	info, err := v.sp.RetrieveAssertionInfo(encodedResponse)
	if err != nil {
		return SAMLClaims{}, err
	}
	if info.WarningInfo != nil {
		if info.WarningInfo.InvalidTime {
			return SAMLClaims{}, fmt.Errorf("SAML assertion time conditions are invalid")
		}
		if info.WarningInfo.NotInAudience {
			return SAMLClaims{}, fmt.Errorf("SAML assertion audience mismatch")
		}
		if info.WarningInfo.ProxyRestriction != nil || info.WarningInfo.OneTimeUse {
			return SAMLClaims{}, fmt.Errorf("SAML assertion contains unsupported proxy or one-time-use restrictions")
		}
	}
	roles := normalizeRoles(splitRoleValues(info.Values.GetAll(v.roleAttribute)))
	if len(roles) == 0 {
		return SAMLClaims{}, fmt.Errorf("SAML assertion has no roles in attribute %q", v.roleAttribute)
	}
	subject := strings.TrimSpace(info.NameID)
	if v.subjectAttribute != "" {
		subject = strings.TrimSpace(info.Values.Get(v.subjectAttribute))
	}
	if subject == "" {
		return SAMLClaims{}, fmt.Errorf("SAML subject is required")
	}
	return SAMLClaims{
		Issuer:                     v.idpIssuer,
		Subject:                    subject,
		Audience:                   v.audience,
		AssertionConsumerURL:       v.assertionConsumerURL,
		Roles:                      roles,
		ResponseSignatureValidated: info.ResponseSignatureValidated,
	}, nil
}

func samlCertificates(file SAMLFile) ([]*x509.Certificate, error) {
	var blocks []byte
	if strings.TrimSpace(file.CertificatePEM) != "" {
		blocks = append(blocks, []byte(file.CertificatePEM)...)
		if blocks[len(blocks)-1] != '\n' {
			blocks = append(blocks, '\n')
		}
	}
	if file.CertificateFile != "" {
		content, err := os.ReadFile(file.CertificateFile)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, content...)
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("SAML certificate_pem or certificate_file is required")
	}
	var certs []*x509.Certificate
	rest := blocks
	for {
		block, remaining := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = remaining
		if block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("SAML certificate material must contain certificates only, got %q", block.Type)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}
	if strings.TrimSpace(string(rest)) != "" {
		return nil, fmt.Errorf("SAML certificate PEM contains trailing non-PEM data")
	}
	return certs, nil
}

func rejectWeakSAMLAlgorithms(encodedResponse string) error {
	raw, err := base64.StdEncoding.DecodeString(encodedResponse)
	if err != nil {
		return err
	}
	lower := strings.ToLower(string(raw))
	for _, marker := range []string{
		"http://www.w3.org/2000/09/xmldsig#rsa-sha1",
		"http://www.w3.org/2000/09/xmldsig#dsa-sha1",
		"http://www.w3.org/2001/04/xmldsig-more#ecdsa-sha1",
		"http://www.w3.org/2000/09/xmldsig#sha1",
	} {
		if strings.Contains(lower, marker) {
			return fmt.Errorf("SAML response uses unsupported SHA-1 XML signature algorithm")
		}
	}
	return nil
}

func splitRoleValues(values []string) []string {
	var roles []string
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
		}) {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				roles = append(roles, trimmed)
			}
		}
	}
	return roles
}
