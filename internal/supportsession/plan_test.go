package supportsession

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestBuildPlanStandardizesVisibleSupportSession(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "support")
	plan := BuildPlan(context.Background(), Options{
		RepoRoot:    ".",
		WorkDir:     workDir,
		GatewayURL:  "http://192.0.2.10:8787",
		Target:      "windows",
		Reason:      "company computer support",
		AutoApprove: true,
		Locale:      "zh-CN",
	})

	if plan["schema_version"] != PlanSchemaVersion || plan["ok"] != true {
		t.Fatalf("unexpected plan identity: %#v", plan)
	}
	autoApprove := plan["auto_approve"].(map[string]any)
	if autoApprove["enabled"] != true || !strings.Contains(autoApprove["scope"].(string), "attended-temporary") {
		t.Fatalf("expected scoped attended-temporary auto approval, got %#v", autoApprove)
	}
	commands := plan["commands"].(map[string]any)
	startGateway := strings.Join(anyStrings(commands["start_gateway"].([]string)), "\x00")
	if !strings.Contains(startGateway, "--rdev-windows-amd64") ||
		!strings.Contains(startGateway, "--rdev-darwin-amd64") ||
		!strings.Contains(startGateway, "--rdev-linux-arm64") {
		t.Fatalf("expected all helper asset flags, got %s", startGateway)
	}
	createInvite := strings.Join(anyStrings(commands["create_invite_cli"].([]string)), "\x00")
	if !strings.Contains(createInvite, "--auto-approve") {
		t.Fatalf("expected auto-approve invite command, got %s", createInvite)
	}
	watch := strings.Join(anyStrings(commands["watch_connection_status"].([]string)), "\x00")
	if !strings.Contains(watch, "support-session") || !strings.Contains(watch, "status") || !strings.Contains(watch, "--wait") {
		t.Fatalf("expected status watch command, got %s", watch)
	}
	target := plan["target_user_instructions"].(map[string]any)
	if !strings.Contains(target["message"].(string), "目标电脑") ||
		!strings.Contains(target["windows"].(string), "bootstrap.ps1") ||
		strings.Contains(target["windows"].(string), "ExecutionPolicy Bypass") {
		t.Fatalf("unexpected target instructions: %#v", target)
	}
}

func TestBuildCreatedReturnsReadyCommandsWithoutPlaceholders(t *testing.T) {
	created := BuildCreated(CreatedOptions{
		GatewayURL:            "http://192.0.2.10:8787",
		ManifestRootPublicKey: "manifest-root:abc",
		Ticket:                model.Ticket{Code: "ABCD-1234", Mode: model.HostModeAttendedTemporary},
		Target:                "windows",
		Locale:                "zh-CN",
		RdevCommand:           "rdev",
		AutoApprove:           true,
	})

	if created["schema_version"] != CreatedSchemaVersion ||
		created["target_command"] == "" ||
		!strings.Contains(created["target_command"].(string), "ABCD-1234") ||
		strings.Contains(created["target_command"].(string), "<ticket-code>") ||
		strings.Contains(created["target_command"].(string), "ExecutionPolicy Bypass") {
		t.Fatalf("expected ready Windows command without unsafe placeholders: %#v", created)
	}
	watch := strings.Join(created["watch_connection_status"].([]string), "\x00")
	if !strings.Contains(watch, "ABCD-1234") ||
		strings.Contains(watch, "<ticket-code>") ||
		!strings.Contains(watch, "--wait") {
		t.Fatalf("expected ready status watcher, got %s", watch)
	}
	flow := strings.Join(created["agent_flow"].([]string), "\n")
	if !strings.Contains(flow, "proactively report") ||
		!strings.Contains(flow, "do not ask the human to assemble") {
		t.Fatalf("expected Agent-native flow, got %s", flow)
	}
}

func TestBuildStatusReportsConnectedFeedback(t *testing.T) {
	status := BuildStatus(StatusOptions{
		TicketCode: "ABCD-1234",
		Locale:     "zh-CN",
		Hosts: []model.Host{{
			ID:       "host_1",
			TicketID: "ticket_1",
			Status:   model.HostStatusActive,
			Name:     "win-dev",
			OS:       "windows",
			Arch:     "amd64",
		}},
	})

	if status["schema_version"] != StatusSchemaVersion ||
		status["connected"] != true ||
		status["status"] != "connected" ||
		!strings.Contains(status["feedback"].(string), "连接已经建立") {
		t.Fatalf("expected connected localized status, got %#v", status)
	}
}

func anyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	out = append(out, values...)
	return out
}
