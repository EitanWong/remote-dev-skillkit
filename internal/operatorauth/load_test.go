package operatorauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadHostedFromFile(t *testing.T) {
	publicKey, privateKey, err := GenerateHostedKey()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "hosted.json")
	file := HostedFile{
		SchemaVersion: HostedSchemaVersion,
		Issuer:        "https://auth.example.test/",
		Audience:      "rdev-gateway",
		RolesClaim:    "roles",
		Keys:          []HostedAuthKey{{KeyID: "k1", PublicKey: EncodePublicKey(publicKey)}},
	}
	content, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	issuer, loaded, err := LoadHosted(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SchemaVersion != HostedSchemaVersion || !issuer.Enabled() {
		t.Fatalf("unexpected load result: %#v", loaded)
	}
	now := time.Now()
	token, err := SignHostedToken("k1", privateKey, HostedClaims{
		Issuer:    "https://auth.example.test/",
		Subject:   "operator@example.test",
		Audience:  "rdev-gateway",
		ExpiresAt: now.Add(time.Hour).Unix(),
		Roles:     []string{RoleOperator},
	}, "roles")
	if err != nil {
		t.Fatal(err)
	}
	if !issuer.AuthorizeBearer("Bearer "+token, RoleOperator) {
		t.Fatal("loaded issuer should authorize valid token")
	}
}

func TestLoadHostedRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	badJSON := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badJSON, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadHosted(badJSON); err == nil {
		t.Fatal("expected bad JSON to fail")
	}
	if _, _, err := LoadHosted(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("expected missing file to fail")
	}
	wrongSchema := filepath.Join(dir, "schema.json")
	content, _ := json.Marshal(HostedFile{SchemaVersion: "rdev.old", Issuer: "https://x/", Audience: "a",
		Keys: []HostedAuthKey{{KeyID: "k", PublicKey: ""}}})
	if err := os.WriteFile(wrongSchema, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadHosted(wrongSchema); err == nil {
		t.Fatal("expected wrong schema to fail")
	}
}

func TestLoadOIDCJWKSFromFile(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jwksSet{Keys: []jwkKey{{
			Kty: "RSA", Kid: "oidc-key", Use: "sig", Alg: "RS256",
			N: EncodeRSAJWKValue(privateKey.PublicKey.N),
			E: EncodeRSAJWKValue(big.NewInt(int64(privateKey.PublicKey.E))),
		}}})
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "oidc.json")
	content, err := json.Marshal(OIDCJWKSFile{
		SchemaVersion: OIDCJWKSSchemaVersion,
		Issuer:        "https://issuer.example.test/",
		Audience:      "rdev-gateway",
		JWKSURL:       server.URL,
		RolesClaim:    "rdev_roles",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	verifier, loaded, err := LoadOIDCJWKS(path)
	if err != nil {
		t.Fatal(err)
	}
	if verifier.KeyCount() != 1 {
		t.Fatalf("expected 1 key, got %d", verifier.KeyCount())
	}
	if loaded.JWKSURL != server.URL {
		t.Fatalf("unexpected loaded file: %#v", loaded)
	}
	now := time.Now()
	token, err := SignOIDCJWKSToken("oidc-key", privateKey, OIDCClaims{
		Issuer:    "https://issuer.example.test/",
		Subject:   "operator@example.test",
		Audiences: []string{"rdev-gateway"},
		ExpiresAt: now.Add(time.Hour).Unix(),
		NotBefore: now.Add(-time.Minute).Unix(),
		Roles:     []string{RoleOperator},
	}, "rdev_roles")
	if err != nil {
		t.Fatal(err)
	}
	if !verifier.AuthorizeBearer("Bearer "+token, RoleOperator) {
		t.Fatal("loaded OIDC verifier should authorize valid token")
	}
}

func TestLoadOIDCJWKSRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOIDCJWKS(bad); err == nil {
		t.Fatal("expected bad JSON to fail")
	}
	if _, _, err := LoadOIDCJWKS(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("expected missing file to fail")
	}
	// Valid JSON but unreachable JWKS URL must fail at load.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	file := OIDCJWKSFile{
		SchemaVersion: OIDCJWKSSchemaVersion,
		Issuer:        "https://issuer.example.test/",
		Audience:      "rdev-gateway",
		JWKSURL:       "https://127.0.0.1:1/jwks",
	}
	if _, err := NewOIDCJWKSVerifier(ctx, file, &http.Client{Timeout: 2 * time.Second}, time.Now); err == nil {
		t.Fatal("expected unreachable JWKS to fail")
	}
}

func TestLoadSAMLFromFile(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	_, _, certPEM := testSAMLCertificate(t, now)
	path := filepath.Join(t.TempDir(), "saml.json")
	content, err := json.Marshal(SAMLFile{
		SchemaVersion:        SAMLSchemaVersion,
		IDPIssuer:            "https://idp.example.test/saml",
		Audience:             "rdev-gateway",
		AssertionConsumerURL: "https://gateway.example.test/saml/acs",
		RoleAttribute:        "rdev_roles",
		CertificatePEM:       certPEM,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	verifier, loaded, err := LoadSAML(path)
	if err != nil {
		t.Fatal(err)
	}
	if verifier.CertificateCount() != 1 {
		t.Fatalf("expected 1 certificate, got %d", verifier.CertificateCount())
	}
	if loaded.IDPIssuer != "https://idp.example.test/saml" {
		t.Fatalf("unexpected loaded file: %#v", loaded)
	}
}

func TestLoadSAMLRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadSAML(bad); err == nil {
		t.Fatal("expected bad JSON to fail")
	}
	if _, _, err := LoadSAML(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("expected missing file to fail")
	}
	emptyCert := filepath.Join(dir, "empty-cert.json")
	content, _ := json.Marshal(SAMLFile{
		SchemaVersion:        SAMLSchemaVersion,
		IDPIssuer:            "https://idp.example.test/saml",
		Audience:             "rdev-gateway",
		AssertionConsumerURL: "https://gateway.example.test/saml/acs",
	})
	if err := os.WriteFile(emptyCert, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadSAML(emptyCert); err == nil {
		t.Fatal("expected missing certificate to fail")
	}
}

func TestValidateHashCornerCases(t *testing.T) {
	valid := "sha256:" + strings.Repeat("ab", 32)
	if err := validateHash(valid); err != nil {
		t.Fatalf("valid hash rejected: %v", err)
	}
	for _, value := range []string{
		"",
		"sha256:",
		"sha1:abc",
		"sha256:" + strings.Repeat("a", 63),
		"sha256:" + strings.Repeat("a", 65),
		"sha256:" + strings.Repeat("zz", 32),
		"plain",
	} {
		if err := validateHash(value); err == nil {
			t.Fatalf("validateHash(%q) should fail", value)
		}
	}
}

func TestInt64ClaimCornerCases(t *testing.T) {
	if got := int64Claim(float64(123)); got != 123 {
		t.Fatalf("float64: got %d", got)
	}
	if got := int64Claim(int64(456)); got != 456 {
		t.Fatalf("int64: got %d", got)
	}
	if got := int64Claim(json.Number("789")); got != 789 {
		t.Fatalf("json.Number: got %d", got)
	}
	if got := int64Claim("123"); got != 0 {
		t.Fatalf("string: got %d", got)
	}
	if got := int64Claim(nil); got != 0 {
		t.Fatalf("nil: got %d", got)
	}
}

func TestRolesClaimValuesCornerCases(t *testing.T) {
	roles, err := rolesClaimValues([]any{"a", "b"})
	if err != nil || len(roles) != 2 {
		t.Fatalf("[]any: %v %v", roles, err)
	}
	roles, err = rolesClaimValues([]string{"a"})
	if err != nil || len(roles) != 1 {
		t.Fatalf("[]string: %v %v", roles, err)
	}
	roles, err = rolesClaimValues("admin auditor")
	if err != nil || len(roles) != 2 {
		t.Fatalf("string fields: %v %v", roles, err)
	}
	if _, err := rolesClaimValues([]any{"a", 42}); err == nil {
		t.Fatal("mixed []any should fail")
	}
	if _, err := rolesClaimValues(42); err == nil {
		t.Fatal("numeric claim should fail")
	}
	if _, err := rolesClaimValues(nil); err == nil {
		t.Fatal("nil claim should fail")
	}
}

func TestOIDCAudiencesCornerCases(t *testing.T) {
	if got := oidcAudiences("single-aud"); len(got) != 1 || got[0] != "single-aud" {
		t.Fatalf("string: %v", got)
	}
	if got := oidcAudiences([]any{"a", 42, "b"}); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("mixed []any should keep strings only: %v", got)
	}
	if got := oidcAudiences([]string{"x"}); len(got) != 1 || got[0] != "x" {
		t.Fatalf("[]string: %v", got)
	}
	if got := oidcAudiences(42); got != nil {
		t.Fatalf("numeric: %v", got)
	}
	if got := oidcAudiences(nil); got != nil {
		t.Fatalf("nil: %v", got)
	}
	if !audienceMatches([]string{"a", "b"}, "b") || audienceMatches([]string{"a"}, "z") || audienceMatches(nil, "z") {
		t.Fatal("audienceMatches edge cases wrong")
	}
}
