package operatorauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOIDCJWKSVerifierAuthorizesRS256TokenByRole(t *testing.T) {
	now := time.Date(2026, 7, 4, 23, 0, 0, 0, time.UTC)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jwksSet{Keys: []jwkKey{{
			Kty: "RSA",
			Kid: "oidc-key",
			Use: "sig",
			Alg: "RS256",
			N:   EncodeRSAJWKValue(privateKey.PublicKey.N),
			E:   EncodeRSAJWKValue(big.NewInt(int64(privateKey.PublicKey.E))),
		}}})
	}))
	defer server.Close()

	verifier, err := NewOIDCJWKSVerifier(context.Background(), OIDCJWKSFile{
		SchemaVersion:    OIDCJWKSSchemaVersion,
		Issuer:           "https://issuer.example.test/",
		Audience:         "rdev-gateway",
		JWKSURL:          server.URL,
		RolesClaim:       "rdev_roles",
		ClockSkewSeconds: 30,
	}, server.Client(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
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
		t.Fatal("expected operator role to authorize")
	}
	if verifier.AuthorizeBearer("Bearer "+token, RoleIssuer) {
		t.Fatal("operator role should not authorize issuer-only action")
	}
}

func TestOIDCJWKSVerifierRejectsWrongAudience(t *testing.T) {
	now := time.Date(2026, 7, 4, 23, 0, 0, 0, time.UTC)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jwksSet{Keys: []jwkKey{{
			Kty: "RSA",
			Kid: "oidc-key",
			Use: "sig",
			Alg: "RS256",
			N:   EncodeRSAJWKValue(privateKey.PublicKey.N),
			E:   EncodeRSAJWKValue(big.NewInt(int64(privateKey.PublicKey.E))),
		}}})
	}))
	defer server.Close()

	verifier, err := NewOIDCJWKSVerifier(context.Background(), OIDCJWKSFile{
		SchemaVersion: OIDCJWKSSchemaVersion,
		Issuer:        "https://issuer.example.test/",
		Audience:      "rdev-gateway",
		JWKSURL:       server.URL,
	}, server.Client(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	token, err := SignOIDCJWKSToken("oidc-key", privateKey, OIDCClaims{
		Issuer:    "https://issuer.example.test/",
		Subject:   "operator@example.test",
		Audiences: []string{"other-audience"},
		ExpiresAt: now.Add(time.Hour).Unix(),
		Roles:     []string{RoleOperator},
	}, "roles")
	if err != nil {
		t.Fatal(err)
	}
	if verifier.AuthorizeBearer("Bearer "+token, RoleOperator) {
		t.Fatal("wrong audience should fail")
	}
}

func TestOIDCJWKSVerifierRejectsUnsafeJWKSURL(t *testing.T) {
	for _, rawURL := range []string{
		"file:///tmp/jwks.json",
		"http://auth.example.test/jwks.json",
		"https://user@example.test/jwks.json",
		"https://issuer.example.test/jwks.json?token=secret",
		"https://issuer.example.test/jwks.json#secret",
	} {
		_, err := NewOIDCJWKSVerifier(context.Background(), OIDCJWKSFile{
			SchemaVersion: OIDCJWKSSchemaVersion,
			Issuer:        "https://issuer.example.test/",
			Audience:      "rdev-gateway",
			JWKSURL:       rawURL,
		}, nil, time.Now)
		if err == nil {
			t.Fatalf("expected unsafe JWKS URL rejection for %q", rawURL)
		}
	}
}
