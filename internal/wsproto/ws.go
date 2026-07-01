package wsproto

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

const (
	MessageReady    = "ready"
	MessageJob      = "job"
	MessageComplete = "complete"
	MessageFail     = "fail"
	MessageArtifact = "artifact"
	MessageNoop     = "noop"
	MessageError    = "error"
)

type Message struct {
	Type            string     `json:"type"`
	HostID          string     `json:"host_id,omitempty"`
	JobID           string     `json:"job_id,omitempty"`
	Job             *model.Job `json:"job,omitempty"`
	ArtifactContent string     `json:"artifact_content,omitempty"`
	Reason          string     `json:"reason,omitempty"`
	Error           string     `json:"error,omitempty"`
}

type Conn struct {
	conn net.Conn
	br   *bufio.Reader
}

func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, fmt.Errorf("websocket upgrade header is required")
	}
	if !headerContains(r.Header, "Connection", "upgrade") {
		return nil, fmt.Errorf("websocket connection upgrade is required")
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		return nil, fmt.Errorf("websocket key is required")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		return nil, fmt.Errorf("websocket version 13 is required")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, fmt.Errorf("response writer cannot hijack connections")
	}
	raw, brw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}
	accept := websocketAccept(key)
	_, err = fmt.Fprintf(brw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	if err := brw.Flush(); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return &Conn{conn: raw, br: brw.Reader}, nil
}

func Dial(ctx context.Context, rawURL string, tlsConfig *tls.Config) (*Conn, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	var address string
	switch parsed.Scheme {
	case "ws":
		address = parsed.Host
	case "wss":
		address = parsed.Host
	default:
		return nil, fmt.Errorf("unsupported websocket scheme %q", parsed.Scheme)
	}
	if !strings.Contains(address, ":") {
		if parsed.Scheme == "wss" {
			address += ":443"
		} else {
			address += ":80"
		}
	}
	dialer := &net.Dialer{}
	raw, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	conn := raw
	if parsed.Scheme == "wss" {
		cfg := tlsConfig
		if cfg == nil {
			cfg = &tls.Config{MinVersion: tls.VersionTLS12}
		} else {
			cfg = cfg.Clone()
		}
		if cfg.ServerName == "" {
			cfg.ServerName = parsed.Hostname()
		}
		tlsConn := tls.Client(raw, cfg)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = raw.Close()
			return nil, err
		}
		conn = tlsConn
	}
	key, err := randomWebSocketKey()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	path := parsed.RequestURI()
	if path == "" {
		path = "/"
	}
	host := parsed.Host
	request := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", path, host, key)
	if _, err := io.WriteString(conn, request); err != nil {
		_ = conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()
		return nil, fmt.Errorf("websocket upgrade failed: %s", resp.Status)
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != websocketAccept(key) {
		_ = conn.Close()
		return nil, fmt.Errorf("websocket accept mismatch")
	}
	return &Conn{conn: conn, br: reader}, nil
}

func (c *Conn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Conn) WriteJSON(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.writeFrame(1, payload, true)
}

func (c *Conn) ReadJSON(value any) error {
	payload, err := c.readFrame()
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, value)
}

func (c *Conn) writeFrame(opcode byte, payload []byte, client bool) error {
	header := []byte{0x80 | opcode}
	maskBit := byte(0)
	if client {
		maskBit = 0x80
	}
	switch {
	case len(payload) < 126:
		header = append(header, maskBit|byte(len(payload)))
	case len(payload) <= 65535:
		header = append(header, maskBit|126, byte(len(payload)>>8), byte(len(payload)))
	default:
		header = append(header, maskBit|127)
		size := make([]byte, 8)
		binary.BigEndian.PutUint64(size, uint64(len(payload)))
		header = append(header, size...)
	}
	if client {
		mask := make([]byte, 4)
		if _, err := rand.Read(mask); err != nil {
			return err
		}
		header = append(header, mask...)
		masked := append([]byte(nil), payload...)
		for i := range masked {
			masked[i] ^= mask[i%4]
		}
		payload = masked
	}
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(payload)
	return err
}

func (c *Conn) readFrame() ([]byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(c.br, header); err != nil {
		return nil, err
	}
	opcode := header[0] & 0x0f
	if opcode == 8 {
		return nil, io.EOF
	}
	if opcode != 1 {
		return nil, fmt.Errorf("unsupported websocket opcode %d", opcode)
	}
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		var raw [2]byte
		if _, err := io.ReadFull(c.br, raw[:]); err != nil {
			return nil, err
		}
		length = uint64(binary.BigEndian.Uint16(raw[:]))
	case 127:
		var raw [8]byte
		if _, err := io.ReadFull(c.br, raw[:]); err != nil {
			return nil, err
		}
		length = binary.BigEndian.Uint64(raw[:])
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.br, mask[:]); err != nil {
			return nil, err
		}
	}
	if length > 16*1024*1024 {
		return nil, fmt.Errorf("websocket frame too large")
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(c.br, payload); err != nil {
		return nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return payload, nil
}

func HTTPToWebSocketURL(gatewayURL, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(gatewayURL, "/") + path)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported gateway scheme %q", parsed.Scheme)
	}
	return parsed.String(), nil
}

func headerContains(header http.Header, name, value string) bool {
	for _, part := range strings.Split(header.Get(name), ",") {
		if strings.EqualFold(strings.TrimSpace(part), value) {
			return true
		}
	}
	return false
}

func websocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func randomWebSocketKey() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}
