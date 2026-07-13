package policy

import (
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestTemporaryDefaultsIncludeFileTransferAndControlledDesktopCapabilities(t *testing.T) {
	defaults := TemporaryDefaults()
	for _, cap := range []Capability{
		CapabilityFileTransferRead,
		CapabilityFileTransferWrite,
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
	} {
		if !containsCapability(defaults, cap) {
			t.Fatalf("temporary defaults should include %s", cap)
		}
	}
	for _, cap := range []Capability{CapabilityClipboardRead, CapabilityClipboardWrite, CapabilityUnattendedAccess} {
		if containsCapability(defaults, cap) {
			t.Fatalf("temporary defaults must keep %s explicit", cap)
		}
	}
}

func TestMergeTemporaryCapabilitiesPreservesExplicitAndAddsMissingDefaults(t *testing.T) {
	got := MergeTemporaryCapabilities([]string{"shell.user", "window.inspect", "shell.user"})
	if got[0] != "shell.user" || got[1] != "window.inspect" || len(got) <= 2 {
		t.Fatalf("unexpected merged capabilities: %#v", got)
	}
	for _, capability := range []string{"screen.screenshot", "input.mouse", "app.launch"} {
		count := 0
		for _, value := range got {
			if value == capability {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("capability %q count = %d in %#v", capability, count, got)
		}
	}
}

func TestDangerousCapabilities(t *testing.T) {
	cases := []Capability{
		CapabilityShellAdminJIT,
		CapabilityPackageInstallAuthorization,
		CapabilityServiceModifyAuthorization,
		CapabilityGUIControlAuthorization,
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

func TestExplainTemporaryModeDangerousCapabilityRequiresAuthorization(t *testing.T) {
	explanation := Explain(model.HostModeAttendedTemporary, CapabilityPackageInstallAuthorization)
	if !explanation.Allowed {
		t.Fatal("package install authorization should be available through session policy")
	}
	if !explanation.Authorization {
		t.Fatal("package install must require authorization")
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
