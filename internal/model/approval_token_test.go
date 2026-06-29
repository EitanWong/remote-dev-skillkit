package model

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"
)

func TestApprovalTokenSignsAndVerifies(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	token, err := NewApprovalToken(ApprovalTokenSpec{
		JobID:        "job_1",
		HostID:       "hst_1",
		ApprovalID:   "git.push",
		Operation:    "git.push",
		OperatorID:   "eitan",
		SigningKeyID: "gateway-dev",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	token, err = token.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	root := NewTrustBundle("gateway-dev", publicKey)
	if err := token.Verify(root, "job_1", "hst_1", "git.push", now.Add(time.Minute)); err != nil {
		t.Fatalf("expected token to verify: %v", err)
	}
}

func TestApprovalTokenRejectsWrongScope(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	token, err := NewApprovalToken(ApprovalTokenSpec{
		JobID:        "job_1",
		HostID:       "hst_1",
		ApprovalID:   "git.push",
		Operation:    "git.push",
		SigningKeyID: "gateway-dev",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	token, err = token.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	root := NewTrustBundle("gateway-dev", publicKey)
	if err := token.Verify(root, "job_2", "hst_1", "git.push", now.Add(time.Minute)); !errors.Is(err, ErrApprovalTokenInvalid) {
		t.Fatalf("expected invalid scope, got %v", err)
	}
}

func TestApprovalTokenRejectsExpiredAndConsumed(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	token, err := NewApprovalToken(ApprovalTokenSpec{
		JobID:        "job_1",
		HostID:       "hst_1",
		ApprovalID:   "git.push",
		Operation:    "git.push",
		SigningKeyID: "gateway-dev",
		ExpiresAt:    now.Add(time.Minute),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	token, err = token.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	root := NewTrustBundle("gateway-dev", publicKey)
	if err := token.Verify(root, "job_1", "hst_1", "git.push", now.Add(2*time.Minute)); !errors.Is(err, ErrApprovalTokenExpired) {
		t.Fatalf("expected expired token, got %v", err)
	}
	consumed := token.Consume(now.Add(30 * time.Second))
	consumed, err = consumed.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := consumed.Verify(root, "job_1", "hst_1", "git.push", now.Add(40*time.Second)); !errors.Is(err, ErrApprovalTokenConsumed) {
		t.Fatalf("expected consumed token, got %v", err)
	}
}

func TestApprovalTokenRejectsTampering(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	token, err := NewApprovalToken(ApprovalTokenSpec{
		JobID:        "job_1",
		HostID:       "hst_1",
		ApprovalID:   "git.push",
		Operation:    "git.push",
		SigningKeyID: "gateway-dev",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	token, err = token.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	token.Operation = "deploy.run"
	root := NewTrustBundle("gateway-dev", publicKey)
	if err := token.Verify(root, "job_1", "hst_1", "deploy.run", now.Add(time.Minute)); !errors.Is(err, ErrApprovalTokenSignature) {
		t.Fatalf("expected signature failure, got %v", err)
	}
}
