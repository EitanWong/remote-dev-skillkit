package acceptance

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

	"github.com/EitanWong/remote-dev-skillkit/internal/shelladapter"
)

const WindowsTemporaryPackageSchemaVersion = "rdev.acceptance-package.windows-temporary.v1"

type WindowsTemporaryPackageOptions struct {
	PlanPath                string
	OutDir                  string
	TranscriptPath          string
	ReleaseVerificationPath string
	AuditPath               string
	NoPersistenceDir        string
	ApprovalProbesDir       string
	NotesPath               string
	Now                     time.Time
}

type WindowsTemporaryPackage struct {
	SchemaVersion       string                           `json:"schema_version"`
	GeneratedAt         time.Time                        `json:"generated_at"`
	OutDir              string                           `json:"out_dir"`
	PlanPath            string                           `json:"plan_path"`
	PlanSchema          string                           `json:"plan_schema"`
	PlanVerification    WindowsTemporaryPlanVerification `json:"plan_verification"`
	Checks              []Check                          `json:"checks"`
	Files               []WindowsTemporaryPackageFile    `json:"files"`
	RedactionRuleCounts map[string]int                   `json:"redaction_rule_counts,omitempty"`
	RequiredEvidence    []string                         `json:"required_evidence"`
	RecommendedActions  []string                         `json:"recommended_actions,omitempty"`
}

type WindowsTemporaryPackageFile struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	SHA256    string `json:"sha256"`
	SizeBytes int    `json:"size_bytes"`
	Source    string `json:"source,omitempty"`
}

func (p WindowsTemporaryPackage) OK() bool {
	if len(p.Checks) == 0 {
		return false
	}
	for _, check := range p.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func PackageWindowsTemporaryEvidence(opts WindowsTemporaryPackageOptions) (WindowsTemporaryPackage, error) {
	if strings.TrimSpace(opts.PlanPath) == "" {
		return WindowsTemporaryPackage{}, fmt.Errorf("plan path is required")
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return WindowsTemporaryPackage{}, fmt.Errorf("output directory is required")
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return WindowsTemporaryPackage{}, err
	}
	if err := prepareAcceptanceOut(outDir); err != nil {
		return WindowsTemporaryPackage{}, err
	}
	planPath, err := filepath.Abs(opts.PlanPath)
	if err != nil {
		return WindowsTemporaryPackage{}, err
	}
	plan, err := readWindowsTemporaryPlan(planPath)
	if err != nil {
		return WindowsTemporaryPackage{}, err
	}
	verification, err := VerifyWindowsTemporaryPlan(planPath)
	if err != nil {
		return WindowsTemporaryPackage{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	pkg := WindowsTemporaryPackage{
		SchemaVersion:    WindowsTemporaryPackageSchemaVersion,
		GeneratedAt:      now.UTC(),
		OutDir:           outDir,
		PlanPath:         planPath,
		PlanSchema:       plan.SchemaVersion,
		PlanVerification: verification,
		RequiredEvidence: plan.RequiredEvidence,
	}
	add := func(name string, passed bool, detail string) {
		pkg.Checks = append(pkg.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	redactor := shelladapter.NewArtifactRedactor()
	add("plan_verification_ok", verification.OK(), failedCheckNames(verification.Checks))

	var files []WindowsTemporaryPackageFile
	if entry, err := copyPackageFile(outDir, "plan/windows-temporary-plan.json", "plan", planPath, nil); err != nil {
		return WindowsTemporaryPackage{}, err
	} else {
		files = append(files, entry)
	}
	launcherPath := resolvePlanPath(filepath.Dir(planPath), plan.LauncherPath, "run-windows-temporary.ps1")
	if entry, err := copyPackageFile(outDir, "plan/run-windows-temporary.ps1", "launcher", launcherPath, nil); err != nil {
		add("launcher_copied", false, err.Error())
	} else {
		files = append(files, entry)
		add("launcher_copied", true, entry.Path)
	}

	files = append(files, copyOptionalEvidence(outDir, "evidence/transcript.txt", "transcript", opts.TranscriptPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/release-verification.txt", "release-verification", opts.ReleaseVerificationPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/audit.txt", "audit", opts.AuditPath, redactor, add)...)
	files = append(files, copyNotesEvidence(outDir, opts.NotesPath, redactor, add)...)

	noPersistenceFiles, noPersistenceNames := copyEvidenceDir(outDir, "evidence/no-persistence", "no-persistence", opts.NoPersistenceDir, redactor, add)
	files = append(files, noPersistenceFiles...)
	approvalFiles, approvalNames := copyEvidenceDir(outDir, "evidence/approval-probes", "approval-probe", opts.ApprovalProbesDir, redactor, add)
	files = append(files, approvalFiles...)

	add("transcript_present", fileEntryKindPresent(files, "transcript"), opts.TranscriptPath)
	releaseOK := releaseVerificationOK(outDir, "evidence/release-verification.txt")
	add("release_verification_present", fileEntryKindPresent(files, "release-verification"), opts.ReleaseVerificationPath)
	add("release_verification_ok", releaseOK, opts.ReleaseVerificationPath)
	add("audit_present", fileEntryKindPresent(files, "audit"), opts.AuditPath)
	add("no_persistence_evidence_complete", namesCoverWindowsCommands(noPersistenceNames, plan.NoPersistenceChecks), missingWindowsEvidenceNames(noPersistenceNames, windowsCommandNames(plan.NoPersistenceChecks)))
	add("approval_probe_evidence_complete", namesCoverApprovalProbes(approvalNames, plan.ApprovalProbes), missingApprovalEvidenceNames(approvalNames, plan.ApprovalProbes))

	if redactor.Redacted() {
		pkg.RedactionRuleCounts = redactor.Counts()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	checksums, checksumEntry, err := writePackageChecksums(outDir, files)
	if err != nil {
		return WindowsTemporaryPackage{}, err
	}
	files = append(files, checksumEntry)
	pkg.Files = files
	add("checksums_written", len(checksums) > 0, "checksums.txt")
	add("package_files_written", len(pkg.Files) >= 5, fmt.Sprintf("%d", len(pkg.Files)))
	if !pkg.OK() {
		pkg.RecommendedActions = []string{
			"Collect missing Windows acceptance evidence from the clean VM run.",
			"Re-run package-windows-temporary after redacting transcripts and verifier output.",
			"Do not publish this acceptance package as release evidence until every check passes.",
		}
	}
	content, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return WindowsTemporaryPackage{}, err
	}
	content = append(content, '\n')
	if _, err := writePackageContent(outDir, "package.json", "package-manifest", content, ""); err != nil {
		return WindowsTemporaryPackage{}, err
	}
	return pkg, nil
}

func readWindowsTemporaryPlan(path string) (WindowsTemporaryPlan, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return WindowsTemporaryPlan{}, err
	}
	var plan WindowsTemporaryPlan
	if err := json.Unmarshal(content, &plan); err != nil {
		return WindowsTemporaryPlan{}, err
	}
	return plan, nil
}

func copyOptionalEvidence(root, bundlePath, kind, source string, redactor *shelladapter.ArtifactRedactor, add func(string, bool, string)) []WindowsTemporaryPackageFile {
	if strings.TrimSpace(source) == "" {
		add(kind+"_copied", false, "missing")
		return nil
	}
	entry, err := copyPackageFile(root, bundlePath, kind, source, redactor)
	if err != nil {
		add(kind+"_copied", false, err.Error())
		return nil
	}
	add(kind+"_copied", true, entry.Path)
	return []WindowsTemporaryPackageFile{entry}
}

func copyNotesEvidence(root, source string, redactor *shelladapter.ArtifactRedactor, add func(string, bool, string)) []WindowsTemporaryPackageFile {
	if strings.TrimSpace(source) == "" {
		return nil
	}
	entry, err := copyPackageFile(root, "evidence/notes.txt", "notes", source, redactor)
	if err != nil {
		add("notes_copied", false, err.Error())
		return nil
	}
	add("notes_copied", true, entry.Path)
	return []WindowsTemporaryPackageFile{entry}
}

func copyEvidenceDir(root, bundleDir, kind, sourceDir string, redactor *shelladapter.ArtifactRedactor, add func(string, bool, string)) ([]WindowsTemporaryPackageFile, []string) {
	if strings.TrimSpace(sourceDir) == "" {
		add(kind+"_dir_copied", false, "missing")
		return nil, nil
	}
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		add(kind+"_dir_copied", false, err.Error())
		return nil, nil
	}
	var files []WindowsTemporaryPackageFile
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := sanitizeEvidenceName(entry.Name())
		if name == "" {
			continue
		}
		source := filepath.Join(sourceDir, entry.Name())
		copied, err := copyPackageFile(root, filepath.ToSlash(filepath.Join(bundleDir, name)), kind, source, redactor)
		if err != nil {
			add(kind+"_dir_copied", false, err.Error())
			return files, names
		}
		files = append(files, copied)
		names = append(names, strings.TrimSuffix(name, filepath.Ext(name)))
	}
	add(kind+"_dir_copied", len(files) > 0, fmt.Sprintf("%d", len(files)))
	return files, names
}

func copyPackageFile(root, bundlePath, kind, source string, redactor *shelladapter.ArtifactRedactor) (WindowsTemporaryPackageFile, error) {
	content, err := os.ReadFile(source)
	if err != nil {
		return WindowsTemporaryPackageFile{}, err
	}
	if redactor != nil {
		content = []byte(redactor.Redact(string(content)))
	}
	return writePackageContent(root, bundlePath, kind, content, source)
}

func writePackageContent(root, bundlePath, kind string, content []byte, source string) (WindowsTemporaryPackageFile, error) {
	clean := filepath.Clean(filepath.FromSlash(bundlePath))
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return WindowsTemporaryPackageFile{}, fmt.Errorf("invalid package path %q", bundlePath)
	}
	path := filepath.Join(root, clean)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return WindowsTemporaryPackageFile{}, err
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return WindowsTemporaryPackageFile{}, err
	}
	sum := sha256.Sum256(content)
	return WindowsTemporaryPackageFile{
		Path:      filepath.ToSlash(clean),
		Kind:      kind,
		SHA256:    "sha256:" + hex.EncodeToString(sum[:]),
		SizeBytes: len(content),
		Source:    source,
	}, nil
}

func writePackageChecksums(root string, files []WindowsTemporaryPackageFile) ([]byte, WindowsTemporaryPackageFile, error) {
	var builder strings.Builder
	for _, file := range files {
		builder.WriteString(strings.TrimPrefix(file.SHA256, "sha256:"))
		builder.WriteString("  ")
		builder.WriteString(file.Path)
		builder.WriteByte('\n')
	}
	content := []byte(builder.String())
	entry, err := writePackageContent(root, "checksums.txt", "checksums", content, "")
	return content, entry, err
}

func releaseVerificationOK(root, path string) bool {
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return false
	}
	var value any
	if err := json.Unmarshal(content, &value); err == nil {
		return jsonHasOKTrue(value)
	}
	compact := strings.Join(strings.Fields(string(content)), "")
	return strings.Contains(compact, `"ok":true`)
}

func jsonHasOKTrue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		if ok, exists := typed["ok"].(bool); exists && ok {
			return true
		}
		for _, child := range typed {
			if jsonHasOKTrue(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if jsonHasOKTrue(child) {
				return true
			}
		}
	}
	return false
}

func fileEntryKindPresent(files []WindowsTemporaryPackageFile, kind string) bool {
	for _, file := range files {
		if file.Kind == kind && file.SizeBytes > 0 {
			return true
		}
	}
	return false
}

func namesCoverWindowsCommands(names []string, commands []WindowsAcceptanceCommand) bool {
	return missingWindowsEvidenceNames(names, windowsCommandNames(commands)) == ""
}

func missingWindowsEvidenceNames(names []string, required map[string]bool) string {
	seen := evidenceNameSet(names)
	var missing []string
	for name := range required {
		if !seen[sanitizeEvidenceStem(name)] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return strings.Join(missing, ",")
}

func namesCoverApprovalProbes(names []string, probes []WindowsApprovalProbe) bool {
	return missingApprovalEvidenceNames(names, probes) == ""
}

func missingApprovalEvidenceNames(names []string, probes []WindowsApprovalProbe) string {
	seen := evidenceNameSet(names)
	var missing []string
	for _, probe := range probes {
		name := sanitizeEvidenceStem(probe.Operation)
		if !seen[name] {
			missing = append(missing, probe.Operation)
		}
	}
	sort.Strings(missing)
	return strings.Join(missing, ",")
}

func evidenceNameSet(names []string) map[string]bool {
	seen := map[string]bool{}
	for _, name := range names {
		seen[sanitizeEvidenceStem(name)] = true
	}
	return seen
}

func sanitizeEvidenceName(name string) string {
	ext := filepath.Ext(name)
	stem := sanitizeEvidenceStem(strings.TrimSuffix(name, ext))
	if stem == "" {
		return ""
	}
	if ext == "" {
		ext = ".txt"
	}
	return stem + strings.ToLower(ext)
}

func sanitizeEvidenceStem(name string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune('-')
		default:
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}
