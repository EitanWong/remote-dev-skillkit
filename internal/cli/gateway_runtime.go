package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
)

const defaultManagedGatewaySigningKeyID = "gateway-managed"

type gatewayRuntime struct {
	Gateway    *gateway.MemoryGateway
	StateStore gateway.StateStore
	Persistent bool
}

func newGatewayRuntime(stateFile, signingKeyFile, signingKeyID, operatorAuthFile string) (gatewayRuntime, error) {
	stateFile = strings.TrimSpace(stateFile)
	signingKeyFile = strings.TrimSpace(signingKeyFile)
	signingKeyID = strings.TrimSpace(signingKeyID)
	operatorAuthFile = strings.TrimSpace(operatorAuthFile)

	if (stateFile == "") != (signingKeyFile == "") {
		return gatewayRuntime{}, fmt.Errorf("--state-file and --signing-key-file must be supplied together")
	}
	if stateFile == "" {
		return gatewayRuntime{Gateway: gateway.NewMemoryGateway()}, nil
	}
	if operatorAuthFile == "" {
		return gatewayRuntime{}, fmt.Errorf("persistent gateway requires --operator-auth-file")
	}
	if signingKeyID == "" {
		signingKeyID = defaultManagedGatewaySigningKeyID
	}

	key, _, err := gateway.LoadOrCreateFileSigningKey(signingKeyFile, signingKeyID)
	if err != nil {
		return gatewayRuntime{}, fmt.Errorf("load gateway signing key: %w", err)
	}
	stateStore, err := gateway.NewFileStateStore(stateFile)
	if err != nil {
		return gatewayRuntime{}, fmt.Errorf("configure gateway state store: %w", err)
	}
	gw := gateway.NewMemoryGatewayWithSigningKey(time.Now, key.ID, key.PublicKey, key.PrivateKey)
	if _, _, err := stateStore.LoadInto(gw); err != nil {
		return gatewayRuntime{}, fmt.Errorf("load gateway state: %w", err)
	}
	return gatewayRuntime{Gateway: gw, StateStore: stateStore, Persistent: true}, nil
}
