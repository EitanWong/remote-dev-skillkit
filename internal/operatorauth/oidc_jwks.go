package operatorauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const OIDCJWKSSchemaVersion = "rdev.oidc-jwks-operator-auth.v1"

type OIDCJWKSFile struct {
	SchemaVersion    string `json:"schema_version"`
	Issuer           string `json:"issuer"`
	Audience         string `json:"audience"`
	JWKSURL          string `json:"jwks_url"`
	RolesClaim       string `json:"roles_claim"`
	ClockSkewSeconds int64  `json:"clock_skew_seconds,omitempty"`
}

type OIDCJWKSVerifier struct {
	issuer     string
	audience   string
	jwksURL    string
	rolesClaim string
	clockSkew  time.Duration
	keys       map[string]*rsa.PublicKey
	now        func() time.Time
	client     *http.Client
}

type OIDCClaims struct {
	Issuer    string
	Subject   string
	Audiences []string
	ExpiresAt int64
	NotBefore int64
	IssuedAt  int64
	Roles     []string
	Extra     map[string]any
}

type jwksSet struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use,omitempty"`
	Alg string `json:"alg,omitempty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func LoadOIDCJWKS(path string) (*OIDCJWKSVerifier, OIDCJWKSFile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, OIDCJWKSFile{}, err
	}
	var file OIDCJWKSFile
	if err := json.Unmarshal(content, &file); err != nil {
		return nil, OIDCJWKSFile{}, err
	}
	verifier, err := NewOIDCJWKSVerifier(context.Background(), file, http.DefaultClient, time.Now)
	if err != nil {
		return nil, OIDCJWKSFile{}, err
	}
	return verifier, file, nil
}

func NewOIDCJWKSVerifier(ctx context.Context, file OIDCJWKSFile, client *http.Client, now func() time.Time) (*OIDCJWKSVerifier, error) {
	file.Issuer = strings.TrimSpace(file.Issuer)
	file.Audience = strings.TrimSpace(file.Audience)
	file.JWKSURL = strings.TrimSpace(file.JWKSURL)
	file.RolesClaim = strings.TrimSpace(file.RolesClaim)
	if file.RolesClaim == "" {
		file.RolesClaim = "roles"
	}
	if file.SchemaVersion != OIDCJWKSSchemaVersion {
		return nil, fmt.Errorf("unsupported OIDC JWKS auth schema %q", file.SchemaVersion)
	}
	if file.Issuer == "" {
		return nil, fmt.Errorf("OIDC JWKS issuer is required")
	}
	if file.Audience == "" {
		return nil, fmt.Errorf("OIDC JWKS audience is required")
	}
	if err := validateOIDCJWKSURL(file.JWKSURL); err != nil {
		return nil, err
	}
	if client == nil {
		client = http.DefaultClient
	}
	if now == nil {
		now = time.Now
	}
	keys, err := fetchOIDCJWKS(ctx, client, file.JWKSURL)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("OIDC JWKS contains no supported RS256 keys")
	}
	skew := time.Duration(file.ClockSkewSeconds) * time.Second
	if skew < 0 {
		return nil, fmt.Errorf("OIDC JWKS clock_skew_seconds must be non-negative")
	}
	return &OIDCJWKSVerifier{
		issuer:     file.Issuer,
		audience:   file.Audience,
		jwksURL:    file.JWKSURL,
		rolesClaim: file.RolesClaim,
		clockSkew:  skew,
		keys:       keys,
		now:        now,
		client:     client,
	}, nil
}

func (v *OIDCJWKSVerifier) Enabled() bool {
	return v != nil && len(v.keys) > 0
}

func (v *OIDCJWKSVerifier) KeyCount() int {
	if v == nil {
		return 0
	}
	return len(v.keys)
}

func (v *OIDCJWKSVerifier) AuthorizeBearer(header string, allowedRoles ...string) bool {
	if !v.Enabled() {
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
	claims, err := v.VerifyToken(token)
	if err != nil {
		return false
	}
	return principalHasRole(Principal{ID: claims.Subject, Roles: claims.Roles}, allowedRoles)
}

func (v *OIDCJWKSVerifier) VerifyToken(token string) (OIDCClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return OIDCClaims{}, fmt.Errorf("OIDC token must be a compact JWT")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return OIDCClaims{}, err
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ,omitempty"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return OIDCClaims{}, err
	}
	if header.Alg != "RS256" {
		return OIDCClaims{}, fmt.Errorf("unsupported OIDC token alg %q", header.Alg)
	}
	publicKey, ok := v.keys[header.Kid]
	if !ok {
		return OIDCClaims{}, fmt.Errorf("unknown OIDC token key %q", header.Kid)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return OIDCClaims{}, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature); err != nil {
		return OIDCClaims{}, fmt.Errorf("OIDC token signature verification failed")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return OIDCClaims{}, err
	}
	claims, err := decodeOIDCClaims(payloadBytes, v.rolesClaim)
	if err != nil {
		return OIDCClaims{}, err
	}
	if claims.Issuer != v.issuer {
		return OIDCClaims{}, fmt.Errorf("OIDC token issuer mismatch")
	}
	if !audienceMatches(claims.Audiences, v.audience) {
		return OIDCClaims{}, fmt.Errorf("OIDC token audience mismatch")
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return OIDCClaims{}, fmt.Errorf("OIDC token subject is required")
	}
	now := v.now().Unix()
	skewSeconds := int64(v.clockSkew / time.Second)
	if claims.ExpiresAt <= now-skewSeconds {
		return OIDCClaims{}, fmt.Errorf("OIDC token expired")
	}
	if claims.NotBefore != 0 && claims.NotBefore > now+skewSeconds {
		return OIDCClaims{}, fmt.Errorf("OIDC token is not valid yet")
	}
	if len(claims.Roles) == 0 {
		return OIDCClaims{}, fmt.Errorf("OIDC token has no roles")
	}
	return claims, nil
}

func fetchOIDCJWKS(ctx context.Context, client *http.Client, jwksURL string) (map[string]*rsa.PublicKey, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch OIDC JWKS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("fetch OIDC JWKS: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var set jwksSet
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("parse OIDC JWKS: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, key := range set.Keys {
		publicKey, err := rsaPublicKeyFromJWK(key)
		if err != nil {
			continue
		}
		if _, exists := keys[key.Kid]; exists {
			return nil, fmt.Errorf("duplicate OIDC JWKS key id %q", key.Kid)
		}
		keys[key.Kid] = publicKey
	}
	return keys, nil
}

func validateOIDCJWKSURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse OIDC JWKS URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("OIDC JWKS URL must use https:// or localhost http://")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("OIDC JWKS URL must not contain credentials, query parameters, or fragments")
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Scheme == "http" && host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return fmt.Errorf("OIDC JWKS URL may use http:// only for localhost test endpoints")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return fmt.Errorf("OIDC JWKS URL missing host")
	}
	return nil
}

func rsaPublicKeyFromJWK(key jwkKey) (*rsa.PublicKey, error) {
	key.Kid = strings.TrimSpace(key.Kid)
	if key.Kid == "" {
		return nil, fmt.Errorf("OIDC JWKS key id is required")
	}
	if key.Kty != "RSA" {
		return nil, fmt.Errorf("unsupported OIDC JWKS key type %q", key.Kty)
	}
	if key.Use != "" && key.Use != "sig" {
		return nil, fmt.Errorf("unsupported OIDC JWKS key use %q", key.Use)
	}
	if key.Alg != "" && key.Alg != "RS256" {
		return nil, fmt.Errorf("unsupported OIDC JWKS key alg %q", key.Alg)
	}
	nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
	if err != nil {
		return nil, err
	}
	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)
	if n.Sign() <= 0 || !e.IsInt64() || e.Int64() < 3 {
		return nil, fmt.Errorf("invalid OIDC JWKS RSA key")
	}
	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}

func decodeOIDCClaims(payload []byte, rolesClaim string) (OIDCClaims, error) {
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return OIDCClaims{}, err
	}
	claims := OIDCClaims{Extra: raw}
	claims.Issuer, _ = raw["iss"].(string)
	claims.Subject, _ = raw["sub"].(string)
	claims.Audiences = oidcAudiences(raw["aud"])
	claims.ExpiresAt = int64Claim(raw["exp"])
	claims.NotBefore = int64Claim(raw["nbf"])
	claims.IssuedAt = int64Claim(raw["iat"])
	roles, err := rolesClaimValues(raw[rolesClaim])
	if err != nil {
		return OIDCClaims{}, err
	}
	claims.Roles = normalizeRoles(roles)
	return claims, nil
}

func oidcAudiences(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	case []string:
		return typed
	default:
		return nil
	}
}

func audienceMatches(audiences []string, expected string) bool {
	for _, audience := range audiences {
		if audience == expected {
			return true
		}
	}
	return false
}

func SignOIDCJWKSToken(keyID string, privateKey *rsa.PrivateKey, claims OIDCClaims, rolesClaim string) (string, error) {
	if keyID == "" {
		return "", fmt.Errorf("key id is required")
	}
	if privateKey == nil {
		return "", fmt.Errorf("private key is required")
	}
	if rolesClaim == "" {
		rolesClaim = "roles"
	}
	aud := any(claims.Audiences)
	if len(claims.Audiences) == 1 {
		aud = claims.Audiences[0]
	}
	header := map[string]string{"alg": "RS256", "kid": keyID, "typ": "JWT"}
	payload := map[string]any{
		"iss": claims.Issuer,
		"sub": claims.Subject,
		"aud": aud,
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
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func EncodeRSAJWKValue(value *big.Int) string {
	return base64.RawURLEncoding.EncodeToString(value.Bytes())
}
