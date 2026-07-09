package model

const (
	SigningAlgEd25519         = "ed25519"
	DefaultTaskTTLSeconds     = 1800
	DefaultTaskMaxOutputBytes = 1024 * 1024
)

type TaskWorkspace struct {
	Root       string   `json:"root,omitempty"`
	WriteScope []string `json:"write_scope,omitempty"`
	Branch     string   `json:"branch,omitempty"`
}

type TaskLimits struct {
	MaxDurationSeconds int    `json:"max_duration_seconds"`
	MaxOutputBytes     int    `json:"max_output_bytes"`
	Network            string `json:"network"`
}
