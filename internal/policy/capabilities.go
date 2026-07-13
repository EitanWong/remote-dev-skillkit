package policy

import (
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

type Capability string

const DefaultWorkspaceRoot = "~"

const (
	CapabilityShellUser                   Capability = "shell.user"
	CapabilityPowerShellUser              Capability = "powershell.user"
	CapabilityShellAdminJIT               Capability = "shell.admin.jit"
	CapabilityFSRead                      Capability = "fs.read"
	CapabilityFSWriteScoped               Capability = "fs.write.scoped"
	CapabilityFileTransferRead            Capability = "file.transfer.read"
	CapabilityFileTransferWrite           Capability = "file.transfer.write"
	CapabilityProcessInspect              Capability = "process.inspect"
	CapabilityElevationRequest            Capability = "elevation.request"
	CapabilityPackageInstallAuthorization Capability = "package.install.requiresAuthorization"
	CapabilityServiceModifyAuthorization  Capability = "service.modify.requiresAuthorization"
	CapabilityGUIView                     Capability = "gui.view"
	CapabilityGUIControlAuthorization     Capability = "gui.control.requiresAuthorization"
	CapabilityAppLaunch                   Capability = "app.launch"
	CapabilityAppClose                    Capability = "app.close"
	CapabilityURLOpen                     Capability = "url.open"
	CapabilityScreenScreenshot            Capability = "screen.screenshot"
	CapabilityScreenRecord                Capability = "screen.record"
	CapabilityWindowInspect               Capability = "window.inspect"
	CapabilityWindowFocus                 Capability = "window.focus"
	CapabilityWindowMove                  Capability = "window.move"
	CapabilityInputKeyboard               Capability = "input.keyboard"
	CapabilityInputMouse                  Capability = "input.mouse"
	CapabilityClipboardRead               Capability = "clipboard.read"
	CapabilityClipboardWrite              Capability = "clipboard.write"
	CapabilityUnattendedAccess            Capability = "unattended.access"
	CapabilityNetworkDiscoveryScoped      Capability = "network.discovery.scoped"
	CapabilityNetworkProbeLAN             Capability = "network.probe.lan"
	CapabilityRelayUse                    Capability = "relay.use"
	CapabilityMeshUse                     Capability = "mesh.use"
	CapabilitySSHTunnel                   Capability = "ssh.tunnel"
	CapabilityDownstreamControl           Capability = "downstream.control.scoped"
	CapabilityDevCodex                    Capability = "dev.codex"
	CapabilityDevClaudeCode               Capability = "claude-code.run"
	CapabilityDevACP                      Capability = "acpx.run"
)

func TemporaryDefaults() []Capability {
	return []Capability{
		CapabilityShellUser,
		CapabilityPowerShellUser,
		CapabilityFSRead,
		CapabilityFSWriteScoped,
		CapabilityFileTransferRead,
		CapabilityFileTransferWrite,
		CapabilityProcessInspect,
		CapabilityElevationRequest,
		CapabilityGUIView,
		CapabilityGUIControlAuthorization,
		CapabilityAppLaunch,
		CapabilityAppClose,
		CapabilityURLOpen,
		CapabilityScreenScreenshot,
		CapabilityScreenRecord,
		CapabilityWindowInspect,
		CapabilityWindowFocus,
		CapabilityWindowMove,
		CapabilityInputKeyboard,
		CapabilityInputMouse,
	}
}

// MergeTemporaryCapabilities keeps explicitly requested capabilities and adds
// any standard temporary-session capabilities that were omitted.
func MergeTemporaryCapabilities(explicit []string) []string {
	result := make([]string, 0, len(explicit)+len(TemporaryDefaults()))
	seen := make(map[string]bool, len(explicit))
	for _, raw := range explicit {
		capability := strings.TrimSpace(raw)
		if capability == "" || seen[capability] {
			continue
		}
		result = append(result, capability)
		seen[capability] = true
	}
	for _, capability := range TemporaryDefaults() {
		value := string(capability)
		if !seen[value] {
			result = append(result, value)
			seen[value] = true
		}
	}
	return result
}

func IsDangerous(cap Capability) bool {
	switch cap {
	case CapabilityShellAdminJIT,
		CapabilityPackageInstallAuthorization,
		CapabilityServiceModifyAuthorization,
		CapabilityGUIView,
		CapabilityGUIControlAuthorization,
		CapabilityAppLaunch,
		CapabilityAppClose,
		CapabilityURLOpen,
		CapabilityScreenScreenshot,
		CapabilityScreenRecord,
		CapabilityWindowFocus,
		CapabilityWindowMove,
		CapabilityInputKeyboard,
		CapabilityInputMouse,
		CapabilityClipboardRead,
		CapabilityClipboardWrite,
		CapabilityUnattendedAccess,
		CapabilityDownstreamControl:
		return true
	default:
		return false
	}
}

type Explanation struct {
	Mode          string `json:"mode"`
	Capability    string `json:"capability"`
	Allowed       bool   `json:"allowed"`
	Authorization bool   `json:"authorization_required"`
	Reason        string `json:"reason"`
}

func Explain(mode model.HostMode, capability Capability) Explanation {
	if !mode.Valid() {
		return Explanation{
			Mode:       string(mode),
			Capability: string(capability),
			Allowed:    false,
			Reason:     "unknown host mode",
		}
	}

	if mode == model.HostModeAttendedTemporary {
		for _, allowed := range TemporaryDefaults() {
			if capability == allowed {
				return Explanation{
					Mode:          string(mode),
					Capability:    string(capability),
					Allowed:       true,
					Authorization: false,
					Reason:        "allowed by temporary-mode defaults",
				}
			}
		}
		if IsDangerous(capability) {
			return Explanation{
				Mode:          string(mode),
				Capability:    string(capability),
				Allowed:       true,
				Authorization: true,
				Reason:        "available only through explicit session capability or operator-reviewed interrupt",
			}
		}
		return Explanation{
			Mode:       string(mode),
			Capability: string(capability),
			Allowed:    false,
			Reason:     "not part of temporary-mode defaults",
		}
	}

	if IsDangerous(capability) {
		return Explanation{
			Mode:          string(mode),
			Capability:    string(capability),
			Allowed:       true,
			Authorization: true,
			Reason:        "dangerous capability requires authorization",
		}
	}

	return Explanation{
		Mode:          string(mode),
		Capability:    string(capability),
		Allowed:       true,
		Authorization: false,
		Reason:        "allowed subject to host and task policy",
	}
}
