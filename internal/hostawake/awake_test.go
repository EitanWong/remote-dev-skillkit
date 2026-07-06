package hostawake

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDisabledLease(t *testing.T) {
	lease := Disabled()
	if lease.Enabled {
		t.Fatal("disabled lease should not be enabled")
	}
	if lease.Method != "disabled" {
		t.Fatalf("unexpected method: %q", lease.Method)
	}
	if !strings.Contains(lease.Detail, "disabled") {
		t.Fatalf("expected disabled detail, got %q", lease.Detail)
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("disabled lease close should be a no-op: %v", err)
	}
}

func TestLeaseJSONOmitsCloseFunction(t *testing.T) {
	lease := Lease{
		Enabled: true,
		Method:  "test-method",
		Detail:  "test-detail",
		close:   func() error { return nil },
	}
	data, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{`"enabled":true`, `"method":"test-method"`, `"detail":"test-detail"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %s in %s", want, body)
		}
	}
	if strings.Contains(body, "close") {
		t.Fatalf("lease JSON should not expose close callback: %s", body)
	}
}
