package policy

import (
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestTemporaryDefaultsExcludeDangerousCapabilities(t *testing.T) {
	for _, cap := range TemporaryDefaults() {
		if IsDangerous(cap) {
			t.Fatalf("temporary default capability %s must not be dangerous", cap)
		}
	}
}

func TestTemporaryDefaultsIncludeFileTransferButExcludeDesktopControl(t *testing.T) {
	defaults := TemporaryDefaults()
	for _, cap := range []Capability{CapabilityFileTransferRead, CapabilityFileTransferWrite} {
		if !containsCapability(defaults, cap) {
			t.Fatalf("temporary defaults should include %s", cap)
		}
	}
	for _, cap := range []Capability{CapabilityScreenRecord, CapabilityInputKeyboard, CapabilityInputMouse, CapabilityUnattendedAccess} {
		if containsCapability(defaults, cap) {
			t.Fatalf("temporary defaults must not include %s", cap)
		}
	}
}

func TestDangerousCapabilities(t *testing.T) {
	cases := []Capability{
		CapabilityShellAdminJIT,
		CapabilityPackageInstallApproval,
		CapabilityServiceModifyApproval,
		CapabilityGUIControlApproval,
		CapabilityScreenScreenshot,
		CapabilityScreenRecord,
		CapabilityWindowFocus,
		CapabilityWindowMove,
		CapabilityInputKeyboard,
		CapabilityInputMouse,
		CapabilityClipboardRead,
		CapabilityClipboardWrite,
		CapabilityUnattendedAccess,
		CapabilityDownstreamControl,
	}
	for _, cap := range cases {
		if !IsDangerous(cap) {
			t.Fatalf("expected %s to be dangerous", cap)
		}
	}
}

func TestExplainTemporaryModeDangerousCapabilityRequiresApproval(t *testing.T) {
	explanation := Explain(model.HostModeAttendedTemporary, CapabilityPackageInstallApproval)
	if !explanation.Allowed {
		t.Fatal("package install approval should be available through approval gate")
	}
	if !explanation.Approval {
		t.Fatal("package install must require approval")
	}
}

func TestExplainTemporaryModeRejectsUnknownCapability(t *testing.T) {
	explanation := Explain(model.HostModeAttendedTemporary, Capability("credential.dump"))
	if explanation.Allowed {
		t.Fatal("credential dumping must not be allowed")
	}
}

func containsCapability(values []Capability, want Capability) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
