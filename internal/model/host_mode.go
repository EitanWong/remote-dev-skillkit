package model

// HostMode describes the execution posture selected by a session policy.
type HostMode string

const (
	HostModeAttendedTemporary HostMode = "attended-temporary"
	HostModeManaged           HostMode = "managed"
	HostModeBreakGlass        HostMode = "break-glass"
)

func (m HostMode) Valid() bool {
	switch m {
	case HostModeAttendedTemporary, HostModeManaged, HostModeBreakGlass:
		return true
	default:
		return false
	}
}
