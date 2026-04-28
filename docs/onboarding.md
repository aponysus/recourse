# Onboarding

This guide is for engineers who know Go and want to understand (or contribute to) `recourse`.

## What is `recourse`?

`recourse` is a **policy-driven, observability-first** resilience library. Call sites provide a low-cardinality key (e.g. `"svc.Method"`), policies decide behavior for that key, and the executor produces structured telemetry for every attempt.

## The public API

The “happy path” is a one-liner with a string key:

```go
user, err := recourse.DoValue[User](
    ctx,
    "user-service.GetUser",
    func(ctx context.Context) (User, error) {
        return client.GetUser(ctx, userID)
    },
)
```

When you need structured “what happened?” data:

```go
ctx, capture := observe.RecordTimeline(ctx)
user, err := recourse.DoValue(ctx, "user-service.GetUser", op)
tl := capture.Timeline()
_ = tl // inspect tl.Attempts, tl.FinalErr, tl.Attributes, ...
```

For advanced usage (explicit wiring), use `retry.NewExecutor` and the `retry` package APIs directly.

## Policy keys: cardinality matters

**Keys must be low-cardinality.** Keys back caches/trackers and must not embed request-specific identifiers.

Good keys:
- `"user-service.GetUser"`
- `"payments.Charge"`
- `"db.Users.Query"`

Bad keys:
- `"user-service.GetUser?user_id=123"`
- `"GET /users/123"`
- `"payments.Charge:tenant=acme"`

Rule of thumb: if the key can take on “millions of unique values”, it doesn’t belong in the key.

## Repo map

The repository is modular by design:

| Package | What it does |
|---|---|
| `recourse` | Thin facade: string-key helpers (`Do*`) and key parsing |
| `retry` | Executor: retries/backoff/timeouts, plus timeline/observer wiring |
| `policy` | Policy keys + schema + normalization/clamping |
| `controlplane` | Policy providers (local/static and remote) |
| `observe` | Timeline types, observer interface, attempt context metadata |
| `classify` | Outcome classifiers + registry |
| `budget` | Budgets/backpressure (per-attempt gating + registry) |
| `hedge` | Hedge triggers/latency tracking |

## Suggested reading order

1. `README.md` (usage + concepts)
2. `docs/extending.md` (extension patterns)
3. Code:
   - `recourse/recourse.go` (facade API)
   - `retry/executor.go` (executor + timeline wiring)
   - `observe/types.go` (timeline/attempt records)
   - `policy/schema.go` (policy schema + normalization)
   - `controlplane/provider.go` (policy provider semantics)

## Contributing

### First contribution path

If you are making your first contribution, start with a change that exercises the public surface without changing core semantics:

- Improve docs, examples, or migration guides.
- Add or tighten tests around an existing behavior.
- Add a small classifier, observer, or budget example.
- Improve diagnostics without changing reason codes or exported fields.

Avoid these areas for a first PR unless you have already discussed the design:

- Policy normalization and default safety behavior.
- Timeline fields, reason codes, and observer event semantics.
- Public API removals or signature changes in stable `v1.x` packages.
- Retry scheduling, hedging, or circuit-breaker behavior that can change production load.

Good first PRs should be small, include the relevant test or example update, and explain the operational behavior they preserve or improve.

### Local dev loop

Run the same aggregate checks CI expects:

```bash
make ci-check
```

This covers root tests, vet, root examples, nested integration/example modules, generated reference docs, and claim-marker validation.

For a faster inner loop while editing:

```bash
make test
make examples-check
make modules-check
make docs-build
```

Format code:

```bash
gofmt -w .
```

### Project conventions

- Keep core packages **stdlib-only**; put non-stdlib deps in `integrations/*`.
- `v1.x` follows SemVer; prefer additive API changes and avoid breaking exported APIs in stable packages.
- Add/adjust tests when changing executor behavior, classification, or observability.
- Keep policy keys low-cardinality (no IDs/tenants/paths in keys).
- Keep `docs/` in sync when changing user-facing behavior.
- Keep examples compiling; public-facing root examples are checked by `make examples-check`, and nested modules are checked by `make modules-check`.
- If generated reference docs change, run `make docs-reference` and include the generated diff intentionally.

### Release checklist

- Update `CHANGELOG.md` (move items from Unreleased into a new version entry).
- Run `make ci-check`.
- Verify the docs site with `make docs-build` if doc navigation or links changed.
- Tag the release (SemVer) and push the tag to trigger release automation.
