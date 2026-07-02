package supportsession

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/hostcap"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
)

const PlanSchemaVersion = "rdev.support-session-plan.v1"

type Options struct {
	RepoRoot    string
	WorkDir     string
	GatewayURL  string
	Addr        string
	Target      string
	Reason      string
	TTLSeconds  int
	AutoApprove bool
	Locale      string
}

func BuildPlan(ctx context.Context, opts Options) map[string]any {
	repoRootInput := strings.TrimSpace(opts.RepoRoot)
	if repoRootInput == "" {
		repoRootInput = "."
	}
	repoRoot, _ := filepath.Abs(repoRootInput)
	workDir := strings.TrimSpace(opts.WorkDir)
	if workDir == "" {
		workDir = filepath.Join(repoRoot, "work", "rdev-support-session")
	}
	workDir, _ = filepath.Abs(workDir)
	gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	if gatewayURL == "" {
		gatewayURL = "http://<reachable-gateway-host>:8787"
	}
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = "0.0.0.0:8787"
	}
	target := strings.TrimSpace(opts.Target)
	if target == "" {
		target = "auto"
	}
	locale := strings.TrimSpace(opts.Locale)
	if locale == "" {
		locale = "auto"
	}
	ttl := opts.TTLSeconds
	if ttl == 0 {
		ttl = 7200
	}
	rdevPath := filepath.Join(workDir, "bin", exeName("rdev", runtime.GOOS))
	windowsRdevPath := filepath.Join(workDir, "bin", "rdev-windows-amd64.exe")
	linuxRdevPath := filepath.Join(workDir, "bin", "rdev-linux-amd64")
	linuxArmRdevPath := filepath.Join(workDir, "bin", "rdev-linux-arm64")
	darwinArmRdevPath := filepath.Join(workDir, "bin", "rdev-darwin-arm64")
	darwinAmdRdevPath := filepath.Join(workDir, "bin", "rdev-darwin-amd64")
	createInviteCommand := []string{
		rdevPath, "invite", "create",
		"--gateway", gatewayURL,
		"--mode", string(model.HostModeAttendedTemporary),
		"--ttl-seconds", strconv.Itoa(ttl),
		"--reason", opts.Reason,
		"--transport", "auto",
	}
	if opts.AutoApprove {
		createInviteCommand = append(createInviteCommand, "--auto-approve")
	}
	inviteBody, _ := json.Marshal(map[string]any{
		"mode":         string(model.HostModeAttendedTemporary),
		"ttl_seconds":  ttl,
		"reason":       opts.Reason,
		"auto_approve": opts.AutoApprove,
		"metadata": map[string]string{
			"connection_entry":  "standard-visible",
			"approval_contract": "target-consent-scoped-ticket",
		},
	})
	return map[string]any{
		"schema_version": PlanSchemaVersion,
		"ok":             true,
		"intent":         "one-command-visible-attended-temporary-connection-entry",
		"repo_root":      repoRoot,
		"work_dir":       workDir,
		"target":         target,
		"locale":         locale,
		"gateway_url":    gatewayURL,
		"auto_approve": map[string]any{
			"enabled":        opts.AutoApprove,
			"scope":          "attended-temporary tickets created by this standard plan only",
			"capabilities":   policyCapabilitiesToStrings(policy.TemporaryDefaults()),
			"security_model": "target consent plus signed manifest plus scoped ticket capabilities",
		},
		"commands": map[string]any{
			"prepare_dirs":           []string{"mkdir", "-p", filepath.Join(workDir, "bin"), filepath.Join(workDir, ".rdev", "keys"), filepath.Join(workDir, ".rdev", "gateway"), filepath.Join(workDir, ".rdev", "audit")},
			"build_local_rdev":       []string{"go", "build", "-o", rdevPath, "./cmd/rdev"},
			"build_windows_rdev":     []string{"env", "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0", "go", "build", "-o", windowsRdevPath, "./cmd/rdev"},
			"build_linux_rdev":       []string{"env", "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0", "go", "build", "-o", linuxRdevPath, "./cmd/rdev"},
			"build_linux_arm64_rdev": []string{"env", "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0", "go", "build", "-o", linuxArmRdevPath, "./cmd/rdev"},
			"build_macos_arm64_rdev": []string{"env", "GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=0", "go", "build", "-o", darwinArmRdevPath, "./cmd/rdev"},
			"build_macos_amd64_rdev": []string{"env", "GOOS=darwin", "GOARCH=amd64", "CGO_ENABLED=0", "go", "build", "-o", darwinAmdRdevPath, "./cmd/rdev"},
			"start_gateway": []string{
				rdevPath, "gateway", "serve", "--dev",
				"--addr", addr,
				"--audit-log", filepath.Join(workDir, ".rdev", "audit", "events.jsonl"),
				"--state", filepath.Join(workDir, ".rdev", "gateway", "state.json"),
				"--signing-key", filepath.Join(workDir, ".rdev", "keys", "gateway-signing-key.json"),
				"--manifest-signing-key", filepath.Join(workDir, ".rdev", "keys", "manifest-root-key.json"),
				"--rdev-windows-amd64", windowsRdevPath,
				"--rdev-linux-amd64", linuxRdevPath,
				"--rdev-linux-arm64", linuxArmRdevPath,
				"--rdev-darwin-arm64", darwinArmRdevPath,
				"--rdev-darwin-amd64", darwinAmdRdevPath,
			},
			"create_invite_http": []string{
				"curl", "-fsS", "-X", "POST", gatewayURL + "/v1/tickets",
				"-H", "Content-Type: application/json",
				"-d", string(inviteBody),
			},
			"create_invite_cli": createInviteCommand,
		},
		"target_user_instructions": LocalizedTargetInstructions(gatewayURL, locale),
		"agent_flow": []string{
			"run prepare_dirs and build commands from repo_root",
			"start gateway with the exact start_gateway argv in a managed terminal/session",
			"create the invite through HTTP or CLI",
			"give target user only the localized join URL or one-line visible script",
			"do not write ad hoc relay/nohup/bootstrap code",
			"after host connects, it is active when auto_approve is enabled; otherwise call rdev.hosts.approve",
		},
		"forbidden": []string{
			"ExecutionPolicy Bypass",
			"hidden install",
			"unverified binary download",
			"manual ticket/root/gateway/transport assembly for target user",
		},
		"detected_host_capabilities": hostcap.Detect(ctx),
	}
}

func LocalizedTargetInstructions(gatewayURL, locale string) map[string]any {
	windows := "powershell -NoProfile -Command \"irm '" + gatewayURL + "/join/<ticket-code>/bootstrap.ps1' | iex\""
	macLinux := "curl -fsSL " + gatewayURL + "/join/<ticket-code>/bootstrap.sh | sh"
	labels := map[string]string{
		"auto":  "Open this visible support command on the target computer. Keep the terminal open while the Agent works.",
		"en":    "Open this visible support command on the target computer. Keep the terminal open while the Agent works.",
		"zh-CN": "在目标电脑上运行这条可见的支持命令。Agent 工作期间请保持终端窗口打开。",
		"ja":    "対象コンピューターでこの表示されるサポートコマンドを実行し、Agent の作業中はターミナルを開いたままにしてください。",
		"ko":    "대상 컴퓨터에서 이 표시되는 지원 명령을 실행하고 Agent가 작업하는 동안 터미널을 열어 두세요.",
		"es":    "Ejecuta este comando visible de soporte en el equipo de destino y deja la terminal abierta mientras trabaja el Agent.",
		"fr":    "Executez cette commande d'assistance visible sur l'ordinateur cible et gardez le terminal ouvert pendant que l'Agent travaille.",
		"de":    "Fuhre diesen sichtbaren Support-Befehl auf dem Zielcomputer aus und lasse das Terminal offen, wahrend der Agent arbeitet.",
		"pt-BR": "Execute este comando visivel de suporte no computador de destino e mantenha o terminal aberto enquanto o Agent trabalha.",
	}
	message, ok := labels[locale]
	if !ok {
		message = labels["en"]
	}
	return map[string]any{
		"message":             message,
		"windows":             windows,
		"macos_linux":         macLinux,
		"join_url_template":   gatewayURL + "/join/<ticket-code>",
		"human_receives_only": []string{"localized join URL", "visible one-line script", "or signed package when published"},
	}
}

func exeName(name, goos string) string {
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}

func policyCapabilitiesToStrings(caps []policy.Capability) []string {
	values := make([]string, 0, len(caps))
	for _, cap := range caps {
		values = append(values, string(cap))
	}
	return values
}
