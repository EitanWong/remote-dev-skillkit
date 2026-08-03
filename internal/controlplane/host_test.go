package controlplane

import (
	"testing"
	"time"
)

func hostStoreHarness() (*MemoryStore, *time.Time) {
	now := time.Date(2026, 8, 3, 4, 0, 0, 0, time.UTC)
	clock := &now
	return NewMemoryStore(func() time.Time { return *clock }), clock
}

func joinTargetHost(t *testing.T, store *MemoryStore, name, fingerprint, platform string, at time.Time) (Session, Endpoint) {
	t.Helper()
	session, err := store.CreateSession(SessionSpec{Reason: "host directory fixture", JoinPolicy: "single-target"})
	if err != nil {
		t.Fatal(err)
	}
	joined, endpoint, _, _, err := store.JoinByCode(session.JoinCode, EndpointSpec{
		Role:                EndpointRoleTarget,
		Name:                name,
		Platform:            platform,
		IdentityFingerprint: fingerprint,
		Capabilities:        []string{"shell.user"},
		Transport:           TransportLongPoll,
	})
	if err != nil {
		t.Fatal(err)
	}
	return joined, endpoint
}

func TestHostDirectoryRegistersTargetOnJoin(t *testing.T) {
	store, now := hostStoreHarness()
	_, endpoint := joinTargetHost(t, store, "dev-win", "fp-win", "windows/amd64", *now)

	hosts := store.Hosts()
	if len(hosts) != 1 {
		t.Fatalf("expected one host record, got %#v", hosts)
	}
	host := hosts[0]
	if host.HostID == "" || host.IdentityFingerprint != "fp-win" {
		t.Fatalf("host record missing identity: %#v", host)
	}
	if host.DisplayName != "dev-win" {
		t.Fatalf("host display name = %q, want dev-win", host.DisplayName)
	}
	if host.Platform != "windows/amd64" || host.State != EndpointStateOnline {
		t.Fatalf("host platform/state = %q/%q", host.Platform, host.State)
	}
	if host.LastSessionID == "" || host.LastEndpointID != endpoint.ID {
		t.Fatalf("host session/endpoint refs = %q/%q", host.LastSessionID, host.LastEndpointID)
	}
}

func TestHostDirectoryRejoinKeepsOperatorDisplayName(t *testing.T) {
	store, now := hostStoreHarness()
	_, _ = joinTargetHost(t, store, "dev-win", "fp-win", "windows/amd64", *now)

	host := store.Hosts()[0]
	renamed, err := store.RenameHost(host.HostID, "Unity Dev Machine")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.DisplayName != "Unity Dev Machine" {
		t.Fatalf("rename did not apply: %#v", renamed)
	}

	// Rejoin with the connector's default name; the operator name must survive.
	*now = now.Add(time.Minute)
	_, _ = joinTargetHost(t, store, "USER-1234", "fp-win", "windows/amd64", *now)
	hosts := store.Hosts()
	if len(hosts) != 1 || hosts[0].DisplayName != "Unity Dev Machine" {
		t.Fatalf("rejoin overwrote operator display name: %#v", hosts)
	}
	if !hosts[0].LastSeenAt.Equal(*now) {
		t.Fatalf("rejoin did not refresh last_seen: %#v", hosts[0])
	}
}

func TestHostDirectoryRenameValidation(t *testing.T) {
	store, _ := hostStoreHarness()
	_, _ = joinTargetHost(t, store, "dev-win", "fp-win", "windows/amd64", time.Now())
	host := store.Hosts()[0]

	for _, bad := range []string{"", "  ", "line\nbreak", "tab\there", "too-long-" + string(make([]byte, 80))} {
		if _, err := store.RenameHost(host.HostID, bad); err == nil {
			t.Fatalf("rename %q should fail", bad)
		}
	}
	if _, err := store.RenameHost("hst_missing", "valid"); err == nil {
		t.Fatal("rename of unknown host should fail")
	}
}

func TestHostDirectorySnapshotRoundTrip(t *testing.T) {
	store, now := hostStoreHarness()
	_, _ = joinTargetHost(t, store, "dev-win", "fp-win", "windows/amd64", *now)
	*now = now.Add(time.Second)
	_, _ = joinTargetHost(t, store, "dev-linux", "fp-linux", "linux/amd64", *now)
	host := store.Hosts()[0]
	if _, err := store.RenameHost(host.HostID, "Unity Dev Machine"); err != nil {
		t.Fatal(err)
	}

	snapshot := store.Snapshot()
	if len(snapshot.Hosts) != 2 {
		t.Fatalf("snapshot hosts = %d, want 2", len(snapshot.Hosts))
	}

	restored := NewMemoryStore(func() time.Time { return *now })
	if err := restored.RestoreSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	hosts := restored.Hosts()
	if len(hosts) != 2 {
		t.Fatalf("restored hosts = %d, want 2", len(hosts))
	}
	byFingerprint := map[string]Host{}
	for _, h := range hosts {
		byFingerprint[h.IdentityFingerprint] = h
	}
	if byFingerprint["fp-linux"].DisplayName != "Unity Dev Machine" {
		t.Fatalf("restored operator name lost: %#v", byFingerprint["fp-linux"])
	}
	if byFingerprint["fp-win"].DisplayName != "dev-win" {
		t.Fatalf("restored connector name lost: %#v", byFingerprint["fp-win"])
	}
}

func TestHostDirectorySortsNewestLastSeenFirst(t *testing.T) {
	store, now := hostStoreHarness()
	_, _ = joinTargetHost(t, store, "old-host", "fp-old", "linux/amd64", *now)
	*now = now.Add(5 * time.Minute)
	_, _ = joinTargetHost(t, store, "new-host", "fp-new", "linux/amd64", *now)

	hosts := store.Hosts()
	if len(hosts) != 2 || hosts[0].IdentityFingerprint != "fp-new" || hosts[1].IdentityFingerprint != "fp-old" {
		t.Fatalf("hosts not sorted by last_seen desc: %#v", hosts)
	}
}
