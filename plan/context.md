# AI Agent Context

You are contributing to a **public, open-source Go library** focused on **resilience, retries, and observability** for distributed systems.

## 0. Language & Compatibility Contract (NON-NEGOTIABLE)

- **Minimum supported Go version:** **Go 1.24**
- **Primary development version:** Go 1.25
- Code **MUST compile and behave correctly on Go 1.24**
- `go.mod` declares `go 1.24`
- Do **not** use:
  - language features introduced after 1.24
  - stdlib APIs added after 1.24
- If unsure whether a feature is ≥1.25, **don’t use it**

Backward compatibility applies to:
- **Public API** (types, functions, behavior)
- **Semantic behavior** (timeouts, retries, cancellation, ordering)

---

## 1. Repo Invariants & Architecture

Hard constraints:

- Core packages are **stdlib-only**
  - `retry`, `policy`, `classify`, `budget`, `hedge`, `observe`, `controlplane`
- Third-party dependencies are allowed **only** in optional integration packages
- Thin public facade, rich internal composition
- Safe degradation is mandatory (no hard failures when optional components are missing)
- No exported mutable globals

Before coding:
1. Identify which phase/package the change belongs to
2. Justify why the change belongs there
3. Call out concurrency, cancellation, and compatibility risks

---

## 2. Go Coding Standards (Library-grade)

- Idiomatic Go, clarity over cleverness
- All exported identifiers require **godoc** explaining:
  - behavior
  - edge cases
  - failure modes
- **No panics** in library code (except impossible programmer errors)
- Respect:
  - `context.Context` cancellation
  - per-attempt timeouts
  - overall deadlines
- No goroutine leaks
- No unbounded memory growth
- Avoid high-cardinality keys in maps or metrics

If behavior is ambiguous, choose the **most conservative option** and emit a reason string.

---

## 3. Concurrency & Cancellation Rules

Any code that launches goroutines or timers must guarantee:

- All goroutines exit on:
  - success
  - terminal failure
  - context cancellation
  - retry exhaustion
- Per-attempt contexts are always canceled
- Channels:
  - bounded
  - single owner closes
  - no sends after cancellation
- Timers:
  - `Stop()` and drain when reused
- No global shared mutable state without synchronization

If you can’t prove this, redesign.

---

## 4. Observability Contract (MANDATORY)

Every operation must emit a consistent **timeline**:

- Every attempt produces an `AttemptRecord`
  - including denied/skipped attempts
- Always populate:
  - start/end times
  - attempt index
  - outcome kind + **stable reason string**
- Timeline finalized exactly once
- Observer hooks called exactly once

When adding new failure or fallback paths:
- invent a **stable, snake_case reason**
- include it in the timeline

Observability is not optional.

---

## 5. Testing Standards (Public OSS Quality)

Every non-trivial change requires tests.

### Required coverage:
- **Unit tests**
  - policy normalization
  - classifiers
  - budgets
  - hedge triggers
- **Integration-style executor tests**
  - retries stop on success
  - non-retryable errors stop immediately
  - deadlines respected
  - hedged attempts cancel losers
- **Concurrency safety**
  - race-detector clean
  - no goroutine leaks

Tests must pass on **Go 1.24 and 1.25**.

---

## 6. Tooling Expectations

Assume CI runs:

- `go test ./...`
- `go test -race ./...` (at least on latest)
- `go vet ./...`

Your output must include:
- files changed
- public API changes (if any)
- how to run tests locally
- explicit TODOs (if unavoidable)

No new dependencies in core packages.

---

## 7. Scope Discipline

- Implement **only** the requested task
- No drive-by refactors
- No breaking public API changes
- Prefer additive changes
- Note larger issues but don’t fix them unless asked

---

## 8. Task Execution Mode (when given a phase/task)

When implementing a specific task:

1. Brief design rationale (1–2 paragraphs)
2. Exact code changes
3. Tests proving:
   - correctness
   - cancellation behavior
   - observability output
4. Verification steps

Constraints:
- Go 1.24 compatible
- OSS-grade stability
- Minimal, reviewable diffs

---

### Mental Model to Keep

This library will be imported into **production distributed systems you do not control**.

Assume:
- weird clocks
- partial failures
- slow downstreams
- aggressive cancellation
- skeptical operators reading timelines

Write code accordingly.
