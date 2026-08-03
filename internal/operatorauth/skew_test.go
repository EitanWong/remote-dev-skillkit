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

// OIDC clock-skew boundaries: expired iff exp <= now-skew; not-yet-valid iff
// nbf > now+skew.

func TestOIDCJWKSVerifierClockSkewBoundaries(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jwksSet{Keys: []jwkKey{{
			Kty: "RSA", Kid: "skew-key", Use: "sig", Alg: "RS256",
			N: EncodeRSAJWKValue(privateKey.PublicKey.N),
			E: EncodeRSAJWKValue(big.NewInt(int64(privateKey.PublicKey.E))),
		}}})
	}))
	defer server.Close()

	verifier, err := NewOIDCJWKSVerifier(context.Background(), OIDCJWKSFile{
		SchemaVersion:    OIDCJWKSSchemaVersion,
		Issuer:           "https://issuer.example.test/",
		Audience:         "rdev-gateway",
		JWKSURL:          server.URL,
		RolesClaim:       "roles",
		ClockSkewSeconds: 30,
	}, server.Client(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	sign := func(exp, nbf int64) string {
		token, err := SignOIDCJWKSToken("skew-key", privateKey, OIDCClaims{
			Issuer:    "https://issuer.example.test/",
			Subject:   "operator@example.test",
			Audiences: []string{"rdev-gateway"},
			ExpiresAt: exp,
			NotBefore: nbf,
			Roles:     []string{RoleOperator},
		}, "roles")
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	nowUnix := now.Unix()

	cases := []struct {
		name string
		exp  int64
		nbf  int64
		ok   bool
	}{
		{"fresh token", nowUnix + 3600, nowUnix - 60, true},
		{"exp exactly at skew boundary", nowUnix - 30, 0, false},
		{"exp one second inside skew", nowUnix - 29, 0, true},
		{"nbf exactly at skew boundary", nowUnix + 3600, nowUnix + 30, true},
		{"nbf one second beyond skew", nowUnix + 3600, nowUnix + 31, false},
		{"no nbf", nowUnix + 3600, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := sign(tc.exp, tc.nbf)
			_, err := verifier.VerifyToken(token)
			if tc.ok && err != nil {
				t.Fatalf("expected valid token, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected token to be rejected")
			}
		})
	}
}

// Hosted (EdDSA) verifier validates exp/nbf with a fixed clock.

func TestHostedIssuerTimeBoundaries(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey, err := GenerateHostedKey()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewHostedIssuer(HostedFile{
		SchemaVersion: HostedSchemaVersion,
		Issuer:        "https://auth.example.test/",
		Audience:      "rdev-gateway",
		Keys:          []HostedAuthKey{{KeyID: "k1", PublicKey: EncodePublicKey(publicKey)}},
	}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	sign := func(exp, nbf int64) string {
		token, err := SignHostedToken("k1", privateKey, HostedClaims{
			Issuer:    "https://auth.example.test/",
			Subject:   "operator@example.test",
			Audience:  "rdev-gateway",
			ExpiresAt: exp,
			NotBefore: nbf,
			Roles:     []string{RoleOperator},
		}, "roles")
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	nowUnix := now.Unix()

	cases := []struct {
		name string
		exp  int64
		nbf  int64
		ok   bool
	}{
		{"fresh token", nowUnix + 3600, 0, true},
		{"expired", nowUnix - 1, 0, false},
		{"exp exactly now is expired", nowUnix, 0, false},
		{"exp one second in future", nowUnix + 1, 0, true},
		{"nbf in future", nowUnix + 3600, nowUnix + 60, false},
		{"nbf in past", nowUnix + 3600, nowUnix - 60, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := sign(tc.exp, tc.nbf)
			_, err := issuer.verifyToken(token)
			if tc.ok && err != nil {
				t.Fatalf("expected valid token, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected token to be rejected")
			}
		})
	}
}
