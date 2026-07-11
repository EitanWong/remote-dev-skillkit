package hostcmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
)

// PermanentJoinFailureExitCode tells bootstrap launchers that retrying the same
// join request cannot succeed. It is intentionally distinct from the ordinary
// failure exit code so transient and unclassified failures keep retrying.
const PermanentJoinFailureExitCode = 78

type permanentJoinFailure struct {
	cause error
}

func (e permanentJoinFailure) Error() string {
	return e.cause.Error()
}

func (e permanentJoinFailure) Unwrap() error {
	return e.cause
}

// ExitCode maps host command errors to the process exit contract used by both
// rdev-host and the full rdev CLI.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var permanent permanentJoinFailure
	if errors.As(err, &permanent) {
		return PermanentJoinFailureExitCode
	}
	return 1
}

// NewJoinSessionResponseError preserves ordinary failure behavior unless a
// non-success response contains a complete Control Plane error envelope that
// explicitly declares the error non-recoverable.
func NewJoinSessionResponseError(statusCode int, status string, body []byte, cause error) error {
	message := gatewayErrorMessage(status, body, cause)
	protocolErr, complete := completeProtocolErrorEnvelope(body)
	if complete {
		message = protocolErr.Message
	}
	joinErr := fmt.Errorf("join session failed: %s", message)
	if statusCode >= 400 && statusCode <= 599 && complete && !protocolErr.Recoverable {
		return permanentJoinFailure{cause: joinErr}
	}
	return joinErr
}

type protocolErrorEnvelope struct {
	Error *rawProtocolError `json:"error"`
}

type rawProtocolError struct {
	SchemaVersion   *string        `json:"schema_version"`
	Code            *string        `json:"code"`
	Message         *string        `json:"message"`
	Recoverable     *bool          `json:"recoverable"`
	RetryAfterMS    *int           `json:"retry_after_ms"`
	UserSummary     *string        `json:"user_summary"`
	AgentNextAction *string        `json:"agent_next_action"`
	Details         map[string]any `json:"details,omitempty"`
}

func completeProtocolErrorEnvelope(body []byte) (controlplane.ProtocolError, bool) {
	var envelope protocolErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Error == nil {
		return controlplane.ProtocolError{}, false
	}
	raw := envelope.Error
	if raw.SchemaVersion == nil || raw.Code == nil || raw.Message == nil || raw.Recoverable == nil ||
		raw.RetryAfterMS == nil || raw.UserSummary == nil || raw.AgentNextAction == nil {
		return controlplane.ProtocolError{}, false
	}
	if *raw.SchemaVersion != controlplane.ErrorSchemaVersion ||
		strings.TrimSpace(*raw.Code) == "" ||
		strings.TrimSpace(*raw.Message) == "" ||
		*raw.RetryAfterMS < 0 ||
		strings.TrimSpace(*raw.UserSummary) == "" ||
		strings.TrimSpace(*raw.AgentNextAction) == "" {
		return controlplane.ProtocolError{}, false
	}
	return controlplane.ProtocolError{
		SchemaVersion:   *raw.SchemaVersion,
		Code:            controlplane.ErrorCode(*raw.Code),
		Message:         *raw.Message,
		Recoverable:     *raw.Recoverable,
		RetryAfterMS:    *raw.RetryAfterMS,
		UserSummary:     *raw.UserSummary,
		AgentNextAction: *raw.AgentNextAction,
		Details:         raw.Details,
	}, true
}
