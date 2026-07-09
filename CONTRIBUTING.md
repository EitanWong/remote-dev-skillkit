# Contributing

Thank you for helping improve Remote Dev Skillkit. This project is safety-first
infrastructure for agent-driven remote development, so contributions must
preserve consent, auditability, and host-local control.

## Development Setup

```bash
go test ./...
go vet ./...
./scripts/check.sh
./scripts/ci/release-smoke.sh
```

Use focused tests while developing, then run the full verification set before
opening a pull request.

## Contribution Rules

- Follow the Agent engineering discipline:
  - Be ashamed of guessing interfaces; be proud of reading the source,
    contracts, docs, schemas, and tests first.
  - Be ashamed of vague execution; be proud of asking for confirmation when
    requirements, environment, authority, or risk are unclear.
  - Before giving a final plan or answer for ambiguous or high-impact work, ask
    the human one question at a time, continue from the answer with the next
    single question, and proceed only when there is about 95% confidence in the
    real goal, constraints, and success criteria.
  - Be ashamed of inventing business intent; be proud of getting human
    confirmation for product goals, customer impact, and policy decisions.
  - Be ashamed of creating unnecessary interfaces; be proud of reusing existing
    APIs, protocol objects, skills, MCP tools, adapters, and project patterns.
  - Be ashamed of skipping verification; be proud of proactively running
    focused tests, full tests, release smoke, readiness checks, and privacy
    scans when relevant.
  - Be ashamed of breaking architecture; be proud of following the signed-task,
    host-policy, authorization, evidence, audit, and release-trust contracts.
  - Be ashamed of pretending to understand; be proud of saying what is unknown
    and how it will be checked.
  - Be ashamed of blind modification; be proud of cautious refactoring that is
    scoped, reversible, reviewed by tests, and aligned with existing structure.
- Use deep reasoning discipline without exposing private chain-of-thought:
  - Rephrase the request, identify explicit and implicit requirements, and map
    knowns, unknowns, constraints, risks, and success criteria before acting.
  - Keep multiple interpretations and implementation paths alive until evidence
    or human confirmation makes the right path clear.
  - Scale analysis to task risk: streamline obvious one-line fixes, but expand
    reasoning for security, release, transport, enrollment, hosted auth/storage,
    connection entry, or remote-control changes.
  - Test assumptions against source, contracts, schemas, docs, and existing
    behavior; correct course when new evidence contradicts the initial plan.
  - Track progress explicitly during complex work: what is established, what is
    still uncertain, current confidence, and what verification remains.
  - Share concise, auditable reasoning summaries and evidence with humans, not
    private internal reasoning or chain-of-thought.
- Keep temporary support visible, foreground, revocable, and non-persistent.
- Keep managed service mode explicit, inspectable, stoppable, and uninstallable.
- Do not add hidden persistence, inbound public listeners on target hosts, UAC or
  sudo bypasses, credential scraping, policy weakening, or unrestricted shell
  access.
- New host capabilities must be signed, host-validated, auditable, revocable, and
  covered by tests.
- High-risk operations such as package installation, elevation, GUI control,
  service changes, publishing, deployment, credential changes, push, and merge
  must go through authorization gates.
- Public adapter integrations must use the adapterkit conformance surfaces when
  possible.

## Pull Request Checklist

- [ ] The change is scoped to the requested behavior.
- [ ] Tests cover success and failure paths.
- [ ] `go test ./...` passes.
- [ ] `go vet ./...` passes.
- [ ] `./scripts/check.sh` passes.
- [ ] `./scripts/ci/release-smoke.sh` passes when release, Skillkit, GitHub,
      enrollment, or adapter behavior changes.
- [ ] Documentation, Skillkit notes, and release checklists are updated when user
      behavior changes.
- [ ] No secrets, tokens, private keys, local `.rdev/` state, or target-machine
      transcripts are committed.

## External Actions

Scripts under `scripts/github/` default to dry-run or plan generation where
possible. Do not create repositories, publish releases, push tags, mutate issues,
or upload artifacts unless the maintainer has explicitly authorized that external
operation.
