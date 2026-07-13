package desktopadapter

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
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
	WorkspaceRoot      string
	OutputPath         string
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
	Error               string   `json:"error,omitempty"`
	Windows             []Window `json:"windows,omitempty"`
	PNGBase64           string   `json:"png_base64,omitempty"`
	Frames              []Frame  `json:"frames,omitempty"`
	ArtifactPath        string   `json:"artifact_path,omitempty"`
	ArtifactContentType string   `json:"artifact_content_type,omitempty"`
	ArtifactBytes       int64    `json:"artifact_bytes,omitempty"`
	ArtifactSHA256      string   `json:"artifact_sha256,omitempty"`
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
	if err != nil && result.Error == "" {
		result.Error = err.Error()
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

func normalizeWindowQuery(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.TrimSuffix(value, ".exe")
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

type persistedArtifact struct {
	Path        string
	ContentType string
	Bytes       int64
	SHA256      string
}

func persistDesktopArtifact(workspaceRoot, outputPath, contentType string, data []byte) (persistedArtifact, error) {
	root, err := workspace.CanonicalDir(workspaceRoot)
	if err != nil {
		return persistedArtifact{}, fmt.Errorf("artifact workspace root must exist: %w", err)
	}
	if strings.TrimSpace(outputPath) == "" || filepath.IsAbs(outputPath) {
		return persistedArtifact{}, fmt.Errorf("artifact output path must be workspace-relative")
	}
	clean := filepath.Clean(outputPath)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || (len(clean) >= 2 && clean[1] == ':') {
		return persistedArtifact{}, fmt.Errorf("artifact output path escapes workspace root")
	}
	target := filepath.Join(root, clean)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return persistedArtifact{}, err
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return persistedArtifact{}, err
	}
	sum := sha256.Sum256(data)
	return persistedArtifact{Path: filepath.ToSlash(clean), ContentType: contentType, Bytes: int64(len(data)), SHA256: fmt.Sprintf("sha256:%x", sum[:])}, nil
}

func encodeFrameBundle(frames [][]byte) ([]byte, error) {
	var out bytes.Buffer
	archive := zip.NewWriter(&out)
	for i, frame := range frames {
		name := fmt.Sprintf("frame-%04d.png", i)
		writer, err := archive.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(writer, bytes.NewReader(frame)); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func encodePNGWithinBudget(img image.Image, budget int) ([]byte, error) {
	if img == nil {
		return nil, fmt.Errorf("image is required")
	}
	if budget <= 0 {
		budget = 12000
	}
	current := img
	for attempt := 0; attempt < 10; attempt++ {
		var out bytes.Buffer
		encoder := png.Encoder{CompressionLevel: png.BestCompression}
		if err := encoder.Encode(&out, current); err != nil {
			return nil, err
		}
		if out.Len() <= budget {
			return out.Bytes(), nil
		}
		bounds := current.Bounds()
		width, height := bounds.Dx(), bounds.Dy()
		if width <= 160 || height <= 90 {
			break
		}
		nextWidth := width * 3 / 4
		nextHeight := height * 3 / 4
		if nextWidth < 160 {
			nextWidth = 160
		}
		if nextHeight < 90 {
			nextHeight = 90
		}
		current = resizeNearest(current, nextWidth, nextHeight)
	}
	return nil, fmt.Errorf("PNG exceeds the %d-byte artifact budget", budget)
}

func resizeNearest(src image.Image, width, height int) image.Image {
	srcBounds := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		sy := srcBounds.Min.Y + y*srcBounds.Dy()/height
		for x := 0; x < width; x++ {
			sx := srcBounds.Min.X + x*srcBounds.Dx()/width
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}
