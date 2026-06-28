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
	return Inventory{
		Name:                  name,
		OS:                    runtime.GOOS,
		Arch:                  runtime.GOARCH,
		AdminLikely:           adminLikely(),
		TemporaryCapabilities: capabilitiesToStrings(policy.TemporaryDefaults()),
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
