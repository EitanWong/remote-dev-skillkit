package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	CheckSchemaVersion = "rdev.update-check.v1"
	PlanSchemaVersion  = "rdev.update-plan.v1"
	DefaultAPIBaseURL  = "https://api.github.com"
	DefaultRepo        = "EitanWong/remote-dev-skillkit"
	GitHubAPIVersion   = "2026-03-10"
)

type Options struct {
	Repo           string
	APIBaseURL     string
	CurrentVersion string
	Platform       string
	Token          string
	Now            time.Time
}

type Check struct {
	SchemaVersion   string         `json:"schema_version"`
	GeneratedAt     time.Time      `json:"generated_at"`
	Repo            string         `json:"repo"`
	APIURL          string         `json:"api_url"`
	CurrentVersion  string         `json:"current_version"`
	LatestVersion   string         `json:"latest_version"`
	UpdateAvailable bool           `json:"update_available"`
	Release         Release        `json:"release"`
	Assets          []ReleaseAsset `json:"assets"`
	Checks          []CheckResult  `json:"checks"`
}

type Plan struct {
	SchemaVersion      string        `json:"schema_version"`
	GeneratedAt        time.Time     `json:"generated_at"`
	Repo               string        `json:"repo"`
	CurrentVersion     string        `json:"current_version"`
	LatestVersion      string        `json:"latest_version"`
	UpdateAvailable    bool          `json:"update_available"`
	Platform           string        `json:"platform"`
	SelectedArchive    *ReleaseAsset `json:"selected_archive,omitempty"`
	ReleaseBundle      *ReleaseAsset `json:"release_bundle,omitempty"`
	ReleaseIndex       *ReleaseAsset `json:"release_index,omitempty"`
	SkillkitArchive    *ReleaseAsset `json:"skillkit_archive,omitempty"`
	DownloadCommands   []string      `json:"download_commands"`
	VerificationSteps  []string      `json:"verification_steps"`
	RecommendedActions []string      `json:"recommended_actions"`
	Checks             []CheckResult `json:"checks"`
}

type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name,omitempty"`
	HTMLURL     string    `json:"html_url"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"published_at,omitempty"`
}

type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest,omitempty"`
	Size               int64  `json:"size"`
	ContentType        string `json:"content_type,omitempty"`
}

type CheckResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type githubRelease struct {
	TagName     string               `json:"tag_name"`
	Name        string               `json:"name"`
	HTMLURL     string               `json:"html_url"`
	Prerelease  bool                 `json:"prerelease"`
	Draft       bool                 `json:"draft"`
	PublishedAt time.Time            `json:"published_at"`
	Assets      []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
	Size               int64  `json:"size"`
	ContentType        string `json:"content_type"`
}

func CheckLatest(ctx context.Context, client *http.Client, opts Options) (Check, error) {
	opts = normalizeOptions(opts)
	if client == nil {
		client = http.DefaultClient
	}
	apiURL, err := latestReleaseURL(opts.APIBaseURL, opts.Repo)
	if err != nil {
		return Check{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	result := Check{
		SchemaVersion:  CheckSchemaVersion,
		GeneratedAt:    now.UTC(),
		Repo:           opts.Repo,
		APIURL:         apiURL,
		CurrentVersion: opts.CurrentVersion,
	}
	add := func(name string, passed bool, detail string) {
		result.Checks = append(result.Checks, CheckResult{Name: name, Passed: passed, Detail: detail})
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return Check{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", GitHubAPIVersion)
	if strings.TrimSpace(opts.Token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(opts.Token))
	}
	resp, err := client.Do(req)
	if err != nil {
		return Check{}, err
	}
	defer resp.Body.Close()
	add("github_latest_release_reachable", resp.StatusCode == http.StatusOK, resp.Status)
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("fetch latest release failed: %s", resp.Status)
	}
	var payload githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Check{}, err
	}
	result.Release = Release{
		TagName:     payload.TagName,
		Name:        payload.Name,
		HTMLURL:     payload.HTMLURL,
		Prerelease:  payload.Prerelease,
		Draft:       payload.Draft,
		PublishedAt: payload.PublishedAt,
	}
	result.LatestVersion = payload.TagName
	for _, asset := range payload.Assets {
		result.Assets = append(result.Assets, ReleaseAsset{
			Name:               asset.Name,
			BrowserDownloadURL: asset.BrowserDownloadURL,
			Digest:             asset.Digest,
			Size:               asset.Size,
			ContentType:        asset.ContentType,
		})
	}
	add("release_tag_present", strings.TrimSpace(payload.TagName) != "", payload.TagName)
	add("release_not_draft", !payload.Draft, fmt.Sprintf("%v", payload.Draft))
	add("release_not_prerelease", !payload.Prerelease, fmt.Sprintf("%v", payload.Prerelease))
	add("release_assets_present", len(result.Assets) > 0, fmt.Sprintf("%d", len(result.Assets)))
	result.UpdateAvailable = IsNewerVersion(opts.CurrentVersion, payload.TagName)
	add("version_comparison_complete", true, fmt.Sprintf("%s -> %s", opts.CurrentVersion, payload.TagName))
	sort.Slice(result.Assets, func(i, j int) bool { return result.Assets[i].Name < result.Assets[j].Name })
	return result, nil
}

func PlanFromCheck(check Check, opts Options) Plan {
	opts = normalizeOptions(opts)
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	platform := opts.Platform
	if strings.TrimSpace(platform) == "" {
		platform = runtime.GOOS + "/" + runtime.GOARCH
	}
	plan := Plan{
		SchemaVersion:   PlanSchemaVersion,
		GeneratedAt:     now.UTC(),
		Repo:            check.Repo,
		CurrentVersion:  check.CurrentVersion,
		LatestVersion:   check.LatestVersion,
		UpdateAvailable: check.UpdateAvailable,
		Platform:        platform,
	}
	add := func(name string, passed bool, detail string) {
		plan.Checks = append(plan.Checks, CheckResult{Name: name, Passed: passed, Detail: detail})
	}
	selected := selectPlatformArchive(check.Assets, platform)
	if selected != nil {
		plan.SelectedArchive = selected
		plan.DownloadCommands = append(plan.DownloadCommands, curlCommand(*selected))
		plan.VerificationSteps = append(plan.VerificationSteps, checksumCommand(*selected))
		plan.VerificationSteps = append(plan.VerificationSteps, "after extraction: rdev release verify-bundle --bundle release-bundle.json --root-public-key <release-root-public-key>")
	}
	bundle := findAsset(check.Assets, "release-bundle.json")
	if bundle != nil {
		plan.ReleaseBundle = bundle
		plan.DownloadCommands = append(plan.DownloadCommands, curlCommand(*bundle))
	}
	index := findAsset(check.Assets, "platform-release-index")
	if index != nil {
		plan.ReleaseIndex = index
		plan.DownloadCommands = append(plan.DownloadCommands, curlCommand(*index))
	}
	skillkit := findAsset(check.Assets, "skillkit")
	if skillkit != nil {
		plan.SkillkitArchive = skillkit
	}
	add("latest_release_known", strings.TrimSpace(check.LatestVersion) != "", check.LatestVersion)
	add("platform_archive_selected", selected != nil, platform)
	add("selected_archive_has_digest", selected != nil && strings.HasPrefix(selected.Digest, "sha256:"), digestDetail(selected))
	add("release_bundle_asset_present", bundle != nil, assetName(bundle))
	add("plan_is_dry_run", true, "no files are modified by update plan")
	plan.RecommendedActions = []string{
		"Review this plan before changing installed binaries or services.",
		"Download the selected archive and verify SHA-256 before extraction.",
		"Verify release-bundle.json with rdev release verify-bundle and the configured release root public key before replacing any running binary.",
		"Stop managed services before replacement, keep the previous binary as rollback, then restart and run rdev version plus rdev doctor.",
	}
	if !check.UpdateAvailable {
		plan.RecommendedActions = append([]string{"No newer full GitHub release was detected for the configured repository."}, plan.RecommendedActions...)
	}
	return plan
}

func IsNewerVersion(current, latest string) bool {
	c, cok := parseVersion(current)
	l, lok := parseVersion(latest)
	if !lok {
		return false
	}
	if !cok {
		return strings.TrimSpace(latest) != "" && strings.TrimSpace(latest) != strings.TrimSpace(current)
	}
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func normalizeOptions(opts Options) Options {
	if strings.TrimSpace(opts.Repo) == "" {
		opts.Repo = DefaultRepo
	}
	if strings.TrimSpace(opts.APIBaseURL) == "" {
		opts.APIBaseURL = DefaultAPIBaseURL
	}
	return opts
}

func latestReleaseURL(base, repo string) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return "", fmt.Errorf("api base URL is required")
	}
	parts := strings.Split(strings.Trim(strings.TrimSpace(repo), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("repo must be OWNER/REPO")
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/releases/latest"
	return u.String(), nil
}

func parseVersion(value string) ([3]int, bool) {
	var result [3]int
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	if idx := strings.IndexAny(value, "-+"); idx >= 0 {
		value = value[:idx]
	}
	parts := strings.Split(value, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return result, false
	}
	for i, part := range parts {
		if part == "" {
			return result, false
		}
		n := 0
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return result, false
			}
			n = n*10 + int(ch-'0')
		}
		result[i] = n
	}
	return result, true
}

func selectPlatformArchive(assets []ReleaseAsset, platform string) *ReleaseAsset {
	slug := strings.NewReplacer("/", "-", "_", "-").Replace(strings.ToLower(strings.TrimSpace(platform)))
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if strings.Contains(name, slug) && (strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".zip")) {
			copied := asset
			return &copied
		}
	}
	return nil
}

func findAsset(assets []ReleaseAsset, needle string) *ReleaseAsset {
	needle = strings.ToLower(needle)
	for _, asset := range assets {
		if strings.Contains(strings.ToLower(asset.Name), needle) {
			copied := asset
			return &copied
		}
	}
	return nil
}

func curlCommand(asset ReleaseAsset) string {
	return fmt.Sprintf("curl -fL -o %s %s", shellQuote(asset.Name), shellQuote(asset.BrowserDownloadURL))
}

func checksumCommand(asset ReleaseAsset) string {
	if !strings.HasPrefix(asset.Digest, "sha256:") {
		return "# asset digest is missing; verify against release-bundle.json before use"
	}
	return fmt.Sprintf("printf '%%s  %%s\\n' %s %s | shasum -a 256 -c -", shellQuote(strings.TrimPrefix(asset.Digest, "sha256:")), shellQuote(asset.Name))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func digestDetail(asset *ReleaseAsset) string {
	if asset == nil {
		return ""
	}
	return asset.Digest
}

func assetName(asset *ReleaseAsset) string {
	if asset == nil {
		return ""
	}
	return asset.Name
}
