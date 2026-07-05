package rustdeskadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const PackageSchemaVersion = "rdev.rustdesk-adapter-package.v1"
const VerificationSchemaVersion = "rdev.rustdesk-adapter-package-verification.v1"
const AcceptanceEvidencePlanSchemaVersion = "rdev.rustdesk-adapter-acceptance-evidence-plan.v1"

// AdapterKind is the canonical identifier for this adapter family.
const AdapterKind = "rustdesk"

// Options configures package generation.
type Options struct {
	OutDir      string
	Name        string
	Variant     string // "rustdesk" | "meshcentral"
	GeneratedAt time.Time
	Force       bool
}

// Package is the machine-readable manifest for a RustDesk/MeshCentral remote
// desktop adapter package. It describes how agents should launch the helper,
// what approvals are required, and what evidence must be collected.
type Package struct {
	SchemaVersion    string        `json:"schema_version"`
	Name             string        `json:"name"`
	GeneratedAt      time.Time     `json:"generated_at"`
	AdapterKind      string        `json:"adapter_kind"`
	Variant          string        `json:"variant"`
	ExternalMutation bool          `json:"external_mutation"`
	ProductionClaim  string        `json:"production_claim"`
	Helper           Helper        `json:"helper"`
	ConnectionPathID string        `json:"connection_path_id"`
	EvidencePlanPath string        `json:"evidence_plan_path"`
	EvidenceRequired []string      `json:"evidence_required"`
	ApprovalRequired []string      `json:"approval_required"`
	AgentRules       []string      `json:"agent_rules"`
	Files            []PackageFile `json:"files"`
	Checks           []Check       `json:"checks"`
}

// Helper describes the remote desktop helper binary.
type Helper struct {
	Tool               string   `json:"tool"`
	Aliases            []string `json:"aliases,omitempty"`
	Scope              string   `json:"scope"`
	SupportedPlatforms []string `json:"supported_platforms"`
	VerifyCommand      string   `json:"verify_command"`
	RuntimeStatus      string   `json:"runtime_status"`
}

// PackageFile is a file included in the package manifest.
type PackageFile struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
	Kind      string `json:"kind"`
}

// Check is a single package check result.
type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// Verification is the result of verifying a package against expected content.
type Verification struct {
	SchemaVersion      string      `json:"schema_version"`
	PackagePath        string      `json:"package_path"`
	PackageDir         string      `json:"package_dir"`
	Name               string      `json:"name,omitempty"`
	AdapterKind        string      `json:"adapter_kind,omitempty"`
	Variant            string      `json:"variant,omitempty"`
	Checks             []Check     `json:"checks"`
	Files              []FileCheck `json:"files"`
	RecommendedActions []string    `json:"recommended_actions,omitempty"`
}

// FileCheck is the result of verifying a single package file.
type FileCheck struct {
	Path           string  `json:"path"`
	Kind           string  `json:"kind"`
	ExpectedSHA256 string  `json:"expected_sha256"`
	ActualSHA256   string  `json:"actual_sha256,omitempty"`
	ExpectedSize   int64   `json:"expected_size"`
	ActualSize     int64   `json:"actual_size,omitempty"`
	Checks         []Check `json:"checks"`
}

// AcceptanceEvidencePlan describes how agents should collect real evidence.
type AcceptanceEvidencePlan struct {
	SchemaVersion    string             `json:"schema_version"`
	GeneratedAt      time.Time          `json:"generated_at"`
	AdapterKind      string             `json:"adapter_kind"`
	Variant          string             `json:"variant"`
	ConnectionPathID string             `json:"connection_path_id"`
	PackagePath      string             `json:"package_path"`
	ExternalMutation bool               `json:"external_mutation"`
	EvidenceFiles    []EvidencePlanFile `json:"evidence_files"`
	AgentRules       []string           `json:"agent_rules"`
	ApprovalRequired []string           `json:"approval_required"`
}

// EvidencePlanFile describes a single expected evidence file.
type EvidencePlanFile struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

func (p Package) OK() bool {
	if len(p.Checks) == 0 || len(p.Files) == 0 {
		return false
	}
	for _, c := range p.Checks {
		if !c.Passed {
			return false
		}
	}
	return true
}

func (v Verification) OK() bool {
	if len(v.Checks) == 0 || len(v.Files) == 0 {
		return false
	}
	for _, c := range v.Checks {
		if !c.Passed {
			return false
		}
	}
	for _, f := range v.Files {
		for _, c := range f.Checks {
			if !c.Passed {
				return false
			}
		}
	}
	return true
}

// Build generates a RustDesk/MeshCentral adapter package in opts.OutDir.
func Build(opts Options) (Package, error) {
	if strings.TrimSpace(opts.OutDir) == "" {
		return Package{}, fmt.Errorf("out is required")
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return Package{}, err
	}
	if err := prepareOut(outDir, opts.Force); err != nil {
		return Package{}, err
	}
	now := opts.GeneratedAt
	if now.IsZero() {
		now = time.Now()
	}
	variant := normalizeVariant(opts.Variant)
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "rdev-" + variant + "-remote-desktop-adapter"
	}
	desc := variantDescriptor(variant)
	pkg := Package{
		SchemaVersion:    PackageSchemaVersion,
		Name:             name,
		GeneratedAt:      now.UTC(),
		AdapterKind:      AdapterKind,
		Variant:          variant,
		ExternalMutation: true, // remote desktop sessions mutate the remote host
		ProductionClaim:  "remote-desktop-adapter-package-surface-only",
		Helper:           desc.Helper,
		ConnectionPathID: desc.ConnectionPathID,
		EvidencePlanPath: "acceptance-evidence-plan.json",
		EvidenceRequired: []string{
			"remote desktop adapter package verification",
			"helper binary detection or reviewed install report",
			"redacted session start evidence (no passwords or private IPs)",
			"host registration and remote session transport evidence",
			"session teardown evidence confirming no persistence",
		},
		ApprovalRequired: []string{
			"creating remote-desktop accounts, keys, IDs, or relay credentials",
			"opening inbound ports, firewall rules, or NAT entries for remote access",
			"installing persistent remote-access services or autostart entries",
			"using paid relay or cloud-managed remote desktop services",
			"storing session recordings or screen captures outside the workspace",
		},
		AgentRules: []string{
			"Use this package to set the declared helper; do not write ad hoc remote-access scripts.",
			"Scaffold evidence before collecting real remote-desktop evidence.",
			"Ask one short question only when the endpoint, credential, relay, or persistence decision is unclear.",
			"Keep real session IDs, passwords, relay addresses, and private IPs outside this package.",
			"Confirm session teardown evidence before marking the job complete.",
		},
	}
	pkg.Checks = packageChecks(pkg)
	files := []struct {
		path    string
		kind    string
		content []byte
	}{
		{"RUSTDESK_ADAPTER.md", "documentation", []byte(renderReadme(pkg))},
	}
	evidencePlan := acceptanceEvidencePlan(pkg, now)
	planContent, err := json.MarshalIndent(evidencePlan, "", "  ")
	if err != nil {
		return Package{}, err
	}
	files = append(files, struct {
		path    string
		kind    string
		content []byte
	}{"acceptance-evidence-plan.json", "acceptance-evidence-plan", append(planContent, '\n')})
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(outDir, f.path), f.content, 0o644); err != nil {
			return Package{}, err
		}
	}
	for _, f := range files {
		entry, err := packageFile(outDir, f.path, f.kind)
		if err != nil {
			return Package{}, err
		}
		pkg.Files = append(pkg.Files, entry)
	}
	sort.Slice(pkg.Files, func(i, j int) bool { return pkg.Files[i].Path < pkg.Files[j].Path })
	manifest, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return Package{}, err
	}
	if err := os.WriteFile(filepath.Join(outDir, "rustdesk-adapter.json"), append(manifest, '\n'), 0o644); err != nil {
		return Package{}, err
	}
	return pkg, nil
}

// Verify checks an existing package directory for correctness.
func Verify(path string) (Verification, error) {
	manifestPath, dir, err := resolveManifest(path)
	if err != nil {
		return Verification{}, err
	}
	v := Verification{
		SchemaVersion: VerificationSchemaVersion,
		PackagePath:   "rustdesk-adapter.json",
		PackageDir:    ".",
	}
	add := func(name string, passed bool, detail string) {
		v.Checks = append(v.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	content, err := os.ReadFile(manifestPath)
	add("manifest_exists", err == nil, "rustdesk-adapter.json")
	if err != nil {
		v.RecommendedActions = failureActions()
		return v, nil
	}
	var pkg Package
	err = json.Unmarshal(content, &pkg)
	add("manifest_json_valid", err == nil, errorDetail(err))
	if err != nil {
		v.RecommendedActions = failureActions()
		return v, nil
	}
	v.Name = pkg.Name
	v.AdapterKind = pkg.AdapterKind
	v.Variant = pkg.Variant
	add("schema_version", pkg.SchemaVersion == PackageSchemaVersion, pkg.SchemaVersion)
	add("adapter_kind", pkg.AdapterKind == AdapterKind, pkg.AdapterKind)
	add("variant_supported", supportedVariant(pkg.Variant), pkg.Variant)
	add("production_claim_scoped", pkg.ProductionClaim == "remote-desktop-adapter-package-surface-only", pkg.ProductionClaim)
	add("approval_boundaries_declared", len(pkg.ApprovalRequired) >= 4, fmt.Sprintf("%d", len(pkg.ApprovalRequired)))
	add("evidence_required_declared", len(pkg.EvidenceRequired) >= 4, fmt.Sprintf("%d", len(pkg.EvidenceRequired)))
	add("agent_rules_present", len(pkg.AgentRules) >= 4, fmt.Sprintf("%d", len(pkg.AgentRules)))
	add("acceptance_evidence_plan_declared", pkg.EvidencePlanPath == "acceptance-evidence-plan.json", pkg.EvidencePlanPath)
	for _, check := range packageChecks(pkg) {
		v.Checks = append(v.Checks, check)
	}
	v.Files = verifyFiles(dir, pkg.Files)
	if !v.OK() {
		v.RecommendedActions = failureActions()
	}
	return v, nil
}

// --- internals ---

type variantDesc struct {
	ConnectionPathID string
	Helper           Helper
}

func normalizeVariant(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "meshcentral", "mesh-central":
		return "meshcentral"
	default:
		return "rustdesk"
	}
}

func supportedVariant(v string) bool {
	switch normalizeVariant(v) {
	case "rustdesk", "meshcentral":
		return true
	default:
		return false
	}
}

func variantDescriptor(variant string) variantDesc {
	switch variant {
	case "meshcentral":
		return variantDesc{
			ConnectionPathID: "existing-meshcentral-remote-access",
			Helper: Helper{
				Tool:               "meshagent",
				Aliases:            []string{"meshcentral-agent"},
				Scope:              "system-or-user",
				SupportedPlatforms: []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"},
				VerifyCommand:      "meshagent -info",
				RuntimeStatus:      "existing-enrolled-agent-only",
			},
		}
	default: // rustdesk
		return variantDesc{
			ConnectionPathID: "existing-rustdesk-remote-desktop",
			Helper: Helper{
				Tool:               "rustdesk",
				Aliases:            []string{"rustdesk-server"},
				Scope:              "user-or-system",
				SupportedPlatforms: []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"},
				VerifyCommand:      "rustdesk --version",
				RuntimeStatus:      "configured-helper-existing-id-only",
			},
		}
	}
}

func packageChecks(pkg Package) []Check {
	return []Check{
		{Name: "schema_version", Passed: pkg.SchemaVersion == PackageSchemaVersion, Detail: pkg.SchemaVersion},
		{Name: "adapter_kind", Passed: pkg.AdapterKind == AdapterKind, Detail: pkg.AdapterKind},
		{Name: "variant_supported", Passed: supportedVariant(pkg.Variant), Detail: pkg.Variant},
		{Name: "production_claim_scoped", Passed: pkg.ProductionClaim == "remote-desktop-adapter-package-surface-only", Detail: pkg.ProductionClaim},
		{Name: "approval_boundaries_declared", Passed: len(pkg.ApprovalRequired) >= 4, Detail: fmt.Sprintf("%d", len(pkg.ApprovalRequired))},
		{Name: "evidence_required_declared", Passed: len(pkg.EvidenceRequired) >= 4, Detail: fmt.Sprintf("%d", len(pkg.EvidenceRequired))},
		{Name: "agent_rules_present", Passed: len(pkg.AgentRules) >= 4, Detail: fmt.Sprintf("%d", len(pkg.AgentRules))},
		{Name: "acceptance_evidence_plan_declared", Passed: pkg.EvidencePlanPath == "acceptance-evidence-plan.json", Detail: pkg.EvidencePlanPath},
	}
}

func acceptanceEvidencePlan(pkg Package, generatedAt time.Time) AcceptanceEvidencePlan {
	return AcceptanceEvidencePlan{
		SchemaVersion:    AcceptanceEvidencePlanSchemaVersion,
		GeneratedAt:      generatedAt.UTC(),
		AdapterKind:      pkg.AdapterKind,
		Variant:          pkg.Variant,
		ConnectionPathID: pkg.ConnectionPathID,
		PackagePath:      "rustdesk-adapter.json",
		ExternalMutation: true,
		EvidenceFiles: []EvidencePlanFile{
			{Name: "helper-status", Path: "helper-status.json", Kind: "json", Required: true, Description: "Helper version and connection-ready status from verify command."},
			{Name: "session-start-transcript", Path: "session-start-transcript.txt", Kind: "transcript", Required: true, Description: "Redacted session start transcript — no session IDs, passwords, or private IPs."},
			{Name: "host-registration", Path: "host-registration.json", Kind: "json", Required: true, Description: "Host registration evidence from rdev host serve."},
			{Name: "session-teardown-transcript", Path: "session-teardown-transcript.txt", Kind: "transcript", Required: true, Description: "Session teardown evidence confirming no persistence was left."},
			{Name: "audit", Path: "audit.jsonl", Kind: "transcript", Required: true, Description: "Redacted audit JSONL covering session start, host registration, and teardown."},
		},
		AgentRules: []string{
			"Scaffold evidence with rdev acceptance scaffold-evidence --rustdesk-adapter-package <dir> before collecting real remote-desktop evidence.",
			"Redact all session IDs, relay addresses, passwords, and private IPs before including in evidence.",
			"Confirm session teardown evidence before marking the job complete.",
			"If endpoint, credential, relay, persistence, or privilege is unclear, ask one short question.",
		},
		ApprovalRequired: append([]string(nil), pkg.ApprovalRequired...),
	}
}

func renderReadme(pkg Package) string {
	return fmt.Sprintf(`# Remote Dev Skillkit Remote Desktop Adapter Package

Schema: %s
Name: %s
Adapter: %s / %s

This package gives Agents a standard remote desktop helper surface. It
contains no real session IDs, passwords, relay addresses, private IPs, or
organisation identifiers.

Verify before use:

    %s

Privileged port-opening, relay-service installation, credential storage,
and persistent remote-access autostart entries require explicit operator
approval.
`, PackageSchemaVersion, pkg.Name, pkg.AdapterKind, pkg.Variant, pkg.Helper.VerifyCommand)
}

func prepareOut(dir string, force bool) error {
	entries, err := os.ReadDir(dir)
	if err == nil {
		if len(entries) > 0 && !force {
			return fmt.Errorf("output directory must be empty: %s", dir)
		}
		if force {
			for _, entry := range entries {
				if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(dir, 0o700)
}

func packageFile(root, path, kind string) (PackageFile, error) {
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return PackageFile{}, err
	}
	sum := sha256.Sum256(content)
	return PackageFile{
		Path:      path,
		SHA256:    "sha256:" + hex.EncodeToString(sum[:]),
		SizeBytes: int64(len(content)),
		Kind:      kind,
	}, nil
}

func resolveManifest(path string) (string, string, error) {
	if strings.TrimSpace(path) == "" {
		return "", "", fmt.Errorf("package is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		return filepath.Join(abs, "rustdesk-adapter.json"), abs, nil
	}
	return abs, filepath.Dir(abs), nil
}

func verifyFiles(root string, files []PackageFile) []FileCheck {
	var out []FileCheck
	for _, f := range files {
		result := FileCheck{
			Path:           f.Path,
			Kind:           f.Kind,
			ExpectedSHA256: f.SHA256,
			ExpectedSize:   f.SizeBytes,
		}
		add := func(name string, passed bool, detail string) {
			result.Checks = append(result.Checks, Check{Name: name, Passed: passed, Detail: detail})
		}
		safe := safePath(f.Path)
		add("file_path_safe", safe, f.Path)
		add("expected_sha256_format", strings.HasPrefix(f.SHA256, "sha256:") && len(strings.TrimPrefix(f.SHA256, "sha256:")) == 64, f.SHA256)
		if safe {
			content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path)))
			add("file_exists", err == nil, errorDetail(err))
			if err == nil {
				sum := sha256.Sum256(content)
				result.ActualSHA256 = "sha256:" + hex.EncodeToString(sum[:])
				result.ActualSize = int64(len(content))
				add("file_sha256_matches", result.ActualSHA256 == f.SHA256, f.SHA256)
				add("file_size_matches", result.ActualSize == f.SizeBytes, fmt.Sprintf("%d", f.SizeBytes))
			}
		}
		out = append(out, result)
	}
	return out
}

func safePath(path string) bool {
	if strings.TrimSpace(path) == "" || strings.Contains(path, `\`) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator)) && !filepath.IsAbs(clean) && filepath.VolumeName(clean) == ""
}

func failureActions() []string {
	return []string{
		"Regenerate the remote desktop adapter package in a clean output directory.",
		"Keep real session IDs, passwords, relay addresses, and private IPs outside package files.",
	}
}

func errorDetail(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
