package gateway

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

type blockingSnapshotStore struct {
	captured chan struct{}
	release  chan struct{}
	once     sync.Once
	mu       sync.Mutex
	durable  []Snapshot
}

func (s *blockingSnapshotStore) LoadInto(*MemoryGateway) (Snapshot, bool, error) {
	return Snapshot{}, false, nil
}

func (s *blockingSnapshotStore) SaveFrom(gw *MemoryGateway) (Snapshot, error) {
	snapshot := gw.Snapshot()
	blocked := false
	s.once.Do(func() {
		blocked = true
		close(s.captured)
	})
	if blocked {
		<-s.release
	}
	s.mu.Lock()
	s.durable = append(s.durable, snapshot)
	s.mu.Unlock()
	return snapshot, nil
}

func (*blockingSnapshotStore) Describe() string { return "blocking" }

func TestSerializedStateStoreMakesRollbackTheLastDurableSave(t *testing.T) {
	gw := NewMemoryGateway()
	ticket, err := gw.CreateTicketWithMetadata(model.HostModeAttendedTemporary, 600, nil, "serialized rollback", map[string]string{"auto_activate": "attended-temporary"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.RegisterHost(model.HostRegistration{TicketCode: ticket.Code, Name: "racing-host", OS: "windows", Arch: "amd64"}); err != nil {
		t.Fatal(err)
	}
	underlying := &blockingSnapshotStore{captured: make(chan struct{}), release: make(chan struct{})}
	store, err := NewSerializedStateStore(underlying)
	if err != nil {
		t.Fatal(err)
	}
	activeDone := make(chan error, 1)
	go func() {
		_, saveErr := store.SaveFrom(gw)
		activeDone <- saveErr
	}()
	<-underlying.captured
	if _, _, err := gw.RollbackTicket(ticket.ID, "publication failed"); err != nil {
		t.Fatal(err)
	}
	rollbackDone := make(chan error, 1)
	go func() {
		_, saveErr := store.SaveFrom(gw)
		rollbackDone <- saveErr
	}()
	select {
	case err := <-rollbackDone:
		t.Fatalf("rollback save bypassed serialized active save: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(underlying.release)
	if err := <-activeDone; err != nil {
		t.Fatal(err)
	}
	if err := <-rollbackDone; err != nil {
		t.Fatal(err)
	}
	underlying.mu.Lock()
	durable := append([]Snapshot(nil), underlying.durable...)
	underlying.mu.Unlock()
	if len(durable) != 2 {
		t.Fatalf("durable saves = %d, want active then revoked", len(durable))
	}
	last := durable[len(durable)-1]
	if len(last.Tickets) != 1 || last.Tickets[0].Status != model.TicketStatusRevoked || len(last.Hosts) != 1 || last.Hosts[0].Status != model.HostStatusRevoked {
		t.Fatalf("last durable snapshot can revive ticket or host: %#v", last)
	}
}

func TestFileStateStoreRoundTrip(t *testing.T) {
	publicKey, privateKey := gatewaySnapshotKeyPair(t)
	now := time.Date(2026, 6, 30, 18, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "store round trip")
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewFileStateStore(filepath.Join(t.TempDir(), "gateway", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.SaveFrom(gw)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tickets) != 1 || snapshot.Tickets[0].ID != ticket.ID {
		t.Fatalf("unexpected saved snapshot: %#v", snapshot.Tickets)
	}

	restarted := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	loaded, ok, err := store.LoadInto(restarted)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected state store load")
	}
	if len(loaded.Tickets) != 1 || loaded.Tickets[0].ID != ticket.ID {
		t.Fatalf("unexpected loaded snapshot: %#v", loaded.Tickets)
	}
}

func TestFileStateStoreRequiresPath(t *testing.T) {
	if _, err := NewFileStateStore(""); err == nil {
		t.Fatal("expected empty path to fail")
	}
}

func TestPostgresStateStoreRejectsInlinePassword(t *testing.T) {
	for _, connInfo := range []string{
		"postgres://user:secret@example.invalid/rdev?sslmode=require",
		"host=example.invalid user=rdev password=secret dbname=rdev",
	} {
		if _, err := NewPostgresStateStore(connInfo); err == nil {
			t.Fatalf("expected inline password to fail for %q", connInfo)
		}
	}
}

func TestPostgresStateStoreRoundTripThroughPSQL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake psql shell fixture uses POSIX shell")
	}
	root := t.TempDir()
	publicKey, privateKey := gatewaySnapshotKeyPair(t)
	now := time.Date(2026, 7, 4, 16, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "postgres store round trip")
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewPostgresStateStore("service=rdev_test")
	if err != nil {
		t.Fatal(err)
	}
	store.PSQLPath = writeFakePSQL(t, root)
	store.Timeout = 2 * time.Second
	snapshot, err := store.SaveFrom(gw)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tickets) != 1 || snapshot.Tickets[0].ID != ticket.ID {
		t.Fatalf("unexpected saved snapshot: %#v", snapshot.Tickets)
	}
	restarted := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	loaded, ok, err := store.LoadInto(restarted)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected postgres state store load")
	}
	if len(loaded.Tickets) != 1 || loaded.Tickets[0].ID != ticket.ID {
		t.Fatalf("unexpected loaded snapshot: %#v", loaded.Tickets)
	}
	transcript, err := os.ReadFile(filepath.Join(root, "psql-transcript.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if text := string(transcript); !strings.Contains(text, "CREATE TABLE IF NOT EXISTS rdev_gateway_snapshots") ||
		!strings.Contains(text, "ON CONFLICT (snapshot_key)") ||
		!strings.Contains(text, "SELECT snapshot_json::text") {
		t.Fatalf("fake psql transcript missing expected SQL:\n%s", text)
	}
}

func TestPostgresStateStoreVerifyRuntime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake psql shell fixture uses POSIX shell")
	}
	root := t.TempDir()
	store, err := NewPostgresStateStore("service=rdev_test")
	if err != nil {
		t.Fatal(err)
	}
	store.PSQLPath = writeFakePSQL(t, root)
	store.Timeout = 2 * time.Second
	if err := store.VerifyRuntime(); err != nil {
		t.Fatal(err)
	}
	transcript, err := os.ReadFile(filepath.Join(root, "psql-transcript.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if text := string(transcript); !strings.Contains(text, "DELETE FROM rdev_gateway_snapshots") {
		t.Fatalf("expected verify cleanup SQL, got:\n%s", text)
	}
}

func TestRedisStreamStateStoreRejectsInlineCredentials(t *testing.T) {
	for _, rawURL := range []string{
		"redis://default:secret@example.invalid:6379/0",
		"rediss://user@example.invalid:6379/0",
		"redis://example.invalid:6379/0?password=secret",
	} {
		if _, err := NewRedisStreamStateStore(rawURL); err == nil {
			t.Fatalf("expected inline credential rejection for %q", rawURL)
		}
	}
}

func TestRedisStreamStateStoreRoundTripThroughRedisCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake redis-cli shell fixture uses POSIX shell")
	}
	root := t.TempDir()
	publicKey, privateKey := gatewaySnapshotKeyPair(t)
	now := time.Date(2026, 7, 4, 20, 30, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "redis store round trip")
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewRedisStreamStateStore("rediss://redis.example.invalid:6379/0")
	if err != nil {
		t.Fatal(err)
	}
	store.CLIPath = writeFakeRedisCLI(t, root)
	store.Timeout = 2 * time.Second
	snapshot, err := store.SaveFrom(gw)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tickets) != 1 || snapshot.Tickets[0].ID != ticket.ID {
		t.Fatalf("unexpected saved snapshot: %#v", snapshot.Tickets)
	}
	restarted := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	loaded, ok, err := store.LoadInto(restarted)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected redis-stream state store load")
	}
	if len(loaded.Tickets) != 1 || loaded.Tickets[0].ID != ticket.ID {
		t.Fatalf("unexpected loaded snapshot: %#v", loaded.Tickets)
	}
	transcript, err := os.ReadFile(filepath.Join(root, "redis-transcript.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if text := string(transcript); !strings.Contains(text, "SET rdev:gateway:snapshot:current") ||
		!strings.Contains(text, "GET rdev:gateway:snapshot:current") ||
		!strings.Contains(text, "XADD rdev:gateway:snapshots") {
		t.Fatalf("fake redis transcript missing expected commands:\n%s", text)
	}
}

func TestRedisStreamStateStoreVerifyRuntime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake redis-cli shell fixture uses POSIX shell")
	}
	root := t.TempDir()
	store, err := NewRedisStreamStateStore("redis://redis.example.invalid:6379/0")
	if err != nil {
		t.Fatal(err)
	}
	store.CLIPath = writeFakeRedisCLI(t, root)
	store.Timeout = 2 * time.Second
	if err := store.VerifyRuntime(); err != nil {
		t.Fatal(err)
	}
	transcript, err := os.ReadFile(filepath.Join(root, "redis-transcript.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if text := string(transcript); !strings.Contains(text, "DEL rdev:gateway:verify:") ||
		!strings.Contains(text, "XADD rdev:gateway:snapshots") {
		t.Fatalf("expected verify Redis commands, got:\n%s", text)
	}
}

func TestS3CompatibleStateStoreRejectsUnsafeLocations(t *testing.T) {
	for _, location := range []string{
		"https://bucket.example.invalid/rdev",
		"s3://user@example-bucket/rdev",
		"s3://example-bucket/rdev?access_key=secret",
		"s3://example-bucket/rdev#secret",
	} {
		if _, err := NewS3CompatibleStateStore(location); err == nil {
			t.Fatalf("expected unsafe S3-compatible location to fail for %q", location)
		}
	}
}

func TestS3CompatibleStateStoreRoundTripThroughAWSCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake aws shell fixture uses POSIX shell")
	}
	root := t.TempDir()
	publicKey, privateKey := gatewaySnapshotKeyPair(t)
	now := time.Date(2026, 7, 4, 22, 15, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "s3-compatible store round trip")
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewS3CompatibleStateStore("s3://rdev-state/rdev/gateway")
	if err != nil {
		t.Fatal(err)
	}
	store.AWSPath = writeFakeAWSCLI(t, root)
	store.Timeout = 2 * time.Second
	snapshot, err := store.SaveFrom(gw)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tickets) != 1 || snapshot.Tickets[0].ID != ticket.ID {
		t.Fatalf("unexpected saved snapshot: %#v", snapshot.Tickets)
	}
	restarted := NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-state", publicKey, privateKey)
	loaded, ok, err := store.LoadInto(restarted)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected s3-compatible state store load")
	}
	if len(loaded.Tickets) != 1 || loaded.Tickets[0].ID != ticket.ID {
		t.Fatalf("unexpected loaded snapshot: %#v", loaded.Tickets)
	}
	transcript, err := os.ReadFile(filepath.Join(root, "aws-transcript.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if text := string(transcript); !strings.Contains(text, "put-object --bucket rdev-state --key rdev/gateway/snapshot-current.json") ||
		!strings.Contains(text, "get-object --bucket rdev-state --key rdev/gateway/snapshot-current.json") {
		t.Fatalf("fake aws transcript missing expected commands:\n%s", text)
	}
}

func TestS3CompatibleStateStoreVerifyRuntime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake aws shell fixture uses POSIX shell")
	}
	root := t.TempDir()
	store, err := NewS3CompatibleStateStore("s3://rdev-state/rdev/gateway")
	if err != nil {
		t.Fatal(err)
	}
	store.AWSPath = writeFakeAWSCLI(t, root)
	store.Timeout = 2 * time.Second
	if err := store.VerifyRuntime(); err != nil {
		t.Fatal(err)
	}
	transcript, err := os.ReadFile(filepath.Join(root, "aws-transcript.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if text := string(transcript); !strings.Contains(text, "delete-object --bucket rdev-state --key rdev/gateway/verify-") ||
		!strings.Contains(text, "get-object --bucket rdev-state --key rdev/gateway/verify-") {
		t.Fatalf("expected verify AWS commands, got:\n%s", text)
	}
}

func writeFakePSQL(t *testing.T, root string) string {
	t.Helper()
	script := filepath.Join(root, "fake-psql.sh")
	state := filepath.Join(root, "snapshot.json")
	input := filepath.Join(root, "psql-input.sql")
	transcript := filepath.Join(root, "psql-transcript.sql")
	content := `#!/bin/sh
set -eu
cat > "` + input + `"
sql="$(cat "` + input + `")"
printf '%s\n---\n' "$sql" >> "` + transcript + `"
case "$sql" in
  *"SELECT snapshot_json::text"*)
    if [ -f "` + state + `" ]; then
      cat "` + state + `"
      printf '\n'
    fi
    ;;
  *"DELETE FROM rdev_gateway_snapshots"*)
    rm -f "` + state + `"
    ;;
  *"INSERT INTO rdev_gateway_snapshots"*)
    python3 - "` + state + `" "` + input + `" <<'PY'
import re
import sys
state = sys.argv[1]
sql_path = sys.argv[2]
with open(sql_path, "r", encoding="utf-8") as handle:
    sql = handle.read()
match = re.search(r"\$rdev_[0-9a-f]+\$(.*)\$rdev_[0-9a-f]+\$::jsonb", sql, re.S)
if not match:
    raise SystemExit("missing JSONB dollar quote")
with open(state, "w", encoding="utf-8") as handle:
    handle.write(match.group(1).strip())
PY
    ;;
esac
`
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return script
}

func writeFakeAWSCLI(t *testing.T, root string) string {
	t.Helper()
	script := filepath.Join(root, "fake-aws.sh")
	stateDir := filepath.Join(root, "aws-objects")
	transcript := filepath.Join(root, "aws-transcript.txt")
	content := `#!/bin/sh
set -eu
service="$1"
operation="$2"
shift 2
bucket=""
key=""
body=""
outfile=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --bucket)
      bucket="$2"
      shift 2
      ;;
    --key)
      key="$2"
      shift 2
      ;;
    --body)
      body="$2"
      shift 2
      ;;
    *)
      if [ -z "$outfile" ]; then
        outfile="$1"
      fi
      shift
      ;;
  esac
done
printf '%s %s --bucket %s --key %s' "$service" "$operation" "$bucket" "$key" >> "` + transcript + `"
if [ -n "$body" ]; then
  printf ' --body %s' "$body" >> "` + transcript + `"
fi
if [ -n "$outfile" ]; then
  printf ' %s' "$outfile" >> "` + transcript + `"
fi
printf '\n' >> "` + transcript + `"
safe_key="$(printf '%s' "$bucket/$key" | tr '/:' '__')"
object="` + stateDir + `/$safe_key"
case "$operation" in
  put-object)
    mkdir -p "` + stateDir + `"
    cp "$body" "$object"
    printf '{"ETag":"fake"}\n'
    ;;
  get-object)
    if [ ! -f "$object" ]; then
      echo "NoSuchKey" >&2
      exit 1
    fi
    cp "$object" "$outfile"
    printf '{"ContentLength":1}\n'
    ;;
  delete-object)
    rm -f "$object"
    printf '{}\n'
    ;;
  *)
    echo "unsupported fake aws operation: $operation" >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return script
}

func writeFakeRedisCLI(t *testing.T, root string) string {
	t.Helper()
	script := filepath.Join(root, "fake-redis-cli.sh")
	state := filepath.Join(root, "redis-state.json")
	transcript := filepath.Join(root, "redis-transcript.txt")
	content := `#!/bin/sh
set -eu
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --raw)
      shift
      ;;
    --url)
      url="$2"
      shift 2
      ;;
    *)
      break
      ;;
  esac
done
cmd="$1"
shift || true
printf '%s %s\n' "$cmd" "$*" >> "` + transcript + `"
case "$cmd" in
  SET)
    printf '%s' "$2" > "` + state + `"
    printf 'OK\n'
    ;;
  GET)
    if [ -f "` + state + `" ]; then
      cat "` + state + `"
      printf '\n'
    fi
    ;;
  DEL)
    rm -f "` + state + `"
    printf '1\n'
    ;;
  XADD)
    printf '0-1\n'
    ;;
  *)
    echo "unsupported fake redis command: $cmd" >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return script
}
