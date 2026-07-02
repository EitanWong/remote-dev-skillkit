package model

import "strings"

const ConnectionEntryPackageCatalogSchemaVersion = "rdev.connection-entry.package-catalog.v1"

type ConnectionEntryPackageCatalog struct {
	SchemaVersion    string                            `json:"schema_version"`
	SelectionMode    string                            `json:"selection_mode"`
	PreferredSurface string                            `json:"preferred_surface"`
	HumanSurfaces    []string                          `json:"human_surfaces"`
	AgentMetadata    []string                          `json:"agent_metadata"`
	Candidates       []ConnectionEntryPackageCandidate `json:"candidates"`
	AgentRules       []string                          `json:"agent_rules"`
}

type ConnectionEntryPackageCandidate struct {
	ID                    string   `json:"id"`
	TargetOS              string   `json:"target_os"`
	TargetArch            string   `json:"target_arch"`
	Label                 string   `json:"label"`
	PackageArtifact       string   `json:"package_artifact"`
	PackageStatus         string   `json:"package_status"`
	FallbackScriptURL     string   `json:"fallback_script_url"`
	FallbackScriptStatus  string   `json:"fallback_script_status"`
	SelectionHints        []string `json:"selection_hints"`
	RequiredReleaseInputs []string `json:"required_release_inputs,omitempty"`
}

func NewConnectionEntryPackageCatalog(joinURL string) ConnectionEntryPackageCatalog {
	joinBase := strings.TrimRight(strings.TrimSpace(joinURL), "/")
	return ConnectionEntryPackageCatalog{
		SchemaVersion:    ConnectionEntryPackageCatalogSchemaVersion,
		SelectionMode:    "agent-or-browser-detected-target-os",
		PreferredSurface: "signed-package-when-published-visible-script-fallback",
		HumanSurfaces: []string{
			"selected signed package when release inputs are available",
			"visible platform bootstrap script fallback",
			"connection_entry.entry_url when target OS cannot be detected",
		},
		AgentMetadata: []string{
			"ticket code",
			"manifest URL",
			"manifest root public key",
			"gateway URL",
			"transport preference and fallback order",
			"release bundle URL, release root public key, package URL, and checksum when package assets are published",
		},
		Candidates: []ConnectionEntryPackageCandidate{
			newConnectionEntryPackageCandidate(joinBase, "windows-amd64", "windows", "amd64", "Windows x64", "rdev-connection-entry-windows-amd64.zip", "bootstrap.ps1", []string{"Windows NT", "Win64", "x64", "amd64"}),
			newConnectionEntryPackageCandidate(joinBase, "windows-arm64", "windows", "arm64", "Windows ARM64", "rdev-connection-entry-windows-arm64.zip", "bootstrap.ps1", []string{"Windows NT", "ARM64", "aarch64"}),
			newConnectionEntryPackageCandidate(joinBase, "darwin-arm64", "darwin", "arm64", "macOS Apple silicon", "rdev-connection-entry-darwin-arm64.tar.gz", "bootstrap.sh", []string{"Mac OS X", "Macintosh", "arm64", "Apple silicon"}),
			newConnectionEntryPackageCandidate(joinBase, "darwin-amd64", "darwin", "amd64", "macOS Intel", "rdev-connection-entry-darwin-amd64.tar.gz", "bootstrap.sh", []string{"Mac OS X", "Macintosh", "Intel", "x86_64"}),
			newConnectionEntryPackageCandidate(joinBase, "linux-amd64", "linux", "amd64", "Linux x64", "rdev-connection-entry-linux-amd64.tar.gz", "bootstrap.sh", []string{"Linux", "x86_64", "amd64"}),
			newConnectionEntryPackageCandidate(joinBase, "linux-arm64", "linux", "arm64", "Linux ARM64", "rdev-connection-entry-linux-arm64.tar.gz", "bootstrap.sh", []string{"Linux", "arm64", "aarch64"}),
		},
		AgentRules: []string{
			"select the candidate from target OS and architecture probes before asking the target-side human",
			"prefer a signed package only when package URL, checksum, signed release bundle, and release root are available",
			"use the visible fallback script when package assets are not published or the architecture is unclear",
			"keep ticket, manifest root, gateway, transport, release, and checksum values in Agent/package metadata",
			"ask one short question only when the target OS or architecture cannot be detected from browser, host, inventory, or operator context",
		},
	}
}

func newConnectionEntryPackageCandidate(joinBase, id, targetOS, targetArch, label, artifact, script string, hints []string) ConnectionEntryPackageCandidate {
	scriptURL := ""
	if joinBase != "" {
		scriptURL = joinBase + "/" + script
	}
	return ConnectionEntryPackageCandidate{
		ID:                   id,
		TargetOS:             targetOS,
		TargetArch:           targetArch,
		Label:                label,
		PackageArtifact:      artifact,
		PackageStatus:        "planned-release-asset-required",
		FallbackScriptURL:    scriptURL,
		FallbackScriptStatus: "available",
		SelectionHints:       append([]string(nil), hints...),
		RequiredReleaseInputs: []string{
			"package_url",
			"package_sha256",
			"release_bundle_url",
			"release_root_public_key",
		},
	}
}
