package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
)

func TestGatewayServeRequiresCompleteManagedStateConfiguration(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "state without signing key",
			args: []string{"gateway", "serve", "--state-file", "/runtime/state.json"},
			want: "--state-file and --signing-key-file must be supplied together",
		},
		{
			name: "signing key without state",
			args: []string{"gateway", "serve", "--signing-key-file", "/runtime/signing-key.json"},
			want: "--state-file and --signing-key-file must be supplied together",
		},
		{
			name: "persistent state without operator auth",
			args: []string{
				"gateway", "serve",
				"--state-file", "/runtime/state.json",
				"--signing-key-file", "/runtime/signing-key.json",
			},
			want: "persistent gateway requires --operator-auth-file",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := NewApp(&stdout, &stderr).Run(context.Background(), tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("gateway serve error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestNewGatewayRuntimeRestoresSessionAndLease(t *testing.T) {
	runtimeDir := t.TempDir()
	statePath := filepath.Join(runtimeDir, "state.json")
	keyPath := filepath.Join(runtimeDir, "signing-key.json")

	first, err := newGatewayRuntime(statePath, keyPath, "managed-gateway", "operators.json")
	if err != nil {
		t.Fatal(err)
	}
	session, err := first.Gateway.CreateSession(controlplane.SessionSpec{Profile: "managed", Reason: "restart persistence"})
	if err != nil {
		t.Fatal(err)
	}
	_, endpoint, lease, err := first.Gateway.JoinSession(session.ID, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-persistent",
		Transport:           controlplane.TransportLongPoll,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, renewed, _, err := first.Gateway.SessionEventsAfter(session.ID, controlplane.EventCursor{
		EndpointID:  endpoint.ID,
		LeaseSecret: lease.Secret,
	}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.StateStore.SaveFrom(first.Gateway); err != nil {
		t.Fatal(err)
	}

	second, err := newGatewayRuntime(statePath, keyPath, "managed-gateway", "operators.json")
	if err != nil {
		t.Fatal(err)
	}
	if first.Gateway.SignedTrustBundle().SigningKeyID != second.Gateway.SignedTrustBundle().SigningKeyID {
		t.Fatal("persistent gateway signing identity changed across restart")
	}
	if err := second.Gateway.ValidateSessionLease(session.ID, endpoint.ID, renewed.Secret); err != nil {
		t.Fatalf("renewed lease did not survive gateway restart: %v", err)
	}
}
