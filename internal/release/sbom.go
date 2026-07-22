//go:build !rdev_bootstrap_focused

package release

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const SBOMSchemaVersion = "SPDX-2.3"

type SBOM struct {
	SPDXID      string        `json:"SPDXID"`
	SPDXVersion string        `json:"spdxVersion"`
	Name        string        `json:"name"`
	DataLicense string        `json:"dataLicense"`
	DocumentNS  string        `json:"documentNamespace"`
	Creation    SBOMCreation  `json:"creationInfo"`
	Packages    []SBOMPackage `json:"packages"`
	Files       []SBOMFile    `json:"files"`
}

type SBOMCreation struct {
	Created  time.Time `json:"created"`
	Creators []string  `json:"creators"`
}

type SBOMPackage struct {
	SPDXID              string `json:"SPDXID"`
	Name                string `json:"name"`
	VersionInfo         string `json:"versionInfo"`
	DownloadLocation    string `json:"downloadLocation"`
	FilesAnalyzed       bool   `json:"filesAnalyzed"`
	LicenseConcluded    string `json:"licenseConcluded"`
	LicenseDeclared     string `json:"licenseDeclared"`
	CopyrightText       string `json:"copyrightText"`
	PackageVerification string `json:"packageVerificationCode,omitempty"`
}

type SBOMFile struct {
	SPDXID           string         `json:"SPDXID"`
	FileName         string         `json:"fileName"`
	FileTypes        []string       `json:"fileTypes"`
	Checksums        []SBOMChecksum `json:"checksums"`
	LicenseConcluded string         `json:"licenseConcluded"`
	CopyrightText    string         `json:"copyrightText"`
	Comment          string         `json:"comment,omitempty"`
}

type SBOMChecksum struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"checksumValue"`
}

func WriteCandidateSBOM(path, version string, artifacts []CandidateArtifact, generatedAt time.Time) (CandidateFile, error) {
	sbom := BuildCandidateSBOM(version, artifacts, generatedAt)
	content, err := json.MarshalIndent(sbom, "", "  ")
	if err != nil {
		return CandidateFile{}, err
	}
	content = append(content, '\n')
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return CandidateFile{}, err
	}
	sha, size, err := fileDigest(path)
	if err != nil {
		return CandidateFile{}, err
	}
	return CandidateFile{
		Path:      "sbom.spdx.json",
		SHA256:    "sha256:" + sha,
		SizeBytes: size,
		Kind:      "sbom",
	}, nil
}

func BuildCandidateSBOM(version string, artifacts []CandidateArtifact, generatedAt time.Time) SBOM {
	files := make([]SBOMFile, 0, len(artifacts))
	for _, artifact := range artifacts {
		files = append(files, SBOMFile{
			SPDXID:    "SPDXRef-File-" + normalizeSPDXID(artifact.Name),
			FileName:  "./" + filepath.ToSlash(artifact.ArtifactPath),
			FileTypes: []string{"BINARY"},
			Checksums: []SBOMChecksum{{
				Algorithm: "SHA256",
				Value:     artifact.SHA256,
			}},
			LicenseConcluded: "NOASSERTION",
			CopyrightText:    "NOASSERTION",
			Comment:          fmt.Sprintf("Remote Dev Skillkit release artifact %s (%d bytes).", artifact.Name, artifact.SizeBytes),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].FileName < files[j].FileName })
	name := "remote-dev-skillkit"
	if strings.TrimSpace(version) != "" {
		name += "-" + strings.TrimSpace(version)
	}
	return SBOM{
		SPDXID:      "SPDXRef-DOCUMENT",
		SPDXVersion: SBOMSchemaVersion,
		Name:        name,
		DataLicense: "CC0-1.0",
		DocumentNS:  "https://example.com/remote-dev-skillkit/sbom/" + normalizeSPDXID(name),
		Creation: SBOMCreation{
			Created:  generatedAt.UTC(),
			Creators: []string{"Tool: rdev release prepare-candidate"},
		},
		Packages: []SBOMPackage{{
			SPDXID:           "SPDXRef-Package-remote-dev-skillkit",
			Name:             "remote-dev-skillkit",
			VersionInfo:      strings.TrimSpace(version),
			DownloadLocation: "NOASSERTION",
			FilesAnalyzed:    true,
			LicenseConcluded: "Apache-2.0",
			LicenseDeclared:  "Apache-2.0",
			CopyrightText:    "NOASSERTION",
		}},
		Files: files,
	}
}

func VerifyCandidateSBOM(candidateDir string, artifacts []CandidateArtifact) []CandidateCheck {
	path := filepath.Join(candidateDir, "sbom.spdx.json")
	content, err := os.ReadFile(path)
	checks := []CandidateCheck{{Name: "sbom_file_exists", Passed: err == nil, Detail: "sbom.spdx.json"}}
	if err != nil {
		return checks
	}
	var sbom SBOM
	err = json.Unmarshal(content, &sbom)
	checks = append(checks,
		CandidateCheck{Name: "sbom_json_valid", Passed: err == nil, Detail: errorDetail(err)},
	)
	if err != nil {
		return checks
	}
	checks = append(checks,
		CandidateCheck{Name: "sbom_spdx_schema", Passed: sbom.SPDXVersion == SBOMSchemaVersion, Detail: sbom.SPDXVersion},
		CandidateCheck{Name: "sbom_package_present", Passed: len(sbom.Packages) > 0, Detail: fmt.Sprintf("%d", len(sbom.Packages))},
		CandidateCheck{Name: "sbom_files_present", Passed: len(sbom.Files) > 0, Detail: fmt.Sprintf("%d", len(sbom.Files))},
	)
	expected := map[string]string{}
	for _, artifact := range artifacts {
		expected["./"+filepath.ToSlash(artifact.ArtifactPath)] = artifact.SHA256
	}
	actual := map[string]string{}
	var malformed []string
	for _, file := range sbom.Files {
		if !strings.HasPrefix(file.FileName, "./") || strings.Contains(file.FileName, `\`) {
			malformed = append(malformed, file.FileName)
			continue
		}
		sha := ""
		for _, checksum := range file.Checksums {
			if checksum.Algorithm == "SHA256" {
				sha = checksum.Value
				break
			}
		}
		if !isHexSHA256String(sha) {
			malformed = append(malformed, file.FileName)
			continue
		}
		actual[file.FileName] = sha
	}
	var missing []string
	var mismatch []string
	for path, sha := range expected {
		got, ok := actual[path]
		if !ok {
			missing = append(missing, path)
			continue
		}
		if got != sha {
			mismatch = append(mismatch, path)
		}
	}
	var unexpected []string
	for path := range actual {
		if _, ok := expected[path]; !ok {
			unexpected = append(unexpected, path)
		}
	}
	sort.Strings(malformed)
	sort.Strings(missing)
	sort.Strings(mismatch)
	sort.Strings(unexpected)
	checks = append(checks,
		CandidateCheck{Name: "sbom_file_entries_valid", Passed: len(malformed) == 0, Detail: strings.Join(malformed, ",")},
		CandidateCheck{Name: "sbom_covers_artifacts", Passed: len(missing) == 0, Detail: strings.Join(missing, ",")},
		CandidateCheck{Name: "sbom_hashes_match_artifacts", Passed: len(mismatch) == 0, Detail: strings.Join(mismatch, ",")},
		CandidateCheck{Name: "sbom_has_no_unexpected_artifacts", Passed: len(unexpected) == 0, Detail: strings.Join(unexpected, ",")},
	)
	return checks
}

func normalizeSPDXID(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '.' || r == '-':
			builder.WriteRune('-')
		default:
			builder.WriteRune('-')
		}
	}
	out := strings.Trim(builder.String(), "-")
	if out == "" {
		return "artifact"
	}
	return out
}
