package hostcap

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
)

type ExecutableStatus struct {
	Found bool   `json:"found"`
	Path  string `json:"path,omitempty"`
}

type Inventory struct {
	Name                  string                      `json:"name"`
	OS                    string                      `json:"os"`
	Arch                  string                      `json:"arch"`
	AdminLikely           bool                        `json:"admin_likely"`
	TemporaryCapabilities []string                    `json:"temporary_capabilities"`
	Executables           map[string]ExecutableStatus `json:"executables"`
}

func Detect(ctx context.Context) Inventory {
	name, err := os.Hostname()
	if err != nil || name == "" {
		name = "unknown-host"
	}
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	if !strings.EqualFold(runtime.GOOS, "windows") {
		filtered := make([]string, 0, len(capabilities))
		for _, capability := range capabilities {
			if !IsDesktopCapability(capability) {
				filtered = append(filtered, capability)
			}
		}
		capabilities = filtered
	}
	return Inventory{
		Name:                  name,
		OS:                    runtime.GOOS,
		Arch:                  runtime.GOARCH,
		AdminLikely:           adminLikely(),
		TemporaryCapabilities: capabilities,
		Executables: map[string]ExecutableStatus{
			"git":        lookup(ctx, "git"),
			"ssh":        lookup(ctx, "ssh"),
			"codex":      lookup(ctx, "codex"),
			"claude":     lookup(ctx, "claude", "claude-code"),
			"acpx":       lookup(ctx, "acpx"),
			"tailscale":  lookup(ctx, "tailscale"),
			"powershell": lookup(ctx, "pwsh", "powershell", "powershell.exe"),
			"winget":     lookup(ctx, "winget"),
		},
	}
}

// IsDesktopCapability reports capabilities implemented only by the native
// desktop backend. Non-Windows hosts must not advertise these capabilities
// while the platform backend remains fail-closed.
func IsDesktopCapability(capability string) bool {
	capability = strings.TrimSpace(capability)
	return strings.HasPrefix(capability, "gui.") ||
		strings.HasPrefix(capability, "screen.") ||
		strings.HasPrefix(capability, "window.") ||
		strings.HasPrefix(capability, "input.") ||
		strings.HasPrefix(capability, "app.") ||
		strings.HasPrefix(capability, "clipboard.") ||
		capability == "url.open"
}

func lookup(ctx context.Context, names ...string) ExecutableStatus {
	for _, name := range names {
		if ctx.Err() != nil {
			return ExecutableStatus{}
		}
		path, err := exec.LookPath(name)
		if err == nil {
			return ExecutableStatus{Found: true, Path: path}
		}
	}
	return ExecutableStatus{}
}

func capabilitiesToStrings(caps []policy.Capability) []string {
	values := make([]string, 0, len(caps))
	for _, cap := range caps {
		values = append(values, string(cap))
	}
	return values
}

func adminLikely() bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(os.Getenv("USERNAME"), "Administrator")
	}
	return os.Geteuid() == 0
}
