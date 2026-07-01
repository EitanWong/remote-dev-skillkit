package operatorauth

import (
	"testing"
	"time"
)

func TestHostedIssuerAuthorizesSignedJWTByRole(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey, err := GenerateHostedKey()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewHostedIssuer(HostedFile{
		SchemaVersion: HostedSchemaVersion,
		Issuer:        "https://auth.example.com/",
		Audience:      "rdev-gateway",
		Keys: []HostedAuthKey{{
			KeyID:     "operator-key",
			PublicKey: EncodePublicKey(publicKey),
		}},
	}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	token, err := SignHostedToken("operator-key", privateKey, HostedClaims{
		Issuer:    "https://auth.example.com/",
		Subject:   "operator@example.com",
		Audience:  "rdev-gateway",
		ExpiresAt: now.Add(time.Hour).Unix(),
		Roles:     []string{RoleOperator},
	}, "roles")
	if err != nil {
		t.Fatal(err)
	}
	if !issuer.AuthorizeBearer("Bearer "+token, RoleOperator) {
		t.Fatal("expected operator role to authorize")
	}
	if issuer.AuthorizeBearer("Bearer "+token, RoleIssuer) {
		t.Fatal("operator role should not authorize issuer-only action")
	}
}

func TestHostedIssuerRejectsWrongAudience(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey, err := GenerateHostedKey()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewHostedIssuer(HostedFile{
		SchemaVersion: HostedSchemaVersion,
		Issuer:        "https://auth.example.com/",
		Audience:      "rdev-gateway",
		Keys: []HostedAuthKey{{
			KeyID:     "operator-key",
			PublicKey: EncodePublicKey(publicKey),
		}},
	}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	token, err := SignHostedToken("operator-key", privateKey, HostedClaims{
		Issuer:    "https://auth.example.com/",
		Subject:   "operator@example.com",
		Audience:  "other-service",
		ExpiresAt: now.Add(time.Hour).Unix(),
		Roles:     []string{RoleOperator},
	}, "roles")
	if err != nil {
		t.Fatal(err)
	}
	if issuer.AuthorizeBearer("Bearer "+token, RoleOperator) {
		t.Fatal("wrong audience should fail")
	}
}

func TestCombinedAuthorizerAcceptsLocalAndHostedSources(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey, err := GenerateHostedKey()
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewHostedIssuer(HostedFile{
		SchemaVersion: HostedSchemaVersion,
		Issuer:        "https://auth.example.com/",
		Audience:      "rdev-gateway",
		Keys: []HostedAuthKey{{
			KeyID:     "operator-key",
			PublicKey: EncodePublicKey(publicKey),
		}},
	}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	auth, err := NewCombined([]Principal{{
		ID:        "auditor",
		Roles:     []string{RoleAuditor},
		TokenHash: HashToken("audit-token"),
	}}, issuer)
	if err != nil {
		t.Fatal(err)
	}
	token, err := SignHostedToken("operator-key", privateKey, HostedClaims{
		Issuer:    "https://auth.example.com/",
		Subject:   "issuer@example.com",
		Audience:  "rdev-gateway",
		ExpiresAt: now.Add(time.Hour).Unix(),
		Roles:     []string{RoleIssuer},
	}, "roles")
	if err != nil {
		t.Fatal(err)
	}
	if !auth.AuthorizeBearer("Bearer audit-token", RoleAuditor) {
		t.Fatal("local auditor token should pass")
	}
	if !auth.AuthorizeBearer("Bearer "+token, RoleIssuer) {
		t.Fatal("hosted issuer token should pass")
	}
}
