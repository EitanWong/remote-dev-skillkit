package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

const FileStateStoreProvider = "file"
const PostgresStateStoreProvider = "postgres"
const RedisStreamStateStoreProvider = "redis-stream"

type StateStore interface {
	LoadInto(*MemoryGateway) (Snapshot, bool, error)
	SaveFrom(*MemoryGateway) (Snapshot, error)
	Describe() string
}

type FileStateStore struct {
	Path string
}

func NewFileStateStore(path string) (FileStateStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return FileStateStore{}, fmt.Errorf("file state store path is required")
	}
	return FileStateStore{Path: path}, nil
}

func (s FileStateStore) LoadInto(gw *MemoryGateway) (Snapshot, bool, error) {
	if gw == nil {
		return Snapshot{}, false, fmt.Errorf("gateway is required")
	}
	return gw.LoadSnapshotIfExists(s.Path)
}

func (s FileStateStore) SaveFrom(gw *MemoryGateway) (Snapshot, error) {
	if gw == nil {
		return Snapshot{}, fmt.Errorf("gateway is required")
	}
	return gw.SaveSnapshot(s.Path)
}

func (s FileStateStore) Describe() string {
	return FileStateStoreProvider + ":" + s.Path
}

type PostgresStateStore struct {
	ConnInfo string
	PSQLPath string
	Timeout  time.Duration
}

func NewPostgresStateStore(connInfo string) (PostgresStateStore, error) {
	connInfo = strings.TrimSpace(connInfo)
	if connInfo == "" {
		return PostgresStateStore{}, fmt.Errorf("postgres state store connection info is required")
	}
	if postgresConnInfoHasInlineSecret(connInfo) {
		return PostgresStateStore{}, fmt.Errorf("postgres state store connection info must not contain inline passwords; use libpq service files, environment, or .pgpass")
	}
	return PostgresStateStore{
		ConnInfo: connInfo,
		PSQLPath: "psql",
		Timeout:  10 * time.Second,
	}, nil
}

func (s PostgresStateStore) LoadInto(gw *MemoryGateway) (Snapshot, bool, error) {
	if gw == nil {
		return Snapshot{}, false, fmt.Errorf("gateway is required")
	}
	if err := s.ensureSchema(); err != nil {
		return Snapshot{}, false, err
	}
	output, err := s.runPSQL("SELECT snapshot_json::text FROM rdev_gateway_snapshots WHERE snapshot_key = 'current';")
	if err != nil {
		return Snapshot{}, false, err
	}
	content := strings.TrimSpace(output)
	if content == "" {
		return Snapshot{}, false, nil
	}
	var snapshot Snapshot
	if err := json.Unmarshal([]byte(content), &snapshot); err != nil {
		return Snapshot{}, false, fmt.Errorf("parse postgres gateway snapshot: %w", err)
	}
	if err := gw.RestoreSnapshot(snapshot); err != nil {
		return Snapshot{}, false, err
	}
	return snapshot, true, nil
}

func (s PostgresStateStore) SaveFrom(gw *MemoryGateway) (Snapshot, error) {
	if gw == nil {
		return Snapshot{}, fmt.Errorf("gateway is required")
	}
	if err := s.ensureSchema(); err != nil {
		return Snapshot{}, err
	}
	snapshot := gw.Snapshot()
	content, err := json.Marshal(snapshot)
	if err != nil {
		return Snapshot{}, err
	}
	if _, err := s.runPSQL(postgresUpsertSnapshotSQL("current", content)); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func (s PostgresStateStore) Describe() string {
	return PostgresStateStoreProvider + ":libpq-connection-info"
}

func (s PostgresStateStore) VerifyRuntime() error {
	if err := s.ensureSchema(); err != nil {
		return err
	}
	probe := []byte(`{"schema_version":"rdev.gateway-storage-probe.v1","ok":true}`)
	key := "verify_" + postgresDollarQuoteTag(probe)
	if _, err := s.runPSQL(postgresUpsertSnapshotSQL(key, probe)); err != nil {
		return err
	}
	output, err := s.runPSQL("SELECT snapshot_json::text FROM rdev_gateway_snapshots WHERE snapshot_key = '" + key + "';")
	if err != nil {
		return err
	}
	if !strings.Contains(output, `"ok": true`) && !strings.Contains(output, `"ok":true`) {
		return fmt.Errorf("postgres state store probe readback did not contain ok=true")
	}
	if _, err := s.runPSQL("DELETE FROM rdev_gateway_snapshots WHERE snapshot_key = '" + key + "';"); err != nil {
		return err
	}
	return nil
}

func (s PostgresStateStore) ensureSchema() error {
	_, err := s.runPSQL(`
CREATE TABLE IF NOT EXISTS rdev_gateway_snapshots (
  snapshot_key TEXT PRIMARY KEY,
  snapshot_json JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);`)
	return err
}

func (s PostgresStateStore) runPSQL(sql string) (string, error) {
	psqlPath := strings.TrimSpace(s.PSQLPath)
	if psqlPath == "" {
		psqlPath = "psql"
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, psqlPath, "--no-psqlrc", "--set", "ON_ERROR_STOP=1", "--quiet", "--tuples-only", "--no-align", "--dbname", s.ConnInfo, "--file", "-")
	cmd.Stdin = strings.NewReader(sql)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("psql state store command failed: %s", detail)
	}
	return stdout.String(), nil
}

func postgresUpsertSnapshotSQL(key string, content []byte) string {
	tag := postgresDollarQuoteTag(content)
	if bytes.Contains(content, []byte("$"+tag+"$")) {
		tag += "_alt"
	}
	return fmt.Sprintf(`
INSERT INTO rdev_gateway_snapshots (snapshot_key, snapshot_json, updated_at)
VALUES ('%s', $%s$%s$%s$::jsonb, NOW())
ON CONFLICT (snapshot_key)
DO UPDATE SET snapshot_json = EXCLUDED.snapshot_json, updated_at = NOW();
`, key, tag, string(content), tag)
}

func postgresDollarQuoteTag(content []byte) string {
	sum := sha256.Sum256(content)
	return "rdev_" + hex.EncodeToString(sum[:])
}

func postgresConnInfoHasInlineSecret(connInfo string) bool {
	lower := strings.ToLower(connInfo)
	if strings.Contains(lower, "password=") {
		return true
	}
	parsed, err := url.Parse(connInfo)
	if err == nil && parsed.User != nil {
		_, hasPassword := parsed.User.Password()
		return hasPassword
	}
	return false
}

type RedisStreamStateStore struct {
	URL       string
	KeyPrefix string
	CLIPath   string
	Timeout   time.Duration
}

func NewRedisStreamStateStore(rawURL string) (RedisStreamStateStore, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return RedisStreamStateStore{}, fmt.Errorf("redis-stream state store URL is required")
	}
	if redisURLHasInlineSecret(rawURL) {
		return RedisStreamStateStore{}, fmt.Errorf("redis-stream state store URL must not contain inline credentials; use REDISCLI_AUTH or an operator-approved secret injector")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return RedisStreamStateStore{}, fmt.Errorf("parse redis-stream state store URL: %w", err)
	}
	if parsed.Scheme != "redis" && parsed.Scheme != "rediss" {
		return RedisStreamStateStore{}, fmt.Errorf("redis-stream state store URL must use redis:// or rediss://")
	}
	if parsed.Host == "" {
		return RedisStreamStateStore{}, fmt.Errorf("redis-stream state store URL missing host")
	}
	return RedisStreamStateStore{
		URL:       rawURL,
		KeyPrefix: "rdev:gateway",
		CLIPath:   "redis-cli",
		Timeout:   10 * time.Second,
	}, nil
}

func (s RedisStreamStateStore) LoadInto(gw *MemoryGateway) (Snapshot, bool, error) {
	if gw == nil {
		return Snapshot{}, false, fmt.Errorf("gateway is required")
	}
	output, err := s.runRedis("GET", s.snapshotKey())
	if err != nil {
		return Snapshot{}, false, err
	}
	content := strings.TrimSpace(output)
	if content == "" {
		return Snapshot{}, false, nil
	}
	var snapshot Snapshot
	if err := json.Unmarshal([]byte(content), &snapshot); err != nil {
		return Snapshot{}, false, fmt.Errorf("parse redis-stream gateway snapshot: %w", err)
	}
	if err := gw.RestoreSnapshot(snapshot); err != nil {
		return Snapshot{}, false, err
	}
	return snapshot, true, nil
}

func (s RedisStreamStateStore) SaveFrom(gw *MemoryGateway) (Snapshot, error) {
	if gw == nil {
		return Snapshot{}, fmt.Errorf("gateway is required")
	}
	snapshot := gw.Snapshot()
	content, err := json.Marshal(snapshot)
	if err != nil {
		return Snapshot{}, err
	}
	if _, err := s.runRedis("SET", s.snapshotKey(), string(content)); err != nil {
		return Snapshot{}, err
	}
	if _, err := s.runRedis("XADD", s.streamKey(), "*", "schema_version", "rdev.gateway-snapshot.v1", "snapshot_key", "current"); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func (s RedisStreamStateStore) Describe() string {
	return RedisStreamStateStoreProvider + ":redis-url"
}

func (s RedisStreamStateStore) VerifyRuntime() error {
	probeKey := s.key("verify:" + redisKeySuffix([]byte(time.Now().UTC().Format(time.RFC3339Nano))))
	probe := `{"schema_version":"rdev.gateway-storage-probe.v1","ok":true}`
	if _, err := s.runRedis("SET", probeKey, probe); err != nil {
		return err
	}
	output, err := s.runRedis("GET", probeKey)
	if err != nil {
		return err
	}
	if !strings.Contains(output, `"ok": true`) && !strings.Contains(output, `"ok":true`) {
		return fmt.Errorf("redis-stream state store probe readback did not contain ok=true")
	}
	if _, err := s.runRedis("XADD", s.streamKey(), "*", "schema_version", "rdev.gateway-storage-probe.v1", "ok", "true"); err != nil {
		return err
	}
	if _, err := s.runRedis("DEL", probeKey); err != nil {
		return err
	}
	return nil
}

func (s RedisStreamStateStore) snapshotKey() string {
	return s.key("snapshot:current")
}

func (s RedisStreamStateStore) streamKey() string {
	return s.key("snapshots")
}

func (s RedisStreamStateStore) key(suffix string) string {
	prefix := strings.Trim(strings.TrimSpace(s.KeyPrefix), ":")
	if prefix == "" {
		prefix = "rdev:gateway"
	}
	return prefix + ":" + suffix
}

func (s RedisStreamStateStore) runRedis(args ...string) (string, error) {
	cliPath := strings.TrimSpace(s.CLIPath)
	if cliPath == "" {
		cliPath = "redis-cli"
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	argv := append([]string{"--raw", "--url", s.URL}, args...)
	cmd := exec.CommandContext(ctx, cliPath, argv...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("redis-cli state store command failed: %s", detail)
	}
	return stdout.String(), nil
}

func redisURLHasInlineSecret(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	for _, marker := range []string{"pass" + "word=", "pass=", "auth="} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.User != nil {
		if parsed.User.Username() != "" {
			return true
		}
		_, hasPassword := parsed.User.Password()
		return hasPassword
	}
	return false
}

func redisKeySuffix(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
