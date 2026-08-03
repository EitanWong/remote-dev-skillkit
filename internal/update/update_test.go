package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsNewerVersion(t *testing.T) {
	cases := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{"newer minor", "v1.2.3", "v1.3.0", true},
		{"newer patch", "v1.2.3", "v1.2.4", true},
		{"newer major", "v1.9.9", "v2.0.0", true},
		{"same", "v1.2.3", "v1.2.3", false},
		{"older", "v1.2.4", "v1.2.3", false},
		{"prerelease newer core", "v1.2.3", "v1.2.4-beta.1", true},
		{"prerelease same core", "v1.2.3", "v1.2.3-rc.1", false},
		{"no v prefix", "1.2.3", "1.2.4", true},
		{"empty current", "", "v1.0.0", true},
		{"empty latest", "v1.0.0", "", false},
		{"garbage latest", "v1.0.0", "not-a-version", false},
		{"garbage current", "garbage", "v1.0.0", true},
		{"two part", "v1.2", "v1.3", true},
		{"leading zeros", "v1.02.3", "v1.2.4", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNewerVersion(tc.current, tc.latest); got != tc.want {
				t.Fatalf("IsNewerVersion(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
			}
		})
	}
}

func TestParseVersionRejectsMalformed(t *testing.T) {
	for _, value := range []string{"", "v", "v1..2", "v1.2.3.4", "v1.2.x", "v1.-2", "abc"} {
		if _, ok := parseVersion(value); ok {
			t.Fatalf("parseVersion(%q) should fail", value)
		}
	}
}

func TestParseVersionStripsSuffixAtSeparator(t *testing.T) {
	for value, want := range map[string][3]int{
		"1.2.3-":   {1, 2, 3},
		"1.2.3+b":  {1, 2, 3},
		"1.2.3-rc": {1, 2, 3},
	} {
		got, ok := parseVersion(value)
		if !ok || got != want {
			t.Fatalf("parseVersion(%q) = %v, %v; want %v", value, got, ok, want)
		}
	}
}

func TestLatestReleaseURL(t *testing.T) {
	got, err := latestReleaseURL("https://api.github.com", "EitanWong/remote-dev-skillkit")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://api.github.com/repos/EitanWong/remote-dev-skillkit/releases/latest"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}

	got, err = latestReleaseURL("https://api.example.test/", "owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://api.example.test/repos/owner/repo/releases/latest" {
		t.Fatalf("trailing slash handling wrong: %q", got)
	}

	if _, err := latestReleaseURL("", "owner/repo"); err == nil {
		t.Fatal("empty base should fail")
	}
	if _, err := latestReleaseURL("https://api.example.test", "owner"); err == nil {
		t.Fatal("single segment repo should fail")
	}
	if _, err := latestReleaseURL("https://api.example.test", "a/b/c"); err == nil {
		t.Fatal("three segment repo should fail")
	}
}

func TestCheckLatestPopulatesCheckFromServer(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	var gotAuthHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		if r.Header.Get("X-GitHub-Api-Version") != GitHubAPIVersion {
			t.Errorf("missing X-GitHub-Api-Version header")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name":     "v2.1.0",
			"name":         "release two one",
			"html_url":     "https://github.com/EitanWong/remote-dev-skillkit/releases/tag/v2.1.0",
			"prerelease":   false,
			"draft":        false,
			"published_at": now.Add(-time.Hour).Format(time.RFC3339),
			"assets": []map[string]any{
				{"name": "z-asset.zip", "browser_download_url": "https://example.test/z.zip", "digest": "sha256:abc", "size": 10, "content_type": "application/zip"},
				{"name": "a-asset.tar.gz", "browser_download_url": "https://example.test/a.tgz", "digest": "sha256:def", "size": 20, "content_type": "application/gzip"},
			},
		})
	}))
	defer server.Close()

	check, err := CheckLatest(context.Background(), server.Client(), Options{
		Repo:           "EitanWong/remote-dev-skillkit",
		APIBaseURL:     server.URL,
		CurrentVersion: "v2.0.0",
		Token:          "  secret-token  ",
		Now:            now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuthHeader != "Bearer secret-token" {
		t.Fatalf("Authorization header = %q", gotAuthHeader)
	}
	if !check.UpdateAvailable {
		t.Fatal("expected update available")
	}
	if check.LatestVersion != "v2.1.0" {
		t.Fatalf("latest version = %q", check.LatestVersion)
	}
	if len(check.Assets) != 2 || check.Assets[0].Name != "a-asset.tar.gz" || check.Assets[1].Name != "z-asset.zip" {
		t.Fatalf("assets not sorted: %#v", check.Assets)
	}
	if len(check.Checks) == 0 || !check.Checks[0].Passed {
		t.Fatalf("expected reachability check recorded: %#v", check.Checks)
	}
}

func TestCheckLatestRecordsNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	check, err := CheckLatest(context.Background(), server.Client(), Options{
		Repo:       "owner/repo",
		APIBaseURL: server.URL,
	})
	if err == nil {
		t.Fatal("expected error for non-200")
	}
	if len(check.Checks) != 1 || check.Checks[0].Name != "github_latest_release_reachable" || check.Checks[0].Passed {
		t.Fatalf("expected failed reachability check, got %#v", check.Checks)
	}
}

func TestCheckLatestRejectsInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	}))
	defer server.Close()

	if _, err := CheckLatest(context.Background(), server.Client(), Options{
		Repo:       "owner/repo",
		APIBaseURL: server.URL,
	}); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestCheckLatestRejectsBadRepoInURL(t *testing.T) {
	if _, err := CheckLatest(context.Background(), nil, Options{APIBaseURL: "https://api.example.test"}); err == nil {
		t.Fatal("expected repo validation error")
	}
}

func TestPlanFromCheckSelectsPlatformArchive(t *testing.T) {
	assets := []ReleaseAsset{
		{Name: "rdev-windows-amd64.zip", BrowserDownloadURL: "https://example.test/w.zip", Digest: "sha256:" + strings.Repeat("ab", 32), Size: 1},
		{Name: "rdev-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.test/l.tgz", Digest: "sha256:" + strings.Repeat("cd", 32), Size: 2},
		{Name: "release-bundle.json", BrowserDownloadURL: "https://example.test/bundle.json", Digest: "", Size: 3},
		{Name: "platform-release-index", BrowserDownloadURL: "https://example.test/index", Digest: "", Size: 4},
		{Name: "skillkit", BrowserDownloadURL: "https://example.test/skillkit.tgz", Digest: "", Size: 5},
	}
	check := Check{
		Repo:            "owner/repo",
		CurrentVersion:  "v1.0.0",
		LatestVersion:   "v1.1.0",
		UpdateAvailable: true,
		Assets:          assets,
	}
	plan := PlanFromCheck(check, Options{Platform: "windows/amd64"})

	if plan.SelectedArchive == nil || plan.SelectedArchive.Name != "rdev-windows-amd64.zip" {
		t.Fatalf("expected windows archive selected, got %#v", plan.SelectedArchive)
	}
	if plan.ReleaseBundle == nil || plan.ReleaseIndex == nil || plan.SkillkitArchive == nil {
		t.Fatal("expected bundle/index/skillkit assets detected")
	}
	if len(plan.DownloadCommands) != 3 {
		t.Fatalf("expected 3 download commands, got %d: %#v", len(plan.DownloadCommands), plan.DownloadCommands)
	}
	foundChecksum := false
	for _, step := range plan.VerificationSteps {
		if strings.HasPrefix(step, "printf '") && strings.Contains(step, "shasum -a 256") {
			foundChecksum = true
		}
	}
	if !foundChecksum {
		t.Fatalf("expected sha256 verification step, got %#v", plan.VerificationSteps)
	}
	hasBundleVerify := false
	for _, step := range plan.VerificationSteps {
		if strings.Contains(step, "verify-bundle") {
			hasBundleVerify = true
		}
	}
	if !hasBundleVerify {
		t.Fatalf("expected release bundle verify step, got %#v", plan.VerificationSteps)
	}
}

func TestPlanFromCheckNoPlatformMatch(t *testing.T) {
	check := Check{
		LatestVersion:   "v1.1.0",
		UpdateAvailable: true,
		Assets: []ReleaseAsset{
			{Name: "rdev-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.test/l.tgz", Digest: "sha256:" + strings.Repeat("cd", 32)},
		},
	}
	plan := PlanFromCheck(check, Options{Platform: "darwin/arm64"})
	if plan.SelectedArchive != nil {
		t.Fatal("expected no archive selected for unsupported platform")
	}
	found := false
	for _, result := range plan.Checks {
		if result.Name == "platform_archive_selected" && !result.Passed {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected failed platform_archive_selected check: %#v", plan.Checks)
	}
	if len(plan.DownloadCommands) != 0 {
		t.Fatalf("expected no download commands, got %#v", plan.DownloadCommands)
	}
}

func TestPlanFromCheckNoUpdateRecommendedActions(t *testing.T) {
	plan := PlanFromCheck(Check{LatestVersion: "v1.0.0", CurrentVersion: "v1.0.0"}, Options{})
	if len(plan.RecommendedActions) == 0 || !strings.Contains(plan.RecommendedActions[0], "No newer") {
		t.Fatalf("expected no-update banner first, got %#v", plan.RecommendedActions)
	}
}

func TestPlanFromCheckMissingDigestProducesWarning(t *testing.T) {
	check := Check{
		LatestVersion:   "v1.1.0",
		UpdateAvailable: true,
		Assets: []ReleaseAsset{
			{Name: "rdev-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.test/l.tgz", Digest: ""},
		},
	}
	plan := PlanFromCheck(check, Options{Platform: "linux/amd64"})
	if len(plan.VerificationSteps) == 0 || !strings.Contains(plan.VerificationSteps[0], "digest is missing") {
		t.Fatalf("expected missing-digest warning, got %#v", plan.VerificationSteps)
	}
	found := false
	for _, result := range plan.Checks {
		if result.Name == "selected_archive_has_digest" && !result.Passed {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected failed digest check: %#v", plan.Checks)
	}
}

func TestPlanDefaultsPlatformToRuntime(t *testing.T) {
	check := Check{LatestVersion: "v1.1.0", UpdateAvailable: true, Assets: nil}
	plan := PlanFromCheck(check, Options{})
	if plan.Platform == "" {
		t.Fatal("expected platform defaulted")
	}
}

func TestShellQuoteEscapesApostrophes(t *testing.T) {
	cases := map[string]string{
		"plain.zip":           "'plain.zip'",
		"a'b.zip":             "'a'\"'\"'b.zip'",
		"rdev; rm -rf / .zip": "'rdev; rm -rf / .zip'",
	}
	for input, want := range cases {
		if got := shellQuote(input); got != want {
			t.Fatalf("shellQuote(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCurlCommandQuotesNameAndURL(t *testing.T) {
	asset := ReleaseAsset{Name: "rdev; rm -rf / .zip", BrowserDownloadURL: "https://example.test/a'b.zip"}
	command := curlCommand(asset)
	if !strings.Contains(command, "'rdev; rm -rf / .zip'") {
		t.Fatalf("asset name must be quoted: %q", command)
	}
	if !strings.Contains(command, "'https://example.test/a'\"'\"'b.zip'") {
		t.Fatalf("URL must be quoted with escaped apostrophe: %q", command)
	}
	if strings.HasPrefix(strings.TrimSpace(command), "curl") == false {
		t.Fatalf("expected curl command: %q", command)
	}
}

func TestSelectPlatformArchiveSlugNormalization(t *testing.T) {
	assets := []ReleaseAsset{
		{Name: "rdev-windows-amd64.zip", BrowserDownloadURL: "u"},
		{Name: "rdev-windows_arm64.zip", BrowserDownloadURL: "u"},
	}
	if got := selectPlatformArchive(assets, "Windows/AMD64"); got == nil || got.Name != "rdev-windows-amd64.zip" {
		t.Fatalf("expected case-insensitive windows-amd64 match, got %#v", got)
	}
	if got := selectPlatformArchive(assets, "windows/arm64"); got == nil || got.Name != "rdev-windows_arm64.zip" {
		t.Fatalf("expected underscore slug match, got %#v", got)
	}
	if got := selectPlatformArchive(assets, "darwin/arm64"); got != nil {
		t.Fatalf("expected no match, got %#v", got)
	}
}
