package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScaffoldAndStatusPostReleaseDownloadEvidence(t *testing.T) {
	root := t.TempDir()
	fixture := writePostReleaseDownloadFixture(t, root, []string{"linux/amd64", "windows/amd64"}, true)
	out := filepath.Join(root, "post-release-scaffold")
	scaffold, err := ScaffoldPostReleaseDownloadEvidence(PostReleaseDownloadScaffoldOptions{
		PostReleaseInstallDir: filepath.Dir(fixture.plan),
		OutDir:                out,
		CreatePlaceholders:    true,
		Now:                   time.Date(2026, 7, 5, 1, 2, 3, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !scaffold.OK || scaffold.ReadyForPackaging || scaffold.SchemaVersion != PostReleaseDownloadScaffoldSchemaVersion {
		t.Fatalf("unexpected scaffold: %#v", scaffold)
	}
	if len(scaffold.EvidenceFiles) != 8 || !scaffold.SkillkitIncluded {
		t.Fatalf("unexpected evidence files: %#v", scaffold.EvidenceFiles)
	}
	status, err := StatusPostReleaseDownloadEvidence(PostReleaseDownloadStatusOptions{
		ScaffoldPath: out,
		Now:          time.Date(2026, 7, 5, 1, 2, 3, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.ReadyForPackaging || status.PlaceholderCount != 8 || status.RequiredReady != 0 {
		t.Fatalf("placeholder evidence must not be ready: %#v", status)
	}

	copyEvidenceFile(t, filepath.Join(fixture.evidenceDir, "linux-amd64-transcript.txt"), filepath.Join(out, "platform-download-evidence", "linux-amd64-transcript.txt"))
	copyEvidenceFile(t, filepath.Join(fixture.evidenceDir, "linux-amd64-candidate-verify.json"), filepath.Join(out, "platform-download-evidence", "linux-amd64-candidate-verify.json"))
	copyEvidenceFile(t, filepath.Join(fixture.evidenceDir, "linux-amd64-bundle-verify.json"), filepath.Join(out, "platform-download-evidence", "linux-amd64-bundle-verify.json"))
	copyEvidenceFile(t, filepath.Join(fixture.evidenceDir, "windows-amd64-transcript.txt"), filepath.Join(out, "platform-download-evidence", "windows-amd64-transcript.txt"))
	copyEvidenceFile(t, filepath.Join(fixture.evidenceDir, "windows-amd64-candidate-verify.json"), filepath.Join(out, "platform-download-evidence", "windows-amd64-candidate-verify.json"))
	copyEvidenceFile(t, filepath.Join(fixture.evidenceDir, "windows-amd64-bundle-verify.json"), filepath.Join(out, "platform-download-evidence", "windows-amd64-bundle-verify.json"))
	copyEvidenceFile(t, filepath.Join(fixture.skillkitDir, "skillkit-transcript.txt"), filepath.Join(out, "skillkit-download-evidence", "skillkit-transcript.txt"))
	copyEvidenceFile(t, filepath.Join(fixture.skillkitDir, "skillkit-verify.json"), filepath.Join(out, "skillkit-download-evidence", "skillkit-verify.json"))

	status, err = StatusPostReleaseDownloadEvidence(PostReleaseDownloadStatusOptions{
		ScaffoldPath: filepath.Join(out, "scaffold-report.json"),
		Now:          time.Date(2026, 7, 5, 1, 2, 3, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !status.ReadyForPackaging || !status.OK || status.RequiredReady != status.RequiredTotal || status.PlaceholderCount != 0 {
		t.Fatalf("real evidence should be ready: %#v", status)
	}
}

func TestScaffoldPostReleaseDownloadInputValidation(t *testing.T) {
	root := t.TempDir()
	fixture := writePostReleaseDownloadFixture(t, root, []string{"linux/amd64"}, false)
	_, err := ScaffoldPostReleaseDownloadEvidence(PostReleaseDownloadScaffoldOptions{
		OutDir: filepath.Join(root, "missing-input"),
		Now:    time.Date(2026, 7, 5, 1, 2, 3, 0, time.UTC),
	})
	if err == nil || !strings.Contains(err.Error(), "post-release install dir or explicit plan and plan verification is required") {
		t.Fatalf("expected missing input error, got %v", err)
	}

	_, err = ScaffoldPostReleaseDownloadEvidence(PostReleaseDownloadScaffoldOptions{
		PostReleaseInstallDir: filepath.Dir(fixture.plan),
		PlanPath:              fixture.plan,
		OutDir:                filepath.Join(root, "multiple-inputs"),
		Now:                   time.Date(2026, 7, 5, 1, 2, 3, 0, time.UTC),
	})
	if err == nil || !strings.Contains(err.Error(), "provide either post-release install dir") {
		t.Fatalf("expected mutually exclusive input error, got %v", err)
	}
}

func copyEvidenceFile(t *testing.T, source, dest string) {
	t.Helper()
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0o600); err != nil {
		t.Fatal(err)
	}
}
