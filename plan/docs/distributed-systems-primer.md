# Distributed Systems Primer (for `rego`)

This primer is *not* a general textbook on distributed systems. It’s a practical, opinionated “what you need to know” guide so you can work effectively on `rego` and use it safely.

If you’re new to distributed systems, start here, then continue to **[Onboarding](onboarding.md)**.

---

## The uncomfortable truth: network calls fail (and fail weirdly)

When your code calls another service over a network (HTTP, gRPC, database, cache), you are leaving the safety of a single process:

- **Latency is variable** (sometimes wildly).
- **Failures are partial**: one dependency can fail while everything else is fine.
- **Failures are ambiguous**: you can’t always tell whether the remote side *didn’t* do the work or *did it but you didn’t hear back*.

A local function call either returns or panics. A network call can:
- time out after the remote side succeeds,
- fail due to a temporary packet loss,
- return “429 Too Many Requests” because the remote side is overloaded,
- succeed but be slow due to tail latency.

This is why `rego` exists: to make these behaviors explicit, observable, and configurable via policy.

---

## Core resilience patterns used by `rego`

`rego` centers on five building blocks:

1. **Retries** (with backoff + jitter + timeouts)
2. **Classification** (decide what is retryable vs not)
3. **Budgets/backpressure** (limit how many retries/hedges you’re allowed to spend)
4. **Hedging** (parallel attempts to reduce tail latency)
5. **Observability** (timelines and observers; see what actually happened)

You will see these concepts repeatedly across the codebase and docs.

---

## Retries: when they help, and when they hurt

### When retries help
Retries are useful for **transient** failures, for example:
- connection resets
- brief DNS hiccups
- temporary overload (sometimes)
- flaky network paths
- a rolling restart where a pod is briefly unavailable

### When retries hurt
Retries make problems worse when:
- the failure is **permanent** (e.g., 400 Bad Request, validation errors)
- the operation is **not safe to repeat** (non-idempotent actions like “charge credit card”)
- the downstream is already overloaded (retries become load amplification)

**Key mental model:** retries are additional load. They are not “free”.

---

## Idempotency: the #1 safety concept for retries

An operation is **idempotent** if repeating it has the same effect as doing it once.

- ✅ Idempotent: `GET /users/123`, “fetch user”, “read from DB”
- ❌ Usually not idempotent: “charge card”, “send email”, “create order” (unless you use idempotency keys)

Even if an operation “usually succeeds once”, retries can cause duplicates when failures are ambiguous:

### The classic ambiguous case
1. Client sends request
2. Server processes and commits
3. Network drops response
4. Client times out and retries
5. Server processes again (duplicate side effect)

**Takeaway:** you should never blindly retry non-idempotent operations unless you have a deduplication strategy (e.g., idempotency keys).

---

## Backoff + jitter: how to avoid retry storms

### Backoff
If an attempt fails, we wait before retrying. The wait typically grows exponentially:

- 10ms → 20ms → 40ms → 80ms → … (bounded by MaxBackoff)

This gives the downstream time to recover.

### Jitter
If 10,000 clients all retry at the same time, they can DDoS the downstream service exactly when it’s weakest. Jitter randomizes the sleep so retries spread out.

`rego` supports jitter strategies like:
- none (deterministic)
- full jitter (random [0..backoff])
- equal jitter (random [backoff/2..backoff])

---

## Timeouts: per-attempt vs overall

A retry loop needs **two** timeout concepts:

### Per-attempt timeout
Caps a single attempt, e.g., “each attempt must finish within 200ms”.

Useful when:
- a dependency can get stuck or very slow
- you want “fast failure + retry” rather than “wait forever”

### Overall timeout
Caps the entire call budget, e.g., “all retries combined must finish within 1s”.

Useful when:
- you must enforce an end-to-end SLO
- you want predictable worst-case latency

**Important:** overall timeout should dominate. If the overall context is cancelled, everything should stop quickly.

---

## Classification: not all errors deserve retries

A key idea in `rego` is **protocol/domain-aware classification**:
- HTTP status codes have meanings (429 vs 404 vs 500)
- gRPC has status codes (Unavailable vs InvalidArgument)
- your own app errors (validation, auth) are usually non-retryable

A classifier turns `(value, err)` into an **Outcome**:
- success
- retryable
- non-retryable
- abort (stop immediately)

This keeps the executor generic and lets integrations provide safe defaults.

---

## Budgets/backpressure: prevent self-inflicted outages

### What is a retry storm?
When a dependency degrades:
1. Clients see errors/latency
2. Clients retry aggressively
3. Extra retry load worsens the dependency
4. Failure cascades outward

Budgets ensure:
- retries are **bounded**
- hedges are **bounded**
- the system degrades gracefully instead of amplifying failures

A budget might be a **token bucket**:
- you have a limited retry “capacity”
- it refills at a steady rate
- once empty, further retries/hedges are denied

Budgets can also be “reservation style” with a release handle, which models scarce resources more accurately.

---

## Tail latency and hedging

Most real systems have “long tail” latency: a small fraction of requests take far longer than average.

### Hedging in one picture
```
time →
primary: |-----------------------(slow)---------------------X  (cancelled)
hedge:                |----(fast)----✓  (winner)
```

Hedging starts a second attempt when the first looks slow. If the hedge succeeds first, we cancel the primary.

### Why hedging helps
If p50 is 20ms but p99 is 500ms, hedging can bring effective tail latency down by racing a second attempt.

### Why hedging is dangerous
Hedging increases load (sometimes a lot). Without budgets and careful triggers, it can become “retry storm but in parallel”.

That’s why `rego`:
- bounds max hedges
- uses trigger scheduling (no busy loops)
- relies on budgets
- requires correct cancellation (no goroutine leaks)

---

## Observability: prove what happened

Retries and hedges can hide problems:
- a call “succeeds eventually” but burns 3 attempts and 800ms
- p99 latency grows but dashboards show success rate is fine

`rego` addresses this with timelines:
- every attempt has start/end times
- each attempt has a classification outcome and reason
- budgets and hedges are visible

If you can’t answer **“how many attempts did this call take?”**, you’re flying blind.

---

## Control planes: why policies must be dynamic

In real incidents, you may need to change retry behavior quickly:
- reduce attempts while a dependency is down
- adjust timeouts
- disable hedging temporarily
- switch classifiers for a new API behavior

A control plane allows fetching policies remotely, with safe caching and “last known good” fallback.

**Golden rule:** your policy system must fail safely. If policy fetch fails, you should not take your whole system down.

---

## Composition: what `rego` does *not* do

`rego` intentionally does **not** include circuit breaking in v1.0. Circuit breaking composes well with retrying, but is a separate concern and can be provided by external libraries.

---

## Practical checklist: adding resilience to a call

Before enabling retries/hedges for a call, confirm:

1. **Is the operation idempotent?**  
   If not, do you have an idempotency key / dedupe mechanism?

2. **What are the retryable conditions?**  
   Use a classifier that understands the protocol/domain.

3. **What’s the end-to-end timeout budget?**  
   Decide per-attempt and overall timeouts.

4. **What backoff/jitter is safe?**  
   Avoid tight retries; use bounded exponential backoff + jitter.

5. **Do we have budgets/backpressure?**  
   Ensure we won’t amplify outages.

6. **Is it observable?**  
   Make timelines/observers available so you can debug.

---

## Where to go next

- **[Onboarding](onboarding.md)** – project layout, reading order, contribution workflow.
- **[Extending `rego`](extending.md)** – write custom classifiers, budgets, triggers, observers.
