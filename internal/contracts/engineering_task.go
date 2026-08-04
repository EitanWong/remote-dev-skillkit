package contracts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
)

const EngineeringTaskSchemaVersion = "rdev.engineering-task.v1"

const (
	EngineeringIsolationGitWorktree   = "git-worktree"
	EngineeringIsolationWorkspaceLock = "workspace-lock"

	EngineeringDirtyPolicyPreserve     = "preserve"
	EngineeringDirtyPolicyRequireClean = "require-clean"

	EngineeringWorktreeCleanupPreserve = "preserve"
	EngineeringWorktreeCleanupRemove   = "remove"
	EngineeringWorktreeCleanupRollback = "rollback"

	EngineeringNetworkDefaultDeny = "default-deny"
	EngineeringNetworkDeny        = "deny"
	EngineeringNetworkAllow       = "allow"
)

const (
	maxEngineeringTaskDurationSeconds = 24 * 60 * 60
	maxEngineeringTaskOutputBytes     = 16 * 1024 * 1024
	maxEngineeringTaskAttempts        = 10
)

var engineeringBaseSHA = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

// EngineeringTask is the stable, typed task contract used inside the existing
// rdev.sessions.task surface. It is deliberately independent of a particular
// adapter: the selected adapter remains the outer session-task routing field.
type EngineeringTask struct {
	SchemaVersion        string                  `json:"schema_version"`
	Goal                 string                  `json:"goal"`
	Workspace            EngineeringWorkspace    `json:"workspace"`
	Plan                 []string                `json:"plan"`
	Acceptance           []string                `json:"acceptance"`
	Verification         EngineeringVerification `json:"verification"`
	Limits               EngineeringTaskLimits   `json:"limits"`
	NetworkPolicy        string                  `json:"network_policy"`
	RequiredCapabilities []string                `json:"required_capabilities"`
	InterruptsRequired   []string                `json:"interrupts_required,omitempty"`
	IdempotencyKey       string                  `json:"idempotency_key"`
}

type EngineeringWorkspace struct {
	Root        string   `json:"root"`
	Handle      string   `json:"handle,omitempty"`
	BaseSHA     string   `json:"base_sha,omitempty"`
	Branch      string   `json:"branch,omitempty"`
	Isolation   string   `json:"isolation"`
	DirtyPolicy string   `json:"dirty_policy"`
	Cleanup     string   `json:"cleanup,omitempty"`
	ReadScope   []string `json:"read_scope"`
	WriteScope  []string `json:"write_scope"`
}

type EngineeringVerification struct {
	Commands      [][]string `json:"commands"`
	AllowCommands []string   `json:"allow_commands"`
}

type EngineeringTaskLimits struct {
	MaxDurationSeconds int `json:"max_duration_seconds"`
	MaxOutputBytes     int `json:"max_output_bytes"`
	MaxAttempts        int `json:"max_attempts"`
}

// AdapterTaskProfile is machine-readable metadata for an existing built-in
// adapter. It describes the outer routing adapter, not a new MCP tool.
type AdapterTaskProfile struct {
	Adapter              string   `json:"adapter"`
	SchemaVersion        string   `json:"schema_version"`
	RequiredCapabilities []string `json:"required_capabilities"`
	// WorkspaceRootRequired declares the host-runner invariant that every
	// adapter task payload must carry an absolute workspace_root. It exists so
	// Agents can validate a payload against the contract before submitting,
	// instead of discovering the requirement through denial round trips.
	WorkspaceRootRequired bool           `json:"workspace_root_required"`
	PayloadExample        map[string]any `json:"payload_example"`
}

// DecodeEngineeringTask rejects unknown fields before normalizing and
// validating the contract. Callers can safely accept generic MCP JSON without
// returning to arbitrary task payloads.
func DecodeEngineeringTask(raw any) (EngineeringTask, error) {
	if raw == nil {
		return EngineeringTask{}, fmt.Errorf("engineering_task is required")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return EngineeringTask{}, fmt.Errorf("encode engineering_task: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var task EngineeringTask
	if err := decoder.Decode(&task); err != nil {
		return EngineeringTask{}, fmt.Errorf("decode engineering_task: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return EngineeringTask{}, fmt.Errorf("decode engineering_task: multiple JSON values")
		}
		return EngineeringTask{}, fmt.Errorf("decode engineering_task: %w", err)
	}
	task.normalize()
	if err := task.Validate(); err != nil {
		return EngineeringTask{}, err
	}
	return task, nil
}

func (t *EngineeringTask) normalize() {
	t.SchemaVersion = strings.TrimSpace(t.SchemaVersion)
	t.Goal = strings.TrimSpace(t.Goal)
	t.Workspace.Root = strings.TrimSpace(t.Workspace.Root)
	t.Workspace.Handle = strings.TrimSpace(t.Workspace.Handle)
	t.Workspace.BaseSHA = strings.TrimSpace(t.Workspace.BaseSHA)
	t.Workspace.Branch = strings.TrimSpace(t.Workspace.Branch)
	t.Workspace.Isolation = strings.TrimSpace(t.Workspace.Isolation)
	t.Workspace.DirtyPolicy = strings.TrimSpace(t.Workspace.DirtyPolicy)
	t.Workspace.Cleanup = strings.TrimSpace(t.Workspace.Cleanup)
	if t.Workspace.Cleanup == "" {
		t.Workspace.Cleanup = EngineeringWorktreeCleanupPreserve
	}
	t.NetworkPolicy = strings.TrimSpace(t.NetworkPolicy)
	t.IdempotencyKey = strings.TrimSpace(t.IdempotencyKey)
	t.Plan = normalizeStrings(t.Plan)
	t.Acceptance = normalizeStrings(t.Acceptance)
	t.Workspace.ReadScope = normalizeStrings(t.Workspace.ReadScope)
	t.Workspace.WriteScope = normalizeStrings(t.Workspace.WriteScope)
	t.Verification.AllowCommands = normalizeStrings(t.Verification.AllowCommands)
	t.RequiredCapabilities = normalizeStrings(t.RequiredCapabilities)
	t.InterruptsRequired = normalizeStrings(t.InterruptsRequired)
	for i := range t.Verification.Commands {
		t.Verification.Commands[i] = normalizeStrings(t.Verification.Commands[i])
	}
}

func normalizeStrings(values []string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func (t EngineeringTask) Validate() error {
	if t.SchemaVersion != EngineeringTaskSchemaVersion {
		return fmt.Errorf("engineering_task schema_version must be %q", EngineeringTaskSchemaVersion)
	}
	if t.Goal == "" {
		return fmt.Errorf("engineering_task goal is required")
	}
	if t.Workspace.Root == "" {
		return fmt.Errorf("engineering_task workspace.root is required for host-local execution")
	}
	if t.Workspace.BaseSHA != "" && !engineeringBaseSHA.MatchString(t.Workspace.BaseSHA) {
		return fmt.Errorf("engineering_task workspace.base_sha must be a 7-64 character hexadecimal Git SHA")
	}
	switch t.Workspace.Isolation {
	case EngineeringIsolationGitWorktree:
		if t.Workspace.BaseSHA == "" {
			return fmt.Errorf("engineering_task workspace.base_sha is required for git-worktree isolation")
		}
	case EngineeringIsolationWorkspaceLock:
	default:
		return fmt.Errorf("engineering_task workspace.isolation must be %q or %q", EngineeringIsolationGitWorktree, EngineeringIsolationWorkspaceLock)
	}
	switch t.Workspace.DirtyPolicy {
	case EngineeringDirtyPolicyPreserve, EngineeringDirtyPolicyRequireClean:
	default:
		return fmt.Errorf("engineering_task workspace.dirty_policy must be %q or %q", EngineeringDirtyPolicyPreserve, EngineeringDirtyPolicyRequireClean)
	}
	switch t.Workspace.Cleanup {
	case EngineeringWorktreeCleanupPreserve, EngineeringWorktreeCleanupRemove, EngineeringWorktreeCleanupRollback:
	default:
		return fmt.Errorf("engineering_task workspace.cleanup must be %q, %q, or %q", EngineeringWorktreeCleanupPreserve, EngineeringWorktreeCleanupRemove, EngineeringWorktreeCleanupRollback)
	}
	if err := validateScopes("read_scope", t.Workspace.Root, t.Workspace.ReadScope, true); err != nil {
		return err
	}
	if err := validateScopes("write_scope", t.Workspace.Root, t.Workspace.WriteScope, false); err != nil {
		return err
	}
	if len(t.Plan) == 0 {
		return fmt.Errorf("engineering_task plan is required")
	}
	if len(t.Acceptance) == 0 {
		return fmt.Errorf("engineering_task acceptance is required")
	}
	if len(t.Verification.Commands) == 0 {
		return fmt.Errorf("engineering_task verification.commands is required")
	}
	for _, command := range t.Verification.Commands {
		if len(command) == 0 || command[0] == "" {
			return fmt.Errorf("engineering_task verification command is required")
		}
	}
	if t.Limits.MaxDurationSeconds <= 0 || t.Limits.MaxDurationSeconds > maxEngineeringTaskDurationSeconds {
		return fmt.Errorf("engineering_task limits.max_duration_seconds must be between 1 and %d", maxEngineeringTaskDurationSeconds)
	}
	if t.Limits.MaxOutputBytes <= 0 || t.Limits.MaxOutputBytes > maxEngineeringTaskOutputBytes {
		return fmt.Errorf("engineering_task limits.max_output_bytes must be between 1 and %d", maxEngineeringTaskOutputBytes)
	}
	if t.Limits.MaxAttempts <= 0 || t.Limits.MaxAttempts > maxEngineeringTaskAttempts {
		return fmt.Errorf("engineering_task limits.max_attempts must be between 1 and %d", maxEngineeringTaskAttempts)
	}
	switch t.NetworkPolicy {
	case EngineeringNetworkDefaultDeny, EngineeringNetworkDeny, EngineeringNetworkAllow:
	default:
		return fmt.Errorf("engineering_task network_policy must be %q, %q, or %q", EngineeringNetworkDefaultDeny, EngineeringNetworkDeny, EngineeringNetworkAllow)
	}
	if err := validateUniqueStrings("required_capabilities", t.RequiredCapabilities, true); err != nil {
		return err
	}
	if err := validateUniqueStrings("interrupts_required", t.InterruptsRequired, false); err != nil {
		return err
	}
	if t.IdempotencyKey == "" {
		return fmt.Errorf("engineering_task idempotency_key is required")
	}
	if len(t.IdempotencyKey) > 256 {
		return fmt.Errorf("engineering_task idempotency_key exceeds 256 characters")
	}
	return nil
}

func (t EngineeringTask) ValidateForAdapter(adapter string) error {
	if err := t.Validate(); err != nil {
		return err
	}
	profile, ok := adapterTaskProfile(adapter)
	if !ok {
		return fmt.Errorf("engineering_task adapter %q is not supported", adapter)
	}
	for _, capability := range profile.RequiredCapabilities {
		if !containsString(t.RequiredCapabilities, capability) {
			return fmt.Errorf("engineering_task adapter %q requires capability %q", adapter, capability)
		}
	}
	switch adapter {
	case "shell", "powershell", "codex", "claude-code", "acpx":
		if len(t.Workspace.WriteScope) == 0 {
			return fmt.Errorf("engineering_task workspace.write_scope is required for %s", adapter)
		}
	}
	return nil
}

func validateScopes(name, root string, values []string, required bool) error {
	if required && len(values) == 0 {
		return fmt.Errorf("engineering_task workspace.%s is required", name)
	}
	if err := validateUniqueStrings("workspace."+name, values, false); err != nil {
		return err
	}
	for _, value := range values {
		if filepath.IsAbs(value) {
			rel, err := filepath.Rel(root, value)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("engineering_task workspace.%s path %q escapes workspace.root", name, value)
			}
			continue
		}
		cleaned := filepath.Clean(value)
		if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return fmt.Errorf("engineering_task workspace.%s path %q escapes workspace.root", name, value)
		}
	}
	return nil
}

func validateUniqueStrings(name string, values []string, required bool) error {
	if required && len(values) == 0 {
		return fmt.Errorf("engineering_task %s is required", name)
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" {
			return fmt.Errorf("engineering_task %s contains an empty value", name)
		}
		if seen[value] {
			return fmt.Errorf("engineering_task %s contains duplicate value %q", name, value)
		}
		seen[value] = true
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// TaskPayload makes the typed contract available to the existing host task
// envelope while retaining the adapter-specific payload as a narrow extension.
func (t EngineeringTask) TaskPayload(extra map[string]any) map[string]any {
	out := make(map[string]any, len(extra)+12)
	for key, value := range extra {
		out[key] = value
	}
	out["engineering_task"] = t
	out["workspace_root"] = t.Workspace.Root
	out["base_sha"] = t.Workspace.BaseSHA
	out["branch"] = t.Workspace.Branch
	out["isolation"] = t.Workspace.Isolation
	out["dirty_policy"] = t.Workspace.DirtyPolicy
	out["worktree_cleanup"] = t.Workspace.Cleanup
	out["read_scope"] = append([]string(nil), t.Workspace.ReadScope...)
	out["write_scope"] = append([]string(nil), t.Workspace.WriteScope...)
	out["verification_commands"] = cloneCommandMatrix(t.Verification.Commands)
	out["allow_verification_commands"] = append([]string(nil), t.Verification.AllowCommands...)
	out["max_attempts"] = t.Limits.MaxAttempts
	out["interrupts_required"] = append([]string(nil), t.InterruptsRequired...)
	if strings.TrimSpace(stringValue(out["prompt"])) == "" {
		out["prompt"] = t.Goal
	}
	return out
}

func (t EngineeringTask) TaskLimits() map[string]any {
	return map[string]any{
		"max_duration_seconds": t.Limits.MaxDurationSeconds,
		"max_output_bytes":     t.Limits.MaxOutputBytes,
		"max_attempts":         t.Limits.MaxAttempts,
		"network":              t.NetworkPolicy,
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func cloneCommandMatrix(values [][]string) [][]string {
	out := make([][]string, 0, len(values))
	for _, value := range values {
		out = append(out, append([]string(nil), value...))
	}
	return out
}

func AdapterTaskProfiles() []AdapterTaskProfile {
	profiles := []AdapterTaskProfile{
		{
			Adapter:               "shell",
			SchemaVersion:         "rdev.shell-result.v1",
			RequiredCapabilities:  []string{"shell.user"},
			WorkspaceRootRequired: true,
			PayloadExample:        map[string]any{"argv": []string{"go", "test", "./..."}, "allow_commands": []string{"go"}, "workspace_root": "C:\\workspace\\repo"},
		},
		{
			Adapter:               "powershell",
			SchemaVersion:         "rdev.powershell-result.v1",
			RequiredCapabilities:  []string{"powershell.user"},
			WorkspaceRootRequired: true,
			PayloadExample:        map[string]any{"command": "dotnet test", "allow_commands": []string{"powershell.exe"}, "powershell_command": "powershell.exe", "workspace_root": "C:\\workspace\\repo"},
		},
		{
			Adapter:               "codex",
			SchemaVersion:         "rdev.codex-result.v1",
			RequiredCapabilities:  []string{"codex.run", "git.diff"},
			WorkspaceRootRequired: true,
			PayloadExample:        map[string]any{"prompt": "Implement the accepted change.", "verification_commands": [][]string{{"go", "test", "./..."}}, "allow_verification_commands": []string{"go"}, "workspace_root": "C:\\workspace\\repo"},
		},
		{
			Adapter:               "claude-code",
			SchemaVersion:         "rdev.claude-code-result.v1",
			RequiredCapabilities:  []string{"claude-code.run", "git.diff"},
			WorkspaceRootRequired: true,
			PayloadExample:        map[string]any{"prompt": "Implement the accepted change.", "verification_commands": [][]string{{"go", "test", "./..."}}, "allow_verification_commands": []string{"go"}, "workspace_root": "C:\\workspace\\repo"},
		},
		{
			Adapter:               "acpx",
			SchemaVersion:         "rdev.acpx-result.v1",
			RequiredCapabilities:  []string{"acpx.run", "git.diff"},
			WorkspaceRootRequired: true,
			PayloadExample:        map[string]any{"prompt": "Implement the accepted change.", "verification_commands": [][]string{{"go", "test", "./..."}}, "allow_verification_commands": []string{"go"}, "workspace_root": "C:\\workspace\\repo"},
		},
		{
			Adapter:               "file",
			SchemaVersion:         "rdev.file-result.v1",
			RequiredCapabilities:  []string{"file.transfer.read"},
			WorkspaceRootRequired: true,
			PayloadExample:        map[string]any{"action": "read", "path": "README.md", "chunk_bytes": 4096, "workspace_root": "C:\\workspace\\repo"},
		},
		{
			Adapter:               "desktop",
			SchemaVersion:         "rdev.desktop-result.v1",
			RequiredCapabilities:  []string{"window.inspect"},
			WorkspaceRootRequired: true,
			PayloadExample:        map[string]any{"action": "window.inspect", "workspace_root": "C:\\workspace\\repo"},
		},
		{
			Adapter:               "host-update",
			SchemaVersion:         "rdev.host-update-result.v1",
			RequiredCapabilities:  []string{"host.update"},
			WorkspaceRootRequired: false,
			PayloadExample:        map[string]any{"expected_sha256": ""},
		},
	}
	out := make([]AdapterTaskProfile, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, AdapterTaskProfile{
			Adapter:               profile.Adapter,
			SchemaVersion:         profile.SchemaVersion,
			RequiredCapabilities:  append([]string(nil), profile.RequiredCapabilities...),
			WorkspaceRootRequired: profile.WorkspaceRootRequired,
			PayloadExample:        cloneAnyMap(profile.PayloadExample),
		})
	}
	return out
}

func adapterTaskProfile(adapter string) (AdapterTaskProfile, bool) {
	for _, profile := range AdapterTaskProfiles() {
		if profile.Adapter == adapter {
			return profile, true
		}
	}
	return AdapterTaskProfile{}, false
}

func cloneAnyMap(source map[string]any) map[string]any {
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

// EngineeringTaskSchema is embedded into the live MCP contract so clients can
// submit a typed task without guessing adapter payload or execution limits.
func EngineeringTaskSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required": []string{
			"schema_version", "goal", "workspace", "plan", "acceptance", "verification", "limits",
			"network_policy", "required_capabilities", "idempotency_key",
		},
		"properties": map[string]any{
			"schema_version": map[string]any{"type": "string", "const": EngineeringTaskSchemaVersion},
			"goal":           nonEmptyStringSchema(),
			"workspace": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"root", "isolation", "dirty_policy", "read_scope", "write_scope"},
				"properties": map[string]any{
					"root":         nonEmptyStringSchema(),
					"handle":       nonEmptyStringSchema(),
					"base_sha":     nonEmptyStringSchema(),
					"branch":       nonEmptyStringSchema(),
					"isolation":    stringEnumSchema(EngineeringIsolationGitWorktree, EngineeringIsolationWorkspaceLock),
					"dirty_policy": stringEnumSchema(EngineeringDirtyPolicyPreserve, EngineeringDirtyPolicyRequireClean),
					"cleanup":      stringEnumSchema(EngineeringWorktreeCleanupPreserve, EngineeringWorktreeCleanupRemove, EngineeringWorktreeCleanupRollback),
					"read_scope":   stringArraySchema(),
					"write_scope":  stringArraySchema(),
				},
			},
			"plan":       stringArraySchema(),
			"acceptance": stringArraySchema(),
			"verification": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"commands", "allow_commands"},
				"properties": map[string]any{
					"commands":       map[string]any{"type": "array", "items": stringArraySchema()},
					"allow_commands": stringArraySchema(),
				},
			},
			"limits": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"max_duration_seconds", "max_output_bytes", "max_attempts"},
				"properties": map[string]any{
					"max_duration_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": maxEngineeringTaskDurationSeconds},
					"max_output_bytes":     map[string]any{"type": "integer", "minimum": 1, "maximum": maxEngineeringTaskOutputBytes},
					"max_attempts":         map[string]any{"type": "integer", "minimum": 1, "maximum": maxEngineeringTaskAttempts},
				},
			},
			"network_policy":        stringEnumSchema(EngineeringNetworkDefaultDeny, EngineeringNetworkDeny, EngineeringNetworkAllow),
			"required_capabilities": stringArraySchema(),
			"interrupts_required":   stringArraySchema(),
			"idempotency_key":       nonEmptyStringSchema(),
		},
		"x-rdev-adapter-profiles": AdapterTaskProfiles(),
	}
}

func nonEmptyStringSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 1}
}

func stringArraySchema() map[string]any {
	return map[string]any{"type": "array", "items": nonEmptyStringSchema()}
}

func stringEnumSchema(values ...string) map[string]any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, value)
	}
	return map[string]any{"type": "string", "enum": items}
}
