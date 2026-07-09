package httpapi

import (
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/operatorauth"
)

func TestCreateTicketAndAudit(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	handler := server.Handler()

	body := bytes.NewBufferString(`{"mode":"attended-temporary","ttl_seconds":600,"reason":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tickets", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["joinUrl"].(string); !ok {
		t.Fatalf("expected joinUrl, got %#v", payload)
	}

	auditReq := httptest.NewRequest(http.MethodGet, "/v1/audit", nil)
	auditRec := httptest.NewRecorder()
	handler.ServeHTTP(auditRec, auditReq)
	if auditRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", auditRec.Code)
	}
	if !bytes.Contains(auditRec.Body.Bytes(), []byte("ticket.create")) {
		t.Fatalf("expected audit response to include ticket.create, got %s", auditRec.Body.String())
	}
}

func TestJoinPageAndBootstrapScripts(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	handler := server.Handler()

	ticket := createTicket(t, handler)
	for _, tc := range []struct {
		path     string
		accept   string
		contains []string
	}{
		{path: "/join/" + ticket.Code, contains: []string{"Connect This Machine", "Recommended Entry", "Agent Package Catalog", "rdev.connection-entry.package-catalog.v1", "planned-release-asset-required", "rdev-connection-entry-windows-amd64.zip", "bootstrap.sh", "bootstrap.ps1"}},
		{path: "/join/" + ticket.Code + "?lang=zh-CN", contains: []string{"连接这台机器", "推荐入口", "Agent 包目录", "接下来会发生什么", "bootstrap.ps1"}},
		{path: "/join/" + ticket.Code, accept: "pt-PT,pt;q=0.9", contains: []string{"Conectar Esta Maquina", "O que acontece depois"}},
		{path: "/join/" + ticket.Code + "/bootstrap.sh", contains: []string{"host serve", "Downloading verified rdev helper", ".gz", "gzip -dc", ".sha256", "--manifest-url", "--manifest-root-public-key", "--transport long-poll", "--once=false", "--max-tasks 0", "--identity-store", "host-identity.json", "caffeinate -dimsu", "systemd-inhibit --what=sleep:idle", "does not bypass lock-screen"}},
		{path: "/join/" + ticket.Code + "/bootstrap.ps1", contains: []string{"host serve", "Downloading verified rdev helper", ".gz", "GzipStream", ".sha256", "--manifest-url", "--manifest-root-public-key", "--transport long-poll", "--once=false", "--max-tasks 0", "--identity-store", "host-identity.json", "ES_DISPLAY_REQUIRED", "does not", "bypass lock-screen policy"}},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.accept != "" {
			req.Header.Set("Accept-Language", tc.accept)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s expected 200, got %d: %s", tc.path, rec.Code, rec.Body.String())
		}
		for _, want := range tc.contains {
			if !strings.Contains(rec.Body.String(), want) {
				t.Fatalf("%s expected %q in body:\n%s", tc.path, want, rec.Body.String())
			}
		}
		if strings.Contains(rec.Body.String(), "ExecutionPolicy Bypass") ||
			strings.Contains(rec.Body.String(), "rdev is required") {
			t.Fatalf("%s should not require preinstalled rdev or bypass execution policy:\n%s", tc.path, rec.Body.String())
		}
	}
}

func TestBootstrapScriptsReportPreconnectBeforeHelperDownload(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	handler := server.Handler()
	ticket := createTicket(t, handler)

	for _, path := range []string{
		"/join/" + ticket.Code + "/bootstrap.sh",
		"/join/" + ticket.Code + "/bootstrap.ps1",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s expected 200, got %d: %s", path, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, want := range []string{"/v1/support-session/preconnect", "downloading-helper", "rdev-bootstrap-preconnect"} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s expected %q in bootstrap body:\n%s", path, want, body)
			}
		}
		if rec.Body.Len() >= 1<<20 {
			t.Fatalf("%s bootstrap must stay below the 1 MB first-connect target, got %d bytes", path, rec.Body.Len())
		}
	}
}

func TestBootstrapScriptsUseVerifiedHelperCache(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	handler := server.Handler()
	ticket := createTicket(t, handler)

	for _, tc := range []struct {
		name      string
		path      string
		contains  []string
		forbidden []string
	}{
		{
			name: "shell",
			path: "/join/" + ticket.Code + "/bootstrap.sh",
			contains: []string{
				"remote-dev-skillkit/helpers",
				"using-cached-helper",
				"verifying-helper",
				"starting-full-helper",
				"shasum -a 256",
				"cp \"$out\" \"$cache_path\"",
			},
			forbidden: []string{"systemctl enable", "launchctl load", "sudo "},
		},
		{
			name: "powershell",
			path: "/join/" + ticket.Code + "/bootstrap.ps1",
			contains: []string{
				"RemoteDevSkillkit",
				"cache",
				"helpers",
				"using-cached-helper",
				"verifying-helper",
				"starting-full-helper",
				"Get-FileHash",
				"Copy-Item -Force -Path $rdevPath -Destination $cachePath",
			},
			forbidden: []string{"New-Service", "sc.exe", "Set-Service"},
		},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s expected 200, got %d: %s", tc.name, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, want := range tc.contains {
			if !strings.Contains(body, want) {
				t.Fatalf("%s expected %q in bootstrap body:\n%s", tc.name, want, body)
			}
		}
		for _, forbidden := range tc.forbidden {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s bootstrap should not contain service/persistence command %q:\n%s", tc.name, forbidden, body)
			}
		}
	}
}

func TestShellBootstrapRetryLoopSurvivesSetE(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	handler := server.Handler()
	ticket := createTicket(t, handler)

	req := httptest.NewRequest(http.MethodGet, "/join/"+ticket.Code+"/bootstrap.sh", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "set -eu") {
		t.Fatalf("expected strict shell mode in bootstrap body:\n%s", body)
	}
	if !strings.Contains(body, "if \"$rdev_cmd\" host serve ") ||
		!strings.Contains(body, "rdev_exit=0") ||
		!strings.Contains(body, "rdev_exit=$?") {
		t.Fatalf("expected retry loop to capture host serve failures without set -e aborting:\n%s", body)
	}
	if strings.Contains(body, "\n  \"$rdev_cmd\" host serve ") {
		t.Fatalf("bootstrap must not run host serve as a bare command under set -e:\n%s", body)
	}
}

func TestBootstrapHelperDownloadsUseRetryBackoff(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	handler := server.Handler()
	ticket := createTicket(t, handler)

	req := httptest.NewRequest(http.MethodGet, "/join/"+ticket.Code+"/bootstrap.sh", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected shell bootstrap 200, got %d: %s", rec.Code, rec.Body.String())
	}
	shellBody := rec.Body.String()
	for _, want := range []string{
		"rdev_curl_retry_flags=\"--retry 3 --retry-delay 2 --connect-timeout 10\"",
		"curl $rdev_curl_retry_flags -fsSL",
		"\"/${asset}.gz\" -o \"$tmp_gz\"",
		"\"/${asset}\" -o \"$out\"",
		"\"/${asset}.sha256\"",
	} {
		if !strings.Contains(shellBody, want) {
			t.Fatalf("expected shell helper download retry/backoff fragment %q in bootstrap body:\n%s", want, shellBody)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/join/"+ticket.Code+"/bootstrap.ps1", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected PowerShell bootstrap 200, got %d: %s", rec.Code, rec.Body.String())
	}
	powerShellBody := rec.Body.String()
	for _, want := range []string{
		"function Invoke-RdevWebRequestWithRetry",
		"[int]$MaxAttempts = 3",
		"Start-Sleep -Seconds",
		"Invoke-RdevWebRequestWithRetry -Uri ('",
		"+ $asset + '.gz') -OutFile $compressedPath",
		"+ $asset) -OutFile $rdevPath",
		"+ $asset + '.sha256')",
	} {
		if !strings.Contains(powerShellBody, want) {
			t.Fatalf("expected PowerShell helper download retry/backoff fragment %q in bootstrap body:\n%s", want, powerShellBody)
		}
	}
}

func TestSupportSessionStatusIncludesPreconnectEvents(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	handler := server.Handler()
	ticket := createTicket(t, handler)

	preconnectBody := bytes.NewBufferString(`{"ticket_code":"` + ticket.Code + `","phase":"downloading-helper","os":"windows","arch":"amd64","asset":"rdev-windows-amd64.exe","source":"rdev-bootstrap-preconnect","message":"downloading helper"}`)
	preconnectReq := httptest.NewRequest(http.MethodPost, "/v1/support-session/preconnect", preconnectBody)
	preconnectReq.Header.Set("Content-Type", "application/json")
	preconnectRec := httptest.NewRecorder()
	handler.ServeHTTP(preconnectRec, preconnectReq)
	if preconnectRec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for preconnect, got %d: %s", preconnectRec.Code, preconnectRec.Body.String())
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/v1/support-session/status?ticket_code="+url.QueryEscape(ticket.Code), nil)
	statusRec := httptest.NewRecorder()
	handler.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for support-session status, got %d: %s", statusRec.Code, statusRec.Body.String())
	}
	var statusPayload struct {
		Status            string `json:"status"`
		Waiting           bool   `json:"waiting"`
		Connected         bool   `json:"connected"`
		Feedback          string `json:"feedback"`
		NextAction        string `json:"next_action"`
		TargetPreconnects []struct {
			TicketCode string `json:"ticket_code"`
			Phase      string `json:"phase"`
			OS         string `json:"os"`
			Arch       string `json:"arch"`
			Asset      string `json:"asset"`
			Source     string `json:"source"`
			SeenCount  int    `json:"seen_count"`
		} `json:"target_preconnects"`
		TargetPreconnectSummary struct {
			Status              string         `json:"status"`
			Phase               string         `json:"phase"`
			AgentInterpretation string         `json:"agent_interpretation"`
			CountByPhase        map[string]int `json:"count_by_phase"`
		} `json:"target_preconnect_summary"`
	}
	if err := json.Unmarshal(statusRec.Body.Bytes(), &statusPayload); err != nil {
		t.Fatal(err)
	}
	if statusPayload.Status != "target-downloading" ||
		statusPayload.Waiting ||
		statusPayload.Connected ||
		!strings.Contains(statusPayload.Feedback, "downloading") ||
		!strings.Contains(statusPayload.NextAction, "Keep waiting") {
		t.Fatalf("preconnect should report target-side download without granting host access, got status %#v", statusPayload)
	}
	if len(statusPayload.TargetPreconnects) != 1 {
		t.Fatalf("expected one preconnect event, got %#v", statusPayload.TargetPreconnects)
	}
	event := statusPayload.TargetPreconnects[0]
	if event.TicketCode != ticket.Code ||
		event.Phase != "downloading-helper" ||
		event.OS != "windows" ||
		event.Arch != "amd64" ||
		event.Asset != "rdev-windows-amd64.exe" ||
		event.Source != "rdev-bootstrap-preconnect" ||
		event.SeenCount != 1 {
		t.Fatalf("unexpected preconnect event: %#v", event)
	}
	if statusPayload.TargetPreconnectSummary.Status != "target-downloading" ||
		statusPayload.TargetPreconnectSummary.Phase != "downloading-helper" ||
		statusPayload.TargetPreconnectSummary.CountByPhase["downloading-helper"] != 1 ||
		!strings.Contains(statusPayload.TargetPreconnectSummary.AgentInterpretation, "not disconnected") {
		t.Fatalf("unexpected preconnect summary: %#v", statusPayload.TargetPreconnectSummary)
	}
}

func TestJoinAssetsServeConfiguredBinaryAndHash(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "rdev.exe")
	if err := os.WriteFile(binaryPath, []byte("fake rdev binary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(gateway.NewMemoryGateway())
	server.Assets.RdevWindowsAMD64Path = binaryPath
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodGet, "/assets/rdev-windows-amd64.exe.sha256", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	sum := sha256.Sum256([]byte("fake rdev binary\n"))
	if strings.TrimSpace(rec.Body.String()) != hex.EncodeToString(sum[:]) {
		t.Fatalf("unexpected sha body: %q", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/assets/rdev-windows-amd64.exe", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "fake rdev binary") {
		t.Fatalf("expected configured binary, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestJoinAssetsServeGzipBinary(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "rdev.exe")
	content := bytes.Repeat([]byte("fake rdev binary\n"), 1024)
	if err := os.WriteFile(binaryPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(gateway.NewMemoryGateway())
	server.Assets.RdevWindowsAMD64Path = binaryPath
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodGet, "/assets/rdev-windows-amd64.exe.gz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/gzip" {
		t.Fatalf("expected application/gzip, got %q", got)
	}
	reader, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("gzip helper body did not round trip")
	}
}

func TestTicketManifestPackageCatalogIncludesHelperAssetMirrors(t *testing.T) {
	dir := t.TempDir()
	windowsHelper := filepath.Join(dir, "rdev-windows-amd64.exe")
	helperContent := []byte("signed helper mirror contract\n")
	if err := os.WriteFile(windowsHelper, helperContent, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(helperContent)
	expectedSHA := "sha256:" + hex.EncodeToString(sum[:])

	gw := gateway.NewMemoryGateway()
	server := NewServer(gw)
	server.Assets.RdevWindowsAMD64Path = windowsHelper
	handler := server.Handler()
	ticket := createTicket(t, handler)

	req := httptest.NewRequest(http.MethodGet, "/v1/tickets/"+ticket.Code+"/manifest", nil)
	req.Host = "gateway.example.test"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected manifest 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Manifest model.JoinManifest `json:"manifest"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid manifest payload: %v\n%s", err, rec.Body.String())
	}
	if err := payload.Manifest.VerifyWithRoot(gw.ManifestRoot(), time.Now()); err != nil {
		t.Fatalf("manifest must remain signed after adding package catalog asset mirrors: %v", err)
	}
	var candidate model.ConnectionEntryPackageCandidate
	for _, item := range payload.Manifest.PackageCatalog.Candidates {
		if item.ID == "windows-amd64" {
			candidate = item
			break
		}
	}
	if candidate.ID == "" {
		t.Fatalf("windows-amd64 candidate missing: %#v", payload.Manifest.PackageCatalog.Candidates)
	}
	if candidate.HelperAsset.Name != "rdev-windows-amd64.exe" ||
		candidate.HelperAsset.ExpectedSHA256 != expectedSHA ||
		candidate.HelperAsset.SHA256URL != "https://gateway.example.test/assets/rdev-windows-amd64.exe.sha256" ||
		len(candidate.HelperAsset.Mirrors) < 2 {
		t.Fatalf("expected helper asset mirror contract, got %#v", candidate.HelperAsset)
	}
	if candidate.HelperAsset.Mirrors[0].URL != "https://gateway.example.test/assets/rdev-windows-amd64.exe.gz" ||
		candidate.HelperAsset.Mirrors[0].Compression != "gzip" ||
		candidate.HelperAsset.Mirrors[1].URL != "https://gateway.example.test/assets/rdev-windows-amd64.exe" ||
		candidate.HelperAsset.Mirrors[1].Compression != "" {
		t.Fatalf("unexpected helper mirrors: %#v", candidate.HelperAsset.Mirrors)
	}
	if candidate.HelperAsset.BootstrapCanRunSessionTasks {
		t.Fatalf("helper asset catalog must not imply rdev-bootstrap can run session tasks: %#v", candidate.HelperAsset)
	}
	if !candidate.HelperAsset.RequiresFullRunnerBeforeSessionTaskRun {
		t.Fatalf("helper asset catalog must require the verified full runner before session task execution: %#v", candidate.HelperAsset)
	}
}

func TestOperatorAuthProtectsControlPlaneMutations(t *testing.T) {
	auth, err := operatorauth.New([]operatorauth.Principal{
		{ID: "operator", Roles: []string{operatorauth.RoleOperator}, TokenHash: operatorauth.HashToken("operator-token")},
		{ID: "auditor", Roles: []string{operatorauth.RoleAuditor}, TokenHash: operatorauth.HashToken("auditor-token")},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServerWithOperatorAuth(gateway.NewMemoryGateway(), "", auth)
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodPost, "/v1/tickets", bytes.NewBufferString(`{"reason":"protected"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for missing token, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/tickets", bytes.NewBufferString(`{"reason":"protected"}`))
	req.Header.Set("Authorization", "Bearer auditor-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for auditor mutation, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/tickets", bytes.NewBufferString(`{"reason":"protected"}`))
	req.Header.Set("Authorization", "Bearer operator-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for operator, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/audit", nil)
	req.Header.Set("Authorization", "Bearer auditor-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for auditor read, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOperatorAuthIssuerCanUseEnrollmentButNotTickets(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	issuerPublicKey, issuerPrivateKey := httpTestKeyPair(t)
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentIssuer(root, issuerPrivateKey)
	ticket, err := gw.CreateTicket(model.HostModeManaged, 600, []string{"shell.user"}, "managed enrollment")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := operatorauth.New([]operatorauth.Principal{{
		ID:        "issuer",
		Roles:     []string{operatorauth.RoleIssuer},
		TokenHash: operatorauth.HashToken("operator-token"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServerWithOperatorAuth(gw, "", auth).Handler()

	req := httptest.NewRequest(http.MethodPost, "/v1/tickets", bytes.NewBufferString(`{"reason":"should fail"}`))
	req.Header.Set("Authorization", "Bearer operator-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected issuer ticket mutation to fail, got %d: %s", rec.Code, rec.Body.String())
	}

	hostPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"ticket_code":          ticket.Code,
		"name":                 "managed-host",
		"os":                   "linux",
		"arch":                 "amd64",
		"capabilities":         []string{"shell.user"},
		"identity_key_id":      "host-test",
		"identity_public_key":  base64.RawURLEncoding.EncodeToString(hostPublicKey),
		"identity_fingerprint": httpHostIdentityFingerprint(hostPublicKey),
		"valid_minutes":        30,
	})
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/enrollment/certificates", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer operator-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected issuer enrollment success, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTrustBundleEndpointUpdatesSignedBundle(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey := httpTestKeyPair(t)
	gw := gateway.NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-dev", publicKey, privateKey)
	server := NewServer(gw)
	handler := server.Handler()

	getReq := httptest.NewRequest(http.MethodGet, "/v1/trust-bundle", nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}
	var getPayload struct {
		TrustBundle model.SignedTrustBundle `json:"trust_bundle"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &getPayload); err != nil {
		t.Fatal(err)
	}
	if getPayload.TrustBundle.Sequence != 1 {
		t.Fatalf("expected initial sequence 1, got %d", getPayload.TrustBundle.Sequence)
	}
	previousHash, err := getPayload.TrustBundle.Hash()
	if err != nil {
		t.Fatal(err)
	}
	next, err := model.NewSignedTrustBundle(model.SignedTrustBundleSpec{
		BundleID:           getPayload.TrustBundle.BundleID,
		Sequence:           2,
		NotBefore:          now,
		NotAfter:           now.Add(time.Hour),
		PreviousBundleHash: previousHash,
		SigningKeyID:       "gateway-dev",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway-dev", publicKey, model.TrustKeyStatusActive, now),
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	next, err = next.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"trust_bundle": next})
	if err != nil {
		t.Fatal(err)
	}
	updateReq := httptest.NewRequest(http.MethodPost, "/v1/trust-bundle", bytes.NewReader(body))
	updateRec := httptest.NewRecorder()
	handler.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", updateRec.Code, updateRec.Body.String())
	}
	var updatePayload struct {
		TrustBundle model.SignedTrustBundle `json:"trust_bundle"`
	}
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updatePayload); err != nil {
		t.Fatal(err)
	}
	if updatePayload.TrustBundle.Sequence != 2 {
		t.Fatalf("expected updated sequence 2, got %d", updatePayload.TrustBundle.Sequence)
	}
	if err := updatePayload.TrustBundle.Verify(model.NewTrustBundle("gateway-dev", publicKey), now); err != nil {
		t.Fatalf("updated trust bundle should verify: %v", err)
	}
	auditReq := httptest.NewRequest(http.MethodGet, "/v1/audit", nil)
	auditRec := httptest.NewRecorder()
	handler.ServeHTTP(auditRec, auditReq)
	if !bytes.Contains(auditRec.Body.Bytes(), []byte("trust_bundle.update")) {
		t.Fatalf("expected audit response to include trust_bundle.update, got %s", auditRec.Body.String())
	}
}

func TestEnrollmentRevocationsEndpointReturnsConfiguredList(t *testing.T) {
	now := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	publicKey, privateKey := httpTestKeyPair(t)
	revocations, err := model.SignHostEnrollmentRevocationList([]model.HostEnrollmentCertificateRevocation{
		{
			CertificateFingerprint: "sha256:enrollment-revoked-for-http-test",
			Reason:                 "host retired",
			RevokedAt:              now,
		},
	}, "enrollment-root", privateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentRoot(model.NewTrustBundle("enrollment-root", publicKey)).
		WithEnrollmentRevocations(revocations)
	handler := NewServer(gw).Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/enrollment/revocations", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Revocations model.HostEnrollmentRevocationList `json:"revocations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if err := model.VerifyHostEnrollmentRevocationListSignature(payload.Revocations, model.NewTrustBundle("enrollment-root", publicKey), now); err != nil {
		t.Fatalf("expected endpoint revocations to verify: %v", err)
	}
	if len(payload.Revocations.RevokedCertificates) != 1 {
		t.Fatalf("expected one revoked certificate, got %d", len(payload.Revocations.RevokedCertificates))
	}
}

func TestEnrollmentRevocationsEndpointReturnsEmptyBaseline(t *testing.T) {
	now := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	publicKey, privateKey := httpTestKeyPair(t)
	revocations, err := model.SignHostEnrollmentRevocationList(nil, "enrollment-root", privateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentRoot(model.NewTrustBundle("enrollment-root", publicKey)).
		WithEnrollmentRevocations(revocations)
	handler := NewServer(gw).Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/enrollment/revocations", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Revocations model.HostEnrollmentRevocationList `json:"revocations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if err := model.VerifyHostEnrollmentRevocationListSignature(payload.Revocations, model.NewTrustBundle("enrollment-root", publicKey), now); err != nil {
		t.Fatalf("expected empty endpoint revocations to verify: %v", err)
	}
	if len(payload.Revocations.RevokedCertificates) != 0 {
		t.Fatalf("expected empty baseline, got %d revoked certificates", len(payload.Revocations.RevokedCertificates))
	}
}

func TestEnrollmentRevocationsEndpointReturnsNotFoundWhenMissing(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	req := httptest.NewRequest(http.MethodGet, "/v1/enrollment/revocations", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("enrollment revocations not configured")) {
		t.Fatalf("expected explanatory error, got %s", rec.Body.String())
	}
}

func TestEnrollmentRevocationsEndpointRequiresOperatorIssuerRole(t *testing.T) {
	now := time.Date(2026, 7, 1, 13, 0, 0, 0, time.UTC)
	publicKey, privateKey := httpTestKeyPair(t)
	revocations, err := model.SignHostEnrollmentRevocationList(nil, "enrollment-root", privateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentRoot(model.NewTrustBundle("enrollment-root", publicKey)).
		WithEnrollmentRevocations(revocations)
	handler := NewServerWithOperatorAuth(gw, "", httpTestOperatorAuth(t)).Handler()

	for _, tc := range []struct {
		name   string
		auth   string
		status int
	}{
		{name: "missing", status: http.StatusUnauthorized},
		{name: "wrong", auth: "Bearer wrong-secret", status: http.StatusUnauthorized},
		{name: "operator role only", auth: "Bearer operator-secret", status: http.StatusUnauthorized},
		{name: "valid", auth: "Bearer issuer-secret", status: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/enrollment/revocations", nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("expected %d, got %d: %s", tc.status, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestEnrollmentCertificatesEndpointIssuesVerifiedCertificate(t *testing.T) {
	now := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	issuerPublicKey, issuerPrivateKey := httpTestKeyPair(t)
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentIssuer(root, issuerPrivateKey)
	handler := NewServer(gw).Handler()
	ticket, err := gw.CreateTicket(model.HostModeManaged, 600, []string{"shell.user", "git.diff"}, "managed enrollment")
	if err != nil {
		t.Fatal(err)
	}
	hostPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"ticket_code":          ticket.Code,
		"name":                 "managed-mac",
		"os":                   "darwin",
		"arch":                 "arm64",
		"capabilities":         []string{"shell.user"},
		"identity_key_id":      "host-test",
		"identity_public_key":  base64.RawURLEncoding.EncodeToString(hostPublicKey),
		"identity_fingerprint": httpHostIdentityFingerprint(hostPublicKey),
		"valid_minutes":        30,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/enrollment/certificates", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Certificate            model.HostEnrollmentCertificate `json:"certificate"`
		CertificateFingerprint string                          `json:"certificate_fingerprint"`
		EnrollmentRoot         model.TrustBundle               `json:"enrollment_root"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.EnrollmentRoot.SigningKeyID != root.SigningKeyID || payload.EnrollmentRoot.PublicKey != root.PublicKey {
		t.Fatalf("unexpected enrollment root: %#v", payload.EnrollmentRoot)
	}
	if err := model.VerifyHostEnrollmentCertificateSignature(payload.Certificate, root, now); err != nil {
		t.Fatalf("issued certificate should verify: %v", err)
	}
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(payload.Certificate)
	if err != nil {
		t.Fatal(err)
	}
	if payload.CertificateFingerprint != fingerprint {
		t.Fatalf("expected fingerprint %q, got %q", fingerprint, payload.CertificateFingerprint)
	}
	if payload.Certificate.TicketCode != ticket.Code || payload.Certificate.HostName != "managed-mac" || payload.Certificate.Mode != model.HostModeManaged {
		t.Fatalf("unexpected issued certificate: %#v", payload.Certificate)
	}
}

func TestEnrollmentCertificatesEndpointRequiresIssuer(t *testing.T) {
	handler := NewServer(gateway.NewMemoryGateway()).Handler()
	req := httptest.NewRequest(http.MethodPost, "/v1/enrollment/certificates", bytes.NewBufferString(`{"ticket_code":"ABCD"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("enrollment issuer not configured")) {
		t.Fatalf("expected issuer error, got %s", rec.Body.String())
	}
}

func TestEnrollmentCertificatesEndpointRequiresOperatorIssuerRole(t *testing.T) {
	now := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	issuerPublicKey, issuerPrivateKey := httpTestKeyPair(t)
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentIssuer(root, issuerPrivateKey)
	ticket, err := gw.CreateTicket(model.HostModeManaged, 600, []string{"shell.user"}, "managed enrollment")
	if err != nil {
		t.Fatal(err)
	}
	hostPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"ticket_code":          ticket.Code,
		"name":                 "managed-mac",
		"os":                   "darwin",
		"arch":                 "arm64",
		"capabilities":         []string{"shell.user"},
		"identity_key_id":      "host-test",
		"identity_public_key":  base64.RawURLEncoding.EncodeToString(hostPublicKey),
		"identity_fingerprint": httpHostIdentityFingerprint(hostPublicKey),
		"valid_minutes":        30,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServerWithOperatorAuth(gw, "", httpTestOperatorAuth(t)).Handler()

	for _, tc := range []struct {
		name   string
		auth   string
		status int
	}{
		{name: "missing", status: http.StatusUnauthorized},
		{name: "wrong", auth: "Bearer wrong-secret", status: http.StatusUnauthorized},
		{name: "operator role only", auth: "Bearer operator-secret", status: http.StatusUnauthorized},
		{name: "valid", auth: "Bearer issuer-secret", status: http.StatusCreated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/enrollment/certificates", bytes.NewReader(body))
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("expected %d, got %d: %s", tc.status, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestEnrollmentCertificatesRenewEndpointRenewsVerifiedCertificate(t *testing.T) {
	now := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	currentNow := now
	issuerPublicKey, issuerPrivateKey := httpTestKeyPair(t)
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return currentNow }).
		WithEnrollmentIssuer(root, issuerPrivateKey)
	ticket, err := gw.CreateTicket(model.HostModeManaged, 600, []string{"shell.user"}, "managed enrollment")
	if err != nil {
		t.Fatal(err)
	}
	hostPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := gw.IssueEnrollmentCertificate(gateway.EnrollmentCertificateRequest{
		TicketCode:          ticket.Code,
		Name:                "managed-mac",
		OS:                  "darwin",
		Arch:                "arm64",
		Capabilities:        []string{"shell.user"},
		IdentityKeyID:       "host-test",
		IdentityPublicKey:   base64.RawURLEncoding.EncodeToString(hostPublicKey),
		IdentityFingerprint: httpHostIdentityFingerprint(hostPublicKey),
		ValidMinutes:        30,
	})
	if err != nil {
		t.Fatal(err)
	}
	previousFingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		t.Fatal(err)
	}
	currentNow = now.Add(5 * time.Minute)
	body, err := json.Marshal(map[string]any{
		"certificate":   certificate,
		"valid_minutes": 120,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServerWithOperatorAuth(gw, "", httpTestOperatorAuth(t)).Handler()

	req := httptest.NewRequest(http.MethodPost, "/v1/enrollment/certificates/renew", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer issuer-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Certificate                    model.HostEnrollmentCertificate `json:"certificate"`
		CertificateFingerprint         string                          `json:"certificate_fingerprint"`
		PreviousCertificateFingerprint string                          `json:"previous_certificate_fingerprint"`
		EnrollmentRoot                 model.TrustBundle               `json:"enrollment_root"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.PreviousCertificateFingerprint != previousFingerprint {
		t.Fatalf("expected previous fingerprint %q, got %q", previousFingerprint, payload.PreviousCertificateFingerprint)
	}
	if payload.EnrollmentRoot.SigningKeyID != root.SigningKeyID || payload.EnrollmentRoot.PublicKey != root.PublicKey {
		t.Fatalf("unexpected enrollment root: %#v", payload.EnrollmentRoot)
	}
	if err := model.VerifyHostEnrollmentCertificateSignature(payload.Certificate, root, currentNow); err != nil {
		t.Fatalf("renewed certificate should verify: %v", err)
	}
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(payload.Certificate)
	if err != nil {
		t.Fatal(err)
	}
	if payload.CertificateFingerprint != fingerprint || payload.CertificateFingerprint == previousFingerprint {
		t.Fatalf("unexpected renewed fingerprint previous=%q payload=%q actual=%q", previousFingerprint, payload.CertificateFingerprint, fingerprint)
	}
}

func TestTrustBundleEndpointRejectsRollback(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey := httpTestKeyPair(t)
	gw := gateway.NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-dev", publicKey, privateKey)
	server := NewServer(gw)
	handler := server.Handler()

	current := gw.SignedTrustBundle()
	hash, err := current.Hash()
	if err != nil {
		t.Fatal(err)
	}
	rollback, err := model.NewSignedTrustBundle(model.SignedTrustBundleSpec{
		BundleID:           current.BundleID,
		Sequence:           1,
		NotBefore:          now,
		NotAfter:           now.Add(time.Hour),
		PreviousBundleHash: hash,
		SigningKeyID:       "gateway-dev",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway-dev", publicKey, model.TrustKeyStatusActive, now),
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	rollback, err = rollback.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"trust_bundle": rollback})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/trust-bundle", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTicketManifestEndpointSignsJoinManifest(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := NewServer(gw)
	handler := server.Handler()
	ticket := createTicket(t, handler)

	req := httptest.NewRequest(http.MethodGet, "/v1/tickets/"+ticket.Code+"/manifest", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Manifest              model.JoinManifest `json:"manifest"`
		ManifestRootPublicKey string             `json:"manifestRootPublicKey"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Manifest.TicketCode != ticket.Code {
		t.Fatalf("expected ticket code %q, got %q", ticket.Code, payload.Manifest.TicketCode)
	}
	if payload.Manifest.TrustFingerprint == "" {
		t.Fatal("manifest should include trust fingerprint")
	}
	if payload.ManifestRootPublicKey == "" {
		t.Fatal("manifest endpoint should include the pinned manifest root public key")
	}
	if payload.Manifest.PackageCatalog.SchemaVersion != model.ConnectionEntryPackageCatalogSchemaVersion {
		t.Fatalf("expected package catalog in join manifest, got %#v", payload.Manifest.PackageCatalog)
	}
	if len(payload.Manifest.PackageCatalog.Candidates) == 0 {
		t.Fatalf("expected package catalog candidates, got %#v", payload.Manifest.PackageCatalog)
	}
	if err := payload.Manifest.Verify(ticket.CreatedAt); err != nil {
		t.Fatalf("expected manifest to verify: %v", err)
	}
}

func TestTicketManifestEndpointSignsGatewayCandidates(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := NewServer(gw)
	handler := server.Handler()
	ticket := createTicket(t, handler)
	candidates := `[{"url":"https://relay.example.test/rdev","kind":"relay","scope":"configured-relay","recommended":true},{"url":"http://192.0.2.10:8787","kind":"lan-private"}]`
	req := httptest.NewRequest(http.MethodGet, "/v1/tickets/"+ticket.Code+"/manifest?gateway_url_candidates="+url.QueryEscape(candidates), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Manifest         model.JoinManifest     `json:"manifest"`
		GatewayTimeProof model.GatewayTimeProof `json:"gateway_time_proof"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Manifest.GatewayCandidates) < 2 || payload.Manifest.GatewayCandidates[0].Kind != "relay" {
		t.Fatalf("expected signed relay candidate first, got %#v", payload.Manifest.GatewayCandidates)
	}
	if err := payload.Manifest.Verify(ticket.CreatedAt); err != nil {
		t.Fatalf("expected manifest to verify: %v", err)
	}
	gatewayTime, err := payload.GatewayTimeProof.Verify(payload.Manifest.Trust, model.GatewayTimeProofPurposeJoinManifest, payload.Manifest)
	if err != nil {
		t.Fatalf("expected gateway time proof to verify: %v", err)
	}
	if err := payload.Manifest.Verify(gatewayTime); err != nil {
		t.Fatalf("expected manifest to verify using gateway time proof: %v", err)
	}
}

func TestTicketManifestEndpointUsesServerSideGatewayCandidateMetadata(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := NewServer(gw)
	handler := server.Handler()
	candidates := `[{"url":"https://relay.example.test/rdev","kind":"relay","scope":"configured-relay","recommended":true},{"url":"http://192.0.2.10:8787","kind":"lan-private"}]`
	createReq := httptest.NewRequest(http.MethodPost, "/v1/tickets", bytes.NewBufferString(`{
		"mode":"attended-temporary",
		"ttl_seconds":600,
		"reason":"metadata manifest",
		"metadata":{"`+gateway.TicketMetadataGatewayCandidates+`":`+strconv.Quote(candidates)+`}
	}`))
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Ticket model.Ticket `json:"ticket"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/tickets/"+created.Ticket.Code+"/manifest", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Manifest model.JoinManifest `json:"manifest"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Manifest.GatewayCandidates) < 2 || payload.Manifest.GatewayCandidates[0].Kind != "relay" {
		t.Fatalf("expected server-side metadata gateway candidates, got %#v", payload.Manifest.GatewayCandidates)
	}
	if err := payload.Manifest.Verify(created.Ticket.CreatedAt); err != nil {
		t.Fatalf("expected manifest to verify: %v", err)
	}
}

func createTicket(t *testing.T, handler http.Handler) model.Ticket {
	t.Helper()
	body := bytes.NewBufferString(`{"mode":"attended-temporary","ttl_seconds":600,"reason":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tickets", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Ticket model.Ticket `json:"ticket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Ticket
}

func httpTestKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return publicKey, privateKey
}

func httpTestOperatorAuth(t *testing.T) *operatorauth.Authorizer {
	t.Helper()
	auth, err := operatorauth.New([]operatorauth.Principal{
		{ID: "operator", Roles: []string{operatorauth.RoleOperator}, TokenHash: operatorauth.HashToken("operator-secret")},
		{ID: "issuer", Roles: []string{operatorauth.RoleIssuer}, TokenHash: operatorauth.HashToken("issuer-secret")},
		{ID: "auditor", Roles: []string{operatorauth.RoleAuditor}, TokenHash: operatorauth.HashToken("auditor-secret")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return auth
}

func httpHostIdentityFingerprint(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}
