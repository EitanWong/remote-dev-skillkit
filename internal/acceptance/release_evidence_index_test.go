package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/hostedprovider"
	"github.com/EitanWong/remote-dev-skillkit/internal/relayadapter"
)

func TestBuildReleaseEvidenceIndex(t *testing.T) {
	root := t.TempDir()
	hostedPackage := writeHostedRuntimePackageForIndexTest(t, root)
	relayPackage := writeRelayPackageForIndexTest(t, root)
	postReleasePackage := writePostReleasePackageForIndexTest(t, root)

	index, err := BuildReleaseEvidenceIndex(ReleaseEvidenceIndexOptions{
		OutDir:                           filepath.Join(root, "release-evidence-index"),
		HostedProviderRuntimePackagePath: hostedPackage,
		RelayAdapterPackagePaths:         []string{relayPackage},
		PostReleaseDownloadPackagePath:   postReleasePackage,
		Now:                              time.Date(2026, 7, 5, 3, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !index.OK || !index.Complete() {
		t.Fatalf("expected release evidence index ok: %#v", index.Checks)
	}
	if index.HostedProviderRuntime == nil || !index.HostedProviderRuntime.OK ||
		index.PostReleaseDownload == nil || !index.PostReleaseDownload.OK ||
		len(index.RelayAdapters) != 1 || !index.RelayAdapters[0].OK {
		t.Fatalf("unexpected index items: %#v", index)
	}
	if index.OutDir != "." {
		t.Fatalf("index should be package-relative, got %q", index.OutDir)
	}
	content, err := os.ReadFile(filepath.Join(root, "release-evidence-index", "release-evidence-index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), root) {
		t.Fatalf("release evidence index should not archive local temp paths: %s", string(content))
	}
	if _, err := os.Stat(filepath.Join(root, "release-evidence-index", "checksums.txt")); err != nil {
		t.Fatalf("expected checksums: %v", err)
	}
}

func TestBuildReleaseEvidenceIndexReportsMissingGates(t *testing.T) {
	root := t.TempDir()
	index, err := BuildReleaseEvidenceIndex(ReleaseEvidenceIndexOptions{
		OutDir: filepath.Join(root, "release-evidence-index"),
		Now:    time.Date(2026, 7, 5, 3, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if index.OK {
		t.Fatal("expected missing release evidence packages to fail closed")
	}
	failures := failedReleaseEvidenceIndexChecks(index.Checks)
	for _, expected := range []string{
		"hosted_provider_runtime_package_present",
		"relay_adapter_package_present",
		"post_release_download_package_present",
	} {
		if !strings.Contains(failures, expected) {
			t.Fatalf("expected failure %q in %s", expected, failures)
		}
	}
}

func writeHostedRuntimePackageForIndexTest(t *testing.T, root string) string {
	t.Helper()
	providerDir := filepath.Join(root, "hosted-provider")
	if _, err := hostedprovider.Build(hostedprovider.Options{
		OutDir:          providerDir,
		Name:            "external-hosted-runtime",
		StorageProvider: "postgres",
		AuthProvider:    "oidc-jwks",
		GeneratedAt:     time.Date(2026, 7, 5, 2, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	evidence := writeHostedProviderRuntimeEvidenceForTest(t, root, "postgres", "oidc-jwks")
	pkg, err := PackageHostedProviderRuntimeEvidence(HostedProviderRuntimePackageOptions{
		HostedProviderPackagePath: providerDir,
		OutDir:                    filepath.Join(root, "hosted-runtime-package"),
		EvidenceDirPath:           evidence.dir,
		Now:                       time.Date(2026, 7, 5, 2, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("hosted runtime package should be ok: %#v", pkg.Checks)
	}
	return pkg.OutDir
}

func writeRelayPackageForIndexTest(t *testing.T, root string) string {
	t.Helper()
	relayDir := filepath.Join(root, "relay-adapter")
	if _, err := relayadapter.Build(relayadapter.Options{
		OutDir:      relayDir,
		AdapterKind: "wireguard",
		GeneratedAt: time.Date(2026, 7, 5, 2, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	evidence := writeRelayAdapterEvidenceForTest(t, root, "existing-wireguard-vpn")
	pkg, err := PackageRelayAdapterEvidence(RelayAdapterPackageOptions{
		RelayAdapterPackagePath: relayDir,
		OutDir:                  filepath.Join(root, "relay-package"),
		EvidenceDirPath:         evidence.dir,
		Now:                     time.Date(2026, 7, 5, 2, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("relay package should be ok: %#v", pkg.Checks)
	}
	return pkg.OutDir
}

func writePostReleasePackageForIndexTest(t *testing.T, root string) string {
	t.Helper()
	fixture := writePostReleaseDownloadFixture(t, root, []string{"linux/amd64", "windows/amd64"}, true)
	scaffoldDir := filepath.Join(root, "post-release-scaffold")
	if _, err := ScaffoldPostReleaseDownloadEvidence(PostReleaseDownloadScaffoldOptions{
		PlanPath:             fixture.plan,
		PlanVerificationPath: fixture.planVerification,
		OutDir:               scaffoldDir,
		CreatePlaceholders:   true,
		Now:                  time.Date(2026, 7, 5, 2, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"linux-amd64-transcript.txt",
		"linux-amd64-candidate-verify.json",
		"linux-amd64-bundle-verify.json",
		"windows-amd64-transcript.txt",
		"windows-amd64-candidate-verify.json",
		"windows-amd64-bundle-verify.json",
	} {
		copyEvidenceFile(t, filepath.Join(fixture.evidenceDir, name), filepath.Join(scaffoldDir, "platform-download-evidence", name))
	}
	copyEvidenceFile(t, filepath.Join(fixture.skillkitDir, "skillkit-transcript.txt"), filepath.Join(scaffoldDir, "skillkit-download-evidence", "skillkit-transcript.txt"))
	copyEvidenceFile(t, filepath.Join(fixture.skillkitDir, "skillkit-verify.json"), filepath.Join(scaffoldDir, "skillkit-download-evidence", "skillkit-verify.json"))
	pkg, err := PackagePostReleaseDownloadEvidence(PostReleaseDownloadPackageOptions{
		ScaffoldPath: scaffoldDir,
		OutDir:       filepath.Join(root, "post-release-package"),
		Now:          time.Date(2026, 7, 5, 2, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("post-release package should be ok: %#v", pkg.Checks)
	}
	return pkg.OutDir
}

func failedReleaseEvidenceIndexChecks(checks []Check) string {
	var failed []string
	for _, check := range checks {
		if !check.Passed {
			failed = append(failed, check.Name)
		}
	}
	return strings.Join(failed, ",")
}
