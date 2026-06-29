package acceptance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/service"
)

func TestRunManagedMacServicePlanWritesVerifiedPlist(t *testing.T) {
	out := filepath.Join(t.TempDir(), "service-plan")
	repo := t.TempDir()
	binaryPath := filepath.Join(t.TempDir(), "rdev")
	plan, err := RunManagedMacServicePlan(context.Background(), ManagedMacServiceOptions{
		RepoRoot:           repo,
		OutDir:             out,
		BinaryPath:         binaryPath,
		GatewayURL:         "https://api.example.com/v1",
		TicketCode:         "ABCD-1234",
		Label:              "com.example.rdev-acceptance",
		LogDir:             filepath.Join(out, "logs"),
		WorkspaceLockStore: filepath.Join(out, "locks"),
		Now:                time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.SchemaVersion != ManagedMacServicePlanSchemaVersion {
		t.Fatalf("unexpected schema %q", plan.SchemaVersion)
	}
	if !allChecksPassed(plan.Checks) {
		t.Fatalf("expected service plan checks to pass: %#v", plan.Checks)
	}
	if _, err := os.Stat(filepath.Join(out, "service-plan.json")); err != nil {
		t.Fatalf("expected service plan file: %v", err)
	}
	status, err := service.InspectMacOSLaunchAgent(plan.PlistPath)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Exists || status.Label != "com.example.rdev-acceptance" {
		t.Fatalf("unexpected status %#v", status)
	}
	commands := joinedServiceCommands(plan.Commands)
	for _, expected := range []string{
		"rdev host service-control --platform macos --action start",
		"rdev host service-control --platform macos --action inspect",
		"rdev acceptance managed-mac",
		"rdev acceptance verify",
		"rdev host service-control --platform macos --action stop",
		"rdev host uninstall-service",
	} {
		if !strings.Contains(commands, expected) {
			t.Fatalf("expected command containing %q in %s", expected, commands)
		}
	}
}

func joinedServiceCommands(commands []ServiceCommand) string {
	var builder strings.Builder
	for _, command := range commands {
		builder.WriteString(command.Shell)
		builder.WriteByte('\n')
	}
	return builder.String()
}
