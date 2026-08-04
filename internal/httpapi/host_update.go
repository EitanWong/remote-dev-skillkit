package httpapi

import (
	"net/http"
	"strconv"
)

// hostUpdateEndpointHeader carries the endpoint ID of the requesting target.
// It pairs with the endpoint lease bearer token; together they are the same
// credentials the host already uses for task fetch and event append, so host
// update downloads introduce no new credential or ticket type.
const hostUpdateEndpointHeader = "X-Rdev-Endpoint-Id"

// serveSessionHostUpdateArtifact serves the configured Windows host connector
// binary to the enrolled target of a session. Authorization is the endpoint
// lease; the response carries the artifact SHA-256 the host verifies before
// staging, and every download is recorded in the persisted audit log.
func (s Server) serveSessionHostUpdateArtifact(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	endpointID := r.Header.Get(hostUpdateEndpointHeader)
	if err := s.Gateway.ValidateSessionLease(sessionID, endpointID, extractBearerToken(r)); err != nil {
		writeControlPlaneError(w, err)
		return
	}
	if len(s.webHandoff.windowsAMD64.Content) == 0 {
		writeError(w, http.StatusServiceUnavailable, "windows-amd64 host artifact is not configured")
		return
	}
	s.Gateway.AppendAudit("target", "session.host-update.fetch", endpointID, "endpoint fetched host update artifact")
	writeWebHandoffSecurityHeaders(w)
	w.Header().Set("Content-Type", "application/vnd.microsoft.portable-executable")
	w.Header().Set("Content-Disposition", `attachment; filename="rdev-host.exe"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(s.webHandoff.windowsAMD64.Content)))
	w.Header().Set("X-Rdev-Sha256", s.webHandoff.windowsAMD64.SHA256)
	_, _ = w.Write(s.webHandoff.windowsAMD64.Content)
}
