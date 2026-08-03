package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
)

const (
	notificationSchemaVersion = "rdev.notification.v1"
	notifyPostTimeout         = 5 * time.Second
)

// SetSessionNotifyURL registers or clears the event push webhook for a
// session. Empty URL clears; HTTPS is required. Every appended session event
// is then POSTed to the URL so an agent (Hermes, OpenClaw, ...) can react to
// host up/down, task lifecycle, and artifact changes without polling.
func (g *MemoryGateway) SetSessionNotifyURL(sessionID, rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL != "" {
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("notify_url must be an absolute https URL")
		}
	}
	if _, err := g.controlPlane().SetSessionNotifyURL(sessionID, rawURL); err != nil {
		return err
	}
	action := "session.notify.clear"
	message := "cleared session event notifications"
	if rawURL != "" {
		action = "session.notify.set"
		message = "registered event webhook " + rawURL
	}
	g.appendAudit("operator", action, sessionID, message)
	return nil
}

// notifyEvent is installed as the store event hook. It is invoked with the
// store lock held, so it only reads the already-resolved notify URL and
// hands the POST to a goroutine.
func (g *MemoryGateway) notifyEvent(sessionID string, event controlplane.Event, notifyURL string) {
	if strings.TrimSpace(notifyURL) == "" {
		return
	}
	go g.postNotification(sessionID, notifyURL, event)
}

func (g *MemoryGateway) postNotification(sessionID, rawURL string, event controlplane.Event) {
	payload := map[string]any{
		"schema_version":   notificationSchemaVersion,
		"session_id":       sessionID,
		"seq":              event.Seq,
		"type":             string(event.Type),
		"from_endpoint_id": event.FromEndpointID,
		"task_id":          event.TaskID,
		"created_at":       event.CreatedAt,
		"payload":          event.Payload,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: notifyPostTimeout}
	resp, err := client.Do(req)
	if err != nil {
		g.appendAudit("gateway", "session.notify.delivery_failed", sessionID, err.Error())
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		g.appendAudit("gateway", "session.notify.delivery_failed", sessionID, fmt.Sprintf("webhook returned HTTP %d", resp.StatusCode))
	}
}
