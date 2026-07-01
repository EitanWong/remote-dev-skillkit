package operatorauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const HostedSchemaVersion = "rdev.hosted-operator-auth.v1"

type HostedFile struct {
	SchemaVersion string          `json:"schema_version"`
	Issuer        string          `json:"issuer"`
	Audience      string          `json:"audience"`
	RolesClaim    string          `json:"roles_claim"`
	Keys          []HostedAuthKey `json:"keys"`
}

type HostedAuthKey struct {
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
}

type HostedIssuer struct {
	issuer     string
	audience   string
	rolesClaim string
	keys       map[string]ed25519.PublicKey
	now        func() time.Time
}

type HostedClaims struct {
	Issuer    string         `json:"iss"`
	Subject   string         `json:"sub"`
	Audience  string         `json:"aud"`
	ExpiresAt int64          `json:"exp"`
	NotBefore int64          `json:"nbf,omitempty"`
	IssuedAt  int64          `json:"iat,omitempty"`
	Roles     []string       `json:"roles,omitempty"`
	Extra     map[string]any `json:"-"`
}

func LoadHosted(path string) (*HostedIssuer, HostedFile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, HostedFile{}, err
	}
	var file HostedFile
	if err := json.Unmarshal(content, &file); err != nil {
		return nil, HostedFile{}, err
	}
	issuer, err := NewHostedIssuer(file, time.Now)
	if err != nil {
		return nil, HostedFile{}, err
	}
	return issuer, file, nil
}

func NewHostedIssuer(file HostedFile, now func() time.Time) (*HostedIssuer, error) {
	if file.SchemaVersion != HostedSchemaVersion {
		return nil, fmt.Errorf("unsupported hosted operator auth schema %q", file.SchemaVersion)
	}
	file.Issuer = strings.TrimSpace(file.Issuer)
	file.Audience = strings.TrimSpace(file.Audience)
	file.RolesClaim = strings.TrimSpace(file.RolesClaim)
	if file.RolesClaim == "" {
		file.RolesClaim = "roles"
	}
	if file.Issuer == "" {
		return nil, fmt.Errorf("hosted operator auth issuer is required")
	}
	if file.Audience == "" {
		return nil, fmt.Errorf("hosted operator auth audience is required")
	}
	if len(file.Keys) == 0 {
		return nil, fmt.Errorf("hosted operator auth requires at least one public key")
	}
	keys := map[string]ed25519.PublicKey{}
	for _, key := range file.Keys {
		key.KeyID = strings.TrimSpace(key.KeyID)
		if key.KeyID == "" {
			return nil, fmt.Errorf("hosted operator auth key_id is required")
		}
		if _, exists := keys[key.KeyID]; exists {
			return nil, fmt.Errorf("duplicate hosted operator auth key_id %q", key.KeyID)
		}
		publicKey, err := decodePublicKey(key.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("hosted operator auth key %q: %w", key.KeyID, err)
		}
		keys[key.KeyID] = publicKey
	}
	if now == nil {
		now = time.Now
	}
	return &HostedIssuer{
		issuer:     file.Issuer,
		audience:   file.Audience,
		rolesClaim: file.RolesClaim,
		keys:       keys,
		now:        now,
	}, nil
}

func (h *HostedIssuer) Enabled() bool {
	return h != nil && len(h.keys) > 0
}

func (h *HostedIssuer) AuthorizeBearer(header string, allowedRoles ...string) bool {
	if !h.Enabled() {
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
	claims, err := h.verifyToken(token)
	if err != nil {
		return false
	}
	return principalHasRole(Principal{ID: claims.Subject, Roles: claims.Roles}, allowedRoles)
}

func (h *HostedIssuer) verifyToken(token string) (HostedClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return HostedClaims{}, fmt.Errorf("hosted operator token must be a compact JWT")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return HostedClaims{}, err
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ,omitempty"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return HostedClaims{}, err
	}
	if header.Alg != "EdDSA" {
		return HostedClaims{}, fmt.Errorf("unsupported hosted operator token alg %q", header.Alg)
	}
	publicKey, ok := h.keys[header.Kid]
	if !ok {
		return HostedClaims{}, fmt.Errorf("unknown hosted operator token key %q", header.Kid)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return HostedClaims{}, err
	}
	signed := []byte(parts[0] + "." + parts[1])
	if !ed25519.Verify(publicKey, signed, signature) {
		return HostedClaims{}, fmt.Errorf("hosted operator token signature verification failed")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return HostedClaims{}, err
	}
	claims, err := decodeHostedClaims(payloadBytes, h.rolesClaim)
	if err != nil {
		return HostedClaims{}, err
	}
	if claims.Issuer != h.issuer {
		return HostedClaims{}, fmt.Errorf("hosted operator token issuer mismatch")
	}
	if claims.Audience != h.audience {
		return HostedClaims{}, fmt.Errorf("hosted operator token audience mismatch")
	}
	if claims.Subject == "" {
		return HostedClaims{}, fmt.Errorf("hosted operator token subject is required")
	}
	now := h.now().Unix()
	if claims.ExpiresAt <= now {
		return HostedClaims{}, fmt.Errorf("hosted operator token expired")
	}
	if claims.NotBefore != 0 && claims.NotBefore > now {
		return HostedClaims{}, fmt.Errorf("hosted operator token is not valid yet")
	}
	if len(claims.Roles) == 0 {
		return HostedClaims{}, fmt.Errorf("hosted operator token has no roles")
	}
	return claims, nil
}

func decodeHostedClaims(payload []byte, rolesClaim string) (HostedClaims, error) {
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return HostedClaims{}, err
	}
	claims := HostedClaims{Extra: raw}
	claims.Issuer, _ = raw["iss"].(string)
	claims.Subject, _ = raw["sub"].(string)
	claims.Audience, _ = raw["aud"].(string)
	claims.ExpiresAt = int64Claim(raw["exp"])
	claims.NotBefore = int64Claim(raw["nbf"])
	claims.IssuedAt = int64Claim(raw["iat"])
	roles, err := rolesClaimValues(raw[rolesClaim])
	if err != nil {
		return HostedClaims{}, err
	}
	claims.Roles = normalizeRoles(roles)
	return claims, nil
}

func rolesClaimValues(value any) ([]string, error) {
	switch typed := value.(type) {
	case []any:
		roles := make([]string, 0, len(typed))
		for _, role := range typed {
			roleText, ok := role.(string)
			if !ok {
				return nil, fmt.Errorf("roles claim must contain strings")
			}
			roles = append(roles, roleText)
		}
		return roles, nil
	case []string:
		return typed, nil
	case string:
		return strings.Fields(typed), nil
	default:
		return nil, fmt.Errorf("roles claim is required")
	}
}

func int64Claim(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case json.Number:
		result, _ := typed.Int64()
		return result
	default:
		return 0
	}
}

func SignHostedToken(keyID string, privateKey ed25519.PrivateKey, claims HostedClaims, rolesClaim string) (string, error) {
	if keyID == "" {
		return "", fmt.Errorf("key id is required")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("invalid private key")
	}
	if rolesClaim == "" {
		rolesClaim = "roles"
	}
	header := map[string]string{"alg": "EdDSA", "kid": keyID, "typ": "JWT"}
	payload := map[string]any{
		"iss": claims.Issuer,
		"sub": claims.Subject,
		"aud": claims.Audience,
		"exp": claims.ExpiresAt,
	}
	if claims.NotBefore != 0 {
		payload["nbf"] = claims.NotBefore
	}
	if claims.IssuedAt != 0 {
		payload["iat"] = claims.IssuedAt
	}
	payload[rolesClaim] = claims.Roles
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(payloadJSON)
	signature := ed25519.Sign(privateKey, []byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func GenerateHostedKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

func EncodePublicKey(publicKey ed25519.PublicKey) string {
	return base64.RawURLEncoding.EncodeToString(publicKey)
}

func decodePublicKey(value string) (ed25519.PublicKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key must be %d bytes", ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}
