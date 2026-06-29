package evidence

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

	"github.com/EitanWong/remote-dev-skillkit/internal/audit"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

const (
	BundleSchemaVersion         = "rdev.evidence-bundle.v1"
	PolicyDecisionSchemaVersion = "rdev.policy-decision.v1"
)

type Input struct {
	Job         model.Job
	Artifacts   []model.Artifact
	AuditEvents []model.AuditEvent
	GeneratedAt time.Time
}

type Manifest struct {
	SchemaVersion   string      `json:"schema_version"`
	GeneratedAt     time.Time   `json:"generated_at"`
	JobID           string      `json:"job_id"`
	JobStatus       string      `json:"job_status"`
	EnvelopeHash    string      `json:"envelope_hash,omitempty"`
	AuditEventCount int         `json:"audit_event_count"`
	AuditRootHash   string      `json:"audit_root_hash"`
	Files           []FileEntry `json:"files"`
}

type FileEntry struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int    `json:"size_bytes"`
	Kind      string `json:"kind"`
}

type PolicyDecision struct {
	SchemaVersion     string             `json:"schema_version"`
	JobID             string             `json:"job_id"`
	Decision          string             `json:"decision"`
	Reason            string             `json:"reason"`
	Capabilities      []string           `json:"capabilities,omitempty"`
	ApprovalsRequired []string           `json:"approvals_required,omitempty"`
	Workspace         model.JobWorkspace `json:"workspace,omitempty"`
}

type ArtifactRecord struct {
	ID            string    `json:"id"`
	JobID         string    `json:"job_id"`
	Kind          string    `json:"kind"`
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	ContentSHA256 string    `json:"content_sha256"`
	ContentBytes  int       `json:"content_bytes"`
	CreatedAt     time.Time `json:"created_at"`
}

func ExportDirectory(dir string, input Input) (Manifest, error) {
	if dir == "" {
		return Manifest{}, fmt.Errorf("output directory is required")
	}
	if input.Job.ID == "" {
		return Manifest{}, fmt.Errorf("job id is required")
	}
	if input.GeneratedAt.IsZero() {
		input.GeneratedAt = time.Now()
	}
	if err := prepareOutputDir(dir); err != nil {
		return Manifest{}, err
	}

	var entries []FileEntry
	add := func(path, kind string, content []byte) error {
		entry, err := writeBundleFile(dir, path, kind, content)
		if err != nil {
			return err
		}
		entries = append(entries, entry)
		return nil
	}

	if err := addJSON("job.json", "job", input.Job, add); err != nil {
		return Manifest{}, err
	}
	envelopeHash := ""
	if input.Job.Envelope != nil {
		if err := addJSON("envelope.json", "envelope", input.Job.Envelope, add); err != nil {
			return Manifest{}, err
		}
		hash, err := hashJSON(input.Job.Envelope)
		if err != nil {
			return Manifest{}, err
		}
		envelopeHash = hash
	}
	if err := addJSON("policy-decision.json", "policy-decision", policyDecision(input.Job), add); err != nil {
		return Manifest{}, err
	}

	artifactRecords, artifactEntries, err := writeArtifacts(dir, input.Artifacts)
	if err != nil {
		return Manifest{}, err
	}
	entries = append(entries, artifactEntries...)
	if err := addJSON("artifacts.json", "artifact-index", artifactRecords, add); err != nil {
		return Manifest{}, err
	}

	auditSlice := filterAudit(input.Job, input.AuditEvents)
	auditJSONL, err := encodeAuditJSONL(auditSlice)
	if err != nil {
		return Manifest{}, err
	}
	if err := add("audit-slice.jsonl", "audit-slice", auditJSONL); err != nil {
		return Manifest{}, err
	}
	auditChain, err := audit.ExportChain(auditSlice, input.GeneratedAt)
	if err != nil {
		return Manifest{}, err
	}
	if err := addJSON("audit-chain.json", "audit-chain", auditChain, add); err != nil {
		return Manifest{}, err
	}

	checksums := encodeChecksums(entries)
	checksumEntry, err := writeBundleFile(dir, "checksums.txt", "checksums", checksums)
	if err != nil {
		return Manifest{}, err
	}
	entries = append(entries, checksumEntry)

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	manifest := Manifest{
		SchemaVersion:   BundleSchemaVersion,
		GeneratedAt:     input.GeneratedAt.UTC(),
		JobID:           input.Job.ID,
		JobStatus:       string(input.Job.Status),
		EnvelopeHash:    envelopeHash,
		AuditEventCount: len(auditSlice),
		AuditRootHash:   auditChain.RootHash,
		Files:           entries,
	}
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	content = append(content, '\n')
	if _, err := writeBundleFile(dir, "manifest.json", "manifest", content); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func prepareOutputDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err == nil {
		if len(entries) > 0 {
			return fmt.Errorf("output directory must be empty: %s", dir)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(dir, 0o700)
}

func addJSON(path, kind string, value any, add func(string, string, []byte) error) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return add(path, kind, content)
}

func writeArtifacts(dir string, artifacts []model.Artifact) ([]ArtifactRecord, []FileEntry, error) {
	records := make([]ArtifactRecord, 0, len(artifacts))
	entries := make([]FileEntry, 0, len(artifacts))
	for _, artifact := range artifacts {
		path := "artifacts/" + artifactFileName(artifact)
		content := []byte(artifact.Content)
		entry, err := writeBundleFile(dir, path, "artifact", content)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, entry)
		records = append(records, ArtifactRecord{
			ID:            artifact.ID,
			JobID:         artifact.JobID,
			Kind:          artifact.Kind,
			Name:          artifact.Name,
			Path:          path,
			ContentSHA256: entry.SHA256,
			ContentBytes:  entry.SizeBytes,
			CreatedAt:     artifact.CreatedAt,
		})
	}
	return records, entries, nil
}

func writeBundleFile(root, bundlePath, kind string, content []byte) (FileEntry, error) {
	clean := filepath.Clean(filepath.FromSlash(bundlePath))
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return FileEntry{}, fmt.Errorf("invalid bundle path %q", bundlePath)
	}
	path := filepath.Join(root, clean)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return FileEntry{}, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return FileEntry{}, err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return FileEntry{}, err
	}
	if err := file.Close(); err != nil {
		return FileEntry{}, err
	}
	sum := sha256.Sum256(content)
	return FileEntry{
		Path:      filepath.ToSlash(clean),
		SHA256:    "sha256:" + hex.EncodeToString(sum[:]),
		SizeBytes: len(content),
		Kind:      kind,
	}, nil
}

func artifactFileName(artifact model.Artifact) string {
	name := sanitizeName(filepath.Base(artifact.Name))
	if name == "" || name == "." {
		name = "artifact.txt"
	}
	id := sanitizeName(artifact.ID)
	if id == "" {
		id = "artifact"
	}
	return id + "-" + name
}

func sanitizeName(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '.', r == '_', r == '-':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	return strings.Trim(builder.String(), "._-")
}

func policyDecision(job model.Job) PolicyDecision {
	decision := "recorded"
	reason := "job record was exported without a signed envelope"
	var capabilities []string
	var approvals []string
	var workspace model.JobWorkspace
	if job.Envelope != nil {
		decision = "allowed"
		reason = "gateway signed job envelope"
		capabilities = append([]string(nil), job.Envelope.Capabilities...)
		approvals = append([]string(nil), job.Envelope.ApprovalsRequired...)
		workspace = job.Envelope.Workspace
	}
	return PolicyDecision{
		SchemaVersion:     PolicyDecisionSchemaVersion,
		JobID:             job.ID,
		Decision:          decision,
		Reason:            reason,
		Capabilities:      capabilities,
		ApprovalsRequired: approvals,
		Workspace:         workspace,
	}
}

func filterAudit(job model.Job, events []model.AuditEvent) []model.AuditEvent {
	targets := map[string]bool{job.ID: true, job.HostID: true}
	if job.Envelope != nil && job.Envelope.TicketID != "" {
		targets[job.Envelope.TicketID] = true
	}
	filtered := make([]model.AuditEvent, 0, len(events))
	for _, event := range events {
		if targets[event.TargetID] {
			filtered = append(filtered, event)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Sequence < filtered[j].Sequence
	})
	return filtered
}

func encodeAuditJSONL(events []model.AuditEvent) ([]byte, error) {
	var builder strings.Builder
	for _, event := range events {
		content, err := json.Marshal(event)
		if err != nil {
			return nil, err
		}
		builder.Write(content)
		builder.WriteByte('\n')
	}
	return []byte(builder.String()), nil
}

func encodeChecksums(entries []FileEntry) []byte {
	sorted := append([]FileEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Path < sorted[j].Path
	})
	var builder strings.Builder
	for _, entry := range sorted {
		builder.WriteString(strings.TrimPrefix(entry.SHA256, "sha256:"))
		builder.WriteString("  ")
		builder.WriteString(entry.Path)
		builder.WriteByte('\n')
	}
	return []byte(builder.String())
}

func hashJSON(value any) (string, error) {
	content, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
