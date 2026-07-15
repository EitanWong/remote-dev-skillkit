package connectionentry

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/agentinvite"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/release"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
)

func TestWindowsConnectionEntryPrefersLayeredBootstrapAndRetainsArchiveFallback(t *testing.T) {
	t.Run("verified layered bootstrap is the primary handoff", func(t *testing.T) {
		fixture := newWindowsLayeredFixture(t)

		plan, err := FromInvite(fixture.options)
		if err != nil {
			t.Fatal(err)
		}
		if plan.EntryPackagePlan == nil {
			t.Fatalf("expected a layered entry package plan: %#v", plan)
		}
		if plan.EntryPackagePlan.PackageMode != "private-windows-layered-handoff" ||
			plan.EntryPackagePlan.PlatformPlanKind != "windows-layered-handoff" {
			t.Fatalf("expected the verified layered handoff to be preferred: %#v", plan.EntryPackagePlan)
		}

		launcherPath := plan.EntryPackagePlan.LauncherPath
		launcher := readTestFile(t, launcherPath)
		normalizedLauncher := normalizePowerShellForTest(string(launcher))
		assertStringsInOrder(t, normalizedLauncher,
			"rdev-bootstrap.exe",
			"layered-run",
			"--manifest-url", fixture.options.LayeredAssetsManifestURL,
			"--root-public-key", fixture.options.ReleaseRootPublicKey,
			"--expected-release-version", fixture.options.LayeredReleaseVersion,
			"--platform", "windows/amd64",
			"--cache-dir",
			"--mode", "temporary",
			"--", "serve",
			"--mode", "temporary",
			"--manifest-url", fixture.invite.ManifestURL,
			"--manifest-root-public-key", fixture.invite.ManifestRootPublicKey,
			"--transport", "auto",
			"--once=false",
			"--max-tasks", "0",
		)
		if !strings.Contains(normalizedLauncher, "LocalApplicationData") ||
			!strings.Contains(normalizedLauncher, "RemoteDevSkillkit") ||
			!strings.Contains(normalizedLauncher, "cache") {
			t.Fatalf("launcher must use the current user's LocalApplicationData cache:\n%s", launcher)
		}
		if !strings.Contains(normalizedLauncher, "SHA256") ||
			!strings.Contains(strings.ToLower(normalizedLauncher), fixture.bootstrapSHA256) {
			t.Fatalf("launcher must recheck the controller-verified bootstrap SHA-256:\n%s", launcher)
		}
		for _, want := range []string{
			"FileAttributes]::ReparsePoint",
			"FileShare]::Read",
			"ComputeHash($bootstrapLock)",
			"$bootstrapLock.Length",
			"WindowsIdentity]::GetCurrent().User.Value",
			"icacls.exe",
			"UNC paths are not allowed",
			"ACL grants access to an untrusted identity",
		} {
			if !strings.Contains(normalizedLauncher, want) {
				t.Fatalf("launcher must protect the bootstrap path and user cache with %q:\n%s", want, launcher)
			}
		}
		if strings.Contains(normalizedLauncher, "$writeMask") {
			t.Fatalf("private handoff ACL validation must reject untrusted read ACEs as well as write ACEs:\n%s", launcher)
		}
		if !strings.Contains(normalizedLauncher, "--transport auto") || strings.Contains(normalizedLauncher, "--transport long-poll") {
			t.Fatalf("layered launcher must preserve the runtime's transport fallback policy:\n%s", launcher)
		}
		for _, fallback := range []string{"archive fallback explicitly", "Start-ConnectionEntry.ps1", "verified"} {
			if !strings.Contains(normalizedLauncher, fallback) {
				t.Fatalf("layered failure must name the separately verified archive recovery command %q:\n%s", fallback, launcher)
			}
		}
		for _, forbidden := range []string{"Start-Process", "Start-Job", "WindowStyle Hidden", "--gateway", "--ticket-code"} {
			if strings.Contains(normalizedLauncher, forbidden) {
				t.Fatalf("layered launcher must stay foreground and obtain gateway data from the signed join manifest; found %q:\n%s", forbidden, launcher)
			}
		}

		handoffDir := filepath.Dir(launcherPath)
		if filepath.Base(handoffDir) != "windows-layered" {
			t.Fatalf("expected a focused windows-layered handoff, got %q", handoffDir)
		}
		fallbackPath := filepath.Join(fixture.options.OutDir, "windows-temporary", "Start-ConnectionEntry.ps1")
		fallback := readTestFile(t, fallbackPath)
		for _, want := range []string{"rdev-verify", fixture.options.ReleaseBundleURL, fixture.options.ReleaseRootPublicKey} {
			if !strings.Contains(string(fallback), want) {
				t.Fatalf("verified archive fallback missing %q:\n%s", want, fallback)
			}
		}
		if !strings.Contains(normalizedLauncher, `..\windows-temporary\Start-ConnectionEntry.ps1`) {
			t.Fatalf("layered launcher must pin the sibling fallback path:\n%s", launcher)
		}
		if strings.Contains(normalizedLauncher, "& $fallbackPath") {
			t.Fatalf("layered verification/runtime failures must not automatically execute the archive fallback:\n%s", launcher)
		}
		assertStringsInOrder(t, normalizedLauncher,
			"try {",
			"ComputeHash($bootstrapLock)",
			"layered-run",
			"exit $layeredExitCode",
			"} catch {",
			"Run the verified archive fallback explicitly",
			"exit 1",
		)
		packagedBootstrap := filepath.Join(handoffDir, "rdev-bootstrap.exe")
		if got := readTestFile(t, packagedBootstrap); !bytes.Equal(got, fixture.bootstrap) {
			t.Fatalf("packaged bootstrap is not an exact copy: got %d bytes, want %d", len(got), len(fixture.bootstrap))
		}
		assertPrivateLayeredHandoff(t, handoffDir, fixture)
	})

	t.Run("missing layered prerequisites retains archive launcher", func(t *testing.T) {
		fixture := newWindowsLayeredFixture(t)
		fixture.options.WindowsBootstrapBinaryPath = ""
		fixture.options.WindowsBootstrapReleaseManifestPath = ""
		fixture.options.LayeredAssetsManifestURL = ""

		plan, err := FromInvite(fixture.options)
		if err != nil {
			t.Fatal(err)
		}
		if plan.EntryPackagePlan == nil || plan.EntryPackagePlan.PlatformPlanKind != "windows-temporary-acceptance-plan" {
			t.Fatalf("expected the existing self-contained runner fallback: %#v", plan.EntryPackagePlan)
		}
		if filepath.Base(plan.EntryPackagePlan.LauncherPath) != "Start-ConnectionEntry.ps1" {
			t.Fatalf("expected Start-ConnectionEntry.ps1 fallback, got %#v", plan.EntryPackagePlan)
		}
		launcher := readTestFile(t, plan.EntryPackagePlan.LauncherPath)
		if strings.Contains(string(launcher), "layered-run") {
			t.Fatalf("missing layered inputs must select the existing fallback, not a partial layered launcher:\n%s", launcher)
		}
	})

	partial := []struct {
		name   string
		mutate func(*Options)
	}{
		{
			name: "only bootstrap binary",
			mutate: func(options *Options) {
				options.WindowsBootstrapReleaseManifestPath = ""
				options.LayeredAssetsManifestURL = ""
			},
		},
		{
			name: "bootstrap and release manifest without layered URL",
			mutate: func(options *Options) {
				options.LayeredAssetsManifestURL = ""
			},
		},
		{
			name: "layered inputs without expected release version",
			mutate: func(options *Options) {
				options.LayeredReleaseVersion = ""
			},
		},
		{
			name: "release manifest and layered URL without bootstrap",
			mutate: func(options *Options) {
				options.WindowsBootstrapBinaryPath = ""
			},
		},
	}
	for _, test := range partial {
		t.Run(test.name+" retains archive launcher", func(t *testing.T) {
			fixture := newWindowsLayeredFixture(t)
			test.mutate(&fixture.options)

			plan, err := FromInvite(fixture.options)
			if err != nil {
				t.Fatalf("partial layered inputs must use the verified archive fallback: %v", err)
			}
			if plan.EntryPackagePlan == nil || plan.EntryPackagePlan.PlatformPlanKind != "windows-temporary-acceptance-plan" {
				t.Fatalf("partial layered inputs did not select the verified archive fallback: %#v", plan.EntryPackagePlan)
			}
			assertNoPartialLayeredOutput(t, fixture.options.OutDir)
		})
	}
}

func TestWindowsLayeredConnectionEntryRejectsUnverifiedControllerHandoff(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *windowsLayeredFixture)
	}{
		{
			name: "release key ID mismatch",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				manifest := fixture.manifest
				manifest.SigningKeyID = "other-release-root"
				manifest = signReleaseManifestForTest(t, manifest, fixture.key)
				writeReleaseManifestForTest(t, fixture.options.WindowsBootstrapReleaseManifestPath, manifest)
			},
		},
		{
			name: "invalid release signature",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				manifest := fixture.manifest
				manifest.Signature = "not-a-valid-ed25519-signature"
				writeReleaseManifestForTest(t, fixture.options.WindowsBootstrapReleaseManifestPath, manifest)
			},
		},
		{
			name: "bootstrap digest mismatch",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				manifest := fixture.manifest
				manifest.ArtifactSHA256 = strings.Repeat("0", 64)
				manifest = signReleaseManifestForTest(t, manifest, fixture.key)
				writeReleaseManifestForTest(t, fixture.options.WindowsBootstrapReleaseManifestPath, manifest)
			},
		},
		{
			name: "bootstrap size mismatch",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				manifest := fixture.manifest
				manifest.ArtifactSize++
				manifest = signReleaseManifestForTest(t, manifest, fixture.key)
				writeReleaseManifestForTest(t, fixture.options.WindowsBootstrapReleaseManifestPath, manifest)
			},
		},
		{
			name: "wrong artifact name",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				manifest := fixture.manifest
				manifest.ArtifactName = "not-rdev-bootstrap.exe"
				manifest = signReleaseManifestForTest(t, manifest, fixture.key)
				writeReleaseManifestForTest(t, fixture.options.WindowsBootstrapReleaseManifestPath, manifest)
			},
		},
		{
			name: "signed bootstrap release version mismatch",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				manifest := fixture.manifest
				manifest.ReleaseVersion = "v0.1.0"
				manifest = signReleaseManifestForTest(t, manifest, fixture.key)
				writeReleaseManifestForTest(t, fixture.options.WindowsBootstrapReleaseManifestPath, manifest)
			},
		},
		{
			name: "signed bootstrap target platform mismatch",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				manifest := fixture.manifest
				manifest.TargetPlatform = "windows/arm64"
				manifest = signReleaseManifestForTest(t, manifest, fixture.key)
				writeReleaseManifestForTest(t, fixture.options.WindowsBootstrapReleaseManifestPath, manifest)
			},
		},
		{
			name: "HTTP layered manifest URL",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				fixture.options.LayeredAssetsManifestURL = "http://downloads.example.com/layered-assets.json"
			},
		},
		{
			name: "layered manifest URL with query",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				fixture.options.LayeredAssetsManifestURL = "https://downloads.example.com/layered-assets.json?channel=test"
			},
		},
		{
			name: "layered manifest URL with fragment",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				fixture.options.LayeredAssetsManifestURL = "https://downloads.example.com/layered-assets.json#latest"
			},
		},
		{
			name: "layered inputs supplied without release root",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				fixture.options.ReleaseRootPublicKey = ""
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWindowsLayeredFixture(t)
			test.mutate(t, &fixture)

			if _, err := FromInvite(fixture.options); err == nil {
				t.Fatal("expected controller-side layered handoff verification to fail closed")
			}
			assertNoPartialLayeredOutput(t, fixture.options.OutDir)
		})
	}
}

type windowsLayeredFixture struct {
	options         Options
	invite          agentinvite.Invite
	key             signing.Key
	manifest        release.Manifest
	controllerDir   string
	bootstrap       []byte
	bootstrapSHA256 string
}

func newWindowsLayeredFixture(t *testing.T) windowsLayeredFixture {
	t.Helper()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	controllerDir := filepath.Join(t.TempDir(), "controller-source-private-token-do-not-publish")
	if err := os.MkdirAll(controllerDir, 0o700); err != nil {
		t.Fatal(err)
	}
	bootstrap := []byte("fixture rdev bootstrap executable bytes\n")
	bootstrapPath := filepath.Join(controllerDir, "rdev-bootstrap.exe")
	if err := os.WriteFile(bootstrapPath, bootstrap, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest, err := release.SignArtifactForRelease(bootstrapPath, key, now, "v0.2.0", "windows/amd64")
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(controllerDir, "rdev-bootstrap.exe.rdev-release.json")
	writeReleaseManifestForTest(t, manifestPath, manifest)
	root := key.ID + ":" + base64.RawURLEncoding.EncodeToString(key.PublicKey)
	invite := testInvite(t, model.HostModeAttendedTemporary)
	digest := sha256.Sum256(bootstrap)

	return windowsLayeredFixture{
		options: Options{
			InviteJSON:                          mustJSON(t, invite),
			OutDir:                              filepath.Join(t.TempDir(), "entry"),
			TargetOS:                            "windows",
			TargetArch:                          "amd64",
			Ownership:                           "third-party",
			SessionMode:                         string(model.HostModeAttendedTemporary),
			WindowsBootstrapBinaryPath:          bootstrapPath,
			WindowsBootstrapReleaseManifestPath: manifestPath,
			LayeredAssetsManifestURL:            "https://downloads.example.com/layered-assets.json",
			LayeredReleaseVersion:               "v0.2.0",
			WindowsBootstrapScriptPath:          filepath.Join("..", "..", "scripts", "bootstrap", "windows-temporary.ps1"),
			WindowsHostDownloadURL:              "https://downloads.example.com/rdev-host.exe",
			WindowsHostExpectedSHA256:           strings.Repeat("a", 64),
			ReleaseBundleURL:                    "https://downloads.example.com/release-bundle.json",
			ReleaseRootPublicKey:                root,
			WindowsVerifierDownloadURL:          "https://downloads.example.com/rdev-verify.exe",
			WindowsVerifierExpectedSHA256:       strings.Repeat("b", 64),
			Now:                                 now,
		},
		invite:          invite,
		key:             key,
		manifest:        manifest,
		controllerDir:   controllerDir,
		bootstrap:       bootstrap,
		bootstrapSHA256: hex.EncodeToString(digest[:]),
	}
}

func signReleaseManifestForTest(t *testing.T, manifest release.Manifest, key signing.Key) release.Manifest {
	t.Helper()
	signed, err := manifest.Sign(key.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func writeReleaseManifestForTest(t *testing.T, path string, manifest release.Manifest) {
	t.Helper()
	if err := release.WriteManifest(path, manifest); err != nil {
		t.Fatal(err)
	}
}

func normalizePowerShellForTest(content string) string {
	normalized := strings.NewReplacer("'", "", "\"", "", "`", "").Replace(content)
	return strings.Join(strings.Fields(normalized), " ")
}

func assertStringsInOrder(t *testing.T, content string, expected ...string) {
	t.Helper()
	offset := 0
	for _, value := range expected {
		index := strings.Index(content[offset:], value)
		if index < 0 {
			t.Fatalf("expected %q after byte %d in launcher:\n%s", value, offset, content)
		}
		offset += index + len(value)
	}
}

func assertPrivateLayeredHandoff(t *testing.T, handoffDir string, fixture windowsLayeredFixture) {
	t.Helper()
	metadataFound := false
	checksumFound := false
	err := filepath.Walk(handoffDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("layered handoff path must be private, got mode %o for %s", info.Mode().Perm(), path)
		}
		if info.IsDir() {
			return nil
		}
		name := strings.ToLower(info.Name())
		if strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".sha256") {
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, forbidden := range []string{
				fixture.controllerDir,
				filepath.ToSlash(fixture.controllerDir),
				fixture.invite.Ticket.Code,
				fixture.invite.GatewayURL,
				"controller-source-private-token-do-not-publish",
			} {
				if strings.Contains(string(content), forbidden) {
					t.Errorf("public/release-like handoff metadata %s leaked %q", path, forbidden)
				}
			}
			metadataFound = metadataFound || strings.HasSuffix(name, ".json")
			if strings.HasSuffix(name, ".sha256") {
				checksumFound = true
				if !strings.Contains(strings.ToLower(string(content)), fixture.bootstrapSHA256) {
					t.Errorf("checksum file %s does not pin the packaged bootstrap digest", path)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !metadataFound || !checksumFound {
		t.Fatalf("layered handoff must include non-sensitive release verification metadata and a checksum file; metadata=%t checksum=%t", metadataFound, checksumFound)
	}
}

func assertNoPartialLayeredOutput(t *testing.T, outDir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(outDir, "windows-layered")); err == nil {
		t.Fatalf("verification failure left a partial windows-layered handoff in %s", outDir)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(outDir); os.IsNotExist(err) {
		return
	} else if err != nil {
		t.Fatal(err)
	}
	err := filepath.Walk(outDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.EqualFold(info.Name(), "rdev-bootstrap.exe") {
			t.Errorf("verification failure copied a bootstrap to %s", path)
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(bytes.ToLower(content), []byte("rdev-bootstrap.exe")) && bytes.Contains(content, []byte("layered-run")) {
			t.Errorf("verification failure wrote a layered launcher to %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
