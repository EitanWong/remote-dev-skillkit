package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestMemoryGatewayCoverageSurface(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gw := NewMemoryGatewayWithSigningKey(clock, "gateway-test", publicKey, privateKey)

	if _, ok := gw.EnrollmentRoot(); ok {
		t.Fatal("unconfigured enrollment root reported as configured")
	}
	if _, ok := gw.EnrollmentRevocations(); ok {
		t.Fatal("unconfigured enrollment revocations reported as configured")
	}
	root := model.NewTrustBundle("enrollment-test", publicKey)
	revocations := model.HostEnrollmentRevocationList{SchemaVersion: "rdev.host-enrollment-revocation-list.v1"}
	enrollmentGateway := NewMemoryGatewayWithSigningKey(clock, "enrollment-gateway", publicKey, privateKey).
		WithEnrollmentRoot(root).WithEnrollmentRevocations(revocations)
	if got, ok := enrollmentGateway.EnrollmentRoot(); !ok || got.SigningKeyID != root.SigningKeyID {
		t.Fatalf("enrollment root = %#v, configured=%t", got, ok)
	}
	if got, ok := enrollmentGateway.EnrollmentRevocations(); !ok || got.SchemaVersion != revocations.SchemaVersion {
		t.Fatalf("enrollment revocations = %#v, configured=%t", got, ok)
	}

	if got := gw.TrustBundle(); got.SigningKeyID != "gateway-test" {
		t.Fatalf("trust bundle = %#v", got)
	}
	if got := gw.ManifestRoot(); got.SigningKeyID != "gateway-test" {
		t.Fatalf("manifest root = %#v", got)
	}
	initial := gw.SignedTrustBundle()
	initialHash, err := initial.Hash()
	if err != nil {
		t.Fatal(err)
	}

	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, []string{"shell.user"}, "coverage")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := gw.JoinManifest(ticket.Code, "http://127.0.0.1:8787", "http://127.0.0.1:8787/join/"+ticket.Code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.JoinManifestTimeProof(manifest); err != nil {
		t.Fatal(err)
	}

	host, err := gw.RegisterHost(model.HostRegistration{TicketCode: ticket.Code, Name: "coverage-host", OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := gw.GenerateHostSecret(host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gw.ValidateHostSecret(host.ID, "") || !gw.ValidateHostSecret(host.ID, secret) {
		t.Fatal("host secret validation did not distinguish empty and valid secrets")
	}
	if err := gw.HeartbeatHost(host.ID, secret); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("pending heartbeat error = %v, want invalid state", err)
	}
	host, err = gw.ActivateHost(host.ID, []string{"shell.user"})
	if err != nil {
		t.Fatal(err)
	}
	if err := gw.HeartbeatHost(host.ID, secret); err != nil {
		t.Fatal(err)
	}
	if !gw.TicketHasConnectedHost(ticket.ID) {
		t.Fatal("active host was not reported as connected")
	}
	if !gw.HostIsLive(host.ID, time.Minute) || gw.HostIsLive("missing", time.Minute) {
		t.Fatal("host liveness did not report active and missing hosts correctly")
	}
	now = now.Add(2 * time.Minute)
	if gw.HostIsLive(host.ID, time.Minute) {
		t.Fatal("stale heartbeat reported as live")
	}
	if gw.TicketHasConnectedHost(ticket.ID) {
		t.Fatal("stale host was reported as connected")
	}
	preconnect := model.SupportSessionPreconnect{TicketCode: ticket.Code, Phase: "started", OS: "darwin", Arch: "arm64", Message: "ready"}
	firstPreconnect, err := gw.RecordSupportSessionPreconnect(preconnect)
	if err != nil || firstPreconnect.SeenCount != 1 {
		t.Fatalf("first preconnect = %#v, err=%v", firstPreconnect, err)
	}
	secondPreconnect, err := gw.RecordSupportSessionPreconnect(preconnect)
	if err != nil || secondPreconnect.SeenCount != 2 {
		t.Fatalf("deduplicated preconnect = %#v, err=%v", secondPreconnect, err)
	}
	if got := gw.SupportSessionPreconnects(ticket.Code); len(got) != 1 {
		t.Fatalf("preconnect count = %d, want 1", len(got))
	}

	if _, err := gw.TrustBundleUpdateForHost(host.ID, -1, ""); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("negative trust sequence error = %v", err)
	}
	if _, err := gw.TrustBundleUpdateForHost(host.ID, 2, ""); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("newer trust sequence error = %v", err)
	}
	if _, err := gw.TrustBundleUpdateForHost(host.ID, initial.Sequence, "sha256:wrong"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("trust hash mismatch error = %v", err)
	}
	if update, err := gw.TrustBundleUpdateForHost(host.ID, initial.Sequence, initialHash); err != nil || update.Status != model.TrustBundleUpdateStatusCurrent {
		t.Fatalf("current trust update = %#v, err=%v", update, err)
	}
	if update, err := gw.TrustBundleUpdateForHost(host.ID, 0, ""); err != nil || update.Status != model.TrustBundleUpdateStatusAvailable {
		t.Fatalf("available trust update = %#v, err=%v", update, err)
	}

	nextUnsigned, err := model.NewSignedTrustBundle(model.SignedTrustBundleSpec{
		BundleID:           "gateway-test-next",
		Sequence:           initial.Sequence + 1,
		PreviousBundleHash: initialHash,
		SigningKeyID:       "gateway-test",
		Keys:               []model.TrustKey{model.NewTrustKey("gateway-test", publicKey, model.TrustKeyStatusActive, now.Add(-time.Minute))},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	next, err := nextUnsigned.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if updated, err := gw.UpdateSignedTrustBundle(next); err != nil || updated.Sequence != next.Sequence {
		t.Fatalf("trust bundle update = %#v, err=%v", updated, err)
	}

	session, err := gw.CreateSession(controlplane.SessionSpec{Reason: "coverage session"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := gw.JoinSession(session.ID, controlplane.EndpointSpec{
		Role:         controlplane.EndpointRoleTarget,
		Name:         "coverage-target",
		Platform:     "darwin/arm64",
		Capabilities: []string{"shell.user"},
	}); err != nil {
		t.Fatal(err)
	}
	if events, err := gw.AppendSessionEventBatch(session.ID, []controlplane.Event{
		{Type: controlplane.EventTypeStatus, IdempotencyKey: "coverage-event-1"},
		{Type: controlplane.EventTypeStatus, IdempotencyKey: "coverage-event-2"},
	}); err != nil || len(events) != 2 {
		t.Fatalf("event batch = %#v, err=%v", events, err)
	}
	if events, _, err := gw.SessionEventsAfterForAgent(session.ID, 0, 10); err != nil || len(events) != 3 {
		t.Fatalf("agent event replay = %#v, err=%v", events, err)
	}
	task, _, err := gw.SubmitSessionTask(session.ID, controlplane.TaskSpec{Adapter: "shell", IdempotencyKey: "coverage-task"})
	if err != nil {
		t.Fatal(err)
	}
	if canceled, _, err := gw.CancelSessionTask(session.ID, task.ID, "coverage", "coverage-cancel"); err != nil || canceled.Status != controlplane.TaskStatusCanceled {
		t.Fatalf("canceled task = %#v, err=%v", canceled, err)
	}
	if len(gw.AuditEvents()) == 0 {
		t.Fatal("gateway audit events were not retained")
	}
	idempotentTicket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "idempotent registration")
	if err != nil {
		t.Fatal(err)
	}
	registration := model.HostRegistration{TicketCode: idempotentTicket.Code, Name: "idempotent-host", OS: "darwin", Arch: "arm64"}
	firstHost, firstSecret, err := gw.RegisterHostWithIdempotencyKey("coverage-registration", "request-hash", registration)
	if err != nil {
		t.Fatal(err)
	}
	secondHost, secondSecret, err := gw.RegisterHostWithIdempotencyKey("coverage-registration", "request-hash", registration)
	if err != nil || firstHost.ID != secondHost.ID || firstSecret != secondSecret {
		t.Fatalf("idempotent registration = (%#v, %q), (%#v, %q), err=%v", firstHost, firstSecret, secondHost, secondSecret, err)
	}
}

func TestStateStoreDescribeCoverage(t *testing.T) {
	if _, err := NewSerializedStateStore(nil); err == nil {
		t.Fatal("nil serialized store was accepted")
	}
	var unconfigured SerializedStateStore
	if unconfigured.Describe() != "serialized:unconfigured" {
		t.Fatal("unconfigured serialized store description changed")
	}
	var nilSerialized *SerializedStateStore
	if _, _, err := nilSerialized.LoadInto(nil); err == nil {
		t.Fatal("nil serialized store load was accepted")
	}
	if _, err := nilSerialized.SaveFrom(nil); err == nil {
		t.Fatal("nil serialized store save was accepted")
	}
	file, err := NewFileStateStore("/tmp/rdev-gateway.json")
	if err != nil {
		t.Fatal(err)
	}
	postgres, err := NewPostgresStateStore("service=rdev-test")
	if err != nil {
		t.Fatal(err)
	}
	redis, err := NewRedisStreamStateStore("redis://127.0.0.1:6379")
	if err != nil {
		t.Fatal(err)
	}
	s3, err := NewS3CompatibleStateStore("s3://bucket/prefix")
	if err != nil {
		t.Fatal(err)
	}
	for _, store := range []StateStore{file, postgres, redis, s3} {
		if store.Describe() == "" {
			t.Fatalf("empty store description for %T", store)
		}
	}
	serialized, err := NewSerializedStateStore(file)
	if err != nil || serialized.Describe() == "" {
		t.Fatalf("serialized store = %v, err=%v", serialized, err)
	}
}
