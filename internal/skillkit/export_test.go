package skillkit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
	assertAdaptiveConfigurationContract(t, manifest.AdaptiveConfiguration)
	for _, skill := range []string{"safe-remote-support", "host-triage", "remote-session-review", "remote-vibe-coding"} {
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
	for _, want := range []string{
		"Adaptive Configuration Contract",
		"probe the installed `rdev` binary",
		"must ask a short follow-up question instead of inventing a value",
		"Examples such as `https://api.example.com/v1`, `/Users/example`, `/home/example`, and `C:\\Users\\Alice` are placeholders",
	} {
		if !strings.Contains(install, want) {
			t.Fatalf("expected install doc to contain %q, got %s", want, install)
		}
	}
	codexDoc := readFile(t, filepath.Join(out, "frameworks", "codex.md"))
	for _, want := range []string{
		"probe the installed `rdev` binary",
		"must ask a short follow-up question instead of inventing a value",
		"framework install path",
	} {
		if !strings.Contains(codexDoc, want) {
			t.Fatalf("expected codex framework doc to contain %q, got %s", want, codexDoc)
		}
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
	assertAdaptiveConfigurationContract(t, onDisk.AdaptiveConfiguration)
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

func TestExportRejectsHiddenFilesInSkillsRoot(t *testing.T) {
	sourceRoot := minimalSkillSourceRoot(t)
	if err := os.WriteFile(filepath.Join(sourceRoot, "skills", ".DS_Store"), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Export(ExportOptions{SourceRoot: sourceRoot, OutDir: filepath.Join(t.TempDir(), "bundle")})
	if err == nil || !strings.Contains(err.Error(), "unsupported file in skills directory") {
		t.Fatalf("expected unsupported hidden file error, got %v", err)
	}
}

func TestExportRejectsHiddenFilesInsideSkill(t *testing.T) {
	sourceRoot := minimalSkillSourceRoot(t)
	if err := os.WriteFile(filepath.Join(sourceRoot, "skills", "remote-vibe-coding", ".DS_Store"), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Export(ExportOptions{SourceRoot: sourceRoot, OutDir: filepath.Join(t.TempDir(), "bundle")})
	if err == nil || !strings.Contains(err.Error(), "unsupported hidden file in skill remote-vibe-coding") {
		t.Fatalf("expected unsupported hidden file error, got %v", err)
	}
}

func TestExportRejectsUnscopedSkillDocs(t *testing.T) {
	sourceRoot := minimalSkillSourceRoot(t)
	if err := os.WriteFile(filepath.Join(sourceRoot, "skills", "remote-vibe-coding", "README.md"), []byte("# extra\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Export(ExportOptions{SourceRoot: sourceRoot, OutDir: filepath.Join(t.TempDir(), "bundle")})
	if err == nil || !strings.Contains(err.Error(), "unsupported file in skill remote-vibe-coding") {
		t.Fatalf("expected unsupported skill file error, got %v", err)
	}
}

func TestRecommendedRdevCommandSkipsNonExecutableGoBinFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not use POSIX executable bits")
	}
	home := t.TempDir()
	gobin := filepath.Join(home, "go-bin")
	if err := os.MkdirAll(gobin, 0o700); err != nil {
		t.Fatal(err)
	}
	rdevPath := filepath.Join(gobin, "rdev")
	if err := os.WriteFile(rdevPath, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GOBIN", gobin)
	t.Setenv("GOPATH", "")
	t.Setenv("PATH", t.TempDir())

	if got := RecommendedRdevCommand(); got == rdevPath {
		t.Fatalf("non-executable GOBIN rdev must not be recommended for MCP config: %s", got)
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
	if !checkPassed(report.Checks, "adaptive_configuration_contract") {
		t.Fatalf("expected adaptive configuration contract check: %#v", report.Checks)
	}
	if !checkPassed(report.Checks, "required_skills_keep_adaptive_contract") {
		t.Fatalf("expected required skills adaptive contract check: %#v", report.Checks)
	}
	if !checkPassed(report.Checks, "skill_agents_metadata") {
		t.Fatalf("expected skill agents metadata check: %#v", report.Checks)
	}
}

func minimalSkillSourceRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills", "remote-vibe-coding"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "mcp"), 0o700); err != nil {
		t.Fatal(err)
	}
	skill := strings.Join([]string{
		"---",
		"name: remote-vibe-coding",
		"description: Test skill.",
		"---",
		"",
		"# Remote Vibe Coding",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, "skills", "remote-vibe-coding", "SKILL.md"), []byte(skill), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "mcp", "tools.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
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
	assertAdaptiveConfigurationContract(t, plan.AdaptiveConfiguration)
	for _, framework := range []string{"codex", "hermes", "generic-mcp-agent"} {
		if !hasFrameworkInstallPlan(plan, framework) {
			t.Fatalf("missing framework install plan %q: %#v", framework, plan.Frameworks)
		}
	}
	codexScript := readFile(t, filepath.Join(out, "install-codex.sh"))
	if !strings.Contains(codexScript, "skillkit verify --bundle") || !strings.Contains(codexScript, "RDEV_SKILLKIT_FORCE") {
		t.Fatalf("codex script should verify and refuse overwrite by default:\n%s", codexScript)
	}
	if !strings.Contains(codexScript, "rdev doctor") || !strings.Contains(codexScript, "ask instead of inventing") {
		t.Fatalf("codex script should preserve adaptive configuration guidance:\n%s", codexScript)
	}
	genericScript := readFile(t, filepath.Join(out, "install-generic-mcp-agent.sh"))
	if !strings.Contains(genericScript, "Set RDEV_GENERIC_AGENT_SKILLS_DIR") {
		t.Fatalf("generic script should require explicit target env:\n%s", genericScript)
	}
	installCommands := readFile(t, filepath.Join(out, "INSTALL_COMMANDS.md"))
	for _, want := range []string{
		"Adaptive Configuration Contract",
		"rdev doctor",
		"rdev mcp tools",
		"ask a short follow-up question instead of inventing a value",
	} {
		if !strings.Contains(installCommands, want) {
			t.Fatalf("expected install commands to contain %q, got %s", want, installCommands)
		}
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
	if !checkPassed(report.Checks, "adaptive_configuration_contract") {
		t.Fatalf("expected install plan adaptive contract check: %#v", report.Checks)
	}
	if !checkPassed(report.Checks, "install_commands_keep_adaptive_contract") {
		t.Fatalf("expected install commands adaptive contract check: %#v", report.Checks)
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	goBin := filepath.Join(home, "go", "bin")
	if err := os.MkdirAll(goBin, 0o755); err != nil {
		t.Fatal(err)
	}
	goBinRdev := filepath.Join(goBin, "rdev")
	if err := os.WriteFile(goBinRdev, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
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
	if report.MCPCommand != goBinRdev+" mcp serve" ||
		!slices.Contains(report.RecommendedNextSteps, "Configure the agent MCP client to execute: "+goBinRdev+" mcp serve.") {
		t.Fatalf("expected install report to recommend absolute MCP command outside PATH, got %#v", report)
	}
	for _, path := range []string{
		"remote-vibe-coding/SKILL.md",
		"safe-remote-support/SKILL.md",
		".remote-dev-skillkit/install.json",
		".remote-dev-skillkit/mcp/tools.json",
		".remote-dev-skillkit/frameworks/codex.md",
	} {
		if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected installed file %s: %v", path, err)
		}
	}
	var installManifest struct {
		SchemaVersion string `json:"schema_version"`
		SkillFiles    []struct {
			Name         string `json:"name"`
			RelativePath string `json:"relative_path"`
			SHA256       string `json:"sha256"`
			SizeBytes    int    `json:"size_bytes"`
		} `json:"skill_files"`
		ReferenceFiles []struct {
			Name         string `json:"name"`
			RelativePath string `json:"relative_path"`
			SHA256       string `json:"sha256"`
			SizeBytes    int    `json:"size_bytes"`
		} `json:"reference_files"`
	}
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(target, ".remote-dev-skillkit", "install.json"))), &installManifest); err != nil {
		t.Fatal(err)
	}
	if installManifest.SchemaVersion != InstallManifestSchemaVersion || len(installManifest.SkillFiles) != 4 || len(installManifest.ReferenceFiles) != 2 {
		t.Fatalf("expected install manifest skill and reference file hashes, got %#v", installManifest)
	}
	for _, skill := range installManifest.SkillFiles {
		if skill.Name == "" || skill.RelativePath == "" || !strings.HasPrefix(skill.SHA256, "sha256:") || skill.SizeBytes <= 0 {
			t.Fatalf("invalid install manifest skill file entry: %#v", skill)
		}
	}
	referenceNames := map[string]bool{}
	for _, ref := range installManifest.ReferenceFiles {
		referenceNames[ref.Name] = true
		if ref.RelativePath == "" || !strings.HasPrefix(ref.SHA256, "sha256:") || ref.SizeBytes <= 0 {
			t.Fatalf("invalid install manifest reference file entry: %#v", ref)
		}
	}
	if !referenceNames["mcp-tools"] || !referenceNames["framework-doc"] {
		t.Fatalf("expected manifest to hash MCP tools and framework doc, got %#v", installManifest.ReferenceFiles)
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

func checkPassed(checks []VerificationCheck, name string) bool {
	for _, check := range checks {
		if check.Name == name && check.Passed {
			return true
		}
	}
	return false
}

func assertAdaptiveConfigurationContract(t *testing.T, contract AdaptiveConfigurationContract) {
	t.Helper()
	if contract.SchemaVersion != AdaptiveConfigurationSchemaVersion {
		t.Fatalf("unexpected adaptive configuration schema %q", contract.SchemaVersion)
	}
	if !contract.Required {
		t.Fatal("adaptive configuration contract must be required")
	}
	for _, want := range []string{
		"rdev doctor",
		"rdev mcp tools",
		"target OS and shell",
		"service manager",
		"gateway configuration",
		"network reachability",
		"proxy and DNS state",
		"NAT/firewall/CGNAT constraints",
		"SSH configuration",
		"installed tunnel or mesh tools",
		"available connection modes",
		"workspace path",
		"framework install path",
	} {
		if !containsString(contract.ProbeBeforeActing, want) {
			t.Fatalf("adaptive configuration probes missing %q: %#v", want, contract.ProbeBeforeActing)
		}
	}
	for _, want := range []string{
		"gateway URL",
		"ticket code",
		"root key",
		"release URL",
		"checksum",
		"framework install path",
		"workspace root",
		"adapter choice",
		"tunnel or mesh authorization",
		"authorization policy",
	} {
		if !containsString(contract.AskIfUnclear, want) {
			t.Fatalf("adaptive configuration ask list missing %q: %#v", want, contract.AskIfUnclear)
		}
	}
	for _, want := range []string{"https://api.example.com/v1", "/Users/example", "/home/example", `C:\Users\Alice`} {
		if !containsString(contract.Placeholders, want) {
			t.Fatalf("adaptive configuration placeholders missing %q: %#v", want, contract.Placeholders)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
