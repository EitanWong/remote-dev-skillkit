package cli

import (
	"errors"
	"io"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

type supportSessionStartDiagnostic struct {
	SchemaVersion   string                            `json:"schema_version"`
	ReadyToSend     bool                              `json:"ready_to_send"`
	Phase           string                            `json:"phase"`
	Reason          string                            `json:"reason"`
	NextActionClass string                            `json:"next_action_class,omitempty"`
	Attempts        []supportSessionDiagnosticAttempt `json:"attempts,omitempty"`
}

type supportSessionDiagnosticAttempt struct {
	ProviderID  string `json:"provider_id"`
	CandidateID string `json:"candidate_id,omitempty"`
	Status      string `json:"status"`
	ErrorClass  string `json:"error_class,omitempty"`
}

type supportSessionStderrEvent struct {
	SchemaVersion string `json:"schema_version"`
	Event         string `json:"event"`
	StatusClass   string `json:"status_class"`
	Connected     bool   `json:"connected"`
	ActionClass   string `json:"action_class"`
}

func newSupportSessionStartDiagnostic(phase, reason, nextAction string, availability tunnel.AvailabilitySet) supportSessionStartDiagnostic {
	diagnostic := supportSessionStartDiagnostic{
		SchemaVersion:   "rdev.support-session-start-diagnostic.v2",
		ReadyToSend:     false,
		Phase:           safeSupportSessionDiagnosticPhase(phase),
		Reason:          safeSupportSessionDiagnosticReason(reason),
		NextActionClass: safeSupportSessionNextAction(nextAction),
	}
	for _, attempt := range availability.Attempts {
		projected := supportSessionDiagnosticAttempt{
			ProviderID:  safeTunnelProviderID(attempt.ProviderID),
			CandidateID: safeTunnelCandidateID(attempt.CandidateID),
			Status:      safeTunnelAttemptStatus(attempt.Status),
			ErrorClass:  safeTunnelAttemptErrorClass(attempt.ErrorClass),
		}
		diagnostic.Attempts = append(diagnostic.Attempts, projected)
	}
	return diagnostic
}

func writeSupportSessionDiagnostic(statusPath string, out io.Writer, diagnostic supportSessionStartDiagnostic) error {
	if err := writeJSONFile0600(statusPath, diagnostic); err != nil {
		return errors.New("support-session diagnostic artifact write failed")
	}
	if err := writeJSON(out, diagnostic); err != nil {
		return errors.New("support-session diagnostic output failed")
	}
	return nil
}

func newSupportSessionStderrEvent(event string, status map[string]any) supportSessionStderrEvent {
	if status["connected"] == true {
		event = "connected"
	}
	connected := event == "connected"
	summary := supportSessionStderrEvent{
		SchemaVersion: "rdev.support-session-foreground-log-event.v1",
		Connected:     connected,
	}
	switch event {
	case "waiting":
		summary.Event = "waiting"
		summary.StatusClass = "waiting"
		summary.ActionClass = "wait-for-target"
	case "pending-activation":
		summary.Event = "pending-activation"
		summary.StatusClass = "pending-activation"
		summary.ActionClass = "review-activation"
	case "connected":
		summary.Event = "connected"
		summary.StatusClass = "connected"
		summary.ActionClass = "report-connection-established"
	default:
		summary.Event = "status"
		summary.StatusClass = "status"
		summary.ActionClass = "inspect-protected-status"
	}
	return summary
}

func safeSupportSessionDiagnosticPhase(value string) string {
	switch value {
	case "provider-selection", "static-bootstrap-probe", "readiness-policy", "published-handoff-invalidation":
		return value
	default:
		return "support-session-start"
	}
}

func safeSupportSessionDiagnosticReason(value string) string {
	switch value {
	case "no_public_gateway_provider_eligible",
		"no_public_gateway_candidate_passed_static_bootstrap_probe",
		"direct_handoff_readiness_not_satisfied",
		"tunnel_availability_lost",
		"support_session_canceled",
		"explicit_gateway_liveness_lost",
		"runtime_not_recovered":
		return value
	default:
		return "support_session_unavailable"
	}
}

func safeSupportSessionNextAction(value string) string {
	switch value {
	case "review-provider-eligibility", "review-provider-availability", "configure-redundant-public-gateway", "generate-new-handoff":
		return value
	default:
		return "inspect-protected-status"
	}
}

func safeTunnelCandidateID(value string) string {
	if len(value) != 16 {
		return ""
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return ""
		}
	}
	return value
}

func safeTunnelAttemptStatus(value tunnel.AttemptStatus) string {
	switch value {
	case tunnel.AttemptStarting, tunnel.AttemptHealthy, tunnel.AttemptDegraded,
		tunnel.AttemptExited, tunnel.AttemptStopped, tunnel.AttemptSkipped:
		return string(value)
	default:
		return "unknown"
	}
}

func safeTunnelAttemptErrorClass(value string) string {
	value = strings.TrimSpace(value)
	switch value {
	case "", "provider-invalid", "start-failed", "provider-id-mismatch", "process-exited",
		"probe-failed", "max-active", "timeout", "canceled", "dns-failed", "marker-mismatch",
		"redirect-rejected", "bootstrap-template-probe-failed", "final-ticket-bootstrap-probe-failed",
		"support-session-canceled", "liveness-probe-failed", "runtime-not-recovered",
		"provider-not-allowed", "provider-not-default", "region-unsupported",
		"regional-evidence-missing", "regional-evidence-not-yet-valid", "regional-evidence-expired",
		"regional-evidence-blocked", "regional-evidence-not-verified", "regional-evidence-invalid",
		"ssh-pin-missing", "ssh-pin-invalid", "tool-unsupported", "failed":
		return value
	default:
		return "failed"
	}
}

func safeEligibilityReason(value string) string {
	value = strings.TrimSpace(value)
	switch value {
	case "provider-not-allowed", "provider-not-default", "region-unsupported",
		"regional-evidence-missing", "regional-evidence-not-yet-valid", "regional-evidence-expired",
		"regional-evidence-blocked", "regional-evidence-not-verified", "regional-evidence-invalid",
		"ssh-pin-missing", "ssh-pin-invalid", "tool-unsupported":
		return value
	default:
		return "failed"
	}
}

func availabilityFromEligibilityEvaluations(evaluations []tunnel.Selection, region tunnel.RegionProfile) tunnel.AvailabilitySet {
	availability := tunnel.AvailabilitySet{SchemaVersion: tunnel.AvailabilitySchemaVersion, Region: region}
	for _, item := range evaluations {
		if item.Eligibility.Eligible {
			continue
		}
		availability.Attempts = append(availability.Attempts, tunnel.Attempt{
			ProviderID: item.Metadata.ID,
			Status:     tunnel.AttemptSkipped,
			ErrorClass: safeEligibilityReason(item.Eligibility.Reason),
		})
	}
	return availability
}
