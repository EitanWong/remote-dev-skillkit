package hostcmd

import (
	"path/filepath"
	"testing"
)

func TestManagedServiceConfigRoundTripProducesManagedServeOptions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, managedServiceConfigFilename)
	input := managedServiceConfig{
		ServiceName: defaultManagedServiceName,
		GatewayURL:  "https://gateway.example.test",
		JoinCode:    "JOIN-TEST",
		StateRoot:   root,
	}
	if err := writeManagedServiceConfig(path, input); err != nil {
		t.Fatal(err)
	}
	config, err := readManagedServiceConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	options := config.serveOptions()
	if options.Mode != "managed" || options.Once || options.Transport != "long-poll" || options.MaxTasks != 0 {
		t.Fatalf("unexpected managed service options: %#v", options)
	}
	if options.IdentityStorePath != filepath.Join(root, managedServiceIdentityFile) || options.TrustStorePath != filepath.Join(root, managedServiceTrustFile) || options.WorkspaceLockStore != filepath.Join(root, managedServiceWorkspaceLocker) {
		t.Fatalf("managed service state paths were not derived from the protected root: %#v", options)
	}
}
