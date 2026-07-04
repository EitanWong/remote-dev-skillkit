package gateway

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

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
