package policy

import "github.com/EitanWong/remote-dev-skillkit/internal/model"

type Capability string

const (
	CapabilityShellUser              Capability = "shell.user"
	CapabilityPowerShellUser         Capability = "powershell.user"
	CapabilityShellAdminJIT          Capability = "shell.admin.jit"
	CapabilityFSRead                 Capability = "fs.read"
	CapabilityFSWriteScoped          Capability = "fs.write.scoped"
	CapabilityFileTransferRead       Capability = "file.transfer.read"
	CapabilityFileTransferWrite      Capability = "file.transfer.write"
	CapabilityProcessInspect         Capability = "process.inspect"
	CapabilityElevationRequest       Capability = "elevation.request"
	CapabilityPackageInstallApproval Capability = "package.install.requiresApproval"
	CapabilityServiceModifyApproval  Capability = "service.modify.requiresApproval"
	CapabilityGUIView                Capability = "gui.view"
	CapabilityGUIControlApproval     Capability = "gui.control.requiresApproval"
	CapabilityAppLaunch              Capability = "app.launch"
	CapabilityAppClose               Capability = "app.close"
	CapabilityURLOpen                Capability = "url.open"
	CapabilityScreenScreenshot       Capability = "screen.screenshot"
	CapabilityScreenRecord           Capability = "screen.record"
	CapabilityWindowInspect          Capability = "window.inspect"
	CapabilityWindowFocus            Capability = "window.focus"
	CapabilityWindowMove             Capability = "window.move"
	CapabilityInputKeyboard          Capability = "input.keyboard"
	CapabilityInputMouse             Capability = "input.mouse"
	CapabilityClipboardRead          Capability = "clipboard.read"
	CapabilityClipboardWrite         Capability = "clipboard.write"
	CapabilityUnattendedAccess       Capability = "unattended.access"
	CapabilityNetworkDiscoveryScoped Capability = "network.discovery.scoped"
	CapabilityNetworkProbeLAN        Capability = "network.probe.lan"
	CapabilityRelayUse               Capability = "relay.use"
	CapabilityMeshUse                Capability = "mesh.use"
	CapabilitySSHTunnel              Capability = "ssh.tunnel"
	CapabilityDownstreamControl      Capability = "downstream.control.scoped"
	CapabilityDevCodex               Capability = "dev.codex"
	CapabilityDevClaudeCode          Capability = "claude-code.run"
	CapabilityDevACP                 Capability = "acpx.run"
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
	}
}

func IsDangerous(cap Capability) bool {
	switch cap {
	case CapabilityShellAdminJIT,
		CapabilityPackageInstallApproval,
		CapabilityServiceModifyApproval,
		CapabilityGUIView,
		CapabilityGUIControlApproval,
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
	Mode       string `json:"mode"`
	Capability string `json:"capability"`
	Allowed    bool   `json:"allowed"`
	Approval   bool   `json:"approval_required"`
	Reason     string `json:"reason"`
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
					Mode:       string(mode),
					Capability: string(capability),
					Allowed:    true,
					Approval:   false,
					Reason:     "allowed by temporary-mode defaults",
				}
			}
		}
		if IsDangerous(capability) {
			return Explanation{
				Mode:       string(mode),
				Capability: string(capability),
				Allowed:    true,
				Approval:   true,
				Reason:     "available only through explicit approval gate",
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
			Mode:       string(mode),
			Capability: string(capability),
			Allowed:    true,
			Approval:   true,
			Reason:     "dangerous capability requires approval",
		}
	}

	return Explanation{
		Mode:       string(mode),
		Capability: string(capability),
		Allowed:    true,
		Approval:   false,
		Reason:     "allowed subject to host and job policy",
	}
}
