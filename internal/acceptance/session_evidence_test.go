package acceptance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
)

func TestVerifySessionEvidenceDirectoryAcceptsCurrentSessionArtifact(t *testing.T) {
	dir := t.TempDir()
	artifactContent := []byte(`{"status":"succeeded"}`)
	if err := os.WriteFile(filepath.Join(dir, "result.json"), artifactContent, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := SessionEvidenceManifest{
		SchemaVersion: SessionEvidenceSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		SessionID:     "ses_test",
		TaskID:        "task_test",
		TaskStatus:    controlplane.TaskStatusSucceeded,
		Artifacts:     []controlplane.ArtifactRef{{ID: "art_test", Name: "result.json"}},
		Files: []SessionEvidenceFile{{
			Path:      "result.json",
			Kind:      "task-result",
			SizeBytes: int64(len(artifactContent)),
			SHA256:    sha256Hex(artifactContent),
		}},
	}
	content, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := VerifySessionEvidenceDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() {
		t.Fatalf("verification report must pass: %#v", report)
	}
}
