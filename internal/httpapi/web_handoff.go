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
	webHandoffLauncherFilename  = "Connect-Rdev.cmd"
)

type webHandoffPageCopy struct {
	Title              string `json:"title"`
	Heading            string `json:"heading"`
	Description        string `json:"description"`
	Download           string `json:"download"`
	MissingProof       string `json:"missingProof"`
	WindowsOnly        string `json:"windowsOnly"`
	Preparing          string `json:"preparing"`
	Downloaded         string `json:"downloaded"`
	Fallback           string `json:"fallback"`
	ClaimFailed        string `json:"claimFailed"`
	FallbackAction     string `json:"fallbackAction"`
	FallbackCopied     string `json:"fallbackCopied"`
	FallbackCopyManual string `json:"fallbackCopyManual"`
	UnexpectedDownload string `json:"unexpectedDownload"`
}

var webHandoffPageCopies = map[string]webHandoffPageCopy{
	"en": {
		Title:              "Remote Dev Host Connection",
		Heading:            "Connect this Windows host",
		Description:        "This link downloads a native Windows launcher. Downloading does not start it; double-click Connect-Rdev.cmd in a visible console window.",
		Download:           "Download Windows launcher",
		MissingProof:       "This handoff link is missing its confirmation fragment.",
		WindowsOnly:        "Open this handoff on a Windows device. It has not been claimed.",
		Preparing:          "Preparing the Windows launcher…",
		Downloaded:         "Downloaded Connect-Rdev.cmd. Double-click it to start the managed connector.",
		Fallback:           "If Connect-Rdev.cmd cannot start, use the PowerShell copy button below. Paste the copied script into the PowerShell window already open; do not save or run it as a .ps1 file.",
		ClaimFailed:        "The handoff could not be claimed. Ask the operator for a fresh link.",
		FallbackAction:     "Connect-Rdev.cmd could not start? Copy PowerShell script",
		FallbackCopied:     "Copied. Paste into the open PowerShell window and press Enter.",
		FallbackCopyManual: "Copy is unavailable. The script is selected below; copy it, then paste it into the open PowerShell window and press Enter.",
		UnexpectedDownload: "The handoff response did not include a launcher.",
	},
	"zh-Hans": {
		Title:              "远程开发主机连接",
		Heading:            "连接这台 Windows 主机",
		Description:        "此链接会下载原生 Windows 启动器。下载本身不会启动它；请在可见的控制台窗口中双击 Connect-Rdev.cmd。",
		Download:           "下载 Windows 启动器",
		MissingProof:       "此连接链接缺少确认片段。",
		WindowsOnly:        "请在 Windows 设备上打开此连接页。它尚未被领取。",
		Preparing:          "正在准备 Windows 启动器…",
		Downloaded:         "已下载 Connect-Rdev.cmd。双击它即可启动托管连接器。",
		Fallback:           "若 Connect-Rdev.cmd 无法启动，请使用下方的 PowerShell 复制按钮。将复制的脚本粘贴到当前已打开的 PowerShell 窗口执行；不要将其保存后直接运行 .ps1 文件。",
		ClaimFailed:        "此连接未能领取。请向操作员获取新的链接。",
		FallbackAction:     "Connect-Rdev.cmd 无法启动？复制 PowerShell 脚本",
		FallbackCopied:     "已复制。请粘贴到当前 PowerShell 窗口并按 Enter 执行。",
		FallbackCopyManual: "浏览器未允许复制。下方脚本已被选中，请复制后粘贴到当前 PowerShell 窗口并按 Enter 执行。",
		UnexpectedDownload: "连接响应未包含启动器。",
	},
	"zh-Hant": {
		Title:              "遠端開發主機連線",
		Heading:            "連線這台 Windows 主機",
		Description:        "此連結會下載原生 Windows 啟動器。下載本身不會啟動它；請在可見的主控台視窗中雙擊 Connect-Rdev.cmd。",
		Download:           "下載 Windows 啟動器",
		MissingProof:       "此連線連結缺少確認片段。",
		WindowsOnly:        "請在 Windows 裝置上開啟此連線頁。它尚未被領取。",
		Preparing:          "正在準備 Windows 啟動器…",
		Downloaded:         "已下載 Connect-Rdev.cmd。雙擊它即可啟動受管連線器。",
		Fallback:           "若 Connect-Rdev.cmd 無法啟動，請使用下方的 PowerShell 複製按鈕。將複製的指令碼貼到目前已開啟的 PowerShell 視窗執行；不要將其儲存後直接執行 .ps1 檔案。",
		ClaimFailed:        "此連線未能領取。請向操作員取得新的連結。",
		FallbackAction:     "Connect-Rdev.cmd 無法啟動？複製 PowerShell 指令碼",
		FallbackCopied:     "已複製。請貼到目前 PowerShell 視窗並按 Enter 執行。",
		FallbackCopyManual: "瀏覽器未允許複製。下方指令碼已被選取，請複製後貼到目前 PowerShell 視窗並按 Enter 執行。",
		UnexpectedDownload: "連線回應未包含啟動器。",
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

	handoff, proof, err := s.Gateway.CreateWebHandoff(gateway.WebHandoffSpec{
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
	link := s.webHandoff.publicBaseURL + "/connect/" + url.PathEscape(handoff.ID) + "#" + proof
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

func (s Server) renderWebHandoffPage(w http.ResponseWriter, r *http.Request, id string) {
	writeWebHandoffSecurityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	locale := webHandoffPageLocale(r.Header.Get("Accept-Language"))
	copy := webHandoffPageCopies[locale]
	claimPath, _ := json.Marshal("/connect/" + url.PathEscape(id) + "/claim")
	copies, _ := json.Marshal(webHandoffPageCopies)
	initialLocale, _ := json.Marshal(locale)
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="%s">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title>
<style>body{font-family:system-ui,sans-serif;max-width:42rem;margin:4rem auto;padding:0 1.25rem;color:#172033}button{padding:.7rem 1rem;font:inherit}#status{min-height:1.5rem}#copy-fallback{display:block;margin-top:.75rem}#fallback-script{box-sizing:border-box;display:block;width:100%%;min-height:14rem;margin-top:.75rem;font-family:ui-monospace,monospace}</style></head>
<body><main><h1 id="heading">%s</h1><p id="description">%s</p><button id="download" type="button">%s</button><p id="status" role="status"></p><button id="copy-fallback" type="button" hidden></button><textarea id="fallback-script" hidden readonly spellcheck="false"></textarea></main>
<script>
(() => {
  const pageCopies = %s;
  const initialLocale = %s;
  const claimPath = %s;
  const button = document.getElementById('download');
  const status = document.getElementById('status');
  const fallback = document.getElementById('copy-fallback');
  const fallbackBootstrap = document.getElementById('fallback-script');
  const systemLocale = Intl.DateTimeFormat().resolvedOptions().locale;
  const languages = [...(navigator.languages || []), navigator.language, systemLocale, initialLocale].filter(Boolean);
  const localeFor = values => {
    for (const value of values) {
      const language = String(value).toLowerCase();
      if (language.startsWith('zh-hant') || language.startsWith('zh-tw') || language.startsWith('zh-hk') || language.startsWith('zh-mo')) return 'zh-Hant';
      if (language.startsWith('zh')) return 'zh-Hans';
    }
    return 'en';
  };
  const locale = localeFor(languages);
  const copy = pageCopies[locale] || pageCopies.en;
  document.documentElement.lang = locale;
  document.title = copy.title;
  document.getElementById('heading').textContent = copy.heading;
  document.getElementById('description').textContent = copy.description;
  button.textContent = copy.download;
  fallback.textContent = copy.fallbackAction;
  fallback.title = copy.fallback;
  fallbackBootstrap.setAttribute('aria-label', copy.fallbackAction);
  const isWindowsBrowser = () => {
    const clientHintPlatform = navigator.userAgentData && navigator.userAgentData.platform;
    const platform = String(clientHintPlatform || navigator.platform || '');
    return /^win/i.test(platform) || /windows/i.test(navigator.userAgent || '');
  };
  const download = (content, filename) => {
    const blob = new Blob([content], {type:'text/plain;charset=utf-8'});
    const link = document.createElement('a');
    link.href = URL.createObjectURL(blob);
    link.download = filename;
    document.body.appendChild(link);
    link.click();
    link.remove();
    window.setTimeout(() => URL.revokeObjectURL(link.href), 0);
  };
  fallback.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(fallbackBootstrap.value);
      status.textContent = copy.fallbackCopied;
    } catch (_) {
      fallbackBootstrap.hidden = false;
      fallbackBootstrap.focus();
      fallbackBootstrap.select();
      status.textContent = copy.fallbackCopyManual;
    }
  });
  const proof = window.location.hash.slice(1);
  if (!proof) { button.disabled = true; status.textContent = copy.missingProof; return; }
  history.replaceState(null, '', window.location.pathname);
  if (!isWindowsBrowser()) { button.disabled = true; status.textContent = copy.windowsOnly; return; }
  button.addEventListener('click', async () => {
    let claimed = false;
    button.disabled = true;
    status.textContent = copy.preparing;
    try {
      const response = await fetch(claimPath, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({proof})});
      if (!response.ok) { throw new Error('claim failed'); }
      const payload = await response.json();
      if (typeof payload.bootstrap !== 'string' || !payload.bootstrap || typeof payload.bootstrap_filename !== 'string' || !payload.bootstrap_filename) {
        throw new Error('launcher missing');
      }
      claimed = true;
      download(payload.bootstrap, payload.bootstrap_filename);
      if (typeof payload.fallback_bootstrap === 'string' && payload.fallback_bootstrap) {
        fallbackBootstrap.value = payload.fallback_bootstrap;
        fallback.hidden = false;
      }
      status.textContent = copy.downloaded;
      if (!fallback.hidden) { status.textContent += ' ' + copy.fallback; }
    } catch (_) {
      status.textContent = claimed ? copy.unexpectedDownload : copy.claimFailed;
      if (!claimed) { button.disabled = false; }
    }
  });
})();
</script></body></html>`, locale, copy.Title, copy.Heading, copy.Description, copy.Download, string(copies), string(initialLocale), string(claimPath))
}

func (s Server) claimWebHandoff(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		Proof string `json:"proof"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWebHandoffRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid web handoff claim")
		return
	}
	handoff, ticket, err := s.Gateway.ClaimWebHandoff(id, request.Proof, defaultArtifactTicketTTL)
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
	launcher := s.webHandoffWindowsLauncher(handoff, session.JoinCode, ticket)
	fallback := s.webHandoffPowerShellFallback(handoff, session.JoinCode, ticket)
	writeJSON(w, http.StatusOK, map[string]any{
		"bootstrap":          launcher,
		"bootstrap_filename": webHandoffLauncherFilename,
		"fallback_bootstrap": fallback,
		"expires_at":         handoff.ArtifactTicketExpiresAt,
	})
}

func (s Server) webHandoffWindowsLauncher(handoff gateway.WebHandoff, joinCode, ticket string) string {
	assetURL := s.webHandoff.publicBaseURL + "/connect/" + url.PathEscape(handoff.ID) + "/rdev-host.exe"
	lines := []string{
		"@echo off",
		"setlocal EnableExtensions DisableDelayedExpansion",
		cmdSetLine("GATEWAY", s.webHandoff.publicBaseURL),
		cmdSetLine("JOIN_CODE", joinCode),
		cmdSetLine("ARTIFACT_URI", assetURL),
		cmdSetLine("ARTIFACT_TICKET", ticket),
		cmdSetLine("ARTIFACT_TICKET_HEADER", webHandoffArtifactTicketKey),
		cmdSetLine("EXPECTED_SHA256", s.webHandoff.windowsAMD64.SHA256),
		"",
		"if not defined LOCALAPPDATA (",
		"  echo LOCALAPPDATA is required to start the managed connector.",
		"  exit /b 1",
		")",
		`set "STATE_ROOT=%LOCALAPPDATA%\RemoteDevSkillkit\managed-host"`,
		`set "HOST_BINARY=%STATE_ROOT%\rdev-host.exe"`,
		`set "TEMP_BINARY=%STATE_ROOT%\rdev-host.download.exe"`,
		`set "IDENTITY_STORE=%STATE_ROOT%\identity.json"`,
		`set "TRUST_STORE=%STATE_ROOT%	rust.json"`,
		`set "LOCK_STORE=%STATE_ROOT%\workspace-locks"`,
		"",
		"where curl.exe >nul 2>nul",
		"if errorlevel 1 (",
		"  echo curl.exe is unavailable. Use the PowerShell fallback from the handoff page.",
		"  exit /b 1",
		")",
		`if not exist "%STATE_ROOT%" mkdir "%STATE_ROOT%"`,
		"if errorlevel 1 (",
		"  echo Failed to create managed connector state directory.",
		"  exit /b 1",
		")",
		`if not exist "%LOCK_STORE%" mkdir "%LOCK_STORE%"`,
		"if errorlevel 1 (",
		"  echo Failed to create workspace lock directory.",
		"  exit /b 1",
		")",
		"",
		"echo Downloading and verifying Remote Dev Skillkit connector...",
		`curl.exe --fail --silent --show-error --location --header "%ARTIFACT_TICKET_HEADER%: %ARTIFACT_TICKET%" --output "%TEMP_BINARY%" "%ARTIFACT_URI%"`,
		"if errorlevel 1 (",
		"  echo Connector download failed.",
		"  exit /b 1",
		")",
		`set "ACTUAL_SHA256="`,
		`for /f "tokens=* delims= " %%H in ('certutil.exe -hashfile "%TEMP_BINARY%" SHA256 ^| findstr /R /I "^[0-9A-F][0-9A-F]"') do if not defined ACTUAL_SHA256 set "ACTUAL_SHA256=%%H"`,
		`set "ACTUAL_SHA256=%ACTUAL_SHA256: =%"`,
		`if /I not "%ACTUAL_SHA256%"=="%EXPECTED_SHA256%" (`,
		"  echo rdev-host.exe SHA-256 verification failed.",
		`  del /q "%TEMP_BINARY%" >nul 2>nul`,
		"  exit /b 1",
		")",
		`move /Y "%TEMP_BINARY%" "%HOST_BINARY%" >nul`,
		"if errorlevel 1 (",
		"  echo Failed to install the verified connector.",
		"  exit /b 1",
		")",
		"",
		"echo Starting managed Remote Dev Skillkit connector in this visible window.",
		`"%HOST_BINARY%" serve --mode managed --gateway "%GATEWAY%" --join-code "%JOIN_CODE%" --once=false --max-tasks 0 --transport long-poll --identity-store "%IDENTITY_STORE%" --trust-store "%TRUST_STORE%" --workspace-lock-store "%LOCK_STORE%"`,
		`set "EXIT_CODE=%ERRORLEVEL%"`,
		"endlocal & exit /b %EXIT_CODE%",
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}

func cmdSetLine(name, value string) string {
	return `set "` + name + "=" + cmdLiteral(value) + `"`
}

func cmdLiteral(value string) string {
	return strings.NewReplacer("^", "^^", "&", "^&", "|", "^|", "<", "^<", ">", "^>", "%", "%%").Replace(value)
}

func (s Server) webHandoffPowerShellFallback(handoff gateway.WebHandoff, joinCode, ticket string) string {
	assetURL := s.webHandoff.publicBaseURL + "/connect/" + url.PathEscape(handoff.ID) + "/rdev-host.exe"
	return fmt.Sprintf(`# Remote Dev Skillkit managed-host bootstrap.
	# Paste this script into an already-open PowerShell window. It does not create
	# a service, scheduled task, firewall rule, or execution-policy bypass.
$ErrorActionPreference = 'Stop'
$gateway = %s
$joinCode = %s
$artifactUri = %s
$artifactTicket = %s
$expectedSHA256 = %s
$stateRoot = Join-Path $env:LOCALAPPDATA 'RemoteDevSkillkit\managed-host'
$hostBinary = Join-Path $stateRoot 'rdev-host.exe'
$tempBinary = Join-Path $stateRoot 'rdev-host.download.exe'
$identityStore = Join-Path $stateRoot 'identity.json'
$trustStore = Join-Path $stateRoot 'trust.json'
$lockStore = Join-Path $stateRoot 'workspace-locks'

New-Item -ItemType Directory -Force -Path $stateRoot, $lockStore | Out-Null
Invoke-WebRequest -Uri $artifactUri -Headers @{ %s = $artifactTicket } -OutFile $tempBinary
$actualSHA256 = (Get-FileHash -LiteralPath $tempBinary -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actualSHA256 -ne $expectedSHA256) { throw 'rdev-host.exe SHA-256 verification failed.' }
Move-Item -LiteralPath $tempBinary -Destination $hostBinary -Force

Write-Host 'Starting managed Remote Dev Skillkit connector in this visible PowerShell window.'
& $hostBinary serve --mode managed --gateway $gateway --join-code $joinCode --once=false --max-tasks 0 --transport long-poll --identity-store $identityStore --trust-store $trustStore --workspace-lock-store $lockStore
$connectorExitCode = $LASTEXITCODE
if ($connectorExitCode -ne 0) { throw "rdev-host.exe exited with code $connectorExitCode." }
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
	case errors.Is(err, gateway.ErrWebHandoffInvalidProof), errors.Is(err, gateway.ErrWebHandoffInvalidTicket):
		writeError(w, http.StatusForbidden, "web handoff authorization failed")
	case errors.Is(err, gateway.ErrWebHandoffSessionInvalid):
		writeError(w, http.StatusConflict, "session is not eligible for a web handoff")
	default:
		writeError(w, http.StatusBadRequest, "web handoff request failed")
	}
}
