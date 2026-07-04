package acceptance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPackageAndVerifyPostReleaseDownloadEvidence(t *testing.T) {
	root := t.TempDir()
	fixture := writePostReleaseDownloadFixture(t, root, []string{"linux/amd64", "windows/amd64"}, true)
	pkg, err := PackagePostReleaseDownloadEvidence(PostReleaseDownloadPackageOptions{
		PlanPath:             fixture.plan,
		PlanVerificationPath: fixture.planVerification,
		OutDir:               filepath.Join(root, "package"),
		EvidenceDir:          fixture.evidenceDir,
		SkillkitEvidenceDir:  fixture.skillkitDir,
		Now:                  time.Date(2026, 7, 4, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("expected post-release package ok: %#v", pkg.Checks)
	}
	if pkg.SchemaVersion != PostReleaseDownloadPackageSchemaVersion {
		t.Fatalf("unexpected schema %q", pkg.SchemaVersion)
	}
	if pkg.Repo != "EitanWong/remote-dev-skillkit" || pkg.Tag != "v0.1.18-dev" {
		t.Fatalf("unexpected repo/tag: %#v", pkg)
	}
	if !pkg.SkillkitIncluded || len(pkg.PlatformTargets) != 2 {
		t.Fatalf("unexpected target metadata: %#v", pkg)
	}
	if pkg.RedactionRuleCounts["github_token"] != 2 {
		t.Fatalf("expected github token redaction, got %#v", pkg.RedactionRuleCounts)
	}
	verification, err := VerifyPostReleaseDownloadAcceptancePackage(pkg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("expected verification ok: %#v", verification.Checks)
	}
	if verification.PackageSchema != PostReleaseDownloadPackageSchemaVersion {
		t.Fatalf("unexpected package schema %q", verification.PackageSchema)
	}
}

func TestPackagePostReleaseDownloadEvidenceFromScaffold(t *testing.T) {
	root := t.TempDir()
	fixture := writePostReleaseDownloadFixture(t, root, []string{"linux/amd64", "windows/amd64"}, true)
	scaffoldDir := filepath.Join(root, "post-release-scaffold")
	scaffold, err := ScaffoldPostReleaseDownloadEvidence(PostReleaseDownloadScaffoldOptions{
		PlanPath:             fixture.plan,
		PlanVerificationPath: fixture.planVerification,
		OutDir:               scaffoldDir,
		CreatePlaceholders:   true,
		Now:                  time.Date(2026, 7, 5, 1, 2, 3, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(scaffold.Commands.Package, " "), "--scaffold") {
		t.Fatalf("expected scaffold-level package command, got %#v", scaffold.Commands.Package)
	}
	copyEvidenceFile(t, filepath.Join(fixture.evidenceDir, "linux-amd64-transcript.txt"), filepath.Join(scaffoldDir, "platform-download-evidence", "linux-amd64-transcript.txt"))
	copyEvidenceFile(t, filepath.Join(fixture.evidenceDir, "linux-amd64-candidate-verify.json"), filepath.Join(scaffoldDir, "platform-download-evidence", "linux-amd64-candidate-verify.json"))
	copyEvidenceFile(t, filepath.Join(fixture.evidenceDir, "linux-amd64-bundle-verify.json"), filepath.Join(scaffoldDir, "platform-download-evidence", "linux-amd64-bundle-verify.json"))
	copyEvidenceFile(t, filepath.Join(fixture.evidenceDir, "windows-amd64-transcript.txt"), filepath.Join(scaffoldDir, "platform-download-evidence", "windows-amd64-transcript.txt"))
	copyEvidenceFile(t, filepath.Join(fixture.evidenceDir, "windows-amd64-candidate-verify.json"), filepath.Join(scaffoldDir, "platform-download-evidence", "windows-amd64-candidate-verify.json"))
	copyEvidenceFile(t, filepath.Join(fixture.evidenceDir, "windows-amd64-bundle-verify.json"), filepath.Join(scaffoldDir, "platform-download-evidence", "windows-amd64-bundle-verify.json"))
	copyEvidenceFile(t, filepath.Join(fixture.skillkitDir, "skillkit-transcript.txt"), filepath.Join(scaffoldDir, "skillkit-download-evidence", "skillkit-transcript.txt"))
	copyEvidenceFile(t, filepath.Join(fixture.skillkitDir, "skillkit-verify.json"), filepath.Join(scaffoldDir, "skillkit-download-evidence", "skillkit-verify.json"))

	pkg, err := PackagePostReleaseDownloadEvidence(PostReleaseDownloadPackageOptions{
		ScaffoldPath: scaffoldDir,
		OutDir:       filepath.Join(root, "package"),
		Now:          time.Date(2026, 7, 5, 2, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("expected scaffold package ok, got %#v", pkg)
	}
	archived := map[string]bool{}
	for _, file := range pkg.Files {
		archived[file.Path] = true
	}
	if !archived["post-release/post-release-install-plan.json"] || !archived["post-release/post-release-install-verification.json"] {
		t.Fatalf("expected scaffold package to archive copied plan files, got %#v", archived)
	}
	verification, err := VerifyPostReleaseDownloadAcceptancePackage(pkg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("expected scaffold package verification ok: %#v", verification.Checks)
	}
}

func TestVerifyPostReleaseDownloadEvidenceRejectsMissingPlatformBundleVerify(t *testing.T) {
	root := t.TempDir()
	fixture := writePostReleaseDownloadFixture(t, root, []string{"linux/amd64"}, false)
	if err := os.Remove(filepath.Join(fixture.evidenceDir, "linux-amd64-bundle-verify.json")); err != nil {
		t.Fatal(err)
	}
	pkg, err := PackagePostReleaseDownloadEvidence(PostReleaseDownloadPackageOptions{
		PlanPath:             fixture.plan,
		PlanVerificationPath: fixture.planVerification,
		OutDir:               filepath.Join(root, "package"),
		EvidenceDir:          fixture.evidenceDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.OK() {
		t.Fatal("expected package to fail without bundle verify evidence")
	}
	verification, err := VerifyPostReleaseDownloadAcceptancePackage(pkg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatal("expected verification to fail")
	}
	if failures := failedPostReleaseAcceptanceChecks(verification.Checks); !strings.Contains(failures, "platform_linux-amd64_bundle_verify_present") {
		t.Fatalf("expected missing bundle verification failure, got %s", failures)
	}
}

func TestPostReleaseDownloadRejectsScaffoldPlaceholderEvidence(t *testing.T) {
	root := t.TempDir()
	fixture := writePostReleaseDownloadFixture(t, root, []string{"linux/amd64"}, true)
	placeholder := []byte("PLACEHOLDER ONLY - replace with real redacted evidence before packaging.\nEvidence: linux transcript\n")
	if err := os.WriteFile(filepath.Join(fixture.evidenceDir, "linux-amd64-transcript.txt"), placeholder, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.skillkitDir, "skillkit-transcript.txt"), placeholder, 0o600); err != nil {
		t.Fatal(err)
	}
	pkg, err := PackagePostReleaseDownloadEvidence(PostReleaseDownloadPackageOptions{
		PlanPath:             fixture.plan,
		PlanVerificationPath: fixture.planVerification,
		OutDir:               filepath.Join(root, "package"),
		EvidenceDir:          fixture.evidenceDir,
		SkillkitEvidenceDir:  fixture.skillkitDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.OK() {
		t.Fatal("expected package to fail with scaffold placeholder post-release evidence")
	}
	failures := failedPostReleaseAcceptanceChecks(pkg.Checks)
	if !strings.Contains(failures, "platform-linux-amd64-transcript_copied") ||
		!strings.Contains(failures, "platform_linux-amd64_transcript_present") ||
		!strings.Contains(failures, "skillkit-transcript_copied") ||
		!strings.Contains(failures, "skillkit_transcript_present") {
		t.Fatalf("expected placeholder copy and presence failures, got %s", failures)
	}
}

func TestVerifyPostReleaseDownloadRejectsArchivedPlaceholderEvidence(t *testing.T) {
	root := t.TempDir()
	fixture := writePostReleaseDownloadFixture(t, root, []string{"linux/amd64"}, false)
	pkg, err := PackagePostReleaseDownloadEvidence(PostReleaseDownloadPackageOptions{
		PlanPath:             fixture.plan,
		PlanVerificationPath: fixture.planVerification,
		OutDir:               filepath.Join(root, "package"),
		EvidenceDir:          fixture.evidenceDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	archived := filepath.Join(pkg.OutDir, "platforms", "linux-amd64", "transcript.txt")
	content := []byte("{\n  \"placeholder\": true,\n  \"replace_before_packaging\": true,\n  \"evidence_name\": \"linux transcript\"\n}\n")
	if err := os.WriteFile(archived, content, 0o600); err != nil {
		t.Fatal(err)
	}
	for i := range pkg.Files {
		if pkg.Files[i].Path == "platforms/linux-amd64/transcript.txt" {
			pkg.Files[i].SizeBytes = len(content)
			pkg.Files[i].SHA256 = "sha256:" + fileSHA256Bytes(content)
		}
	}
	manifest, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg.OutDir, "package.json"), append(manifest, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyPostReleaseDownloadAcceptancePackage(pkg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatal("expected verification to fail with archived placeholder evidence")
	}
	if failures := failedPostReleaseFileChecks(verification.Files); !strings.Contains(failures, "platforms/linux-amd64/transcript.txt:file_not_placeholder") {
		t.Fatalf("expected file_not_placeholder failure, got %s", failures)
	}
}

type postReleaseDownloadFixture struct {
	plan             string
	planVerification string
	evidenceDir      string
	skillkitDir      string
}

func writePostReleaseDownloadFixture(t *testing.T, root string, targets []string, includeSkillkit bool) postReleaseDownloadFixture {
	t.Helper()
	dir := filepath.Join(root, "post-release-fixture")
	evidenceDir := filepath.Join(dir, "platform-evidence")
	skillkitDir := filepath.Join(dir, "skillkit-evidence")
	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(skillkitDir, 0o700); err != nil {
		t.Fatal(err)
	}
	platforms := make([]map[string]any, 0, len(targets))
	for _, target := range targets {
		platforms = append(platforms, map[string]any{"target": target})
		slug := postReleaseTargetSlug(target)
		writeFixtureFile(t, evidenceDir, slug+"-transcript.txt", "downloaded and verified "+target+"\nghp_abcdefghijklmnopqrstuvwx\n")
		writeFixtureJSON(t, evidenceDir, slug+"-candidate-verify.json", map[string]any{"ok": true, "schema_version": "rdev.release-candidate-verification.v1"})
		writeFixtureJSON(t, evidenceDir, slug+"-bundle-verify.json", map[string]any{"ok": true, "schema_version": "rdev.release-bundle-verification.v1"})
	}
	skillkit := map[string]any{}
	if includeSkillkit {
		skillkit = map[string]any{
			"archive": map[string]any{"name": "remote-dev-skillkit.tar.gz"},
		}
		writeFixtureFile(t, skillkitDir, "skillkit-transcript.txt", "downloaded skillkit archive\n")
		writeFixtureJSON(t, skillkitDir, "skillkit-verify.json", map[string]any{"ok": true, "schema_version": "rdev.skillkit-verification.v1"})
	}
	plan := filepath.Join(dir, "post-release-install-plan.json")
	writeFixtureJSON(t, dir, "post-release-install-plan.json", map[string]any{
		"schema_version": "rdev.post-release-install-plan.v1",
		"repo":           "EitanWong/remote-dev-skillkit",
		"tag":            "v0.1.18-dev",
		"platforms":      platforms,
		"skillkit":       skillkit,
	})
	planVerification := filepath.Join(dir, "post-release-install-verification.json")
	writeFixtureJSON(t, dir, "post-release-install-verification.json", map[string]any{
		"schema_version": "rdev.post-release-install-verification.v1",
		"ok":             true,
	})
	return postReleaseDownloadFixture{
		plan:             plan,
		planVerification: planVerification,
		evidenceDir:      evidenceDir,
		skillkitDir:      skillkitDir,
	}
}

func writeFixtureJSON(t *testing.T, dir, name string, value any) {
	t.Helper()
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, dir, name, string(content)+"\n")
}

func writeFixtureFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func failedPostReleaseAcceptanceChecks(checks []Check) string {
	var failed []string
	for _, check := range checks {
		if !check.Passed {
			failed = append(failed, check.Name)
		}
	}
	return strings.Join(failed, ",")
}

func failedPostReleaseFileChecks(files []RelayPackageFileCheck) string {
	var failed []string
	for _, file := range files {
		for _, check := range file.Checks {
			if !check.Passed {
				failed = append(failed, file.Path+":"+check.Name)
			}
		}
	}
	return strings.Join(failed, ",")
}
