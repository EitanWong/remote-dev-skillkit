package operatorauth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInitDefaultWritesHashedTokensOnly(t *testing.T) {
	result, err := InitDefault(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.File.Principals) != 4 {
		t.Fatalf("expected 4 principals, got %d", len(result.File.Principals))
	}
	authPath := filepath.Join(t.TempDir(), "operators.json")
	tokenDir := filepath.Join(t.TempDir(), "tokens")
	if err := WriteFile(authPath, result.File, false); err != nil {
		t.Fatal(err)
	}
	if err := WriteTokenFiles(tokenDir, result.Tokens, false); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	for id, token := range result.Tokens {
		if strings.Contains(string(content), token) {
			t.Fatalf("auth file leaked token for %s", id)
		}
		tokenContent, err := os.ReadFile(filepath.Join(tokenDir, id+".token"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(tokenContent)) != token {
			t.Fatalf("unexpected token file for %s", id)
		}
	}
	auth, _, err := Load(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if !auth.AuthorizeBearer("Bearer "+result.Tokens["admin"], RoleOperator) {
		t.Fatal("admin should satisfy operator role")
	}
	if auth.AuthorizeBearer("Bearer "+result.Tokens["auditor"], RoleOperator) {
		t.Fatal("auditor should not satisfy operator role")
	}
}

func TestAuthorizeBearerRejectsUnknownToken(t *testing.T) {
	auth, err := New([]Principal{{
		ID:        "operator",
		Roles:     []string{RoleOperator},
		TokenHash: HashToken("known-token"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if auth.AuthorizeBearer("", RoleOperator) {
		t.Fatal("missing token should fail")
	}
	if auth.AuthorizeBearer("Bearer wrong-token", RoleOperator) {
		t.Fatal("wrong token should fail")
	}
	if !auth.AuthorizeBearer("Bearer known-token", RoleOperator) {
		t.Fatal("known operator token should pass")
	}
}
