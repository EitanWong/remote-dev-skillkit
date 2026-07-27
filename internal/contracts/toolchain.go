package contracts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
)

const (
	ToolchainRequestSchemaVersion = "rdev.toolchain-request.v1"
	ToolchainPolicySchemaVersion  = "rdev.toolchain-policy.v1"

	ToolchainCodex      = "codex"
	ToolchainClaudeCode = "claude-code"

	ToolchainRegionGlobal     = "global"
	ToolchainRegionCNMainland = "cn-mainland"

	ToolchainProxyInherit  = "inherit"
	ToolchainProxyDisabled = "disabled"

	ToolchainAuthAPIKey = "api-key"
	ToolchainAuthBearer = "bearer"

	ToolchainArchiveZIP   = "zip"
	ToolchainArchiveTarGZ = "tar.gz"
)

var (
	toolchainExactVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$`)
	toolchainID           = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	toolchainEnvName      = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)
	toolchainSHA256       = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// ToolchainRequest is a bounded, non-secret installation request. It is safe
// to carry through the control plane because it references credentials only by
// environment-variable name; values never appear in this contract.
type ToolchainRequest struct {
	SchemaVersion string          `json:"schema_version"`
	Tool          string          `json:"tool"`
	Version       string          `json:"version"`
	Execute       bool            `json:"execute"`
	Policy        ToolchainPolicy `json:"policy"`
}

// ToolchainPolicy supplies only trusted installation and endpoint metadata.
// Proxy addresses are intentionally out of band: inherited host proxy settings
// stay local and are never copied into task payloads or artifacts.
type ToolchainPolicy struct {
	SchemaVersion string                  `json:"schema_version"`
	Region        string                  `json:"region"`
	ProxyMode     string                  `json:"proxy_mode"`
	Registries    []ToolchainRegistry     `json:"registries"`
	NodeBootstrap *ToolchainNodeBootstrap `json:"node_bootstrap,omitempty"`
	Endpoint      ToolchainEndpoint       `json:"endpoint,omitempty"`
}

type ToolchainRegistry struct {
	ID      string   `json:"id"`
	URL     string   `json:"url"`
	Regions []string `json:"regions"`
}

// ToolchainNodeBootstrap is an optional portable, user-scoped Node runtime.
// It lets fresh hosts install agent packages without a privileged system
// package-manager action or an arbitrary shell installer.
type ToolchainNodeBootstrap struct {
	Version           string                `json:"version"`
	MaxArchiveBytes   int64                 `json:"max_archive_bytes"`
	MaxExtractedBytes int64                 `json:"max_extracted_bytes"`
	Sources           []ToolchainNodeSource `json:"sources"`
}

type ToolchainNodeSource struct {
	ID      string   `json:"id"`
	URL     string   `json:"url"`
	SHA256  string   `json:"sha256"`
	Format  string   `json:"format"`
	Regions []string `json:"regions"`
}

// ToolchainEndpoint is optional. CredentialEnv must name an already-provisioned
// host-local secret; it is never a credential literal.
type ToolchainEndpoint struct {
	BaseURL       string `json:"base_url,omitempty"`
	Model         string `json:"model,omitempty"`
	CredentialEnv string `json:"credential_env,omitempty"`
	AuthMode      string `json:"auth_mode,omitempty"`
}

// DecodeToolchainRequest rejects unknown fields before normalizing and
// validating untrusted MCP/CLI JSON.
func DecodeToolchainRequest(raw any) (ToolchainRequest, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return ToolchainRequest{}, fmt.Errorf("encode toolchain request: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var request ToolchainRequest
	if err := decoder.Decode(&request); err != nil {
		return ToolchainRequest{}, fmt.Errorf("decode toolchain request: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ToolchainRequest{}, fmt.Errorf("decode toolchain request: trailing JSON value")
	}
	request.Normalize()
	if err := request.Validate(); err != nil {
		return ToolchainRequest{}, err
	}
	return request, nil
}

// TaskPayload returns the normalized object shape expected by hostrunner. The
// conversion prevents an in-memory typed value from bypassing the same JSON
// shape used by a remote gateway request.
func (r ToolchainRequest) TaskPayload() map[string]any {
	encoded, err := json.Marshal(r)
	if err != nil {
		return map[string]any{}
	}
	var request map[string]any
	if err := json.Unmarshal(encoded, &request); err != nil {
		return map[string]any{}
	}
	return map[string]any{"toolchain_request": request}
}

func (r *ToolchainRequest) Normalize() {
	r.SchemaVersion = strings.TrimSpace(r.SchemaVersion)
	r.Tool = strings.ToLower(strings.TrimSpace(r.Tool))
	r.Version = strings.TrimSpace(r.Version)
	r.Policy.Normalize()
}

func (p *ToolchainPolicy) Normalize() {
	p.SchemaVersion = strings.TrimSpace(p.SchemaVersion)
	p.Region = strings.ToLower(strings.TrimSpace(p.Region))
	p.ProxyMode = strings.ToLower(strings.TrimSpace(p.ProxyMode))
	for index := range p.Registries {
		p.Registries[index].ID = strings.ToLower(strings.TrimSpace(p.Registries[index].ID))
		p.Registries[index].URL = strings.TrimSpace(p.Registries[index].URL)
		for regionIndex := range p.Registries[index].Regions {
			p.Registries[index].Regions[regionIndex] = strings.ToLower(strings.TrimSpace(p.Registries[index].Regions[regionIndex]))
		}
	}
	if p.NodeBootstrap != nil {
		p.NodeBootstrap.Normalize()
	}
	p.Endpoint.BaseURL = strings.TrimSpace(p.Endpoint.BaseURL)
	p.Endpoint.Model = strings.TrimSpace(p.Endpoint.Model)
	p.Endpoint.CredentialEnv = strings.TrimSpace(p.Endpoint.CredentialEnv)
	p.Endpoint.AuthMode = strings.ToLower(strings.TrimSpace(p.Endpoint.AuthMode))
}

func (r ToolchainRequest) Validate() error {
	if r.SchemaVersion != ToolchainRequestSchemaVersion {
		return fmt.Errorf("toolchain_request schema_version must be %q", ToolchainRequestSchemaVersion)
	}
	switch r.Tool {
	case ToolchainCodex, ToolchainClaudeCode:
	default:
		return fmt.Errorf("toolchain_request tool must be %q or %q", ToolchainCodex, ToolchainClaudeCode)
	}
	if !toolchainExactVersion.MatchString(r.Version) {
		return fmt.Errorf("toolchain_request version must be an exact semantic version, not a floating tag")
	}
	return r.Policy.Validate()
}

func (p ToolchainPolicy) Validate() error {
	if p.SchemaVersion != ToolchainPolicySchemaVersion {
		return fmt.Errorf("toolchain policy schema_version must be %q", ToolchainPolicySchemaVersion)
	}
	switch p.Region {
	case ToolchainRegionGlobal, ToolchainRegionCNMainland:
	default:
		return fmt.Errorf("toolchain policy region must be %q or %q", ToolchainRegionGlobal, ToolchainRegionCNMainland)
	}
	switch p.ProxyMode {
	case ToolchainProxyInherit, ToolchainProxyDisabled:
	default:
		return fmt.Errorf("toolchain policy proxy_mode must be %q or %q", ToolchainProxyInherit, ToolchainProxyDisabled)
	}
	if len(p.Registries) == 0 {
		return fmt.Errorf("toolchain policy registries are required")
	}
	seenRegistryIDs := make(map[string]bool, len(p.Registries))
	for _, registry := range p.Registries {
		if !toolchainID.MatchString(registry.ID) || seenRegistryIDs[registry.ID] {
			return fmt.Errorf("toolchain registry id must be a unique lowercase identifier")
		}
		seenRegistryIDs[registry.ID] = true
		if err := validateToolchainHTTPSURL("registry", registry.URL); err != nil {
			return err
		}
		if len(registry.Regions) == 0 {
			return fmt.Errorf("toolchain registry %q regions are required", registry.ID)
		}
		seenRegions := make(map[string]bool, len(registry.Regions))
		for _, region := range registry.Regions {
			if seenRegions[region] {
				return fmt.Errorf("toolchain registry %q contains duplicate region %q", registry.ID, region)
			}
			seenRegions[region] = true
			switch region {
			case ToolchainRegionGlobal, ToolchainRegionCNMainland:
			default:
				return fmt.Errorf("toolchain registry %q has unsupported region %q", registry.ID, region)
			}
		}
	}
	if len(p.EligibleRegistries()) == 0 {
		return fmt.Errorf("toolchain policy has no registry eligible for region %q", p.Region)
	}
	if p.NodeBootstrap != nil {
		if err := p.NodeBootstrap.Validate(p.Region); err != nil {
			return err
		}
	}
	return p.Endpoint.Validate()
}

func (e ToolchainEndpoint) Validate() error {
	hasEndpoint := e.BaseURL != "" || e.Model != "" || e.CredentialEnv != "" || e.AuthMode != ""
	if !hasEndpoint {
		return nil
	}
	if err := validateToolchainHTTPSURL("endpoint", e.BaseURL); err != nil {
		return err
	}
	if e.Model == "" {
		return fmt.Errorf("toolchain endpoint model is required when endpoint routing is configured")
	}
	if !toolchainEnvName.MatchString(e.CredentialEnv) {
		return fmt.Errorf("toolchain endpoint credential_env must be an uppercase environment-variable name")
	}
	switch e.AuthMode {
	case ToolchainAuthAPIKey, ToolchainAuthBearer:
	default:
		return fmt.Errorf("toolchain endpoint auth_mode must be %q or %q", ToolchainAuthAPIKey, ToolchainAuthBearer)
	}
	return nil
}

func validateToolchainHTTPSURL(kind, raw string) error {
	value, err := url.Parse(raw)
	if err != nil || value.Scheme != "https" || value.Host == "" || value.User != nil || value.RawQuery != "" || value.Fragment != "" {
		return fmt.Errorf("toolchain %s URL must be an HTTPS URL without credentials, query, or fragment", kind)
	}
	return nil
}

// EligibleRegistries preserves policy ordering. A mainland mirror may therefore
// be listed before an official global source, producing deterministic fallback.
func (p ToolchainPolicy) EligibleRegistries() []ToolchainRegistry {
	result := make([]ToolchainRegistry, 0, len(p.Registries))
	for _, registry := range p.Registries {
		for _, region := range registry.Regions {
			if region == p.Region {
				result = append(result, ToolchainRegistry{
					ID:      registry.ID,
					URL:     registry.URL,
					Regions: append([]string(nil), registry.Regions...),
				})
				break
			}
		}
	}
	return result
}

func (b *ToolchainNodeBootstrap) Normalize() {
	b.Version = strings.TrimSpace(b.Version)
	for index := range b.Sources {
		b.Sources[index].ID = strings.ToLower(strings.TrimSpace(b.Sources[index].ID))
		b.Sources[index].URL = strings.TrimSpace(b.Sources[index].URL)
		b.Sources[index].SHA256 = strings.ToLower(strings.TrimSpace(b.Sources[index].SHA256))
		b.Sources[index].Format = strings.ToLower(strings.TrimSpace(b.Sources[index].Format))
		for regionIndex := range b.Sources[index].Regions {
			b.Sources[index].Regions[regionIndex] = strings.ToLower(strings.TrimSpace(b.Sources[index].Regions[regionIndex]))
		}
	}
}

func (b ToolchainNodeBootstrap) Validate(region string) error {
	if !toolchainExactVersion.MatchString(b.Version) {
		return fmt.Errorf("toolchain node bootstrap version must be an exact semantic version")
	}
	if b.MaxArchiveBytes <= 0 || b.MaxArchiveBytes > 1<<30 {
		return fmt.Errorf("toolchain node bootstrap max_archive_bytes must be between 1 and %d", int64(1<<30))
	}
	if b.MaxExtractedBytes <= 0 || b.MaxExtractedBytes > 2<<30 {
		return fmt.Errorf("toolchain node bootstrap max_extracted_bytes must be between 1 and %d", int64(2<<30))
	}
	if len(b.Sources) == 0 {
		return fmt.Errorf("toolchain node bootstrap sources are required")
	}
	seenIDs := make(map[string]bool, len(b.Sources))
	for _, source := range b.Sources {
		if !toolchainID.MatchString(source.ID) || seenIDs[source.ID] {
			return fmt.Errorf("toolchain node bootstrap source id must be a unique lowercase identifier")
		}
		seenIDs[source.ID] = true
		if err := validateToolchainHTTPSURL("node bootstrap source", source.URL); err != nil {
			return err
		}
		if !toolchainSHA256.MatchString(source.SHA256) {
			return fmt.Errorf("toolchain node bootstrap source %q sha256 must be 64 lowercase hexadecimal characters", source.ID)
		}
		switch source.Format {
		case ToolchainArchiveZIP, ToolchainArchiveTarGZ:
		default:
			return fmt.Errorf("toolchain node bootstrap source %q format must be %q or %q", source.ID, ToolchainArchiveZIP, ToolchainArchiveTarGZ)
		}
		if len(source.Regions) == 0 {
			return fmt.Errorf("toolchain node bootstrap source %q regions are required", source.ID)
		}
		seenRegions := make(map[string]bool, len(source.Regions))
		for _, sourceRegion := range source.Regions {
			if seenRegions[sourceRegion] {
				return fmt.Errorf("toolchain node bootstrap source %q contains duplicate region %q", source.ID, sourceRegion)
			}
			seenRegions[sourceRegion] = true
			switch sourceRegion {
			case ToolchainRegionGlobal, ToolchainRegionCNMainland:
			default:
				return fmt.Errorf("toolchain node bootstrap source %q has unsupported region %q", source.ID, sourceRegion)
			}
		}
	}
	if len(b.EligibleSources(region)) == 0 {
		return fmt.Errorf("toolchain node bootstrap has no source eligible for region %q", region)
	}
	return nil
}

// EligibleSources preserves source order for deterministic trusted fallback.
func (b ToolchainNodeBootstrap) EligibleSources(region string) []ToolchainNodeSource {
	result := make([]ToolchainNodeSource, 0, len(b.Sources))
	for _, source := range b.Sources {
		for _, sourceRegion := range source.Regions {
			if sourceRegion == region {
				result = append(result, ToolchainNodeSource{
					ID:      source.ID,
					URL:     source.URL,
					SHA256:  source.SHA256,
					Format:  source.Format,
					Regions: append([]string(nil), source.Regions...),
				})
				break
			}
		}
	}
	return result
}

func ToolchainRequiredCapabilities() []string {
	return []string{"package.install.requiresAuthorization"}
}

// ToolchainRequestSchema is embedded into rdev.sessions.task so a coordinating
// Agent can submit a typed, auditable bootstrap task rather than arbitrary shell.
func ToolchainRequestSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"schema_version", "tool", "version", "execute", "policy"},
		"properties": map[string]any{
			"schema_version": map[string]any{"type": "string", "const": ToolchainRequestSchemaVersion},
			"tool":           stringEnumSchema(ToolchainCodex, ToolchainClaudeCode),
			"version":        nonEmptyStringSchema(),
			"execute":        boolSchema(),
			"policy": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"schema_version", "region", "proxy_mode", "registries"},
				"properties": map[string]any{
					"schema_version": map[string]any{"type": "string", "const": ToolchainPolicySchemaVersion},
					"region":         stringEnumSchema(ToolchainRegionGlobal, ToolchainRegionCNMainland),
					"proxy_mode":     stringEnumSchema(ToolchainProxyInherit, ToolchainProxyDisabled),
					"registries": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []string{"id", "url", "regions"},
							"properties": map[string]any{
								"id":      nonEmptyStringSchema(),
								"url":     nonEmptyStringSchema(),
								"regions": stringArraySchema(),
							},
						},
					},
					"node_bootstrap": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"required":             []string{"version", "max_archive_bytes", "max_extracted_bytes", "sources"},
						"properties": map[string]any{
							"version":             nonEmptyStringSchema(),
							"max_archive_bytes":   map[string]any{"type": "integer", "minimum": 1},
							"max_extracted_bytes": map[string]any{"type": "integer", "minimum": 1},
							"sources": map[string]any{
								"type": "array",
								"items": map[string]any{
									"type":                 "object",
									"additionalProperties": false,
									"required":             []string{"id", "url", "sha256", "format", "regions"},
									"properties": map[string]any{
										"id":      nonEmptyStringSchema(),
										"url":     nonEmptyStringSchema(),
										"sha256":  nonEmptyStringSchema(),
										"format":  stringEnumSchema(ToolchainArchiveZIP, ToolchainArchiveTarGZ),
										"regions": stringArraySchema(),
									},
								},
							},
						},
					},
					"endpoint": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"base_url":       nonEmptyStringSchema(),
							"model":          nonEmptyStringSchema(),
							"credential_env": nonEmptyStringSchema(),
							"auth_mode":      stringEnumSchema(ToolchainAuthAPIKey, ToolchainAuthBearer),
						},
					},
				},
			},
		},
	}
}

func boolSchema() map[string]any {
	return map[string]any{"type": "boolean"}
}
