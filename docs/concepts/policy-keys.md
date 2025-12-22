# Policy keys

`recourse` selects behavior using a **policy key**: a stable, low-cardinality identifier for a call site.

Examples:

- ✅ `"user-service.GetUser"`
- ✅ `"payments.Charge"`
- ❌ `"GET /users/123"`
- ❌ `"user-service.GetUser?user_id=123"`

Why low-cardinality matters:

- Keys back caches/trackers (e.g., policy caches, latency trackers in later phases).
- High-cardinality keys create unbounded memory and noisy telemetry.

## Parsing

`policy.ParseKey("svc.Method")` yields `policy.PolicyKey{Namespace:"svc", Name:"Method"}`.
If no dot is present, the entire string becomes `Name` and `Namespace` is empty.

