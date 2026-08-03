package gateway

import (
	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
)

// Hosts returns the durable target directory, newest last-seen first.
func (g *MemoryGateway) Hosts() []controlplane.Host {
	return g.controlPlane().Hosts()
}

// RenameHost sets the operator-controlled display name for one host record
// and records the lifecycle action in the durable audit log.
func (g *MemoryGateway) RenameHost(hostID, displayName string) (controlplane.Host, error) {
	host, err := g.controlPlane().RenameHost(hostID, displayName)
	if err == nil {
		g.appendAudit("operator", "host.rename", hostID, "renamed host directory entry")
	}
	return host, err
}
