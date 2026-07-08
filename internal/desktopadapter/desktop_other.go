//go:build !windows

package desktopadapter

import (
	"context"
	"fmt"
)

func executePlatform(ctx context.Context, spec Spec) (ResultArtifact, error) {
	return ResultArtifact{
		SchemaVersion:       ResultSchemaVersion,
		Adapter:             "desktop",
		Action:              spec.Action,
		Status:              "unavailable",
		DesktopSessionState: "desktop_session_unavailable",
		Detail:              "native desktop backend is implemented for Windows first; this platform returns fail-closed until a native backend is added",
	}, fmt.Errorf("desktop_session_unavailable: native desktop backend is not available on this platform")
}
