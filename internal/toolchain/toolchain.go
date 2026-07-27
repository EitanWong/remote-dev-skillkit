package toolchain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
)

const (
	ResultSchemaVersion         = "rdev.toolchain-result.v1"
	RuntimeProfileSchemaVersion = "rdev.agent-runtime-profile.v1"

	StatusPlanned   = "planned"
	StatusInstalled = "installed"
	StatusFailed    = "failed"
)

type Options struct {
	// Root defaults to ~/.rdev/toolchains. It is intentionally user-scoped.
	Root string
	// NPMCommand defaults to the npm found on PATH. It may be injected by tests.
	NPMCommand string
	// FindNPM is used only when NPMCommand is empty. It makes fresh-host
	// bootstrap behavior independently testable.
	FindNPM func() (string, error)
	// Fetcher retrieves verified portable-runtime archives when no npm exists.
	Fetcher   ArtifactFetcher
	Runner    Runner
	LookupEnv func(string) (string, bool)
	GOOS      string
}

// Runner keeps package-manager execution injectable. Implementations must not
// return raw command output in result artifacts because installers can echo
// incidental credentials or private registry paths.
type Runner interface {
	Run(ctx context.Context, argv []string, env map[string]string) (CommandResult, error)
}

type CommandResult struct {
	ExitCode int `json:"exit_code"`
}

type InstallAttempt struct {
	SourceID  string `json:"source_id"`
	ExitCode  int    `json:"exit_code"`
	Succeeded bool   `json:"succeeded"`
}

type BootstrapAttempt struct {
	SourceID  string `json:"source_id"`
	Succeeded bool   `json:"succeeded"`
}

type BootstrapResult struct {
	Runtime  string             `json:"runtime"`
	Version  string             `json:"version"`
	SourceID string             `json:"source_id"`
	Cached   bool               `json:"cached"`
	Attempts []BootstrapAttempt `json:"attempts,omitempty"`
}

type Result struct {
	SchemaVersion           string           `json:"schema_version"`
	Status                  string           `json:"status"`
	Tool                    string           `json:"tool"`
	Version                 string           `json:"version"`
	Package                 string           `json:"package"`
	PlannedSources          []string         `json:"planned_sources"`
	PlannedBootstrapSources []string         `json:"planned_bootstrap_sources,omitempty"`
	Bootstrap               *BootstrapResult `json:"bootstrap,omitempty"`
	Attempts                []InstallAttempt `json:"attempts,omitempty"`
	Verification            *CommandResult   `json:"verification,omitempty"`
	RuntimeProfile          RuntimeProfile   `json:"runtime_profile"`
	RuntimeProfilePath      string           `json:"runtime_profile_path"`
}

// RuntimeProfile holds non-secret launch metadata. CredentialEnv is an env-var
// reference only. The actual credential stays in the target host environment
// and is only mapped into Claude Code's child process at launch time.
type RuntimeProfile struct {
	SchemaVersion string                      `json:"schema_version"`
	ID            string                      `json:"id"`
	Tool          string                      `json:"tool"`
	Version       string                      `json:"version"`
	Command       string                      `json:"command"`
	NodeBinDir    string                      `json:"node_bin_dir,omitempty"`
	ConfigDir     string                      `json:"config_dir,omitempty"`
	CodexProfile  string                      `json:"codex_profile,omitempty"`
	Endpoint      contracts.ToolchainEndpoint `json:"endpoint,omitempty"`
}

type layout struct {
	npmPrefix   string
	nodeRoot    string
	configDir   string
	profilePath string
}

func Ensure(ctx context.Context, request contracts.ToolchainRequest, options Options) (Result, error) {
	request.Normalize()
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	root, err := resolveRoot(options.Root)
	if err != nil {
		return Result{}, err
	}
	layout := layoutFor(root, request.Tool, request.Version)
	profile := newRuntimeProfile(request, layout, options.GOOS)
	registries := request.Policy.EligibleRegistries()
	result := Result{
		SchemaVersion:           ResultSchemaVersion,
		Status:                  StatusPlanned,
		Tool:                    request.Tool,
		Version:                 request.Version,
		Package:                 packageName(request.Tool),
		PlannedSources:          registryIDs(registries),
		PlannedBootstrapSources: nodeSourceIDs(request.Policy.NodeBootstrap, request.Policy.Region),
		RuntimeProfile:          profile,
		RuntimeProfilePath:      layout.profilePath,
	}
	if !request.Execute {
		return result, nil
	}
	if err := os.MkdirAll(layout.npmPrefix, 0o700); err != nil {
		return result, fmt.Errorf("create user-scoped toolchain prefix: %w", err)
	}
	runner := options.Runner
	if runner == nil {
		runner = execRunner{}
	}
	npmCommand := strings.TrimSpace(options.NPMCommand)
	if npmCommand == "" {
		find := options.FindNPM
		if find == nil {
			find = findNPM
		}
		npmCommand, err = find()
		if err != nil {
			if request.Policy.NodeBootstrap == nil {
				result.Status = StatusFailed
				return result, err
			}
			var bootstrap BootstrapResult
			npmCommand, profile.NodeBinDir, bootstrap, err = ensurePortableNode(ctx, layout.nodeRoot, *request.Policy.NodeBootstrap, request.Policy.Region, options)
			result.Bootstrap = &bootstrap
			result.RuntimeProfile = profile
			if err != nil {
				result.Status = StatusFailed
				return result, fmt.Errorf("verified portable Node bootstrap did not complete")
			}
		}
	}
	installed := false
	for _, registry := range registries {
		argv := npmInstallArgv(npmCommand, layout.npmPrefix, registry.URL, packageName(request.Tool), request.Version)
		environment := npmEnvironment(registry.URL, request.Policy.ProxyMode)
		for key, value := range profile.commandEnvironment(options.LookupEnv) {
			environment[key] = value
		}
		commandResult, runErr := runner.Run(ctx, argv, environment)
		attempt := InstallAttempt{SourceID: registry.ID, ExitCode: commandResult.ExitCode, Succeeded: runErr == nil && commandResult.ExitCode == 0}
		result.Attempts = append(result.Attempts, attempt)
		if attempt.Succeeded {
			installed = true
			break
		}
	}
	if !installed {
		result.Status = StatusFailed
		return result, fmt.Errorf("toolchain %s@%s could not be installed from any trusted registry", request.Tool, request.Version)
	}
	verification, verifyErr := runner.Run(ctx, []string{profile.Command, "--version"}, profile.commandEnvironment(options.LookupEnv))
	result.Verification = &verification
	if verifyErr != nil || verification.ExitCode != 0 {
		result.Status = StatusFailed
		return result, fmt.Errorf("toolchain %s installation completed but local command verification failed", request.Tool)
	}
	if err := writeRuntimeProfile(layout, profile); err != nil {
		result.Status = StatusFailed
		return result, err
	}
	result.Status = StatusInstalled
	return result, nil
}

func registryIDs(registries []contracts.ToolchainRegistry) []string {
	result := make([]string, 0, len(registries))
	for _, registry := range registries {
		result = append(result, registry.ID)
	}
	return result
}

func nodeSourceIDs(bootstrap *contracts.ToolchainNodeBootstrap, region string) []string {
	if bootstrap == nil {
		return nil
	}
	sources := bootstrap.EligibleSources(region)
	result := make([]string, 0, len(sources))
	for _, source := range sources {
		result = append(result, source.ID)
	}
	return result
}

func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home for toolchain: %w", err)
	}
	return filepath.Join(home, ".rdev", "toolchains"), nil
}

func resolveRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		var err error
		root, err = DefaultRoot()
		if err != nil {
			return "", err
		}
	}
	return filepath.Abs(root)
}

func layoutFor(root, tool, version string) layout {
	base := filepath.Join(root, "agents", tool, version)
	return layout{
		npmPrefix:   filepath.Join(base, "npm-prefix"),
		nodeRoot:    filepath.Join(root, "runtimes", "node"),
		configDir:   filepath.Join(base, "config"),
		profilePath: filepath.Join(root, "profiles", tool+"-"+version+".json"),
	}
}

func newRuntimeProfile(request contracts.ToolchainRequest, paths layout, goos string) RuntimeProfile {
	if goos == "" {
		goos = runtime.GOOS
	}
	command := toolCommand(paths.npmPrefix, request.Tool, goos)
	profile := RuntimeProfile{
		SchemaVersion: RuntimeProfileSchemaVersion,
		ID:            request.Tool + "-" + request.Version,
		Tool:          request.Tool,
		Version:       request.Version,
		Command:       command,
		Endpoint:      request.Policy.Endpoint,
	}
	if request.Tool == contracts.ToolchainCodex && request.Policy.Endpoint.BaseURL != "" {
		profile.ConfigDir = paths.configDir
		profile.CodexProfile = "rdev"
	}
	return profile
}

func packageName(tool string) string {
	switch tool {
	case contracts.ToolchainCodex:
		return "@openai/codex"
	case contracts.ToolchainClaudeCode:
		return "@anthropic-ai/claude-code"
	default:
		return ""
	}
}

func toolCommand(prefix, tool, goos string) string {
	command := tool
	if tool == contracts.ToolchainClaudeCode {
		command = "claude"
	}
	if goos == "windows" {
		return filepath.Join(prefix, command+".cmd")
	}
	return filepath.Join(prefix, "bin", command)
}

func findNPM() (string, error) {
	for _, candidate := range []string{"npm", "npm.cmd"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("npm is required to install the requested agent toolchain; provision a user-scoped Node.js runtime first")
}

func npmInstallArgv(npmCommand, prefix, registryURL, packageName, version string) []string {
	return []string{
		npmCommand,
		"install",
		"--global",
		"--prefix", prefix,
		"--registry", registryURL,
		"--no-audit",
		"--no-fund",
		packageName + "@" + version,
	}
}

func npmEnvironment(registryURL, proxyMode string) map[string]string {
	env := map[string]string{
		"NPM_CONFIG_REGISTRY": registryURL,
		"npm_config_registry": registryURL,
	}
	if proxyMode == contracts.ToolchainProxyDisabled {
		for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
			env[name] = ""
		}
	}
	return env
}

func (p RuntimeProfile) commandEnvironment(lookup func(string) (string, bool)) map[string]string {
	env := map[string]string{}
	if p.NodeBinDir == "" {
		return env
	}
	if lookup == nil {
		lookup = os.LookupEnv
	}
	pathValue, _ := lookup("PATH")
	if pathValue == "" {
		env["PATH"] = p.NodeBinDir
		return env
	}
	env["PATH"] = p.NodeBinDir + string(os.PathListSeparator) + pathValue
	return env
}

func (p RuntimeProfile) LaunchEnvironment(lookup func(string) (string, bool)) (map[string]string, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	if p.SchemaVersion != RuntimeProfileSchemaVersion {
		return nil, fmt.Errorf("unsupported runtime profile schema %q", p.SchemaVersion)
	}
	if err := p.Endpoint.Validate(); err != nil {
		return nil, err
	}
	env := p.commandEnvironment(lookup)
	switch p.Tool {
	case contracts.ToolchainCodex:
		if p.Endpoint.BaseURL != "" {
			credential, ok := lookup(p.Endpoint.CredentialEnv)
			if !ok || strings.TrimSpace(credential) == "" {
				return nil, fmt.Errorf("required credential environment variable %q is not available", p.Endpoint.CredentialEnv)
			}
		}
		if p.ConfigDir != "" {
			env["CODEX_HOME"] = p.ConfigDir
		}
	case contracts.ToolchainClaudeCode:
		if p.Endpoint.BaseURL == "" {
			return env, nil
		}
		credential, ok := lookup(p.Endpoint.CredentialEnv)
		if !ok || strings.TrimSpace(credential) == "" {
			return nil, fmt.Errorf("required credential environment variable %q is not available", p.Endpoint.CredentialEnv)
		}
		env["ANTHROPIC_BASE_URL"] = p.Endpoint.BaseURL
		env["ANTHROPIC_MODEL"] = p.Endpoint.Model
		if p.Endpoint.AuthMode == contracts.ToolchainAuthBearer {
			env["ANTHROPIC_AUTH_TOKEN"] = credential
		} else {
			env["ANTHROPIC_API_KEY"] = credential
		}
	default:
		return nil, fmt.Errorf("unsupported runtime profile tool %q", p.Tool)
	}
	return env, nil
}

func LoadRuntimeProfile(path string) (RuntimeProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RuntimeProfile{}, fmt.Errorf("read runtime profile: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var profile RuntimeProfile
	if err := decoder.Decode(&profile); err != nil {
		return RuntimeProfile{}, fmt.Errorf("decode runtime profile: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return RuntimeProfile{}, fmt.Errorf("decode runtime profile: trailing JSON value")
	}
	if profile.SchemaVersion != RuntimeProfileSchemaVersion || profile.ID == "" || profile.Command == "" {
		return RuntimeProfile{}, fmt.Errorf("invalid runtime profile")
	}
	if !filepath.IsAbs(profile.Command) {
		return RuntimeProfile{}, fmt.Errorf("runtime profile command must be absolute")
	}
	for _, path := range []string{profile.NodeBinDir, profile.ConfigDir} {
		if path != "" && !filepath.IsAbs(path) {
			return RuntimeProfile{}, fmt.Errorf("runtime profile managed path must be absolute")
		}
	}
	if _, err := profile.LaunchEnvironment(func(string) (string, bool) { return "present", true }); err != nil {
		return RuntimeProfile{}, err
	}
	return profile, nil
}

// LoadRuntimeProfileByID constrains profile lookup to the managed toolchain
// root. Profile IDs are opaque values returned by Ensure, never host paths.
func LoadRuntimeProfileByID(root, id string) (RuntimeProfile, error) {
	id = strings.TrimSpace(id)
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id || strings.ContainsAny(id, `\\/`) {
		return RuntimeProfile{}, fmt.Errorf("invalid runtime profile id")
	}
	resolvedRoot, err := resolveRoot(root)
	if err != nil {
		return RuntimeProfile{}, err
	}
	profile, err := LoadRuntimeProfile(filepath.Join(resolvedRoot, "profiles", id+".json"))
	if err != nil {
		return RuntimeProfile{}, err
	}
	if profile.ID != id {
		return RuntimeProfile{}, fmt.Errorf("runtime profile embedded ID does not match requested profile ID")
	}
	return profile, nil
}

func writeRuntimeProfile(paths layout, profile RuntimeProfile) error {
	if err := os.MkdirAll(filepath.Dir(paths.profilePath), 0o700); err != nil {
		return fmt.Errorf("create runtime profile directory: %w", err)
	}
	if profile.Tool == contracts.ToolchainCodex && profile.ConfigDir != "" {
		if err := os.MkdirAll(profile.ConfigDir, 0o700); err != nil {
			return fmt.Errorf("create Codex profile directory: %w", err)
		}
		if err := writePrivateFile(filepath.Join(profile.ConfigDir, profile.CodexProfile+".config.toml"), []byte(codexProfileTOML(profile))); err != nil {
			return fmt.Errorf("write Codex runtime profile: %w", err)
		}
	}
	encoded, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return fmt.Errorf("encode runtime profile: %w", err)
	}
	if err := writePrivateFile(paths.profilePath, append(encoded, '\n')); err != nil {
		return fmt.Errorf("write runtime profile: %w", err)
	}
	return nil
}

func codexProfileTOML(profile RuntimeProfile) string {
	return strings.Join([]string{
		"model_provider = \"rdev\"",
		"model = " + strconv.Quote(profile.Endpoint.Model),
		"",
		"[model_providers.rdev]",
		"name = \"Rdev managed provider\"",
		"base_url = " + strconv.Quote(profile.Endpoint.BaseURL),
		"env_key = " + strconv.Quote(profile.Endpoint.CredentialEnv),
		"wire_api = \"responses\"",
		"",
	}, "\n")
}

func writePrivateFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(dir, ".rdev-toolchain-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, argv []string, environment map[string]string) (CommandResult, error) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return CommandResult{ExitCode: -1}, fmt.Errorf("command is required")
	}
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if len(environment) > 0 {
		command.Env = mergeEnvironment(os.Environ(), environment)
	}
	err := command.Run()
	if err == nil {
		return CommandResult{ExitCode: 0}, nil
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		return CommandResult{ExitCode: exitError.ExitCode()}, err
	}
	return CommandResult{ExitCode: -1}, err
}

func mergeEnvironment(base []string, overrides map[string]string) []string {
	values := make(map[string]string, len(base)+len(overrides))
	order := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}
	for key, value := range overrides {
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}
	result := make([]string, 0, len(order))
	for _, key := range order {
		result = append(result, key+"="+values[key])
	}
	return result
}

func cloneEnvironment(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
