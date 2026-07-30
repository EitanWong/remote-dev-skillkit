package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/operatorauth"
)

const (
	defaultWebHandoffTTL        = 30 * time.Minute
	minimumWebHandoffTTL        = time.Minute
	maximumWebHandoffTTL        = 24 * time.Hour
	defaultArtifactTicketTTL    = 15 * time.Minute
	maxWebHandoffRequestBytes   = 4 << 10
	webHandoffArtifactTicketKey = "X-Rdev-Handoff-Ticket"
)

type webHandoffPageCopy struct {
	Title                 string            `json:"title"`
	Heading               string            `json:"heading"`
	Description           string            `json:"description"`
	DetectedPlatform      string            `json:"detectedPlatform"`
	PlatformNames         map[string]string `json:"platformNames"`
	WindowsSteps          []string          `json:"windowsSteps"`
	NonWindowsHeading     string            `json:"nonWindowsHeading"`
	NonWindowsDescription string            `json:"nonWindowsDescription"`
	NonWindowsSteps       []string          `json:"nonWindowsSteps"`
	CopyAction            string            `json:"copyAction"`
	WindowsOnly           string            `json:"windowsOnly"`
	Preparing             string            `json:"preparing"`
	Ready                 string            `json:"ready"`
	ClaimFailed           string            `json:"claimFailed"`
	Copied                string            `json:"copied"`
	CopyManual            string            `json:"copyManual"`
	UnexpectedBootstrap   string            `json:"unexpectedBootstrap"`
}

var webHandoffPageCopies = map[string]webHandoffPageCopy{
	"en": {
		Title:                 "Remote Dev Host Connection",
		Heading:               "Connect this Windows host",
		Description:           "This page prepares a short-lived managed-host installer. Copy it, paste it into an already-open PowerShell window, and press Enter. The connector is fetched, verified, and installed as a Windows service after visible approval.",
		DetectedPlatform:      "Detected system: {platform}.",
		PlatformNames:         map[string]string{"windows": "Windows", "macos": "macOS", "linux": "Linux", "other": "an unsupported system"},
		WindowsSteps:          []string{"Open Windows PowerShell.", "Click “Copy connection command” below.", "Paste it into the already-open PowerShell window and press Enter.", "Approve the visible Windows service installation; then you may close PowerShell."},
		NonWindowsHeading:     "Open this handoff on the target Windows host",
		NonWindowsDescription: "This link prepares a Windows connector, but this browser is running on {platform}.",
		NonWindowsSteps:       []string{"Do not claim this link on this device.", "Open the same link on the target Windows computer.", "Then follow the Windows connection steps shown there."},
		CopyAction:            "Copy connection command",
		WindowsOnly:           "This handoff has not been claimed. Open the same link on the target Windows computer.",
		Preparing:             "Preparing connection command…",
		Ready:                 "Connection command ready. Copy it, paste it into the open PowerShell window, and press Enter.",
		ClaimFailed:           "The handoff could not be claimed. Ask the operator for a fresh link.",
		Copied:                "Copied. Paste into the open PowerShell window and press Enter.",
		CopyManual:            "Copy is unavailable. The command is selected below; copy it, then paste it into the open PowerShell window and press Enter.",
		UnexpectedBootstrap:   "The handoff response did not include a connection command.",
	},
	"zh-Hans": {
		Title:                 "远程开发主机连接",
		Heading:               "连接这台 Windows 主机",
		Description:           "此页面会准备一条短期托管主机安装命令。复制后粘贴到当前已打开的 PowerShell 窗口并按 Enter；连接器会自动获取、校验，并在可见确认后安装为 Windows 服务。",
		DetectedPlatform:      "已检测到系统：{platform}。",
		PlatformNames:         map[string]string{"windows": "Windows", "macos": "macOS", "linux": "Linux", "other": "不受支持的系统"},
		WindowsSteps:          []string{"打开 Windows PowerShell。", "点击下方“复制连接命令”。", "将命令粘贴到已打开的 PowerShell 窗口并按 Enter。", "在可见提示中确认 Windows 服务安装；之后即可关闭 PowerShell。"},
		NonWindowsHeading:     "请在目标 Windows 主机上打开此连接页",
		NonWindowsDescription: "此链接将准备 Windows 连接器，但当前浏览器运行在 {platform}。",
		NonWindowsSteps:       []string{"不要在当前设备领取此链接。", "请在目标 Windows 电脑上打开同一链接。", "然后按该页面显示的 Windows 连接步骤操作。"},
		CopyAction:            "复制连接命令",
		WindowsOnly:           "此连接尚未领取。请在目标 Windows 主机上打开同一链接。",
		Preparing:             "正在准备连接命令…",
		Ready:                 "连接命令已就绪。复制后粘贴到当前 PowerShell 窗口并按 Enter 执行。",
		ClaimFailed:           "此连接未能领取。请向操作员获取新的链接。",
		Copied:                "已复制。请粘贴到当前 PowerShell 窗口并按 Enter 执行。",
		CopyManual:            "浏览器未允许复制。下方命令已被选中，请复制后粘贴到当前 PowerShell 窗口并按 Enter 执行。",
		UnexpectedBootstrap:   "连接响应未包含连接命令。",
	},
	"zh-Hant": {
		Title:                 "遠端開發主機連線",
		Heading:               "連線這台 Windows 主機",
		Description:           "此頁面會準備一條短期受管主機安裝指令。複製後貼到目前已開啟的 PowerShell 視窗並按 Enter；連線器會自動取得、驗證，並在可見確認後安裝為 Windows 服務。",
		DetectedPlatform:      "已偵測到系統：{platform}。",
		PlatformNames:         map[string]string{"windows": "Windows", "macos": "macOS", "linux": "Linux", "other": "不支援的系統"},
		WindowsSteps:          []string{"開啟 Windows PowerShell。", "按一下下方的「複製連線指令」。", "將指令貼到已開啟的 PowerShell 視窗並按 Enter。", "在可見提示中確認 Windows 服務安裝；之後即可關閉 PowerShell。"},
		NonWindowsHeading:     "請在目標 Windows 主機上開啟此連線頁",
		NonWindowsDescription: "此連結會準備 Windows 連線器，但目前瀏覽器執行於 {platform}。",
		NonWindowsSteps:       []string{"不要在目前裝置領取此連結。", "請在目標 Windows 電腦上開啟同一個連結。", "再依該頁面顯示的 Windows 連線步驟操作。"},
		CopyAction:            "複製連線指令",
		WindowsOnly:           "此連線尚未被領取。請在目標 Windows 電腦上開啟同一個連結。",
		Preparing:             "正在準備連線指令…",
		Ready:                 "連線指令已就緒。複製後貼到目前 PowerShell 視窗並按 Enter 執行。",
		ClaimFailed:           "此連線未能領取。請向操作員取得新的連結。",
		Copied:                "已複製。請貼到目前 PowerShell 視窗並按 Enter 執行。",
		CopyManual:            "瀏覽器未允許複製。下方指令已被選取，請複製後貼到目前 PowerShell 視窗並按 Enter 執行。",
		UnexpectedBootstrap:   "連線回應未包含連線指令。",
	},
}

// WebHandoffAsset is an immutable host executable that a claimed browser
// handoff may deliver. It is loaded at gateway startup rather than embedded in
// source or fetched from another provider.
type WebHandoffAsset struct {
	Filename string
	Content  []byte
	SHA256   string
}

// WebHandoffOptions enables browser handoffs for a public HTTPS gateway.
// An empty WindowsAMD64 asset keeps the landing routes available but refuses
// creation so operators cannot issue a link that has no host executable.
type WebHandoffOptions struct {
	PublicBaseURL string
	WindowsAMD64  WebHandoffAsset
}

type webHandoffConfig struct {
	publicBaseURL string
	windowsAMD64  WebHandoffAsset
}

func NewWindowsAMD64WebHandoffAsset(filename string, content []byte) (WebHandoffAsset, error) {
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "." || filename == "" || len(content) == 0 {
		return WebHandoffAsset{}, fmt.Errorf("windows-amd64 host binary is required")
	}
	copied := append([]byte(nil), content...)
	sum := sha256.Sum256(copied)
	return WebHandoffAsset{
		Filename: filename,
		Content:  copied,
		SHA256:   hex.EncodeToString(sum[:]),
	}, nil
}

func LoadWindowsAMD64WebHandoffAsset(path string) (WebHandoffAsset, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return WebHandoffAsset{}, fmt.Errorf("windows-amd64 host binary path is required")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return WebHandoffAsset{}, fmt.Errorf("read windows-amd64 host binary: %w", err)
	}
	return NewWindowsAMD64WebHandoffAsset(filepath.Base(path), content)
}

// WithWebHandoff returns a copy of the server that can issue short-lived web
// handoffs through the configured public HTTPS gateway.
func (s Server) WithWebHandoff(options WebHandoffOptions) (Server, error) {
	baseURL, err := normalizeWebHandoffBaseURL(options.PublicBaseURL)
	if err != nil {
		return Server{}, err
	}
	asset := options.WindowsAMD64
	if len(asset.Content) > 0 {
		asset, err = NewWindowsAMD64WebHandoffAsset(asset.Filename, asset.Content)
		if err != nil {
			return Server{}, err
		}
	}
	s.webHandoff = webHandoffConfig{publicBaseURL: baseURL, windowsAMD64: asset}
	return s, nil
}

func normalizeWebHandoffBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("web handoff public base URL must be an absolute HTTPS URL without credentials, query, or fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("web handoff public base URL must not include a path")
	}
	parsed.Path = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (s Server) createWebHandoff(w http.ResponseWriter, r *http.Request, sessionID string) {
	if !s.authorizeOperator(r, operatorauth.RoleOperator) {
		writeProtocolError(w, http.StatusForbidden, protocolHTTPError(controlplane.ErrUnauthorizedEndpoint, "operator role is required", false))
		return
	}
	if len(s.webHandoff.windowsAMD64.Content) == 0 {
		writeError(w, http.StatusServiceUnavailable, "windows-amd64 web handoff asset is not configured")
		return
	}

	var request struct {
		Platform    string `json:"platform"`
		ExpiresInMS int    `json:"expires_in_ms"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWebHandoffRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid web handoff request")
		return
	}
	if strings.TrimSpace(request.Platform) == "" {
		request.Platform = gateway.WebHandoffPlatformWindowsAMD64
	}
	if request.Platform != gateway.WebHandoffPlatformWindowsAMD64 {
		writeError(w, http.StatusBadRequest, "unsupported web handoff platform")
		return
	}
	ttl, err := webHandoffTTL(request.ExpiresInMS)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	session, err := s.Gateway.Session(sessionID)
	if err != nil {
		writeControlPlaneError(w, err)
		return
	}
	selectedGateway, err := normalizeWebHandoffBaseURL(session.SelectedGatewayURL)
	if err != nil || selectedGateway != s.webHandoff.publicBaseURL {
		writeError(w, http.StatusBadRequest, "session selected gateway must match the configured web handoff gateway")
		return
	}

	handoff, err := s.Gateway.CreateWebHandoff(gateway.WebHandoffSpec{
		SessionID: session.ID,
		Platform:  request.Platform,
		ExpiresAt: s.Gateway.Now().Add(ttl),
	})
	if err != nil {
		writeWebHandoffError(w, err)
		return
	}
	if !s.persistState(w) {
		return
	}
	link := s.webHandoff.publicBaseURL + "/connect/" + url.PathEscape(handoff.ID)
	writeJSON(w, http.StatusCreated, map[string]any{
		"handoff": map[string]any{
			"schema_version":      handoff.SchemaVersion,
			"id":                  handoff.ID,
			"session_id":          handoff.SessionID,
			"platform":            handoff.Platform,
			"url":                 link,
			"expires_at":          handoff.ExpiresAt,
			"artifact_filename":   s.webHandoff.windowsAMD64.Filename,
			"artifact_sha256":     s.webHandoff.windowsAMD64.SHA256,
			"artifact_size_bytes": len(s.webHandoff.windowsAMD64.Content),
		},
	})
}

func webHandoffTTL(expiresInMS int) (time.Duration, error) {
	if expiresInMS == 0 {
		return defaultWebHandoffTTL, nil
	}
	if expiresInMS < int(minimumWebHandoffTTL/time.Millisecond) || expiresInMS > int(maximumWebHandoffTTL/time.Millisecond) {
		return 0, fmt.Errorf("expires_in_ms must be between %d and %d", minimumWebHandoffTTL/time.Millisecond, maximumWebHandoffTTL/time.Millisecond)
	}
	return time.Duration(expiresInMS) * time.Millisecond, nil
}

func (s Server) webHandoffRoute(w http.ResponseWriter, r *http.Request) {
	id, action, ok := splitWebHandoffPath(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown web handoff endpoint")
		return
	}
	switch {
	case r.Method == http.MethodGet && action == "":
		s.renderWebHandoffPage(w, r, id)
	case r.Method == http.MethodPost && action == "claim":
		s.claimWebHandoff(w, r, id)
	case r.Method == http.MethodGet && action == "rdev-host.exe":
		s.serveWebHandoffWindowsHost(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "unknown web handoff endpoint")
	}
}

func splitWebHandoffPath(path string) (id string, action string, ok bool) {
	rest := strings.TrimPrefix(path, "/connect/")
	if rest == path {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	switch {
	case len(parts) == 1 && parts[0] != "":
		return parts[0], "", true
	case len(parts) == 2 && parts[0] != "" && parts[1] != "":
		return parts[0], parts[1], true
	default:
		return "", "", false
	}
}

func webHandoffPageLocale(acceptLanguage string) string {
	for _, value := range strings.Split(acceptLanguage, ",") {
		language := strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
		switch {
		case strings.HasPrefix(language, "zh-hant"), strings.HasPrefix(language, "zh-tw"), strings.HasPrefix(language, "zh-hk"), strings.HasPrefix(language, "zh-mo"):
			return "zh-Hant"
		case strings.HasPrefix(language, "zh"):
			return "zh-Hans"
		}
	}
	return "en"
}

func webHandoffClientPlatform(userAgent string) string {
	userAgent = strings.ToLower(userAgent)
	switch {
	case strings.Contains(userAgent, "android"), strings.Contains(userAgent, "iphone"), strings.Contains(userAgent, "ipad"), strings.Contains(userAgent, "ipod"):
		return "other"
	case strings.Contains(userAgent, "windows"):
		return "windows"
	case strings.Contains(userAgent, "macintosh"), strings.Contains(userAgent, "mac os x"):
		return "macos"
	case strings.Contains(userAgent, "linux"), strings.Contains(userAgent, "x11"):
		return "linux"
	default:
		return "other"
	}
}

func (s Server) renderWebHandoffPage(w http.ResponseWriter, r *http.Request, id string) {
	writeWebHandoffSecurityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	locale := webHandoffPageLocale(r.Header.Get("Accept-Language"))
	copy := webHandoffPageCopies[locale]
	claimPath, _ := json.Marshal("/connect/" + url.PathEscape(id) + "/claim")
	copies, _ := json.Marshal(webHandoffPageCopies)
	initialLocale, _ := json.Marshal(locale)
	initialPlatform, _ := json.Marshal(webHandoffClientPlatform(r.UserAgent()))
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="%s">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title>
<style>body{font-family:system-ui,sans-serif;max-width:42rem;margin:4rem auto;padding:0 1.25rem;color:#172033}button{padding:.7rem 1rem;font:inherit}#status,#detected-platform{min-height:1.5rem}#connection-script{box-sizing:border-box;display:block;width:100%%;min-height:14rem;margin-top:.75rem;font-family:ui-monospace,monospace}</style></head>
<body><main><h1 id="heading">%s</h1><p id="description">%s</p><p id="detected-platform"></p><ol id="connection-steps"></ol><button id="copy-command" type="button">%s</button><p id="status" role="status"></p><textarea id="connection-script" hidden readonly spellcheck="false"></textarea></main>
<script>
(() => {
  const pageCopies = %s;
  const initialLocale = %s;
  const initialPlatform = %s;
  const claimPath = %s;
  const button = document.getElementById('copy-command');
  const status = document.getElementById('status');
  const connectionBootstrap = document.getElementById('connection-script');
  const heading = document.getElementById('heading');
  const description = document.getElementById('description');
  const detectedPlatform = document.getElementById('detected-platform');
  const connectionSteps = document.getElementById('connection-steps');
  const browserLanguages = [...(navigator.languages || []), navigator.language].filter(Boolean);
  const localeFor = values => {
    for (const value of values) {
      const language = String(value).toLowerCase();
      if (language.startsWith('zh-hant') || language.startsWith('zh-tw') || language.startsWith('zh-hk') || language.startsWith('zh-mo')) return 'zh-Hant';
      if (language.startsWith('zh')) return 'zh-Hans';
    }
    return 'en';
  };
  const locale = localeFor(browserLanguages.length ? browserLanguages : [initialLocale]);
  const copy = pageCopies[locale] || pageCopies.en;
  document.documentElement.lang = locale;
  document.title = copy.title;
  button.textContent = copy.copyAction;
  connectionBootstrap.setAttribute('aria-label', copy.copyAction);
  const platformFor = () => {
    const clientHintPlatform = navigator.userAgentData && navigator.userAgentData.platform;
    const platform = String(clientHintPlatform || navigator.platform || navigator.userAgent || initialPlatform).toLowerCase();
    if (/android|iphone|ipad|ipod/.test(platform)) return 'other';
    if (/win/.test(platform)) return 'windows';
    if (/mac/.test(platform)) return 'macos';
    if (/linux|x11/.test(platform)) return 'linux';
    return initialPlatform;
  };
  const platform = platformFor();
  const isWindows = platform === 'windows';
  const platformName = (copy.platformNames && copy.platformNames[platform]) || copy.platformNames.other;
  const replacePlatform = value => String(value).replace('{platform}', platformName);
  heading.textContent = isWindows ? copy.heading : copy.nonWindowsHeading;
  description.textContent = replacePlatform(isWindows ? copy.description : copy.nonWindowsDescription);
  detectedPlatform.textContent = replacePlatform(copy.detectedPlatform);
  const steps = isWindows ? copy.windowsSteps : copy.nonWindowsSteps;
  connectionSteps.replaceChildren(...steps.map(step => {
    const item = document.createElement('li');
    item.textContent = replacePlatform(step);
    return item;
  }));
  button.hidden = !isWindows;
  button.disabled = !isWindows;
  const selectForManualCopy = () => {
    connectionBootstrap.hidden = false;
    connectionBootstrap.focus();
    connectionBootstrap.select();
  };
  const copyBootstrap = async () => {
    try {
      await navigator.clipboard.writeText(connectionBootstrap.value);
      status.textContent = copy.copied;
    } catch (_) {
      selectForManualCopy();
      status.textContent = copy.copyManual;
    }
  };
  const claim = async () => {
    let claimed = false;
    button.disabled = true;
    status.textContent = copy.preparing;
    try {
      const response = await fetch(claimPath, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({})});
      if (!response.ok) { throw new Error('claim failed'); }
      const payload = await response.json();
      if (typeof payload.bootstrap !== 'string' || !payload.bootstrap) {
        throw new Error('connection command missing');
      }
      claimed = true;
      connectionBootstrap.value = payload.bootstrap;
      connectionBootstrap.hidden = false;
      button.disabled = false;
      status.textContent = copy.ready;
    } catch (_) {
      status.textContent = claimed ? copy.unexpectedBootstrap : copy.claimFailed;
    }
  };
  if (!isWindows) { status.textContent = copy.windowsOnly; return; }
  button.addEventListener('click', copyBootstrap);
  claim();
})();
</script></body></html>`, locale, copy.Title, copy.Heading, copy.Description, copy.CopyAction, string(copies), string(initialLocale), string(initialPlatform), string(claimPath))
}

func (s Server) claimWebHandoff(w http.ResponseWriter, r *http.Request, id string) {
	var request struct{}
	r.Body = http.MaxBytesReader(w, r.Body, maxWebHandoffRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid web handoff claim")
		return
	}
	handoff, ticket, err := s.Gateway.ClaimWebHandoff(id, defaultArtifactTicketTTL)
	if err != nil {
		writeWebHandoffError(w, err)
		return
	}
	session, err := s.Gateway.Session(handoff.SessionID)
	if err != nil {
		writeControlPlaneError(w, err)
		return
	}
	selectedGateway, normalizeErr := normalizeWebHandoffBaseURL(session.SelectedGatewayURL)
	if normalizeErr != nil || selectedGateway != s.webHandoff.publicBaseURL {
		writeError(w, http.StatusBadRequest, "session selected gateway must match the configured web handoff gateway")
		return
	}
	if !s.persistState(w) {
		return
	}
	writeWebHandoffSecurityHeaders(w)
	bootstrap := s.webHandoffPowerShellBootstrap(handoff, session.JoinCode, ticket)
	writeJSON(w, http.StatusOK, map[string]any{
		"bootstrap":  bootstrap,
		"expires_at": handoff.ArtifactTicketExpiresAt,
	})
}

func (s Server) webHandoffPowerShellBootstrap(handoff gateway.WebHandoff, joinCode, ticket string) string {
	assetURL := s.webHandoff.publicBaseURL + "/connect/" + url.PathEscape(handoff.ID) + "/rdev-host.exe"
	return fmt.Sprintf(`# Remote Dev Skillkit managed-host bootstrap.
	# Paste this script into an already-open PowerShell window. It verifies the
	# connector, then requests a visible administrator-approved Windows service install.
& {
$ErrorActionPreference = 'Stop'
$gateway = %s
$joinCode = %s
$artifactUri = %s
$artifactTicket = %s
$expectedSHA256 = %s
$stagingRoot = Join-Path $env:LOCALAPPDATA 'RemoteDevSkillkit\managed-host'
$serviceRoot = Join-Path $env:ProgramData 'RemoteDevSkillkit\managed-host'
$serviceName = 'RemoteDevSkillkitHost'
$hostBinary = Join-Path $stagingRoot 'rdev-host.exe'
$tempBinary = Join-Path $stagingRoot 'rdev-host.download.exe'
$identityStore = Join-Path $stagingRoot 'identity.json'
$trustStore = Join-Path $stagingRoot 'trust.json'

New-Item -ItemType Directory -Force -Path $stagingRoot | Out-Null
Invoke-WebRequest -Uri $artifactUri -Headers @{ %s = $artifactTicket } -OutFile $tempBinary
$actualSHA256 = (Get-FileHash -LiteralPath $tempBinary -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actualSHA256 -ne $expectedSHA256) { throw 'rdev-host.exe download SHA-256 verification failed.' }
Copy-Item -LiteralPath $tempBinary -Destination $hostBinary -Force
$installedSHA256 = (Get-FileHash -LiteralPath $hostBinary -Algorithm SHA256).Hash.ToLowerInvariant()
if ($installedSHA256 -ne $expectedSHA256) { throw 'rdev-host.exe staging SHA-256 verification failed.' }
Remove-Item -LiteralPath $tempBinary -Force

Write-Host 'Requesting the visible Windows service installation approval.'
$installArgs = @('service', 'install', '--service-name', $serviceName, '--gateway', $gateway, '--join-code', $joinCode, '--state-root', $serviceRoot, '--identity-source', $identityStore, '--trust-source', $trustStore)
$install = Start-Process -FilePath $hostBinary -ArgumentList $installArgs -Verb RunAs -Wait -PassThru
if ($install.ExitCode -eq 0) {
  Write-Host 'Managed Remote Dev Skillkit service is installed and started. You may close this PowerShell window.'
} else {
  throw "rdev-host.exe service install exited with code $($install.ExitCode)."
}
}
`, powershellLiteral(s.webHandoff.publicBaseURL), powershellLiteral(joinCode), powershellLiteral(assetURL), powershellLiteral(ticket), powershellLiteral(s.webHandoff.windowsAMD64.SHA256), powershellLiteral(webHandoffArtifactTicketKey))
}

func powershellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func (s Server) serveWebHandoffWindowsHost(w http.ResponseWriter, r *http.Request, id string) {
	if len(s.webHandoff.windowsAMD64.Content) == 0 {
		writeError(w, http.StatusServiceUnavailable, "windows-amd64 web handoff asset is not configured")
		return
	}
	if _, err := s.Gateway.ValidateWebHandoffArtifactTicket(id, r.Header.Get(webHandoffArtifactTicketKey)); err != nil {
		writeWebHandoffError(w, err)
		return
	}
	writeWebHandoffSecurityHeaders(w)
	w.Header().Set("Content-Type", "application/vnd.microsoft.portable-executable")
	w.Header().Set("Content-Disposition", "attachment; filename=\"rdev-host.exe\"")
	w.Header().Set("Content-Length", strconv.Itoa(len(s.webHandoff.windowsAMD64.Content)))
	_, _ = w.Write(s.webHandoff.windowsAMD64.Content)
}

func writeWebHandoffSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; connect-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
}

func writeWebHandoffError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, gateway.ErrWebHandoffNotFound):
		writeError(w, http.StatusNotFound, "web handoff not found")
	case errors.Is(err, gateway.ErrWebHandoffExpired), errors.Is(err, gateway.ErrWebHandoffClaimed):
		writeError(w, http.StatusGone, "web handoff has expired or was already claimed")
	case errors.Is(err, gateway.ErrWebHandoffInvalidTicket):
		writeError(w, http.StatusForbidden, "web handoff authorization failed")
	case errors.Is(err, gateway.ErrWebHandoffSessionInvalid):
		writeError(w, http.StatusConflict, "session is not eligible for a web handoff")
	default:
		writeError(w, http.StatusBadRequest, "web handoff request failed")
	}
}
