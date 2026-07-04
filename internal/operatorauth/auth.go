package operatorauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	SchemaVersion = "rdev.operator-auth.v1"

	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleIssuer   = "issuer"
	RoleAuditor  = "auditor"
)

type Principal struct {
	ID        string   `json:"id"`
	Roles     []string `json:"roles"`
	TokenHash string   `json:"token_hash"`
}

type File struct {
	SchemaVersion string      `json:"schema_version"`
	HashAlg       string      `json:"hash_alg"`
	Principals    []Principal `json:"principals"`
	CreatedAt     string      `json:"created_at,omitempty"`
}

type Authorizer struct {
	principals []Principal
	hosted     []BearerAuthSource
}

type BearerAuthSource interface {
	Enabled() bool
	AuthorizeBearer(header string, allowedRoles ...string) bool
}

type InitResult struct {
	File   File
	Tokens map[string]string
}

func New(principals []Principal) (*Authorizer, error) {
	if len(principals) == 0 {
		return nil, fmt.Errorf("operator auth requires at least one principal")
	}
	seen := map[string]bool{}
	normalized := make([]Principal, 0, len(principals))
	for _, principal := range principals {
		principal.ID = strings.TrimSpace(principal.ID)
		principal.TokenHash = strings.TrimSpace(principal.TokenHash)
		principal.Roles = normalizeRoles(principal.Roles)
		if principal.ID == "" {
			return nil, fmt.Errorf("operator principal id is required")
		}
		if seen[principal.ID] {
			return nil, fmt.Errorf("duplicate operator principal id %q", principal.ID)
		}
		seen[principal.ID] = true
		if len(principal.Roles) == 0 {
			return nil, fmt.Errorf("operator principal %q requires at least one role", principal.ID)
		}
		if principal.TokenHash == "" {
			return nil, fmt.Errorf("operator principal %q requires token_hash", principal.ID)
		}
		if err := validateHash(principal.TokenHash); err != nil {
			return nil, fmt.Errorf("operator principal %q: %w", principal.ID, err)
		}
		normalized = append(normalized, principal)
	}
	return &Authorizer{principals: normalized}, nil
}

func NewCombined(principals []Principal, hosted ...*HostedIssuer) (*Authorizer, error) {
	sources := make([]BearerAuthSource, 0, len(hosted))
	for _, issuer := range hosted {
		sources = append(sources, issuer)
	}
	return NewCombinedSources(principals, sources...)
}

func NewCombinedSources(principals []Principal, hosted ...BearerAuthSource) (*Authorizer, error) {
	if len(principals) == 0 && len(hosted) == 0 {
		return nil, fmt.Errorf("operator auth requires at least one principal or hosted issuer")
	}
	var auth *Authorizer
	if len(principals) > 0 {
		created, err := New(principals)
		if err != nil {
			return nil, err
		}
		auth = created
	} else {
		auth = &Authorizer{}
	}
	for _, issuer := range hosted {
		if issuer == nil || !issuer.Enabled() {
			continue
		}
		auth.hosted = append(auth.hosted, issuer)
	}
	if len(auth.principals) == 0 && len(auth.hosted) == 0 {
		return nil, fmt.Errorf("operator auth requires at least one enabled auth source")
	}
	return auth, nil
}

func Load(path string) (*Authorizer, File, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, File{}, err
	}
	var file File
	if err := json.Unmarshal(content, &file); err != nil {
		return nil, File{}, err
	}
	if file.SchemaVersion != SchemaVersion {
		return nil, File{}, fmt.Errorf("unsupported operator auth schema %q", file.SchemaVersion)
	}
	if file.HashAlg != "sha256" {
		return nil, File{}, fmt.Errorf("unsupported operator auth hash algorithm %q", file.HashAlg)
	}
	auth, err := New(file.Principals)
	if err != nil {
		return nil, File{}, err
	}
	return auth, file, nil
}

func InitDefault(now time.Time) (InitResult, error) {
	specs := []struct {
		id    string
		roles []string
	}{
		{id: "admin", roles: []string{RoleAdmin}},
		{id: "operator", roles: []string{RoleOperator}},
		{id: "issuer", roles: []string{RoleIssuer}},
		{id: "auditor", roles: []string{RoleAuditor}},
	}
	tokens := map[string]string{}
	principals := make([]Principal, 0, len(specs))
	for _, spec := range specs {
		token, err := GenerateToken()
		if err != nil {
			return InitResult{}, err
		}
		tokens[spec.id] = token
		principals = append(principals, Principal{
			ID:        spec.id,
			Roles:     spec.roles,
			TokenHash: HashToken(token),
		})
	}
	file := File{
		SchemaVersion: SchemaVersion,
		HashAlg:       "sha256",
		Principals:    principals,
		CreatedAt:     now.UTC().Format(time.RFC3339),
	}
	return InitResult{File: file, Tokens: tokens}, nil
}

func WriteFile(path string, file File, force bool) error {
	content, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	flags := os.O_WRONLY | os.O_CREATE
	if force {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	out, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := out.Write(content); err != nil {
		return err
	}
	return out.Chmod(0o600)
}

func WriteTokenFiles(dir string, tokens map[string]string, force bool) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	ids := make([]string, 0, len(tokens))
	for id := range tokens {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		path := filepath.Join(dir, id+".token")
		flags := os.O_WRONLY | os.O_CREATE
		if force {
			flags |= os.O_TRUNC
		} else {
			flags |= os.O_EXCL
		}
		out, err := os.OpenFile(path, flags, 0o600)
		if err != nil {
			return err
		}
		if _, err := out.WriteString(tokens[id] + "\n"); err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Chmod(0o600); err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (a *Authorizer) Enabled() bool {
	return a != nil && (len(a.principals) > 0 || len(a.hosted) > 0)
}

func (a *Authorizer) AuthorizeBearer(header string, allowedRoles ...string) bool {
	if !a.Enabled() {
		return true
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return false
	}
	hash := HashToken(token)
	for _, principal := range a.principals {
		if subtle.ConstantTimeCompare([]byte(principal.TokenHash), []byte(hash)) != 1 {
			continue
		}
		return principalHasRole(principal, allowedRoles)
	}
	for _, hosted := range a.hosted {
		if hosted.AuthorizeBearer(header, allowedRoles...) {
			return true
		}
	}
	return false
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func GenerateToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "rdev_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func normalizeRoles(roles []string) []string {
	seen := map[string]bool{}
	normalized := make([]string, 0, len(roles))
	for _, role := range roles {
		role = strings.TrimSpace(strings.ToLower(role))
		if role == "" || seen[role] {
			continue
		}
		switch role {
		case RoleAdmin, RoleOperator, RoleIssuer, RoleAuditor:
			seen[role] = true
			normalized = append(normalized, role)
		}
	}
	sort.Strings(normalized)
	return normalized
}

func principalHasRole(principal Principal, allowedRoles []string) bool {
	if len(allowedRoles) == 0 {
		return true
	}
	roleSet := map[string]bool{}
	for _, role := range principal.Roles {
		roleSet[role] = true
	}
	if roleSet[RoleAdmin] {
		return true
	}
	for _, role := range allowedRoles {
		if roleSet[role] {
			return true
		}
	}
	return false
}

func validateHash(value string) error {
	if !strings.HasPrefix(value, "sha256:") {
		return fmt.Errorf("token_hash must use sha256:<hex>")
	}
	hexPart := strings.TrimPrefix(value, "sha256:")
	if len(hexPart) != 64 {
		return fmt.Errorf("token_hash must contain a 32-byte SHA-256 digest")
	}
	if _, err := hex.DecodeString(hexPart); err != nil {
		return fmt.Errorf("token_hash has invalid hex: %w", err)
	}
	return nil
}
