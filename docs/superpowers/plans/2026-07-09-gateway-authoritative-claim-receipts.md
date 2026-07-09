# Gateway-Authoritative Claim Receipts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make authorized task execution robust when the target host wall clock is wrong by using a gateway-signed task claim receipt as the hostrunner time authority.

**Architecture:** The gateway remains the authority for ticket, task envelope, and authorization token validity. When a host claims a task, the gateway attaches a signed `TaskClaimReceipt` to the returned `model.Task`, binding the claim to the task id, host id, envelope hash, authorization token ids, gateway claim time, and claim expiry. The hostrunner verifies signatures, binding, nonce, scope, capabilities, and receipt integrity; for time-bound envelope/token checks it uses the signed gateway claim time instead of the host wall clock.

**Tech Stack:** Go standard library, Ed25519 signatures, existing `internal/model`, `internal/gateway`, `internal/hostrunner`, `internal/cli`, and `internal/hostcmd` patterns.

## Global Constraints

- Do not trust target host wall clock for gateway-issued validity decisions.
- Keep replay protection via nonce and authorization token consumption.
- Keep short-lived claim semantics; the claim receipt must not authorize execution beyond the gateway-authorized claim window.
- Use TDD: every behavior change gets a failing test first.
- Preserve existing JSON compatibility by adding optional fields, not renaming existing fields.

---

### Task 1: Model Envelope Hash And Claim Receipt

**Files:**
- Modify: `internal/model/task_envelope.go`
- Create: `internal/model/task_claim_receipt.go`
- Test: `internal/model/task_claim_receipt_test.go`

**Interfaces:**
- Produces: `func (e TaskEnvelope) Hash() (string, error)`
- Produces: `type TaskClaimReceipt`
- Produces: `func NewTaskClaimReceipt(spec TaskClaimReceiptSpec, now time.Time) (TaskClaimReceipt, error)`
- Produces: `func (r TaskClaimReceipt) Sign(privateKey ed25519.PrivateKey) (TaskClaimReceipt, error)`
- Produces: `func (r TaskClaimReceipt) Verify(root TrustBundle, task Task) error`

- [ ] Write failing model tests for signed receipt verification, envelope tamper rejection, receipt scope rejection, and bad host clock irrelevance.
- [ ] Implement `TaskEnvelope.Hash()` using the same unsigned JSON bytes used for signature verification and a `sha256:` hex prefix.
- [ ] Implement `TaskClaimReceipt` signing and verification with existing model signing style.
- [ ] Run `go test ./internal/model -run 'TaskClaimReceipt|TaskEnvelope' -count=1`.

### Task 2: Gateway Signs Receipt On Claim

**Files:**
- Modify: `internal/model/task.go`
- Modify: `internal/gateway/memory.go`
- Test: `internal/gateway/memory_test.go`

**Interfaces:**
- Produces: `model.Task.ClaimReceipt *model.TaskClaimReceipt`
- Consumes: `model.NewTaskClaimReceipt`

- [ ] Write failing gateway test proving `NextTaskForAuthenticatedHost` returns a task with a signed claim receipt.
- [ ] Write failing gateway test proving a task whose envelope or authorizations expired by gateway time is not claimable.
- [ ] Attach a signed claim receipt when transitioning a queued task to running.
- [ ] Store and return the receipt with the running task for audit/debug visibility.
- [ ] Run `go test ./internal/gateway -run 'ClaimReceipt|DoesNotClaimTaskUntilRequiredAuthorizationIsSigned' -count=1`.

### Task 3: Hostrunner Uses Gateway Claim Time

**Files:**
- Modify: `internal/hostrunner/runner.go`
- Test: `internal/hostrunner/runner_test.go`

**Interfaces:**
- Consumes: `model.Task.ClaimReceipt`
- Consumes: `TaskClaimReceipt.Verify`

- [ ] Write failing hostrunner test proving an authorized claimed task executes when host wall clock is wrong by one day.
- [ ] Write failing hostrunner test proving a missing/tampered claim receipt is denied before adapter execution for gateway-claimed tasks.
- [ ] Verify the claim receipt before envelope/token time checks.
- [ ] Use `ClaimReceipt.ClaimedAt` as the time argument for envelope and authorization token verification when present.
- [ ] Keep legacy direct `RunDevTask` tests working by allowing tasks without a claim receipt in test/local direct execution paths.
- [ ] Run `go test ./internal/hostrunner -run 'ClaimReceipt|WrongClock|Authorization' -count=1`.

### Task 4: CLI/Host Command Preserve Receipt

**Files:**
- Modify as needed: `internal/cli/cli.go`
- Modify as needed: `internal/hostcmd/hostcmd.go`
- Test: `internal/cli/cli_test.go`

**Interfaces:**
- Consumes: `model.Task.ClaimReceipt` through existing JSON decoding.

- [ ] Verify HTTP task polling/decoding carries `claim_receipt` into hostrunner.
- [ ] Add a focused test only if existing JSON path drops the optional field.
- [ ] Run `go test ./internal/cli ./internal/hostcmd -count=1`.

### Task 5: Verification And Remote Gate

**Files:**
- No planned source edits.

- [ ] Run `gofmt` on changed Go files.
- [ ] Run `go test ./... -count=1`.
- [ ] Run `go vet ./...`.
- [ ] Run `git diff --check`.
- [ ] Build `.tmp/bin/rdev`, `.tmp/bin/rdev-host`, and `.tmp/bin/rdev-bootstrap`.
- [ ] Re-run the Windows Gate 3 authorization flow with a freshly joined helper and verify authorization no longer depends on target wall clock.
