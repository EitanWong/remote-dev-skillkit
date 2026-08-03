package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
)

// failingStateStore fails every save; LoadInto is a no-op.
type failingStateStore struct{}

func (failingStateStore) LoadInto(*gateway.MemoryGateway) (gateway.Snapshot, bool, error) {
	return gateway.Snapshot{}, false, nil
}
func (failingStateStore) SaveFrom(*gateway.MemoryGateway) (gateway.Snapshot, error) {
	return gateway.Snapshot{}, errors.New("simulated persist failure")
}
func (failingStateStore) Describe() string { return "failing" }

// Every mutating handler routes through the same persistState helper, so one
// failing-store case covers the whole class: the state change is applied in
// memory but the response must be a 500 so the caller knows it is not durable.
func TestHTTPPersistStateFailureReturns500(t *testing.T) {
	handler := NewServerWithStateStore(gateway.NewMemoryGateway(), failingStateStore{}).Handler()

	rec := postJSON(t, handler, "/v1/sessions", `{"profile":"attended-temporary","reason":"test","join_policy":"single-target","reconnect_grace_ms":120000}`, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "persist gateway state") {
		t.Fatalf("expected persist failure message, got %s", rec.Body.String())
	}
}
