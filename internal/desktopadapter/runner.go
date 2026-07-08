package desktopadapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const ResultSchemaVersion = "rdev.desktop-result.v1"

type Spec struct {
	Action             string
	URL                string
	App                string
	WindowID           string
	Title              string
	Text               string
	X                  int
	Y                  int
	Width              int
	Height             int
	Button             string
	Frames             int
	IntervalMillis     int
	MaxDurationSeconds int
	MaxOutputBytes     int
}

type Window struct {
	ID        string `json:"id"`
	Title     string `json:"title,omitempty"`
	ProcessID uint32 `json:"process_id,omitempty"`
	Visible   bool   `json:"visible"`
}

type Frame struct {
	Index      int    `json:"index"`
	CapturedAt string `json:"captured_at"`
	PNGBase64  string `json:"png_base64"`
	Bytes      int    `json:"bytes"`
}

type ResultArtifact struct {
	SchemaVersion       string   `json:"schema_version"`
	Adapter             string   `json:"adapter"`
	Action              string   `json:"action"`
	Status              string   `json:"status"`
	Detail              string   `json:"detail,omitempty"`
	Windows             []Window `json:"windows,omitempty"`
	PNGBase64           string   `json:"png_base64,omitempty"`
	Frames              []Frame  `json:"frames,omitempty"`
	ClipboardText       string   `json:"clipboard_text,omitempty"`
	WindowID            string   `json:"window_id,omitempty"`
	Target              string   `json:"target,omitempty"`
	DesktopSessionState string   `json:"desktop_session_state,omitempty"`
	StartedAt           string   `json:"started_at"`
	EndedAt             string   `json:"ended_at"`
	DurationMillis      int64    `json:"duration_millis"`
}

func Execute(spec Spec) (ResultArtifact, error) {
	return ExecuteContext(context.Background(), spec)
}

func ExecuteContext(ctx context.Context, spec Spec) (ResultArtifact, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	maxDuration := spec.MaxDurationSeconds
	if maxDuration <= 0 {
		maxDuration = 30
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(maxDuration)*time.Second)
	defer cancel()
	started := time.Now().UTC()
	spec.Action = NormalizeAction(spec.Action)
	artifact := ResultArtifact{
		SchemaVersion: ResultSchemaVersion,
		Adapter:       "desktop",
		Action:        spec.Action,
		Status:        "started",
		StartedAt:     started.Format(time.RFC3339Nano),
	}
	if spec.Action == "" {
		return finish(artifact, started), fmt.Errorf("desktop action is required")
	}
	select {
	case <-ctx.Done():
		artifact.Status = "canceled"
		return finish(artifact, started), ctx.Err()
	default:
	}
	result, err := executePlatform(ctx, spec)
	if result.SchemaVersion == "" {
		result.SchemaVersion = ResultSchemaVersion
	}
	if result.Adapter == "" {
		result.Adapter = "desktop"
	}
	if result.Action == "" {
		result.Action = spec.Action
	}
	if result.StartedAt == "" {
		result.StartedAt = artifact.StartedAt
	}
	if result.Status == "" {
		result.Status = "succeeded"
	}
	return finish(result, started), err
}

func NormalizeAction(action string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	action = strings.ReplaceAll(action, "_", ".")
	switch action {
	case "windows", "window.list", "window.inspect":
		return "window.inspect"
	case "screenshot":
		return "screen.screenshot"
	case "record":
		return "screen.record"
	case "focus":
		return "window.focus"
	case "move":
		return "window.move"
	case "keyboard":
		return "input.keyboard"
	case "mouse":
		return "input.mouse"
	case "launch":
		return "app.launch"
	case "close":
		return "app.close"
	case "url", "open.url":
		return "url.open"
	case "clipboard":
		return "clipboard.read"
	case "clipboard.read", "clipboard.write":
		return action
	default:
		return action
	}
}

func finish(artifact ResultArtifact, started time.Time) ResultArtifact {
	ended := time.Now().UTC()
	artifact.EndedAt = ended.Format(time.RFC3339Nano)
	artifact.DurationMillis = ended.Sub(started).Milliseconds()
	return artifact
}

func (r ResultArtifact) ArtifactContent() string {
	content, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return ""
	}
	return string(content)
}

func encodeFrame(index int, png []byte) Frame {
	return Frame{
		Index:      index,
		CapturedAt: time.Now().UTC().Format(time.RFC3339Nano),
		PNGBase64:  base64.StdEncoding.EncodeToString(png),
		Bytes:      len(png),
	}
}
