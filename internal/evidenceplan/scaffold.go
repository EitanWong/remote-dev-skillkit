package evidenceplan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const ScaffoldSchemaVersion = "rdev.acceptance-evidence-scaffold.v1"
const StatusSchemaVersion = "rdev.acceptance-evidence-status.v1"

type Options struct {
	PlanPath                  string
	HostedProviderPackagePath string
	RelayAdapterPackagePath   string
	OutDir                    string
	PackageDir                string
	CreatePlaceholders        bool
	Force                     bool
	GeneratedAt               time.Time
}

type StatusOptions struct {
	ScaffoldPath string
	GeneratedAt  time.Time
}

type Scaffold struct {
	SchemaVersion         string           `json:"schema_version"`
	GeneratedAt           time.Time        `json:"generated_at"`
	OK                    bool             `json:"ok"`
	ReadyForPackaging     bool             `json:"ready_for_packaging"`
	PlanPath              string           `json:"plan_path"`
	PlanSchema            string           `json:"plan_schema"`
	PlanKind              string           `json:"plan_kind"`
	OutDir                string           `json:"out_dir"`
	PackageDir            string           `json:"package_dir,omitempty"`
	CreatePlaceholders    bool             `json:"create_placeholders"`
	EvidenceFiles         []ScaffoldFile   `json:"evidence_files"`
	Commands              ScaffoldCommands `json:"commands"`
	ChecklistPath         string           `json:"checklist_path"`
	PlanCopyPath          string           `json:"plan_copy_path"`
	ReportPath            string           `json:"report_path"`
	AgentRules            []string         `json:"agent_rules"`
	AuthorizationRequired []string         `json:"authorization_required,omitempty"`
	UnsupportedClaims     []string         `json:"unsupported_claims,omitempty"`
	Checks                []ScaffoldCheck  `json:"checks"`
	RecommendedActions    []string         `json:"recommended_actions,omitempty"`
}

type Status struct {
	SchemaVersion      string           `json:"schema_version"`
	GeneratedAt        time.Time        `json:"generated_at"`
	OK                 bool             `json:"ok"`
	ReadyForPackaging  bool             `json:"ready_for_packaging"`
	ScaffoldPath       string           `json:"scaffold_path"`
	ReportPath         string           `json:"report_path"`
	PlanPath           string           `json:"plan_path"`
	PlanSchema         string           `json:"plan_schema"`
	PlanKind           string           `json:"plan_kind"`
	OutDir             string           `json:"out_dir"`
	PackageDir         string           `json:"package_dir,omitempty"`
	RequiredReady      int              `json:"required_ready"`
	RequiredTotal      int              `json:"required_total"`
	PlaceholderCount   int              `json:"placeholder_count"`
	MissingCount       int              `json:"missing_count"`
	EmptyCount         int              `json:"empty_count"`
	EvidenceFiles      []StatusFile     `json:"evidence_files"`
	Commands           ScaffoldCommands `json:"commands"`
	Checks             []ScaffoldCheck  `json:"checks"`
	RecommendedActions []string         `json:"recommended_actions,omitempty"`
}

type ScaffoldFile struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	Required    bool   `json:"required"`
	Flag        string `json:"flag"`
	Description string `json:"description"`
	Exists      bool   `json:"exists"`
	Placeholder bool   `json:"placeholder"`
}

type StatusFile struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	Required    bool   `json:"required"`
	Flag        string `json:"flag"`
	Description string `json:"description"`
	Exists      bool   `json:"exists"`
	Placeholder bool   `json:"placeholder"`
	Empty       bool   `json:"empty"`
	SizeBytes   int64  `json:"size_bytes"`
	Ready       bool   `json:"ready"`
}

type ScaffoldCommands struct {
	Preflight []string `json:"preflight,omitempty"`
	DryRun    []string `json:"dry_run,omitempty"`
	Run       []string `json:"run,omitempty"`
	Package   []string `json:"package"`
	Verify    []string `json:"verify"`
}

type ScaffoldCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type rawPlan struct {
	SchemaVersion         string        `json:"schema_version"`
	StorageProvider       string        `json:"storage_provider"`
	AuthProvider          string        `json:"auth_provider"`
	AdapterKind           string        `json:"adapter_kind"`
	ConnectionPathID      string        `json:"connection_path_id"`
	PackagePath           string        `json:"package_path"`
	ExternalMutation      bool          `json:"external_mutation"`
	EvidenceFiles         []rawPlanFile `json:"evidence_files"`
	PackageCommand        []string      `json:"package_command"`
	VerifyCommand         []string      `json:"verify_command"`
	PreflightCommands     []string      `json:"preflight_commands"`
	DryRunCommand         []string      `json:"dry_run_command"`
	RunCommand            []string      `json:"run_command"`
	AgentRules            []string      `json:"agent_rules"`
	AuthorizationRequired []string      `json:"authorization_required"`
	UnsupportedClaims     []string      `json:"unsupported_claims"`
}

type rawPlanFile struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	Required    bool   `json:"required"`
	Flag        string `json:"flag"`
	Description string `json:"description"`
}

func Build(opts Options) (Scaffold, error) {
	if strings.TrimSpace(opts.OutDir) == "" {
		return Scaffold{}, fmt.Errorf("out is required")
	}
	opts, err := resolvePlanInput(opts)
	if err != nil {
		return Scaffold{}, err
	}
	planPath, err := filepath.Abs(opts.PlanPath)
	if err != nil {
		return Scaffold{}, err
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return Scaffold{}, err
	}
	if err := prepareOut(outDir, opts.Force); err != nil {
		return Scaffold{}, err
	}
	content, err := os.ReadFile(planPath)
	if err != nil {
		return Scaffold{}, err
	}
	var plan rawPlan
	if err := json.Unmarshal(content, &plan); err != nil {
		return Scaffold{}, err
	}
	kind, err := planKind(plan.SchemaVersion)
	if err != nil {
		return Scaffold{}, err
	}
	now := opts.GeneratedAt
	if now.IsZero() {
		now = time.Now()
	}
	packageDir := strings.TrimSpace(opts.PackageDir)
	if packageDir == "" {
		packageDir = filepath.Dir(planPath)
	}
	if abs, err := filepath.Abs(packageDir); err == nil {
		packageDir = abs
	}
	scaffold := Scaffold{
		SchemaVersion:         ScaffoldSchemaVersion,
		GeneratedAt:           now.UTC(),
		OK:                    true,
		ReadyForPackaging:     false,
		PlanPath:              planPath,
		PlanSchema:            plan.SchemaVersion,
		PlanKind:              kind,
		OutDir:                outDir,
		PackageDir:            packageDir,
		CreatePlaceholders:    opts.CreatePlaceholders,
		ChecklistPath:         filepath.Join(outDir, "AGENT_CHECKLIST.md"),
		PlanCopyPath:          filepath.Join(outDir, filepath.Base(planPath)),
		ReportPath:            filepath.Join(outDir, "scaffold-report.json"),
		AgentRules:            append([]string(nil), plan.AgentRules...),
		AuthorizationRequired: append([]string(nil), plan.AuthorizationRequired...),
		UnsupportedClaims:     append([]string(nil), plan.UnsupportedClaims...),
	}
	addCheck := func(name string, passed bool, detail string) {
		scaffold.Checks = append(scaffold.Checks, ScaffoldCheck{Name: name, Passed: passed, Detail: detail})
		if !passed {
			scaffold.OK = false
		}
	}
	addCheck("schema_supported", true, plan.SchemaVersion)
	addCheck("external_mutation_false", !plan.ExternalMutation, fmt.Sprintf("%t", plan.ExternalMutation))
	addCheck("package_path_declared", strings.TrimSpace(plan.PackagePath) != "", plan.PackagePath)
	addCheck("evidence_files_declared", len(plan.EvidenceFiles) > 0, fmt.Sprintf("%d", len(plan.EvidenceFiles)))
	addCheck("package_command_declared", len(plan.PackageCommand) > 0, strings.Join(plan.PackageCommand, " "))
	addCheck("verify_command_declared", len(plan.VerifyCommand) > 0, strings.Join(plan.VerifyCommand, " "))
	seen := map[string]bool{}
	for _, file := range plan.EvidenceFiles {
		safe := safeRelativePath(file.Path)
		addCheck("evidence_path_safe:"+file.Name, safe, file.Path)
		addCheck("evidence_path_unique:"+file.Name, !seen[file.Path], file.Path)
		seen[file.Path] = true
		entry := ScaffoldFile{
			Name:        file.Name,
			Path:        filepath.Join(outDir, filepath.FromSlash(file.Path)),
			Kind:        file.Kind,
			Required:    file.Required,
			Flag:        file.Flag,
			Description: file.Description,
			Placeholder: opts.CreatePlaceholders,
		}
		if safe && opts.CreatePlaceholders {
			if err := writePlaceholder(outDir, file); err != nil {
				return Scaffold{}, err
			}
			entry.Exists = true
		} else if safe {
			if _, err := os.Stat(entry.Path); err == nil {
				entry.Exists = true
			}
		}
		scaffold.EvidenceFiles = append(scaffold.EvidenceFiles, entry)
	}
	scaffold.Commands = commandsForPlan(kind, plan, packageDir, outDir)
	if err := os.WriteFile(scaffold.PlanCopyPath, append(content, '\n'), 0o644); err != nil {
		return Scaffold{}, err
	}
	if opts.CreatePlaceholders {
		scaffold.RecommendedActions = append(scaffold.RecommendedActions, "Replace every placeholder evidence file with real command output before running the package command.")
	} else {
		scaffold.RecommendedActions = append(scaffold.RecommendedActions, "Collect the listed evidence files, then run the package command from this report.")
	}
	scaffold.RecommendedActions = append(scaffold.RecommendedActions, "Do not claim production acceptance until the generated package verifies with the matching rdev acceptance verify command.")
	sort.Slice(scaffold.EvidenceFiles, func(i, j int) bool { return scaffold.EvidenceFiles[i].Path < scaffold.EvidenceFiles[j].Path })
	if err := os.WriteFile(scaffold.ChecklistPath, []byte(renderChecklist(scaffold)), 0o644); err != nil {
		return Scaffold{}, err
	}
	report, err := json.MarshalIndent(scaffold, "", "  ")
	if err != nil {
		return Scaffold{}, err
	}
	if err := os.WriteFile(scaffold.ReportPath, append(report, '\n'), 0o644); err != nil {
		return Scaffold{}, err
	}
	return scaffold, nil
}

func resolvePlanInput(opts Options) (Options, error) {
	hasPlan := strings.TrimSpace(opts.PlanPath) != ""
	hasHostedPackage := strings.TrimSpace(opts.HostedProviderPackagePath) != ""
	hasRelayPackage := strings.TrimSpace(opts.RelayAdapterPackagePath) != ""
	inputCount := 0
	for _, present := range []bool{hasPlan, hasHostedPackage, hasRelayPackage} {
		if present {
			inputCount++
		}
	}
	if inputCount == 0 {
		return Options{}, fmt.Errorf("plan, hosted provider package, or relay adapter package is required")
	}
	if inputCount > 1 {
		return Options{}, fmt.Errorf("provide only one of plan, hosted provider package, or relay adapter package")
	}
	if hasPlan {
		return opts, nil
	}
	var packagePath string
	var planName string
	var label string
	if hasHostedPackage {
		packagePath = opts.HostedProviderPackagePath
		planName = "runtime-evidence-plan.json"
		label = "hosted provider package"
	} else {
		packagePath = opts.RelayAdapterPackagePath
		planName = "acceptance-evidence-plan.json"
		label = "relay adapter package"
	}
	dir, err := resolvePackageDir(packagePath, label)
	if err != nil {
		return Options{}, err
	}
	opts.PlanPath = filepath.Join(dir, planName)
	if strings.TrimSpace(opts.PackageDir) == "" {
		opts.PackageDir = dir
	}
	return opts, nil
}

func resolvePackageDir(path, label string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("%s is required: %w", label, err)
	}
	if info.IsDir() {
		return abs, nil
	}
	return filepath.Dir(abs), nil
}

func StatusForScaffold(opts StatusOptions) (Status, error) {
	if strings.TrimSpace(opts.ScaffoldPath) == "" {
		return Status{}, fmt.Errorf("scaffold is required")
	}
	scaffoldPath, err := filepath.Abs(opts.ScaffoldPath)
	if err != nil {
		return Status{}, err
	}
	reportPath := scaffoldPath
	if info, err := os.Stat(scaffoldPath); err == nil && info.IsDir() {
		reportPath = filepath.Join(scaffoldPath, "scaffold-report.json")
	}
	content, err := os.ReadFile(reportPath)
	if err != nil {
		return Status{}, err
	}
	var scaffold Scaffold
	if err := json.Unmarshal(content, &scaffold); err != nil {
		return Status{}, err
	}
	now := opts.GeneratedAt
	if now.IsZero() {
		now = time.Now()
	}
	status := Status{
		SchemaVersion: StatusSchemaVersion,
		GeneratedAt:   now.UTC(),
		OK:            true,
		ScaffoldPath:  scaffoldPath,
		ReportPath:    reportPath,
		PlanPath:      scaffold.PlanPath,
		PlanSchema:    scaffold.PlanSchema,
		PlanKind:      scaffold.PlanKind,
		OutDir:        scaffold.OutDir,
		PackageDir:    scaffold.PackageDir,
		Commands:      scaffold.Commands,
		RequiredTotal: countRequired(scaffold.EvidenceFiles),
		RecommendedActions: []string{
			"Collect or replace every required evidence file before packaging.",
			"Run the package command only when ready_for_packaging is true.",
			"Run the matching verify command and require ok=true before making any production claim.",
		},
	}
	addCheck := func(name string, passed bool, detail string) {
		status.Checks = append(status.Checks, ScaffoldCheck{Name: name, Passed: passed, Detail: detail})
		if !passed {
			status.OK = false
		}
	}
	addCheck("scaffold_schema", scaffold.SchemaVersion == ScaffoldSchemaVersion, scaffold.SchemaVersion)
	addCheck("scaffold_ok", scaffold.OK, fmt.Sprintf("%t", scaffold.OK))
	addCheck("evidence_files_declared", len(scaffold.EvidenceFiles) > 0, fmt.Sprintf("%d", len(scaffold.EvidenceFiles)))
	addCheck("package_command_declared", len(scaffold.Commands.Package) > 0, strings.Join(scaffold.Commands.Package, " "))
	addCheck("verify_command_declared", len(scaffold.Commands.Verify) > 0, strings.Join(scaffold.Commands.Verify, " "))
	for _, file := range scaffold.EvidenceFiles {
		entry := statusForFile(file)
		if file.Required {
			if entry.Exists && entry.Ready {
				status.RequiredReady++
			}
			if !entry.Exists {
				status.MissingCount++
			}
			if entry.Empty {
				status.EmptyCount++
			}
			if entry.Placeholder {
				status.PlaceholderCount++
			}
			addCheck("required_evidence_ready:"+file.Name, entry.Ready, entry.Path)
		} else if entry.Placeholder {
			status.PlaceholderCount++
		}
		status.EvidenceFiles = append(status.EvidenceFiles, entry)
	}
	status.ReadyForPackaging = status.OK &&
		status.RequiredTotal > 0 &&
		status.RequiredReady == status.RequiredTotal &&
		status.PlaceholderCount == 0 &&
		status.MissingCount == 0 &&
		status.EmptyCount == 0
	addCheck("ready_for_packaging", status.ReadyForPackaging, fmt.Sprintf("%d/%d required ready", status.RequiredReady, status.RequiredTotal))
	if status.ReadyForPackaging {
		status.RecommendedActions = []string{
			"Run the package command from this report.",
			"Run the matching verify command and require ok=true before making any production claim.",
		}
	}
	sort.Slice(status.EvidenceFiles, func(i, j int) bool { return status.EvidenceFiles[i].Path < status.EvidenceFiles[j].Path })
	return status, nil
}

func planKind(schema string) (string, error) {
	switch schema {
	case "rdev.hosted-provider-runtime-evidence-plan.v1":
		return "hosted-provider-runtime", nil
	case "rdev.relay-adapter-acceptance-evidence-plan.v1":
		return "relay-adapter", nil
	default:
		return "", fmt.Errorf("unsupported evidence plan schema %q", schema)
	}
}

func prepareOut(dir string, force bool) error {
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("out exists and is not a directory: %s", dir)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			if !force {
				return fmt.Errorf("out must be empty or use --force: %s", dir)
			}
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(dir, 0o755)
}

func safeRelativePath(path string) bool {
	if strings.TrimSpace(path) == "" || strings.Contains(path, `\`) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator)) && !filepath.IsAbs(clean) && filepath.VolumeName(clean) == ""
}

func countRequired(files []ScaffoldFile) int {
	count := 0
	for _, file := range files {
		if file.Required {
			count++
		}
	}
	return count
}

func statusForFile(file ScaffoldFile) StatusFile {
	entry := StatusFile{
		Name:        file.Name,
		Path:        file.Path,
		Kind:        file.Kind,
		Required:    file.Required,
		Flag:        file.Flag,
		Description: file.Description,
	}
	info, err := os.Stat(file.Path)
	if err != nil || info.IsDir() {
		return entry
	}
	entry.Exists = true
	entry.SizeBytes = info.Size()
	entry.Empty = info.Size() == 0
	content, err := os.ReadFile(file.Path)
	if err == nil {
		entry.Placeholder = evidenceContentIsPlaceholder(content)
	}
	entry.Ready = entry.Exists && !entry.Empty && !entry.Placeholder
	return entry
}

func evidenceContentIsPlaceholder(content []byte) bool {
	lower := strings.ToLower(string(content))
	if strings.Contains(lower, "placeholder only - replace with real redacted evidence before packaging") ||
		strings.Contains(lower, `"replace_before_packaging": true`) ||
		strings.Contains(lower, `"replace_before_packaging":true`) {
		return true
	}
	var value any
	if err := json.Unmarshal(content, &value); err != nil {
		return false
	}
	return jsonHasBoolField(value, "placeholder", true) && jsonHasBoolField(value, "replace_before_packaging", true)
}

func jsonHasBoolField(value any, name string, expected bool) bool {
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	actual, ok := object[name].(bool)
	return ok && actual == expected
}

func writePlaceholder(outDir string, file rawPlanFile) error {
	path := filepath.Join(outDir, filepath.FromSlash(file.Path))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var content string
	if file.Kind == "json" {
		content = "{\n  \"placeholder\": true,\n  \"replace_before_packaging\": true,\n  \"evidence_name\": " + quote(file.Name) + "\n}\n"
	} else {
		content = "PLACEHOLDER ONLY - replace with real redacted evidence before packaging.\nEvidence: " + file.Name + "\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func commandsForPlan(kind string, plan rawPlan, packageDir, outDir string) ScaffoldCommands {
	replace := func(values []string) []string {
		out := make([]string, 0, len(values))
		for _, value := range values {
			switch value {
			case "<provider-package-dir>", "<relay-adapter-package-dir>":
				out = append(out, packageDir)
			case "<hosted-runtime-evidence-out>", "<relay-adapter-evidence-out>":
				out = append(out, outDir)
			default:
				out = append(out, value)
			}
		}
		return out
	}
	commands := ScaffoldCommands{
		Package: replace(plan.PackageCommand),
		Verify:  replace(plan.VerifyCommand),
	}
	if kind == "hosted-provider-runtime" {
		commands.Preflight = append([]string(nil), plan.PreflightCommands...)
	}
	if kind == "relay-adapter" {
		commands.DryRun = replace(plan.DryRunCommand)
		commands.Run = replace(plan.RunCommand)
	}
	return commands
}

func renderChecklist(scaffold Scaffold) string {
	var b strings.Builder
	b.WriteString("# Acceptance Evidence Checklist\n\n")
	b.WriteString("- Schema: `" + scaffold.PlanSchema + "`\n")
	b.WriteString("- Kind: `" + scaffold.PlanKind + "`\n")
	b.WriteString("- Ready for packaging: `false` until all required files contain real redacted evidence.\n")
	b.WriteString("- Package command: `" + strings.Join(scaffold.Commands.Package, " ") + "`\n")
	b.WriteString("- Verify command: `" + strings.Join(scaffold.Commands.Verify, " ") + "`\n\n")
	if len(scaffold.Commands.Preflight) > 0 {
		b.WriteString("## Preflight\n\n")
		for _, command := range scaffold.Commands.Preflight {
			b.WriteString("- [ ] `" + command + "`\n")
		}
		b.WriteString("\n")
	}
	if len(scaffold.Commands.DryRun) > 0 || len(scaffold.Commands.Run) > 0 {
		b.WriteString("## Runner\n\n")
		if len(scaffold.Commands.DryRun) > 0 {
			b.WriteString("- [ ] Dry run: `" + strings.Join(scaffold.Commands.DryRun, " ") + "`\n")
		}
		if len(scaffold.Commands.Run) > 0 {
			b.WriteString("- [ ] Real run: `" + strings.Join(scaffold.Commands.Run, " ") + "`\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("## Evidence Files\n\n")
	for _, file := range scaffold.EvidenceFiles {
		required := "optional"
		if file.Required {
			required = "required"
		}
		b.WriteString("- [ ] `" + file.Path + "` (" + required + ", flag `" + file.Flag + "`): " + file.Description + "\n")
	}
	b.WriteString("\n## Final Gate\n\n")
	b.WriteString("- [ ] Replace placeholders with real evidence if placeholders were created.\n")
	b.WriteString("- [ ] Run the package command.\n")
	b.WriteString("- [ ] Run the verify command and require `ok=true` before making any production claim.\n")
	return b.String()
}

func quote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
