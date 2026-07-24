package toolchain

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
)

type recordedCommand struct {
	argv []string
	env  map[string]string
}

type fakeRunner struct {
	commands []recordedCommand
	results  []CommandResult
	errors   []error
}

func (r *fakeRunner) Run(_ context.Context, argv []string, env map[string]string) (CommandResult, error) {
	r.commands = append(r.commands, recordedCommand{argv: append([]string(nil), argv...), env: cloneEnvironment(env)})
	index := len(r.commands) - 1
	var result CommandResult
	if index < len(r.results) {
		result = r.results[index]
	}
	var err error
	if index < len(r.errors) {
		err = r.errors[index]
	}
	return result, err
}

type fakeArtifactFetcher struct {
	archive []byte
	calls   []contracts.ToolchainNodeSource
}

func (f *fakeArtifactFetcher) Fetch(_ context.Context, source contracts.ToolchainNodeSource, destination string, _ int64) error {
	f.calls = append(f.calls, source)
	return os.WriteFile(destination, f.archive, 0o600)
}

func testCodexRequest(region string) contracts.ToolchainRequest {
	return contracts.ToolchainRequest{
		SchemaVersion: contracts.ToolchainRequestSchemaVersion,
		Tool:          contracts.ToolchainCodex,
		Version:       "0.144.6",
		Execute:       true,
		Policy: contracts.ToolchainPolicy{
			SchemaVersion: contracts.ToolchainPolicySchemaVersion,
			Region:        region,
			ProxyMode:     contracts.ToolchainProxyInherit,
			Registries: []contracts.ToolchainRegistry{
				{ID: "cn-mirror", URL: "https://registry.example.cn/npm/", Regions: []string{contracts.ToolchainRegionCNMainland}},
				{ID: "official", URL: "https://registry.npmjs.org/", Regions: []string{contracts.ToolchainRegionGlobal, contracts.ToolchainRegionCNMainland}},
			},
			Endpoint: contracts.ToolchainEndpoint{
				BaseURL:       "https://agents.example.test/v1",
				Model:         "test-model",
				CredentialEnv: "RDEV_CODEX_API_KEY",
				AuthMode:      contracts.ToolchainAuthBearer,
			},
		},
	}
}

func TestEnsureUsesMainlandMirrorAndWritesSecretFreeCodexProfile(t *testing.T) {
	root := t.TempDir()
	runner := &fakeRunner{results: []CommandResult{{ExitCode: 0}, {ExitCode: 0}}}
	result, err := Ensure(context.Background(), testCodexRequest(contracts.ToolchainRegionCNMainland), Options{
		Root:       root,
		NPMCommand: "npm-test",
		Runner:     runner,
		LookupEnv: func(name string) (string, bool) {
			return "value-that-must-not-be-persisted", name == "RDEV_CODEX_API_KEY"
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusInstalled || len(runner.commands) != 2 {
		t.Fatalf("unexpected result %#v; commands %#v", result, runner.commands)
	}
	install := runner.commands[0]
	if install.argv[0] != "npm-test" || !containsArg(install.argv, "@openai/codex@0.144.6") || install.env["NPM_CONFIG_REGISTRY"] != "https://registry.example.cn/npm/" {
		t.Fatalf("unexpected install command: %#v", install)
	}
	if result.Attempts[0].SourceID != "cn-mirror" || !result.Attempts[0].Succeeded {
		t.Fatalf("mirror evidence = %#v", result.Attempts)
	}
	profileBytes, err := os.ReadFile(result.RuntimeProfilePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(profileBytes), "value-that-must-not-be-persisted") {
		t.Fatalf("runtime profile persisted credential material: %s", profileBytes)
	}
	configBytes, err := os.ReadFile(filepath.Join(result.RuntimeProfile.ConfigDir, "rdev.config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configBytes), `env_key = "RDEV_CODEX_API_KEY"`) || strings.Contains(string(configBytes), "value-that-must-not-be-persisted") {
		t.Fatalf("unexpected Codex config: %s", configBytes)
	}
}

func TestEnsureFallsBackToNextTrustedRegistry(t *testing.T) {
	runner := &fakeRunner{
		results: []CommandResult{{ExitCode: 1}, {ExitCode: 0}, {ExitCode: 0}},
		errors:  []error{errors.New("mirror unavailable"), nil, nil},
	}
	result, err := Ensure(context.Background(), testCodexRequest(contracts.ToolchainRegionCNMainland), Options{
		Root:       t.TempDir(),
		NPMCommand: "npm-test",
		Runner:     runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Attempts) != 2 || result.Attempts[0].SourceID != "cn-mirror" || result.Attempts[0].Succeeded || result.Attempts[1].SourceID != "official" || !result.Attempts[1].Succeeded {
		t.Fatalf("fallback attempts = %#v", result.Attempts)
	}
	if runner.commands[1].env["NPM_CONFIG_REGISTRY"] != "https://registry.npmjs.org/" {
		t.Fatalf("official fallback did not set registry: %#v", runner.commands[1])
	}
}

func TestRuntimeProfileMapsClaudeCredentialOnlyAtLaunch(t *testing.T) {
	profile := RuntimeProfile{
		SchemaVersion: RuntimeProfileSchemaVersion,
		Tool:          contracts.ToolchainClaudeCode,
		Endpoint: contracts.ToolchainEndpoint{
			BaseURL:       "https://agents.example.test/anthropic",
			Model:         "claude-test",
			CredentialEnv: "RDEV_CLAUDE_TOKEN",
			AuthMode:      contracts.ToolchainAuthBearer,
		},
	}
	env, err := profile.LaunchEnvironment(func(name string) (string, bool) {
		return "launch-only-secret", name == "RDEV_CLAUDE_TOKEN"
	})
	if err != nil {
		t.Fatal(err)
	}
	if env["ANTHROPIC_BASE_URL"] != "https://agents.example.test/anthropic" || env["ANTHROPIC_MODEL"] != "claude-test" || env["ANTHROPIC_AUTH_TOKEN"] != "launch-only-secret" {
		t.Fatalf("unexpected Claude launch environment: %#v", env)
	}
	if env["ANTHROPIC_API_KEY"] != "" {
		t.Fatalf("bearer profile should not set ANTHROPIC_API_KEY: %#v", env)
	}
}

func TestEnsureBootstrapsVerifiedPortableNodeWhenNPMIsMissing(t *testing.T) {
	request := testCodexRequest(contracts.ToolchainRegionCNMainland)
	archive := portableNodeZip(t)
	request.Policy.NodeBootstrap = &contracts.ToolchainNodeBootstrap{
		Version:           "22.14.0",
		MaxArchiveBytes:   1024 * 1024,
		MaxExtractedBytes: 4 * 1024 * 1024,
		Sources: []contracts.ToolchainNodeSource{{
			ID:      "node-cn-mirror",
			URL:     "https://mirror.example.cn/node.zip",
			SHA256:  sha256Hex(archive),
			Format:  contracts.ToolchainArchiveZIP,
			Regions: []string{contracts.ToolchainRegionCNMainland},
		}},
	}
	fetcher := &fakeArtifactFetcher{archive: archive}
	runner := &fakeRunner{results: []CommandResult{{ExitCode: 0}, {ExitCode: 0}}}
	root := t.TempDir()
	result, err := Ensure(context.Background(), request, Options{
		Root: root,
		FindNPM: func() (string, error) {
			return "", errors.New("npm is absent")
		},
		Fetcher: fetcher,
		Runner:  runner,
		GOOS:    "linux",
		LookupEnv: func(name string) (string, bool) {
			return "value-that-must-not-be-persisted", name == "RDEV_CODEX_API_KEY"
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fetcher.calls) != 1 || fetcher.calls[0].ID != "node-cn-mirror" || result.Bootstrap == nil || result.Bootstrap.SourceID != "node-cn-mirror" {
		t.Fatalf("bootstrap evidence = %#v, fetcher = %#v", result.Bootstrap, fetcher.calls)
	}
	if len(result.PlannedBootstrapSources) != 1 || result.PlannedBootstrapSources[0] != "node-cn-mirror" {
		t.Fatalf("bootstrap plan source evidence = %#v", result.PlannedBootstrapSources)
	}
	if len(runner.commands) != 2 || !strings.Contains(runner.commands[0].argv[0], filepath.Join("runtimes", "node", "22.14.0")) {
		t.Fatalf("managed npm was not used: %#v", runner.commands)
	}
	if result.RuntimeProfile.NodeBinDir == "" {
		t.Fatalf("runtime profile omitted managed Node path: %#v", result.RuntimeProfile)
	}
	if !strings.HasPrefix(runner.commands[0].env["PATH"], result.RuntimeProfile.NodeBinDir) {
		t.Fatalf("managed npm did not receive managed Node PATH: %#v", runner.commands[0].env)
	}
	env, err := result.RuntimeProfile.LaunchEnvironment(func(name string) (string, bool) {
		if name == "RDEV_CODEX_API_KEY" {
			return "launch-only-secret", true
		}
		if name == "PATH" {
			return "/system/bin", true
		}
		return "", false
	})
	if err != nil || !strings.HasPrefix(env["PATH"], result.RuntimeProfile.NodeBinDir+string(os.PathListSeparator)) {
		t.Fatalf("managed Node path was not prepended: env=%#v err=%v", env, err)
	}
}

func TestEnsurePortableNodeFallsBackAfterDigestMismatch(t *testing.T) {
	archive := portableNodeZip(t)
	request := testCodexRequest(contracts.ToolchainRegionCNMainland)
	request.Policy.NodeBootstrap = &contracts.ToolchainNodeBootstrap{
		Version:           "22.14.0",
		MaxArchiveBytes:   1024 * 1024,
		MaxExtractedBytes: 4 * 1024 * 1024,
		Sources: []contracts.ToolchainNodeSource{
			{ID: "bad-digest", URL: "https://mirror.example.cn/bad.zip", SHA256: strings.Repeat("0", 64), Format: contracts.ToolchainArchiveZIP, Regions: []string{contracts.ToolchainRegionCNMainland}},
			{ID: "verified-fallback", URL: "https://registry.example.test/node.zip", SHA256: sha256Hex(archive), Format: contracts.ToolchainArchiveZIP, Regions: []string{contracts.ToolchainRegionCNMainland}},
		},
	}
	fetcher := &fakeArtifactFetcher{archive: archive}
	result, err := Ensure(context.Background(), request, Options{
		Root:    t.TempDir(),
		Fetcher: fetcher,
		FindNPM: func() (string, error) { return "", errors.New("npm absent") },
		Runner:  &fakeRunner{results: []CommandResult{{ExitCode: 0}, {ExitCode: 0}}},
		GOOS:    "linux",
		LookupEnv: func(string) (string, bool) {
			return "present", true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Bootstrap == nil || result.Bootstrap.SourceID != "verified-fallback" || len(result.Bootstrap.Attempts) != 2 || result.Bootstrap.Attempts[0].Succeeded || !result.Bootstrap.Attempts[1].Succeeded {
		t.Fatalf("unexpected portable Node fallback evidence: %#v", result.Bootstrap)
	}
	if len(fetcher.calls) != 2 || fetcher.calls[0].ID != "bad-digest" || fetcher.calls[1].ID != "verified-fallback" {
		t.Fatalf("unexpected portable Node fetch order: %#v", fetcher.calls)
	}
}

func TestExtractPortableNodeArchiveRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "node.zip")
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	entry, err := archive.Create("../escaped")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("bad")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, buffer.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extractNodeArchive(archivePath, filepath.Join(root, "extracted"), contracts.ToolchainArchiveZIP, 1024); err == nil {
		t.Fatal("expected archive traversal rejection")
	}
	if _, err := os.Stat(filepath.Join(root, "escaped")); !os.IsNotExist(err) {
		t.Fatalf("archive traversal created a file outside extraction root: %v", err)
	}
}

func TestLoadRuntimeProfileByIDRejectsMismatchedEmbeddedID(t *testing.T) {
	root := t.TempDir()
	requestedID := "codex-0.144.6"
	profile := RuntimeProfile{
		SchemaVersion: RuntimeProfileSchemaVersion,
		ID:            "codex-0.144.7",
		Tool:          contracts.ToolchainCodex,
		Version:       "0.144.6",
		Command:       "/managed/codex",
	}
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "profiles"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "profiles", requestedID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRuntimeProfileByID(root, requestedID); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected mismatched profile ID rejection, got %v", err)
	}
}

func portableNodeZip(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, entry := range []string{
		"node-v22.14.0-linux-x64/bin/node",
		"node-v22.14.0-linux-x64/bin/npm",
	} {
		writer, err := archive.Create(entry)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte("fixture")); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func containsArg(argv []string, want string) bool {
	for _, arg := range argv {
		if arg == want {
			return true
		}
	}
	return false
}
