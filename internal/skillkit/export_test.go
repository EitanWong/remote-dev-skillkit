package skillkit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportWritesInstallableSkillkitBundle(t *testing.T) {
	sourceRoot := filepath.Join("..", "..")
	out := filepath.Join(t.TempDir(), "bundle")
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)

	manifest, err := Export(ExportOptions{
		SourceRoot:  sourceRoot,
		OutDir:      out,
		GatewayURL:  "https://api.example.com/v1",
		GeneratedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	if manifest.SchemaVersion != ManifestSchemaVersion {
		t.Fatalf("unexpected schema %q", manifest.SchemaVersion)
	}
	if manifest.GatewayURL != "https://api.example.com/v1" {
		t.Fatalf("unexpected gateway URL %q", manifest.GatewayURL)
	}
	for _, skill := range []string{"safe-remote-support", "host-triage", "remote-job-review", "remote-vibe-coding"} {
		if !hasSkill(manifest, skill) {
			t.Fatalf("expected skill %q in manifest: %#v", skill, manifest.Skills)
		}
		if _, err := os.Stat(filepath.Join(out, "skills", skill, "SKILL.md")); err != nil {
			t.Fatalf("expected exported skill %s: %v", skill, err)
		}
	}
	for _, path := range []string{
		"manifest.json",
		"INSTALL.md",
		"mcp/tools.json",
		"frameworks/codex.md",
		"frameworks/claude-code.md",
		"frameworks/hermes.md",
		"frameworks/openclaw-opencode.md",
		"frameworks/generic-mcp-agent.md",
	} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected bundle file %s: %v", path, err)
		}
	}
	install := readFile(t, filepath.Join(out, "INSTALL.md"))
	if !strings.Contains(install, "https://api.example.com/v1") {
		t.Fatalf("expected gateway URL in install doc, got %s", install)
	}
	content := readFile(t, filepath.Join(out, "manifest.json"))
	var onDisk Manifest
	if err := json.Unmarshal([]byte(content), &onDisk); err != nil {
		t.Fatal(err)
	}
	if len(onDisk.Files) == 0 {
		t.Fatal("manifest should include file checksums")
	}
	if !hasFile(onDisk, "mcp/tools.json") {
		t.Fatalf("manifest should include mcp/tools.json: %#v", onDisk.Files)
	}
}

func TestExportRejectsNonEmptyOutputDirectory(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Export(ExportOptions{SourceRoot: filepath.Join("..", ".."), OutDir: out})
	if err == nil {
		t.Fatal("expected non-empty output directory to fail")
	}
	if !strings.Contains(err.Error(), "must be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyAcceptsExportedBundle(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle")
	if _, err := Export(ExportOptions{
		SourceRoot: filepath.Join("..", ".."),
		OutDir:     out,
		GatewayURL: "https://api.example.com/v1",
	}); err != nil {
		t.Fatal(err)
	}

	report, err := Verify(VerifyOptions{BundleDir: out})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() {
		t.Fatalf("expected exported bundle to verify: %#v", report.Checks)
	}
	if report.SchemaVersion != VerificationSchemaVersion {
		t.Fatalf("unexpected schema %q", report.SchemaVersion)
	}
	if report.FilesVerified == 0 {
		t.Fatalf("expected verified files")
	}
}

func TestPlanInstallGeneratesVerifiableFrameworkScripts(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "bundle")
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	if _, err := Export(ExportOptions{
		SourceRoot:  filepath.Join("..", ".."),
		OutDir:      bundle,
		GatewayURL:  "https://api.example.com/v1",
		GeneratedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "install-plan")

	plan, err := PlanInstall(InstallPlanOptions{
		BundleDir:   bundle,
		OutDir:      out,
		Frameworks:  []string{"codex", "hermes", "generic"},
		RdevCommand: "rdev-test",
		GeneratedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	if plan.SchemaVersion != InstallPlanSchemaVersion {
		t.Fatalf("unexpected schema %q", plan.SchemaVersion)
	}
	if plan.ExternalMutation {
		t.Fatal("install planning must not mutate external state")
	}
	for _, framework := range []string{"codex", "hermes", "generic-mcp-agent"} {
		if !hasFrameworkInstallPlan(plan, framework) {
			t.Fatalf("missing framework install plan %q: %#v", framework, plan.Frameworks)
		}
	}
	codexScript := readFile(t, filepath.Join(out, "install-codex.sh"))
	if !strings.Contains(codexScript, "skillkit verify --bundle") || !strings.Contains(codexScript, "RDEV_SKILLKIT_FORCE") {
		t.Fatalf("codex script should verify and refuse overwrite by default:\n%s", codexScript)
	}
	genericScript := readFile(t, filepath.Join(out, "install-generic-mcp-agent.sh"))
	if !strings.Contains(genericScript, "Set RDEV_GENERIC_AGENT_SKILLS_DIR") {
		t.Fatalf("generic script should require explicit target env:\n%s", genericScript)
	}

	report, err := VerifyInstallPlan(filepath.Join(out, "install-plan.json"), now)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() {
		t.Fatalf("expected install plan to verify: %#v", report.Checks)
	}
	if report.SchemaVersion != InstallPlanVerificationSchemaVersion {
		t.Fatalf("unexpected verification schema %q", report.SchemaVersion)
	}
	if report.FilesVerified == 0 || report.FrameworksVerified != 3 {
		t.Fatalf("unexpected verification counts: files=%d frameworks=%d", report.FilesVerified, report.FrameworksVerified)
	}
}

func TestVerifyInstallPlanRejectsTamperedScript(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "bundle")
	if _, err := Export(ExportOptions{
		SourceRoot: filepath.Join("..", ".."),
		OutDir:     bundle,
	}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "install-plan")
	if _, err := PlanInstall(InstallPlanOptions{
		BundleDir:  bundle,
		OutDir:     out,
		Frameworks: []string{"codex"},
	}); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(out, "install-codex.sh")
	if err := os.WriteFile(scriptPath, []byte(readFile(t, scriptPath)+"\nrm -rf \"$HOME\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	report, err := VerifyInstallPlan(filepath.Join(out, "install-plan.json"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if report.OK() {
		t.Fatalf("expected tampered install plan to fail")
	}
	if !checkDetailContains(report.Checks, "listed_files_sha256_match", "install-codex.sh") {
		t.Fatalf("expected sha256 failure for tampered script: %#v", report.Checks)
	}
	if !checkDetailContains(report.Checks, "install_scripts_no_forbidden_mutation", "install-codex.sh:rm -rf") {
		t.Fatalf("expected forbidden mutation failure: %#v", report.Checks)
	}
}

func TestVerifyInstallPlanRejectsUnlistedFile(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "bundle")
	if _, err := Export(ExportOptions{
		SourceRoot: filepath.Join("..", ".."),
		OutDir:     bundle,
	}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "install-plan")
	if _, err := PlanInstall(InstallPlanOptions{
		BundleDir:  bundle,
		OutDir:     out,
		Frameworks: []string{"codex"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "surprise.ps1"), []byte("Write-Output surprise\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := VerifyInstallPlan(filepath.Join(out, "install-plan.json"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if report.OK() {
		t.Fatal("expected unlisted install plan file verification to fail")
	}
	if !checkDetailContains(report.Checks, "install_plan_has_no_unlisted_files", "surprise.ps1") {
		t.Fatalf("expected unlisted file failure: %#v", report.Checks)
	}
}

func TestPlanInstallRejectsUnsupportedFramework(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "bundle")
	if _, err := Export(ExportOptions{
		SourceRoot: filepath.Join("..", ".."),
		OutDir:     bundle,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := PlanInstall(InstallPlanOptions{
		BundleDir:  bundle,
		OutDir:     filepath.Join(t.TempDir(), "install-plan"),
		Frameworks: []string{"unsupported-agent"},
	})
	if err == nil {
		t.Fatal("expected unsupported framework to fail")
	}
	if !strings.Contains(err.Error(), "unsupported framework") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallDryRunDoesNotCopyFiles(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "bundle")
	if _, err := Export(ExportOptions{
		SourceRoot: filepath.Join("..", ".."),
		OutDir:     bundle,
	}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "codex-skills")

	report, err := Install(InstallOptions{
		BundleDir:   bundle,
		Framework:   "codex",
		TargetDir:   target,
		GeneratedAt: time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() {
		t.Fatalf("expected dry-run install report to verify: %#v", report.Checks)
	}
	if report.Executed || report.LocalMutation {
		t.Fatalf("dry-run must not execute or mutate: %#v", report)
	}
	if _, err := os.Stat(filepath.Join(target, "remote-vibe-coding")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not copy skills, stat err=%v", err)
	}
}

func TestInstallExecuteCopiesVerifiedBundle(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "bundle")
	if _, err := Export(ExportOptions{
		SourceRoot: filepath.Join("..", ".."),
		OutDir:     bundle,
	}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "codex-skills")

	report, err := Install(InstallOptions{
		BundleDir: bundle,
		Framework: "codex",
		TargetDir: target,
		Execute:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() || !report.Executed || !report.LocalMutation || report.ExternalMutation {
		t.Fatalf("unexpected install report: %#v", report)
	}
	for _, path := range []string{
		"remote-vibe-coding/SKILL.md",
		"safe-remote-support/SKILL.md",
		".remote-dev-skillkit/mcp/tools.json",
		".remote-dev-skillkit/frameworks/codex.md",
	} {
		if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected installed file %s: %v", path, err)
		}
	}
}

func TestInstallRejectsConflictUnlessForced(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "bundle")
	if _, err := Export(ExportOptions{
		SourceRoot: filepath.Join("..", ".."),
		OutDir:     bundle,
	}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "codex-skills")
	if err := os.MkdirAll(filepath.Join(target, "remote-vibe-coding"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "remote-vibe-coding", "old.txt"), []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := Install(InstallOptions{
		BundleDir: bundle,
		Framework: "codex",
		TargetDir: target,
		Execute:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK() {
		t.Fatalf("expected conflict to fail without force")
	}
	if !checkDetailContains(report.Checks, "existing_skill_conflicts", "remote-vibe-coding") {
		t.Fatalf("expected conflict check: %#v", report.Checks)
	}

	forced, err := Install(InstallOptions{
		BundleDir: bundle,
		Framework: "codex",
		TargetDir: target,
		Execute:   true,
		Force:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !forced.OK() || !forced.Executed {
		t.Fatalf("expected forced install to pass: %#v", forced.Checks)
	}
	if _, err := os.Stat(filepath.Join(target, "remote-vibe-coding", "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected force to replace old skill directory, stat err=%v", err)
	}
}

func TestInstallGenericRequiresExplicitTarget(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "bundle")
	if _, err := Export(ExportOptions{
		SourceRoot: filepath.Join("..", ".."),
		OutDir:     bundle,
	}); err != nil {
		t.Fatal(err)
	}

	report, err := Install(InstallOptions{
		BundleDir: bundle,
		Framework: "generic",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK() {
		t.Fatalf("expected generic install without target to fail")
	}
	if !checkDetailContains(report.Checks, "generic_target_explicit", "RDEV_GENERIC_AGENT_SKILLS_DIR") {
		t.Fatalf("expected explicit target failure: %#v", report.Checks)
	}
}

func TestInstallGenericUsesTargetEnv(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "bundle")
	if _, err := Export(ExportOptions{
		SourceRoot: filepath.Join("..", ".."),
		OutDir:     bundle,
	}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "generic-skills")
	t.Setenv("RDEV_GENERIC_AGENT_SKILLS_DIR", target)

	report, err := Install(InstallOptions{
		BundleDir: bundle,
		Framework: "generic",
		Execute:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() || !report.Executed {
		t.Fatalf("expected env target install to pass: %#v", report.Checks)
	}
	if report.TargetDir != target {
		t.Fatalf("expected target %q, got %q", target, report.TargetDir)
	}
	if _, err := os.Stat(filepath.Join(target, "remote-vibe-coding", "SKILL.md")); err != nil {
		t.Fatalf("expected installed generic skill: %v", err)
	}
}

func TestVerifyRejectsTamperedBundleFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle")
	if _, err := Export(ExportOptions{
		SourceRoot: filepath.Join("..", ".."),
		OutDir:     out,
	}); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(out, "skills", "remote-vibe-coding", "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := Verify(VerifyOptions{BundleDir: out})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK() {
		t.Fatalf("expected tampered bundle verification to fail")
	}
	if !checkDetailContains(report.Checks, "listed_files_sha256_match", "skills/remote-vibe-coding/SKILL.md") {
		t.Fatalf("expected sha256 failure for tampered skill: %#v", report.Checks)
	}
}

func TestVerifyRejectsUnlistedBundleFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle")
	if _, err := Export(ExportOptions{
		SourceRoot: filepath.Join("..", ".."),
		OutDir:     out,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "surprise.txt"), []byte("not in manifest\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := Verify(VerifyOptions{BundleDir: out})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK() {
		t.Fatalf("expected unlisted bundle file verification to fail")
	}
	if !checkDetailContains(report.Checks, "bundle_has_no_unlisted_files", "surprise.txt") {
		t.Fatalf("expected unlisted file failure: %#v", report.Checks)
	}
}

func hasSkill(manifest Manifest, name string) bool {
	for _, skill := range manifest.Skills {
		if skill.Name == name {
			return true
		}
	}
	return false
}

func hasFile(manifest Manifest, path string) bool {
	for _, file := range manifest.Files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func hasFrameworkInstallPlan(plan InstallPlan, framework string) bool {
	for _, item := range plan.Frameworks {
		if item.Framework == framework {
			return true
		}
	}
	return false
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func checkDetailContains(checks []VerificationCheck, name, value string) bool {
	for _, check := range checks {
		if check.Name == name && strings.Contains(check.Detail, value) {
			return true
		}
	}
	return false
}
