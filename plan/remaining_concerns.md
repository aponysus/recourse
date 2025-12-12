# Updated concerns checklist & implementation rubric (with citations)

This checklist is meant to be kept alongside the implementation plan as a *review and “definition of done” rubric*. Each item includes the plan reference(s) that justify the requirement.

> **How to use:** treat unchecked items as “not implemented/verified yet.” When you implement an item, add (or confirm) the referenced unit/integration tests, and confirm the behavior is observable via timelines/attributes where applicable.

---

## A. Policy normalization & safety guardrails

- [ ] **Normalization exists and is mandatory**: `EffectivePolicy.Normalize()` applies defaults, enforces explicit hard caps/floors, records clamping/defaulting in policy metadata, validates enums, and returns typed errors for fundamentally invalid config. fileciteturn2file1L59-L65  
- [ ] **Hard caps/floors are explicit + testable** (examples):  
  - `Retry.MaxAttempts` clamp `1..10` (default 3)  
  - `Hedge.MaxHedges` clamp `1..3` (default 2 when enabled)  
  - `InitialBackoff >= 1ms`, `MaxBackoff <= 30s`, `HedgeDelay >= 10ms`, etc. fileciteturn2file1L69-L78  
- [ ] **Normalization is applied everywhere**: all policy providers **and** the executor boundary call `Normalize()` before use. fileciteturn2file1L79-L79 fileciteturn1file10L105-L109  
- [ ] **Clamping is observable**: executor emits timeline/attempt attributes like `policy_normalized=true` and `policy_clamped_fields=...` when normalization modifies a policy. fileciteturn1file3L1-L1 fileciteturn2file11L4-L5  
- [ ] **Policy metadata supports operational facts** (source + normalization notes) without changing control-plane schema. fileciteturn2file1L54-L58  

**Verification tests**
- [ ] Unit tests for `Normalize()` covering each clamp/floor and “records that it happened” metadata. fileciteturn2file1L62-L65  
- [ ] Executor test asserts timeline attributes include normalization notes when normalization changed inputs. fileciteturn2file11L4-L6  

---

## B. Control plane provider semantics & outage safety

- [ ] **Remote provider uses cache + TTL + LKG with refresh backoff**: maintains `{Policy, fetchedAt, nextRefreshAt}` and returns cached LKG when `now < nextRefreshAt` to avoid hammering during outages. fileciteturn2file7L18-L24 fileciteturn2file7L40-L42  
- [ ] **Typed errors for fetch failures**: on fetch failure with LKG, return `(LKG, ErrPolicyFetchFailed)`; if no LKG, return `(zeroPolicy, ErrProviderUnavailable)` so executor can apply `MissingPolicyMode`. fileciteturn2file7L26-L29  
- [ ] **Not-found is explicit and negatively cached**: 404 returns `ErrPolicyNotFound` (no silent substitution), with negative caching (short TTL) to protect the control plane from high-QPS missing keys. fileciteturn2file7L30-L33  
- [ ] **ToEffective() normalizes and rejects invalid configs**: conversion calls `Normalize()` and surfaces fundamentally invalid configs as conversion errors so fallback behavior (LKG/executor modes) engages. fileciteturn2file7L1-L1 fileciteturn1file5L49-L50  
- [ ] **Provider “policy + error” is supported**: providers may return a non-zero policy with non-nil error to indicate fallback path (e.g., LKG). fileciteturn1file3L13-L16  

**Executor alignment**
- [ ] Executor treats `err != nil` as “not authoritative,” even if policy is also returned; under `FailureFallback`, prefer returned non-zero policy; always record error kind + source in timelines. fileciteturn2file0L40-L46 fileciteturn2file11L4-L5  

**Verification tests**
- [ ] RemoteProvider `httptest.Server` tests: TTL caching, outage returns LKG **and** typed error, 404 not-found + negative caching. fileciteturn2file7L52-L60  
- [ ] Executor test matrix for `MissingPolicyMode` (Fallback/Allow/Deny) when provider returns (policy, err). fileciteturn2file0L40-L46  

---

## C. Observability contract & performance guardrails

- [ ] **Timeline schema supports call-level attributes** (policy source, fallbacks, normalization notes, etc.). fileciteturn2file4L40-L50  
- [ ] **Observer interface is stable and includes hedging hooks** (`OnHedgeSpawn`, `OnHedgeCancel`). fileciteturn2file4L57-L68  
- [ ] **Attempt metadata is available via context** (`observe.AttemptInfo`). fileciteturn1file9L5-L19  
- [ ] **Two execution paths exist**:  
  - full timeline path when timeline requested or observer non-noop  
  - fast path when observer is `NoopObserver` and timeline not requested (avoid per-attempt allocs while preserving semantics). fileciteturn1file1L109-L113  
- [ ] **Policy resolution path is recorded** in timeline attributes (`policy_error`, `policy_source`, `missing_policy_mode`, etc.). fileciteturn2file11L3-L6  

**Verification / hardening**
- [ ] Benchmarks include fast path and hedged groups; track allocations and latency overhead. fileciteturn2file13L15-L24  
- [ ] Add sampling/truncation only if benchmarks show allocation pressure. fileciteturn2file13L66-L72  

---

## D. Classification safety & integration boundaries

- [ ] **Classifier contract is type-safe**: if classifier expects a specific type and receives incompatible input, it returns `OutcomeNonRetryable` or `OutcomeAbort` with reason `"classifier_type_mismatch"` and helpful attributes; mismatches are never treated as retryable. fileciteturn1file12L36-L41  
- [ ] **HTTP classifier works without a core→integration dependency**: classify a typed integration error via a `classify`-owned interface (so intermediate retryable responses can be drained/closed safely). fileciteturn1file12L34-L35 fileciteturn1file12L71-L79  

**Verification tests**
- [ ] Unit tests: classifier mismatch yields non-retryable/abort and includes `expected_type`/`got_type` style attributes. fileciteturn1file12L38-L41  

---

## E. HTTP integration safety (resource lifecycle + idempotency)

- [ ] **DoHTTP prevents connection leaks on retries**: non-2xx outcomes become a typed error; response body is drained (bounded) + closed before returning; helper returns `nil` response on non-2xx so callers don’t have to close bodies on error. fileciteturn1file2L23-L27  
- [ ] **Retries are idempotency-gated**: idempotent methods retried by default; non-idempotent retries require opt-in; refuse retries if body is not replayable unless a replayable body factory is provided. fileciteturn1file2L21-L23  
- [ ] **Retry-After honored via classifier backoff override**. fileciteturn1file2L27-L28  

**Verification tests**
- [ ] Integration test: retryable HTTP status does not leak connections (e.g., confirm transport reuse via `httptest.Server` + counting connections) and response bodies are closed on retry. fileciteturn1file2L23-L26  

---

## F. Budgets & backpressure semantics (retry vs hedge)

- [ ] **Budget gating is per-attempt and attempt-kind aware** (`KindRetry` vs `KindHedge`). fileciteturn2file6L15-L23  
- [ ] **Attempt index is global per call** (monotonic in launch order), not reset per retry group; budgets must not assume completion order. fileciteturn2file2L45-L46  
- [ ] **Denied attempt behavior is nuanced**:  
  - `KindRetry`: first-attempt denial fails fast; subsequent denial stops retries and returns the last real attempt error (with “stopped due to budget” signal in timeline).  
  - `KindHedge`: denial is recorded but does not fail the call. fileciteturn2file2L57-L63  

**Verification tests**
- [ ] Unit test: deny second retry attempt → op called once; timeline includes denied record; final error is first attempt error (not synthetic budget error). fileciteturn2file2L68-L72  

---

## G. Hedging determinism, cancellation correctness, and race safety

- [ ] **Group outcome is deterministic with explicit precedence**: `Success > NonRetryable > Abort > Retryable`. fileciteturn1file7L40-L46  
- [ ] **`CancelOnFirstTerminal` semantics are explicit**:  
  - true: fail fast on first terminal failure (excluding internal-cancel) and cancel others  
  - false: stop spawning new hedges after terminal failures but allow in-flight attempts so success can still win; final outcome by precedence. fileciteturn1file7L35-L39  
- [ ] **Internal vs external cancellation is distinguished** so internal-cancel outcomes do not override success/failure decisions. fileciteturn1file7L42-L46 fileciteturn2file15L30-L35  
- [ ] **Scheduled (timer-based) hedge spawning** avoids tight polling, and missing trigger behavior follows `MissingTriggerMode` (fallback to fixed delay or disable hedging). fileciteturn1file7L53-L63  

**Verification / hardening**
- [ ] Stress tests and race tests ensure no goroutine leaks, no deadlocks, deterministic results under randomized race timing. fileciteturn2file13L28-L33 fileciteturn2file13L34-L47  

---

## H. Shared cache/eviction primitives & cardinality guardrails

- [ ] **One shared stdlib-only eviction primitive** (`internal/cache`) is used by *both* RemoteProvider cache and per-key latency trackers to avoid duplicating tricky concurrency/LRU/TTL logic. fileciteturn2file14L66-L73 fileciteturn2file7L18-L20  
- [ ] **Minimum cache features**: thread-safe LRU, optional TTL, touch-on-access, optional on-evict hook, stress tests. fileciteturn2file14L74-L80  
- [ ] **Latency tracker map is bounded** (`MaxTrackers` + eviction); warn on high-cardinality keys. fileciteturn2file14L84-L89  
- [ ] **Hardening includes cardinality guardrails**: verify eviction and add warnings when per-key tracker/cache counts exceed thresholds. fileciteturn2file13L61-L65  

---

## I. Global default executor & ergonomics (footgun control)

- [ ] **No exported mutable global**; default executor is lazily initialized via `sync.Once`/`atomic.Pointer`. fileciteturn1file2L3-L4 fileciteturn1file11L55-L59  
- [ ] **SetGlobal is startup-only**: calling after `DefaultExecutor()` init returns error (or warn/no-op); docs recommend explicit executors in libraries. fileciteturn1file2L5-L6 fileciteturn1file11L57-L60  
- [ ] **Zero-config wrappers exist** but preserve additive timeline variants. fileciteturn1file11L43-L48  

---

## J. “Failure modes audit” (the project-wide correctness backstop)

- [ ] Hardening phase explicitly documents and tests:  
  - provider returns LKG/default with typed error → executor records fallback + respects `MissingPolicyMode`  
  - control plane down → LKG when available, otherwise executor fallback; refresh backoff prevents hammering  
  - budget denial semantics (retry vs hedge) as specified. fileciteturn2file13L49-L60  

---

## Optional “remaining watch items” (not blockers, but worth tracking)

- [ ] Decide whether `FailureDeny` should deny on *any* provider error (even with LKG) or only when no usable policy exists; ensure docs/tests match intended semantics. fileciteturn2file0L40-L46  
- [ ] Consider whether HTTP helper should eventually offer an “inspect response body on error” advanced option, without undermining the safe default behavior. fileciteturn1file2L23-L27  

---
