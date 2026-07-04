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

type Options struct {
	PlanPath           string
	OutDir             string
	PackageDir         string
	CreatePlaceholders bool
	Force              bool
	GeneratedAt        time.Time
}

type Scaffold struct {
	SchemaVersion      string           `json:"schema_version"`
	GeneratedAt        time.Time        `json:"generated_at"`
	OK                 bool             `json:"ok"`
	ReadyForPackaging  bool             `json:"ready_for_packaging"`
	PlanPath           string           `json:"plan_path"`
	PlanSchema         string           `json:"plan_schema"`
	PlanKind           string           `json:"plan_kind"`
	OutDir             string           `json:"out_dir"`
	PackageDir         string           `json:"package_dir,omitempty"`
	CreatePlaceholders bool             `json:"create_placeholders"`
	EvidenceFiles      []ScaffoldFile   `json:"evidence_files"`
	Commands           ScaffoldCommands `json:"commands"`
	ChecklistPath      string           `json:"checklist_path"`
	PlanCopyPath       string           `json:"plan_copy_path"`
	ReportPath         string           `json:"report_path"`
	AgentRules         []string         `json:"agent_rules"`
	ApprovalRequired   []string         `json:"approval_required,omitempty"`
	UnsupportedClaims  []string         `json:"unsupported_claims,omitempty"`
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
	SchemaVersion     string        `json:"schema_version"`
	StorageProvider   string        `json:"storage_provider"`
	AuthProvider      string        `json:"auth_provider"`
	AdapterKind       string        `json:"adapter_kind"`
	ConnectionPathID  string        `json:"connection_path_id"`
	PackagePath       string        `json:"package_path"`
	ExternalMutation  bool          `json:"external_mutation"`
	EvidenceFiles     []rawPlanFile `json:"evidence_files"`
	PackageCommand    []string      `json:"package_command"`
	VerifyCommand     []string      `json:"verify_command"`
	PreflightCommands []string      `json:"preflight_commands"`
	DryRunCommand     []string      `json:"dry_run_command"`
	RunCommand        []string      `json:"run_command"`
	AgentRules        []string      `json:"agent_rules"`
	ApprovalRequired  []string      `json:"approval_required"`
	UnsupportedClaims []string      `json:"unsupported_claims"`
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
	if strings.TrimSpace(opts.PlanPath) == "" {
		return Scaffold{}, fmt.Errorf("plan is required")
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return Scaffold{}, fmt.Errorf("out is required")
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
		SchemaVersion:      ScaffoldSchemaVersion,
		GeneratedAt:        now.UTC(),
		OK:                 true,
		ReadyForPackaging:  false,
		PlanPath:           planPath,
		PlanSchema:         plan.SchemaVersion,
		PlanKind:           kind,
		OutDir:             outDir,
		PackageDir:         packageDir,
		CreatePlaceholders: opts.CreatePlaceholders,
		ChecklistPath:      filepath.Join(outDir, "AGENT_CHECKLIST.md"),
		PlanCopyPath:       filepath.Join(outDir, filepath.Base(planPath)),
		ReportPath:         filepath.Join(outDir, "scaffold-report.json"),
		AgentRules:         append([]string(nil), plan.AgentRules...),
		ApprovalRequired:   append([]string(nil), plan.ApprovalRequired...),
		UnsupportedClaims:  append([]string(nil), plan.UnsupportedClaims...),
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
