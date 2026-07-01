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

const ProvenanceSchemaVersion = "rdev.release-provenance.v1"

type Provenance struct {
	SchemaVersion    string               `json:"schema_version"`
	Version          string               `json:"version"`
	GeneratedAt      time.Time            `json:"generated_at"`
	Builder          ProvenanceBuilder    `json:"builder"`
	Invocation       ProvenanceInvocation `json:"invocation"`
	Source           ProvenanceSource     `json:"source"`
	ExternalMutation bool                 `json:"external_mutation"`
	Subjects         []ProvenanceSubject  `json:"subjects"`
	Materials        []ProvenanceSubject  `json:"materials,omitempty"`
}

type ProvenanceBuilder struct {
	ID      string `json:"id"`
	Command string `json:"command"`
}

type ProvenanceInvocation struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

type ProvenanceSource struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit,omitempty"`
	Dirty      bool   `json:"dirty,omitempty"`
}

type ProvenanceSubject struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
	Kind      string `json:"kind,omitempty"`
}

func WriteCandidateProvenance(path string, candidate Candidate, files []CandidateFile, generatedAt time.Time) (CandidateFile, error) {
	provenance := BuildCandidateProvenance(candidate, files, generatedAt)
	content, err := json.MarshalIndent(provenance, "", "  ")
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
		Path:      "provenance.json",
		SHA256:    "sha256:" + sha,
		SizeBytes: size,
		Kind:      "provenance",
	}, nil
}

func BuildCandidateProvenance(candidate Candidate, files []CandidateFile, generatedAt time.Time) Provenance {
	subjects := make([]ProvenanceSubject, 0, len(candidate.Artifacts)+4)
	for _, artifact := range candidate.Artifacts {
		subjects = append(subjects, ProvenanceSubject{
			Name:      artifact.Name,
			Path:      filepath.ToSlash(artifact.ArtifactPath),
			SHA256:    artifact.SHA256,
			SizeBytes: artifact.SizeBytes,
			Kind:      "artifact",
		})
	}
	for _, file := range files {
		if !provenanceSubjectKind(file.Kind) {
			continue
		}
		subjects = append(subjects, ProvenanceSubject{
			Name:      filepath.Base(file.Path),
			Path:      file.Path,
			SHA256:    strings.TrimPrefix(file.SHA256, "sha256:"),
			SizeBytes: file.SizeBytes,
			Kind:      file.Kind,
		})
	}
	sort.Slice(subjects, func(i, j int) bool {
		if subjects[i].Kind == subjects[j].Kind {
			return subjects[i].Path < subjects[j].Path
		}
		return subjects[i].Kind < subjects[j].Kind
	})
	return Provenance{
		SchemaVersion: ProvenanceSchemaVersion,
		Version:       candidate.Version,
		GeneratedAt:   generatedAt.UTC(),
		Builder: ProvenanceBuilder{
			ID:      "remote-dev-skillkit/rdev",
			Command: "rdev release prepare-candidate",
		},
		Invocation: ProvenanceInvocation{
			Command: "rdev release prepare-candidate",
		},
		Source: ProvenanceSource{
			Repository: "remote-dev-skillkit",
		},
		ExternalMutation: false,
		Subjects:         subjects,
	}
}

func VerifyCandidateProvenance(candidateDir string, candidate Candidate) []CandidateCheck {
	path := filepath.Join(candidateDir, "provenance.json")
	content, err := os.ReadFile(path)
	checks := []CandidateCheck{{Name: "provenance_file_exists", Passed: err == nil, Detail: "provenance.json"}}
	if err != nil {
		return checks
	}
	var provenance Provenance
	err = json.Unmarshal(content, &provenance)
	checks = append(checks, CandidateCheck{Name: "provenance_json_valid", Passed: err == nil, Detail: errorDetail(err)})
	if err != nil {
		return checks
	}
	checks = append(checks,
		CandidateCheck{Name: "provenance_schema", Passed: provenance.SchemaVersion == ProvenanceSchemaVersion, Detail: provenance.SchemaVersion},
		CandidateCheck{Name: "provenance_external_mutation_false", Passed: !provenance.ExternalMutation, Detail: fmt.Sprintf("%t", provenance.ExternalMutation)},
		CandidateCheck{Name: "provenance_version_matches_candidate", Passed: provenance.Version == candidate.Version, Detail: provenance.Version},
		CandidateCheck{Name: "provenance_subjects_present", Passed: len(provenance.Subjects) > 0, Detail: fmt.Sprintf("%d", len(provenance.Subjects))},
	)
	expected := expectedProvenanceSubjects(candidate)
	actual := map[string]ProvenanceSubject{}
	var malformed []string
	for _, subject := range provenance.Subjects {
		if !safeCandidatePath(subject.Path) || !isHexSHA256String(subject.SHA256) {
			malformed = append(malformed, subject.Path)
			continue
		}
		actual[subject.Path] = subject
	}
	var missing []string
	var mismatch []string
	for subjectPath, want := range expected {
		got, ok := actual[subjectPath]
		if !ok {
			missing = append(missing, subjectPath)
			continue
		}
		if got.SHA256 != want.SHA256 || got.SizeBytes != want.SizeBytes || got.Kind != want.Kind {
			mismatch = append(mismatch, subjectPath)
		}
	}
	var unexpected []string
	for subjectPath := range actual {
		if _, ok := expected[subjectPath]; !ok {
			unexpected = append(unexpected, subjectPath)
		}
	}
	sort.Strings(malformed)
	sort.Strings(missing)
	sort.Strings(mismatch)
	sort.Strings(unexpected)
	checks = append(checks,
		CandidateCheck{Name: "provenance_subject_entries_valid", Passed: len(malformed) == 0, Detail: strings.Join(malformed, ",")},
		CandidateCheck{Name: "provenance_covers_subjects", Passed: len(missing) == 0, Detail: strings.Join(missing, ",")},
		CandidateCheck{Name: "provenance_hashes_match_subjects", Passed: len(mismatch) == 0, Detail: strings.Join(mismatch, ",")},
		CandidateCheck{Name: "provenance_has_no_unexpected_subjects", Passed: len(unexpected) == 0, Detail: strings.Join(unexpected, ",")},
	)
	return checks
}

func expectedProvenanceSubjects(candidate Candidate) map[string]ProvenanceSubject {
	expected := map[string]ProvenanceSubject{}
	for _, artifact := range candidate.Artifacts {
		expected[filepath.ToSlash(artifact.ArtifactPath)] = ProvenanceSubject{
			Name:      artifact.Name,
			Path:      filepath.ToSlash(artifact.ArtifactPath),
			SHA256:    artifact.SHA256,
			SizeBytes: artifact.SizeBytes,
			Kind:      "artifact",
		}
	}
	for _, file := range candidate.Files {
		if !provenanceSubjectKind(file.Kind) {
			continue
		}
		expected[file.Path] = ProvenanceSubject{
			Name:      filepath.Base(file.Path),
			Path:      file.Path,
			SHA256:    strings.TrimPrefix(file.SHA256, "sha256:"),
			SizeBytes: file.SizeBytes,
			Kind:      file.Kind,
		}
	}
	return expected
}

func provenanceSubjectKind(kind string) bool {
	switch kind {
	case "release-bundle", "sbom", "skillkit":
		return true
	default:
		return false
	}
}
