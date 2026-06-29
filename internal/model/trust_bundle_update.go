package model

const (
	TrustBundleUpdateSchemaVersion   = "rdev.trust-bundle-update.v1"
	TrustBundleUpdateStatusCurrent   = "current"
	TrustBundleUpdateStatusAvailable = "update_available"
)

type TrustBundleUpdate struct {
	SchemaVersion   string             `json:"schema_version"`
	Status          string             `json:"status"`
	HostID          string             `json:"host_id"`
	CurrentSequence int                `json:"current_sequence"`
	CurrentHash     string             `json:"current_hash"`
	TrustBundle     *SignedTrustBundle `json:"trust_bundle,omitempty"`
}

func NewCurrentTrustBundleUpdate(hostID string, current SignedTrustBundle, currentHash string) TrustBundleUpdate {
	return TrustBundleUpdate{
		SchemaVersion:   TrustBundleUpdateSchemaVersion,
		Status:          TrustBundleUpdateStatusCurrent,
		HostID:          hostID,
		CurrentSequence: current.Sequence,
		CurrentHash:     currentHash,
	}
}

func NewAvailableTrustBundleUpdate(hostID string, current SignedTrustBundle, currentHash string) TrustBundleUpdate {
	bundle := current
	return TrustBundleUpdate{
		SchemaVersion:   TrustBundleUpdateSchemaVersion,
		Status:          TrustBundleUpdateStatusAvailable,
		HostID:          hostID,
		CurrentSequence: current.Sequence,
		CurrentHash:     currentHash,
		TrustBundle:     &bundle,
	}
}
