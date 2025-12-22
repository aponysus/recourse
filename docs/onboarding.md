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
| `controlplane` | Policy providers (today: static provider; remote provider planned) |
| `observe` | Timeline types, observer interface, attempt context metadata |
| `classify` | Outcome classifiers + registry |
| `budget` | Budgets/backpressure (per-attempt gating + registry) |
| `hedge` | Hedge triggers/latency tracking (planned) |

## Suggested reading order

1. `README.md` (usage + concepts)
2. `docs/roadmap.md` (what’s implemented vs planned)
3. `docs/extending.md` (early draft extension patterns)
4. Code:
   - `recourse/recourse.go` (facade API)
   - `retry/executor.go` (executor + timeline wiring)
   - `observe/types.go` (timeline/attempt records)
   - `policy/schema.go` (policy schema + normalization)
   - `controlplane/provider.go` (policy provider semantics)

## Contributing

### Local dev loop

Run tests locally:

```bash
go test ./...
```

Optional checks:

```bash
go test -race ./...
go vet ./...
```

Format code:

```bash
gofmt -w .
```

### Project conventions

- Keep core packages **stdlib-only**; put non-stdlib deps in `integrations/*`.
- Prefer additive API changes during `v0.x`; avoid breaking exported APIs.
- Add/adjust tests when changing executor behavior, classification, or observability.
- Keep policy keys low-cardinality (no IDs/tenants/paths in keys).
- Keep `docs/` in sync when changing user-facing behavior.
