package connectionrunner

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/agentinvite"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestBuildWritesSelfContainedRunnerPackage(t *testing.T) {
	out := filepath.Join(t.TempDir(), "runner")
	pkg, err := Build(Options{
		Invite:       testInvite(t),
		OutDir:       out,
		TargetOS:     "windows",
		TargetArch:   "amd64",
		SessionMode:  string(model.HostModeAttendedTemporary),
		RdevCommand:  "rdev",
		GeneratedAt:  time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC),
		WritePackage: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.SchemaVersion != ManifestSchemaVersion ||
		pkg.Plan.SchemaVersion != PlanSchemaVersion ||
		pkg.ManifestPath == "" ||
		pkg.LauncherPath == "" ||
		pkg.PlanPath == "" {
		t.Fatalf("expected full runner package, got %#v", pkg)
	}
	for _, path := range []string{pkg.ManifestPath, pkg.LauncherPath, pkg.PlanPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated file %s: %v", path, err)
		}
	}
	launcher, err := os.ReadFile(pkg.LauncherPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(launcher), "connection-entry run --runner-manifest") {
		t.Fatalf("launcher should call runner, got:\n%s", string(launcher))
	}
	if !pkg.Manifest.NoManualAssembly ||
		!slices.Contains(pkg.Manifest.AgentOnlyParameters, "manifest_root_public_key") ||
		len(pkg.Manifest.ConnectionPaths) < 7 {
		t.Fatalf("runner should carry agent-only metadata and connection paths: %#v", pkg.Manifest)
	}
	if pkg.LauncherSHA256 == "" {
		t.Fatalf("expected launcher checksum")
	}
}

func TestRunDryRunSelectsDirectGateway(t *testing.T) {
	out := filepath.Join(t.TempDir(), "runner")
	pkg, err := Build(Options{
		Invite:       testInvite(t),
		OutDir:       out,
		TargetOS:     "linux",
		TargetArch:   "amd64",
		SessionMode:  string(model.HostModeAttendedTemporary),
		WritePackage: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(RunOptions{
		ManifestPath: pkg.ManifestPath,
		DryRun:       true,
		HTTPProbe: func(rawURL string, timeout time.Duration) error {
			if rawURL == "" {
				return errors.New("missing url")
			}
			return nil
		},
		LookPath: func(name string) (string, error) {
			return "", errors.New("not found")
		},
		Now: time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SelectedPath != "native-direct-gateway" ||
		result.SelectedTransport != "auto" ||
		len(result.HostServeArgs) == 0 ||
		!slices.Contains(result.HostServeArgs, "--manifest-root-public-key") {
		t.Fatalf("expected direct host serve dry-run, got %#v", result)
	}
	if result.Executed {
		t.Fatalf("dry-run must not execute host serve")
	}
}

func TestRunReportsManualActionWhenNoPathWorks(t *testing.T) {
	out := filepath.Join(t.TempDir(), "runner")
	pkg, err := Build(Options{
		Invite:       testInvite(t),
		OutDir:       out,
		TargetOS:     "linux",
		TargetArch:   "amd64",
		SessionMode:  string(model.HostModeAttendedTemporary),
		WritePackage: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(RunOptions{
		ManifestPath: pkg.ManifestPath,
		DryRun:       true,
		HTTPProbe: func(rawURL string, timeout time.Duration) error {
			return errors.New("blocked")
		},
		LookPath: func(name string) (string, error) {
			return "", errors.New("not found")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SelectedPath != "" || len(result.ManualActionRequired) == 0 {
		t.Fatalf("expected manual action when every path fails, got %#v", result)
	}
	if len(result.ProbeResults) < 4 {
		t.Fatalf("expected probe evidence, got %#v", result.ProbeResults)
	}
}

func TestRunUsesConfiguredRelayGatewayOverride(t *testing.T) {
	t.Setenv("RDEV_RELAY_GATEWAY_URL", "http://127.0.0.1:8787")
	out := filepath.Join(t.TempDir(), "runner")
	pkg, err := Build(Options{
		Invite:       testInvite(t),
		OutDir:       out,
		TargetOS:     "linux",
		TargetArch:   "amd64",
		SessionMode:  string(model.HostModeAttendedTemporary),
		WritePackage: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(RunOptions{
		ManifestPath: pkg.ManifestPath,
		DryRun:       true,
		HTTPProbe: func(rawURL string, timeout time.Duration) error {
			if rawURL == "http://127.0.0.1:8787" {
				return nil
			}
			return errors.New("direct blocked")
		},
		LookPath: func(name string) (string, error) {
			if name == "chisel" {
				return "/usr/local/bin/chisel", nil
			}
			return "", errors.New("not found")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SelectedPath != "existing-frp-or-chisel-relay" ||
		result.SelectedGatewayURL != "http://127.0.0.1:8787" {
		t.Fatalf("expected relay gateway override, got %#v", result)
	}
	joined := strings.Join(result.HostServeArgs, " ")
	if !strings.Contains(joined, "--gateway http://127.0.0.1:8787") ||
		!strings.Contains(joined, "--manifest-url http://127.0.0.1:8787/v1/tickets/") {
		t.Fatalf("expected rewritten manifest URL for selected relay gateway, got %v", result.HostServeArgs)
	}
}

func TestRunDryRunReportsConfiguredHelperStart(t *testing.T) {
	t.Setenv("RDEV_RELAY_GATEWAY_URL", "http://127.0.0.1:8787")
	t.Setenv("RDEV_RELAY_START_ARGV_JSON", `["chisel","client","relay.example.invalid","R:8787:127.0.0.1:8787"]`)
	out := filepath.Join(t.TempDir(), "runner")
	pkg, err := Build(Options{
		Invite:       testInvite(t),
		OutDir:       out,
		TargetOS:     "linux",
		TargetArch:   "amd64",
		SessionMode:  string(model.HostModeAttendedTemporary),
		WritePackage: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(RunOptions{
		ManifestPath: pkg.ManifestPath,
		DryRun:       true,
		HTTPProbe: func(rawURL string, timeout time.Duration) error {
			if rawURL == "http://127.0.0.1:8787" {
				return errors.New("helper not started during dry-run")
			}
			return errors.New("direct blocked")
		},
		LookPath: func(name string) (string, error) {
			if name == "chisel" {
				return "/usr/local/bin/chisel", nil
			}
			return "", errors.New("not found")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SelectedPath != "existing-frp-or-chisel-relay" ||
		!result.HelperStartConfigured ||
		result.HelperStarted ||
		result.HelperStartTool != "chisel" ||
		result.Executed {
		t.Fatalf("expected dry-run helper start report without execution, got %#v", result)
	}
}

func TestRunStartsConfiguredHelperBeforeHostServe(t *testing.T) {
	t.Setenv("RDEV_RELAY_GATEWAY_URL", "http://127.0.0.1:8787")
	t.Setenv("RDEV_RELAY_START_ARGV_JSON", `["chisel","client","relay.example.invalid","R:8787:127.0.0.1:8787"]`)
	out := filepath.Join(t.TempDir(), "runner")
	pkg, err := Build(Options{
		Invite:       testInvite(t),
		OutDir:       out,
		TargetOS:     "linux",
		TargetArch:   "amd64",
		SessionMode:  string(model.HostModeAttendedTemporary),
		WritePackage: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	result, err := Run(RunOptions{
		ManifestPath: pkg.ManifestPath,
		HTTPProbe: func(rawURL string, timeout time.Duration) error {
			if rawURL == "http://127.0.0.1:8787" && slices.Contains(events, "helper-started") {
				events = append(events, "helper-probed")
				return nil
			}
			return errors.New("not reachable yet")
		},
		LookPath: func(name string) (string, error) {
			if name == "chisel" {
				return "/usr/local/bin/chisel", nil
			}
			return "", errors.New("not found")
		},
		HelperStarter: func(argv []string) (func() error, error) {
			if got := strings.Join(argv, " "); !strings.Contains(got, "chisel client") {
				t.Fatalf("unexpected helper argv: %v", argv)
			}
			events = append(events, "helper-started")
			return func() error {
				events = append(events, "helper-cleaned")
				return nil
			}, nil
		},
		CommandRunner: func(command string, args []string) error {
			events = append(events, "host-serve")
			if command != "rdev" || !slices.Contains(args, "--gateway") {
				t.Fatalf("unexpected host serve command: %s %v", command, args)
			}
			return nil
		},
		ProbeTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.HelperStartConfigured || !result.HelperStarted || !result.Executed {
		t.Fatalf("expected helper and host serve execution, got %#v", result)
	}
	want := []string{"helper-started", "helper-probed", "host-serve", "helper-cleaned"}
	if strings.Join(events, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected event order: got %v want %v", events, want)
	}
}

func TestRunRejectsHelperStartForWrongTool(t *testing.T) {
	t.Setenv("RDEV_RELAY_GATEWAY_URL", "http://127.0.0.1:8787")
	t.Setenv("RDEV_RELAY_START_ARGV_JSON", `["sh","-c","chisel client relay.example.invalid"]`)
	out := filepath.Join(t.TempDir(), "runner")
	pkg, err := Build(Options{
		Invite:       testInvite(t),
		OutDir:       out,
		TargetOS:     "linux",
		TargetArch:   "amd64",
		SessionMode:  string(model.HostModeAttendedTemporary),
		WritePackage: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Run(RunOptions{
		ManifestPath: pkg.ManifestPath,
		DryRun:       true,
		HTTPProbe: func(rawURL string, timeout time.Duration) error {
			return errors.New("blocked")
		},
		LookPath: func(name string) (string, error) {
			if name == "chisel" {
				return "/usr/local/bin/chisel", nil
			}
			return "", errors.New("not found")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "only allows") {
		t.Fatalf("expected helper tool allow-list error, got %v", err)
	}
}

func TestRunInstallsMissingHelperDependencyBeforeHostServe(t *testing.T) {
	t.Setenv("RDEV_RELAY_GATEWAY_URL", "http://127.0.0.1:8787")
	t.Setenv("RDEV_RELAY_START_ARGV_JSON", `["chisel","client","relay.example.invalid","R:8787:127.0.0.1:8787"]`)
	t.Setenv("RDEV_RELAY_INSTALL_ACTION_JSON", `{"schema_version":"rdev.connection-entry.dependency-install-action.v1","tool":"chisel","scope":"user","argv":["rdev","deps","install","chisel","--scope","user"],"reason":"relay helper for Connection Entry"}`)
	out := filepath.Join(t.TempDir(), "runner")
	pkg, err := Build(Options{
		Invite:       testInvite(t),
		OutDir:       out,
		TargetOS:     "linux",
		TargetArch:   "amd64",
		SessionMode:  string(model.HostModeAttendedTemporary),
		WritePackage: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	installed := false
	result, err := Run(RunOptions{
		ManifestPath: pkg.ManifestPath,
		HTTPProbe: func(rawURL string, timeout time.Duration) error {
			if rawURL == "http://127.0.0.1:8787" && slices.Contains(events, "helper-started") {
				events = append(events, "helper-probed")
				return nil
			}
			return errors.New("not reachable yet")
		},
		LookPath: func(name string) (string, error) {
			if name == "chisel" && installed {
				return "/tmp/rdev-tools/chisel", nil
			}
			return "", errors.New("not found")
		},
		DependencyInstaller: func(action DependencyInstallAction) (DependencyInstallResult, error) {
			if action.Tool != "chisel" || action.Scope != "user" {
				t.Fatalf("unexpected install action: %#v", action)
			}
			events = append(events, "dependency-installed")
			installed = true
			return DependencyInstallResult{InstalledBinary: "/tmp/rdev-tools/chisel"}, nil
		},
		HelperStarter: func(argv []string) (func() error, error) {
			if argv[0] != "/tmp/rdev-tools/chisel" {
				t.Fatalf("expected installed helper path, got %v", argv)
			}
			events = append(events, "helper-started")
			return func() error {
				events = append(events, "helper-cleaned")
				return nil
			}, nil
		},
		CommandRunner: func(command string, args []string) error {
			events = append(events, "host-serve")
			return nil
		},
		ProbeTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DependencyInstallConfigured || !result.DependencyInstalled || result.DependencyInstallTool != "chisel" ||
		!result.HelperStarted || !result.Executed {
		t.Fatalf("expected install, helper start, and host serve, got %#v", result)
	}
	want := []string{"dependency-installed", "helper-started", "helper-probed", "host-serve", "helper-cleaned"}
	if strings.Join(events, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected event order: got %v want %v", events, want)
	}
}

func TestRunRejectsUnsafeDependencyInstallAction(t *testing.T) {
	t.Setenv("RDEV_RELAY_GATEWAY_URL", "http://127.0.0.1:8787")
	t.Setenv("RDEV_RELAY_START_ARGV_JSON", `["chisel","client","relay.example.invalid","R:8787:127.0.0.1:8787"]`)
	t.Setenv("RDEV_RELAY_INSTALL_ACTION_JSON", `{"tool":"chisel","scope":"user","argv":["powershell","-ExecutionPolicy","Bypass","-Command","Install-Chisel"]}`)
	out := filepath.Join(t.TempDir(), "runner")
	pkg, err := Build(Options{
		Invite:       testInvite(t),
		OutDir:       out,
		TargetOS:     "linux",
		TargetArch:   "amd64",
		SessionMode:  string(model.HostModeAttendedTemporary),
		WritePackage: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Run(RunOptions{
		ManifestPath: pkg.ManifestPath,
		DryRun:       true,
		HTTPProbe: func(rawURL string, timeout time.Duration) error {
			return errors.New("blocked")
		},
		LookPath: func(name string) (string, error) {
			return "", errors.New("not found")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "ExecutionPolicy Bypass") {
		t.Fatalf("expected unsafe install action rejection, got %v", err)
	}
}

func TestDependencyInstallResultFromJSON(t *testing.T) {
	result := dependencyInstallResultFromJSON([]byte(`{
		"ok": true,
		"installed_binary": "/tmp/rdev-tools/chisel",
		"dependency_report": {
			"installed_binary": "/tmp/rdev-tools/ignored"
		}
	}`))
	if result.InstalledBinary != "/tmp/rdev-tools/chisel" {
		t.Fatalf("unexpected top-level installed binary: %#v", result)
	}
	result = dependencyInstallResultFromJSON([]byte(`{
		"ok": true,
		"dependency_report": {
			"installed_binary": "/tmp/rdev-tools/frpc"
		}
	}`))
	if result.InstalledBinary != "/tmp/rdev-tools/frpc" {
		t.Fatalf("unexpected nested installed binary: %#v", result)
	}
}

func testInvite(t *testing.T) agentinvite.Invite {
	t.Helper()
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	ticket, err := model.NewTicket(model.HostModeAttendedTemporary, 600, []string{"shell.user"}, "repair", now)
	if err != nil {
		t.Fatal(err)
	}
	invite, err := agentinvite.New(agentinvite.Options{
		GatewayURL:            "https://api.example.com/v1",
		JoinURL:               "https://api.example.com/join/" + ticket.Code,
		ManifestURL:           "https://api.example.com/v1/tickets/" + ticket.Code + "/manifest",
		ManifestRootPublicKey: "manifest-root:" + strings.Repeat("d", 43),
		Ticket:                ticket,
		Transport:             "auto",
		NetworkScope:          "auto",
		AuthorityProfile:      "max-control",
		CreatedAt:             now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return invite
}
