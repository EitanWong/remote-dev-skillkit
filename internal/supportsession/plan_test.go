package supportsession

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
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
	target := plan["target_user_instructions"].(map[string]any)
	if !strings.Contains(target["message"].(string), "目标电脑") ||
		!strings.Contains(target["windows"].(string), "bootstrap.ps1") ||
		strings.Contains(target["windows"].(string), "ExecutionPolicy Bypass") {
		t.Fatalf("unexpected target instructions: %#v", target)
	}
}

func anyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	out = append(out, values...)
	return out
}
