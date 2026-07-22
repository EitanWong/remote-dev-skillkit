package acceptance

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/bootstrapcmd/windowsentry"
)

const WindowsTemporaryPlanSchemaVersion = "rdev.acceptance.windows-temporary-plan.v1"

const maxWindowsTemporaryHandoffBytes int64 = 1 << 20

// The closed ZIP remains subject to the 1 MiB delivery gate. These separate
// limits also bound decompression work while allowing the signed PE bootstrap
// to be larger than its compressed representation.
const (
	maxWindowsTemporaryHandoffEntryBytes    int64 = 8 << 20
	maxWindowsTemporaryHandoffExpandedBytes int64 = 16 << 20
)

const (
	windowsTemporaryColdLayeredRunEvidence = "cold-layered-run.json from a real clean Windows cold run with from_cache=false."
	windowsTemporaryWarmLayeredRunEvidence = "warm-layered-run.json from the immediate cached Windows rerun with from_cache=true."
)

func windowsTemporaryRequiredEvidence() []string {
	return []string{
		"Measured Windows-ConnectionEntry.zip size and SHA-256 from the delivered handoff.",
		"Visible launcher attempt order and exactly one rdev-bootstrap core-start transition.",
		"Signed bootstrap and layered core verification output.",
		"Session registration, route reselection, task, revoke, and cancellation audit events.",
		"No-persistence inspection output for services, scheduled tasks, Run keys, startup folders, and firewall rules.",
		windowsTemporaryColdLayeredRunEvidence,
		windowsTemporaryWarmLayeredRunEvidence,
	}
}

type WindowsTemporaryOptions struct {
	OutDir             string
	HandoffArchivePath string
	Force              bool
	Now                time.Time
}

type WindowsTemporaryPlan struct {
	SchemaVersion            string                     `json:"schema_version"`
	GeneratedAt              time.Time                  `json:"generated_at"`
	Platform                 string                     `json:"platform"`
	OutDir                   string                     `json:"-"`
	HandoffArchivePath       string                     `json:"handoff_archive_path"`
	HandoffArchiveSHA256     string                     `json:"handoff_archive_sha256"`
	HandoffArchiveSizeBytes  int64                      `json:"handoff_archive_size_bytes"`
	PowerShellLauncher       string                     `json:"powershell_launcher"`
	CommandLauncher          string                     `json:"command_launcher"`
	PreferredLauncher        string                     `json:"preferred_launcher"`
	FallbackOrder            []string                   `json:"fallback_order"`
	BootstrapCommand         string                     `json:"bootstrap_command"`
	ArchiveRecoveryAutomatic bool                       `json:"archive_recovery_automatic"`
	Checks                   []Check                    `json:"checks"`
	Commands                 []WindowsAcceptanceCommand `json:"commands"`
	NoPersistenceChecks      []WindowsAcceptanceCommand `json:"no_persistence_checks"`
	DenialProbes             []WindowsDenialProbe       `json:"denial_probes"`
	RecommendedActions       []string                   `json:"recommended_actions"`
	RequiredEvidence         []string                   `json:"required_evidence"`
}

type WindowsAcceptanceCommand struct {
	Name    string   `json:"name"`
	Purpose string   `json:"purpose"`
	Shell   string   `json:"shell"`
	Argv    []string `json:"argv,omitempty"`
	Manual  bool     `json:"manual"`
}

type WindowsDenialProbe struct {
	Operation        string `json:"operation"`
	ExpectedArtifact string `json:"expected_artifact"`
	Purpose          string `json:"purpose"`
}

func RunWindowsTemporaryPlan(opts WindowsTemporaryOptions) (WindowsTemporaryPlan, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return WindowsTemporaryPlan{}, fmt.Errorf("out directory is required")
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return WindowsTemporaryPlan{}, err
	}
	if err := prepareAcceptanceOut(outDir); err != nil {
		return WindowsTemporaryPlan{}, err
	}
	sourceArchive, archiveContent, archiveSHA256, archiveSize, err := inspectWindowsLayeredAcceptanceArchive(opts.HandoffArchivePath)
	if err != nil {
		return WindowsTemporaryPlan{}, err
	}
	archivePath := filepath.Join(outDir, "Windows-ConnectionEntry.zip")
	if err := writeAcceptanceFile(archivePath, archiveContent, opts.Force); err != nil {
		return WindowsTemporaryPlan{}, err
	}
	plan := WindowsTemporaryPlan{
		SchemaVersion:            WindowsTemporaryPlanSchemaVersion,
		GeneratedAt:              now.UTC(),
		Platform:                 "windows/amd64",
		OutDir:                   outDir,
		HandoffArchivePath:       filepath.Base(archivePath),
		HandoffArchiveSHA256:     archiveSHA256,
		HandoffArchiveSizeBytes:  archiveSize,
		PowerShellLauncher:       "Start-ConnectionEntry.ps1",
		CommandLauncher:          "Start-ConnectionEntry.cmd",
		PreferredLauncher:        "powershell",
		FallbackOrder:            []string{"powershell", "powershell-bypass", "cmd"},
		BootstrapCommand:         "rdev-bootstrap layered-run",
		ArchiveRecoveryAutomatic: false,
		Commands:                 windowsTemporaryCommands(),
		NoPersistenceChecks:      windowsNoPersistenceChecks(),
		DenialProbes:             windowsDenialProbes(),
		RecommendedActions: []string{
			"Verify the delivered archive digest and inspect both visible launchers before the Windows run.",
			"Extract the private handoff on a clean Windows 10/11 host and start with the visible PowerShell launcher.",
			"Accept only the scoped temporary endpoint expected by this session.",
			"Run bounded diagnostic and repair tasks, then collect evidence and audit exports.",
			"Revoke the temporary host and run every no-persistence check before publishing the transcript.",
		},
		RequiredEvidence: windowsTemporaryRequiredEvidence(),
	}
	plan.Checks = windowsTemporaryChecks(plan, sourceArchive)
	if err := writeWindowsTemporaryPlan(filepath.Join(outDir, "windows-temporary-plan.json"), plan); err != nil {
		return WindowsTemporaryPlan{}, err
	}
	return plan, nil
}

func windowsTemporaryChecks(plan WindowsTemporaryPlan, sourceArchive string) []Check {
	return []Check{
		{Name: "handoff_archive_staged", Passed: sourceArchive != "", Detail: filepath.Base(plan.HandoffArchivePath)},
		{Name: "handoff_archive_sha256", Passed: isHexSHA256(plan.HandoffArchiveSHA256), Detail: plan.HandoffArchiveSHA256},
		{Name: "handoff_archive_size", Passed: plan.HandoffArchiveSizeBytes > 0 && plan.HandoffArchiveSizeBytes <= maxWindowsTemporaryHandoffBytes, Detail: fmt.Sprintf("%d", plan.HandoffArchiveSizeBytes)},
		{Name: "powershell_preferred", Passed: plan.PreferredLauncher == "powershell" && plan.PowerShellLauncher == "Start-ConnectionEntry.ps1", Detail: plan.PreferredLauncher},
		{Name: "fallback_order", Passed: slices.Equal(plan.FallbackOrder, []string{"powershell", "powershell-bypass", "cmd"}), Detail: strings.Join(plan.FallbackOrder, ",")},
		{Name: "bootstrap_only", Passed: plan.BootstrapCommand == "rdev-bootstrap layered-run", Detail: plan.BootstrapCommand},
		{Name: "archive_recovery_not_automatic", Passed: !plan.ArchiveRecoveryAutomatic},
		{Name: "no_persistence_checks_present", Passed: len(plan.NoPersistenceChecks) >= 5},
		{Name: "denial_probes_present", Passed: len(plan.DenialProbes) >= 4},
	}
}

func windowsTemporaryCommands() []WindowsAcceptanceCommand {
	return []WindowsAcceptanceCommand{
		{
			Name:    "review_handoff",
			Purpose: "Verify the delivered ZIP digest and inspect both visible launchers before execution.",
			Shell:   "Get-FileHash -Algorithm SHA256 -LiteralPath '.\\Windows-ConnectionEntry.zip'",
			Argv:    []string{"powershell.exe", "-NoProfile", "-Command", "Get-FileHash -Algorithm SHA256 -LiteralPath '.\\Windows-ConnectionEntry.zip'"},
			Manual:  true,
		},
		{
			Name:    "run_foreground_temporary_host",
			Purpose: "Extract the private handoff and run its preferred visible PowerShell entry.",
			Shell:   "powershell.exe -NoProfile -File '.\\Start-ConnectionEntry.ps1'",
			Argv:    []string{"powershell.exe", "-NoProfile", "-File", ".\\Start-ConnectionEntry.ps1"},
			Manual:  true,
		},
		{
			Name:    "start_transcript",
			Purpose: "Capture a local transcript before running the launcher on the Windows host.",
			Shell:   "Start-Transcript -Path (Join-Path $env:TEMP 'rdev-windows-temporary-transcript.txt')",
			Manual:  true,
		},
		{
			Name:    "stop_transcript",
			Purpose: "Stop the transcript after the host exits or is revoked.",
			Shell:   "Stop-Transcript",
			Manual:  true,
		},
	}
}

func inspectWindowsLayeredAcceptanceArchive(path string) (string, []byte, string, int64, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil, "", 0, fmt.Errorf("Windows layered handoff archive is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", nil, "", 0, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", nil, "", 0, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxWindowsTemporaryHandoffBytes {
		return "", nil, "", 0, fmt.Errorf("Windows layered handoff archive must be a regular file at or below %d bytes", maxWindowsTemporaryHandoffBytes)
	}
	reader, err := zip.OpenReader(abs)
	if err != nil {
		return "", nil, "", 0, err
	}
	defer reader.Close()
	required := map[string]bool{
		"Start-ConnectionEntry.ps1":            false,
		"Start-ConnectionEntry.cmd":            false,
		"rdev-bootstrap.exe":                   false,
		"rdev-bootstrap.exe.rdev-release.json": false,
		"rdev-bootstrap.exe.sha256":            false,
		"windows-layered-verification.json":    false,
		"ARCHIVE-RECOVERY.txt":                 false,
	}
	seen := make(map[string]bool, len(reader.File))
	var expandedSize int64
	for _, file := range reader.File {
		if file.Name != filepath.Base(file.Name) || seen[strings.ToLower(file.Name)] {
			return "", nil, "", 0, fmt.Errorf("Windows layered handoff contains an unsafe or duplicate entry")
		}
		if !file.Mode().IsRegular() || file.UncompressedSize64 == 0 || file.UncompressedSize64 > uint64(maxWindowsTemporaryHandoffEntryBytes) {
			return "", nil, "", 0, fmt.Errorf("Windows layered handoff contains an entry above the uncompressed size limit")
		}
		if file.UncompressedSize64 > uint64(maxWindowsTemporaryHandoffExpandedBytes)-uint64(expandedSize) {
			return "", nil, "", 0, fmt.Errorf("Windows layered handoff exceeds the uncompressed size limit")
		}
		expandedSize += int64(file.UncompressedSize64)
		seen[strings.ToLower(file.Name)] = true
		if _, ok := required[file.Name]; !ok {
			return "", nil, "", 0, fmt.Errorf("Windows layered handoff contains unexpected entry %q", file.Name)
		}
		required[file.Name] = true
		entry, err := file.Open()
		if err != nil {
			return "", nil, "", 0, err
		}
		content, readErr := io.ReadAll(io.LimitReader(entry, maxWindowsTemporaryHandoffEntryBytes+1))
		closeErr := entry.Close()
		if readErr != nil || closeErr != nil || uint64(len(content)) != file.UncompressedSize64 {
			return "", nil, "", 0, fmt.Errorf("read and verify Windows layered handoff entry")
		}
		if file.Name != "Start-ConnectionEntry.ps1" && file.Name != "Start-ConnectionEntry.cmd" {
			continue
		}
		lower := strings.ToLower(string(content))
		if !strings.Contains(lower, "rdev-bootstrap") {
			return "", nil, "", 0, fmt.Errorf("Windows layered launcher %q does not use rdev-bootstrap", file.Name)
		}
		for _, forbidden := range []string{"rdev host serve", "rdev-host.exe", "rdev-bootstrap upgrade", "get-command rdev"} {
			if strings.Contains(lower, forbidden) {
				return "", nil, "", 0, fmt.Errorf("Windows layered launcher %q contains a legacy helper path", file.Name)
			}
		}
	}
	for name, present := range required {
		if !present {
			return "", nil, "", 0, fmt.Errorf("Windows layered handoff is missing %q", name)
		}
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return "", nil, "", 0, err
	}
	digest := sha256.Sum256(content)
	return abs, content, hex.EncodeToString(digest[:]), int64(len(content)), nil
}

func windowsNoPersistenceChecks() []WindowsAcceptanceCommand {
	return []WindowsAcceptanceCommand{
		{
			Name:    "services",
			Purpose: "Confirm temporary mode did not install a Windows Service.",
			Shell:   "Get-Service | Where-Object { $_.Name -match '(^|[-_.])rdev($|[-_.])|remote[- ]?dev' -or $_.DisplayName -match '(^|[-_. ])rdev($|[-_. ])|remote[- ]?dev' } | Select-Object Name, Status, StartType, DisplayName",
			Manual:  true,
		},
		{
			Name:    "scheduled_tasks",
			Purpose: "Confirm temporary mode did not create scheduled tasks.",
			Shell:   "Get-ScheduledTask | Where-Object { $_.TaskName -match '(^|[-_.])rdev($|[-_.])|remote[- ]?dev' -or $_.TaskPath -match '(^|[-_.\\])rdev($|[-_.\\])|remote[- ]?dev' } | Select-Object TaskPath, TaskName, State",
			Manual:  true,
		},
		{
			Name:    "hkcu_run_key",
			Purpose: "Confirm temporary mode did not add current-user Run-key autorun entries.",
			Shell:   "Get-ItemProperty -Path 'HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Run' | Select-Object *rdev*, *RemoteDev*",
			Manual:  true,
		},
		{
			Name:    "hklm_run_key",
			Purpose: "Confirm temporary mode did not add machine Run-key autorun entries.",
			Shell:   "Get-ItemProperty -Path 'HKLM:\\Software\\Microsoft\\Windows\\CurrentVersion\\Run' | Select-Object *rdev*, *RemoteDev*",
			Manual:  true,
		},
		{
			Name:    "startup_folders",
			Purpose: "Confirm temporary mode did not add startup-folder shortcuts or scripts.",
			Shell:   "Get-ChildItem \"$env:APPDATA\\Microsoft\\Windows\\Start Menu\\Programs\\Startup\", \"$env:ProgramData\\Microsoft\\Windows\\Start Menu\\Programs\\StartUp\" -ErrorAction SilentlyContinue | Where-Object { $_.Name -match '(^|[-_.])rdev($|[-_.])|remote[- ]?dev' }",
			Manual:  true,
		},
		{
			Name:    "firewall_rules",
			Purpose: "Confirm temporary mode did not add firewall rules.",
			Shell:   "Get-NetFirewallRule -ErrorAction SilentlyContinue | Where-Object { $_.DisplayName -match '(^|[-_. ])rdev($|[-_. ])|remote[- ]?dev' -or $_.Name -match '(^|[-_.])rdev($|[-_.])|remote[- ]?dev' } | Select-Object DisplayName, Enabled, Direction, Action",
			Manual:  true,
		},
	}
}

func windowsDenialProbes() []WindowsDenialProbe {
	return []WindowsDenialProbe{
		{Operation: "package.install", ExpectedArtifact: "rdev.host-denial.v1", Purpose: "Package installation is denied before side effects in temporary mode."},
		{Operation: "elevation.request", ExpectedArtifact: "rdev.host-denial.v1", Purpose: "Privilege elevation is denied before side effects in temporary mode."},
		{Operation: "service.manage", ExpectedArtifact: "rdev.host-denial.v1", Purpose: "Service mutation is denied before side effects in temporary mode."},
		{Operation: "gui.control", ExpectedArtifact: "rdev.host-denial.v1", Purpose: "GUI control is denied unless a visible session capability explicitly allows it."},
		{Operation: "credential.change", ExpectedArtifact: "rdev.host-denial.v1", Purpose: "Credential access or mutation is denied before side effects in temporary mode."},
	}
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func writeWindowsTemporaryPlan(path string, plan WindowsTemporaryPlan) error {
	content, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return err
	}
	return windowsentry.ProtectPrivatePath(path, false)
}

func fileSHA256(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}

func isHexSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
