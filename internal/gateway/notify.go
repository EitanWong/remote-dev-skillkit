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
// session. Empty URL clears; HTTPS is required (loopback HTTP is allowed so a
// local agent webhook such as Hermes on the same host can receive pushes).
// A `secret` query parameter is extracted and used to sign every delivery
// (X-Gitlab-Token header); the stored URL and audit record never include it.
// Every appended session event is then POSTed to the URL so an agent (Hermes,
// OpenClaw, ...) can react to host up/down, task lifecycle, and artifact
// changes without polling.
func (g *MemoryGateway) SetSessionNotifyURL(sessionID, rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	secret := ""
	if rawURL != "" {
		parsed, err := url.Parse(rawURL)
		if err != nil || !validNotifyURL(rawURL) {
			return fmt.Errorf("notify_url must be an absolute https URL (or http on loopback)")
		}
		if q := parsed.Query(); q.Has("secret") {
			secret = q.Get("secret")
			q.Del("secret")
			parsed.RawQuery = q.Encode()
			rawURL = parsed.String()
		}
	}
	if _, err := g.controlPlane().SetSessionNotifyURL(sessionID, rawURL); err != nil {
		return err
	}
	if secret != "" {
		g.notifySecretsMu.Lock()
		if g.notifySecrets == nil {
			g.notifySecrets = map[string]string{}
		}
		g.notifySecrets[sessionID] = secret
		g.notifySecretsMu.Unlock()
	} else {
		g.notifySecretsMu.Lock()
		delete(g.notifySecrets, sessionID)
		g.notifySecretsMu.Unlock()
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

// validNotifyURL accepts https URLs anywhere, plus http URLs on loopback
// addresses only (local agent webhooks). This keeps SSRF surface to the
// local host while enabling same-host agent integrations.
func validNotifyURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	if parsed.Scheme != "http" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

// notifyEvent is installed as the store event hook. It is invoked with the
// store lock held, so it only reads the already-resolved notify URL and
// signing secret, then hands the POST to a goroutine.
func (g *MemoryGateway) notifyEvent(sessionID string, event controlplane.Event, notifyURL string) {
	if strings.TrimSpace(notifyURL) == "" {
		return
	}
	secret := g.notifySecret(sessionID)
	go g.postNotification(sessionID, notifyURL, secret, event)
}

// notifySecrets holds webhook signing secrets by session ID. It lives at the
// gateway layer (not on controlplane.Session) so session serialization —
// snapshots, host join responses, audit — can never leak it.
func (g *MemoryGateway) notifySecret(sessionID string) string {
	g.notifySecretsMu.RLock()
	defer g.notifySecretsMu.RUnlock()
	return g.notifySecrets[sessionID]
}

func (g *MemoryGateway) postNotification(sessionID, rawURL, secret string, event controlplane.Event) {
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
	if secret != "" {
		// GitLab-style token header: the webhook platform (e.g. Hermes)
		// verifies it against its configured subscription secret.
		req.Header.Set("X-Gitlab-Token", secret)
	}
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
