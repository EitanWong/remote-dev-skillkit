package model

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"
)

func TestJobEnvelopeSignsAndVerifies(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	ticket, host, job := envelopeFixtures(t, now)

	envelope, err := NewJobEnvelope(job, host, ticket, JobEnvelopeSpec{
		OperatorID:        "eitan",
		Workspace:         JobWorkspace{Root: "/repo", WriteScope: []string{"/repo"}, Branch: "rdev/job"},
		ApprovalsRequired: []string{"git.push"},
		Payload:           map[string]any{"prompt": "fix tests"},
		TTLSeconds:        600,
		SigningKeyID:      "test-key",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err = envelope.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Signature == "" {
		t.Fatal("signature must be set")
	}
	if err := envelope.VerifyForHost(publicKey, host.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("expected envelope to verify: %v", err)
	}
	if envelope.ExpiresAt.After(ticket.ExpiresAt) {
		t.Fatal("envelope must not outlive its ticket")
	}
}

func TestJobEnvelopeRejectsWrongHost(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	ticket, host, job := envelopeFixtures(t, now)
	envelope, err := NewJobEnvelope(job, host, ticket, JobEnvelopeSpec{}, now)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err = envelope.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := envelope.VerifyForHost(publicKey, "hst_other", now); err == nil {
		t.Fatal("expected wrong host binding to fail")
	}
}

func TestJobEnvelopeRejectsExpiredEnvelope(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	ticket, host, job := envelopeFixtures(t, now)
	envelope, err := NewJobEnvelope(job, host, ticket, JobEnvelopeSpec{TTLSeconds: 60}, now)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err = envelope.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := envelope.Verify(publicKey, now.Add(61*time.Second)); !errors.Is(err, ErrEnvelopeExpired) {
		t.Fatalf("expected expired error, got %v", err)
	}
}

func TestJobEnvelopeRejectsTampering(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	ticket, host, job := envelopeFixtures(t, now)
	envelope, err := NewJobEnvelope(job, host, ticket, JobEnvelopeSpec{}, now)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err = envelope.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	envelope.Intent = "different intent"
	if err := envelope.Verify(publicKey, now); !errors.Is(err, ErrEnvelopeSignature) {
		t.Fatalf("expected signature error, got %v", err)
	}
}

func envelopeFixtures(t *testing.T, now time.Time) (Ticket, Host, Job) {
	t.Helper()
	ticket, err := NewTicket(HostModeAttendedTemporary, 600, []string{"shell.user"}, "test", now)
	if err != nil {
		t.Fatal(err)
	}
	host, err := NewHost(ticket, HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "win-temp",
		OS:           "windows",
		Arch:         "amd64",
		Capabilities: []string{"shell.user"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	job, err := NewJob(host.ID, "powershell", "diagnose", map[string]any{"cwd": "C:\\Users\\Public"}, now)
	if err != nil {
		t.Fatal(err)
	}
	return ticket, host, job
}
