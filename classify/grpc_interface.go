package classify

// GRPCError is a classify-owned interface that allows gRPC classifiers to
// recognize retry semantics without importing grpc packages.
//
// Provide a subset of the standard gRPC status interface.
type GRPCError interface {
	GRPCStatus() any // Returns *status.Status, but typed as any to avoid dep
}

// Note: We can't easily inspect *status.Status without importing the type.
// But wait, the standard "GRPCStatus() *status.Status" relies on the return type matching.
// If we define interface { GRPCStatus() interface{} } it won't match { GRPCStatus() *status.Status }.
// Go interfaces require exact signature match.
//
// So we cannot use an interface check for `GRPCStatus() *status.Status` without importing `status`.
//
// Alternative: Check for `interface { GRPCStatus() *Status }` where Status is defined locally? No.
//
// We might implement a heuristic or just require users to register the classifier.
//
// If we want ergonomics, maybe we can't completely avoid the dependency in `classify`?
// BUT user explicitly asked for "opt-in" dependencies. So `classify` MUST NOT depend on gRPC.
//
// Soln: Use `AutoClassifier` extension or "Chain".
//
// Actually, `integrations/grpc` provides the classifier.
// Implement `Classifier` in `integrations/grpc`.
// The user has to USE it.
//
// If `NewDefaultExecutor` is used, the default is `AutoClassifier`.
// If I want gRPC calls to be classified correctly, I must tell `AutoClassifier` about it?
// Or I must replace the default classifier?
//
// Suggestion: When `WithClassifier()` is used in `integrations/grpc`, it should ALSO wrap or interact with the default.
//
// Or, simply: The user must set `ClassifierName: "grpc"` in their policy.
// "Zero-config" implies they shouldn't have to.
//
// Maybe `AutoClassifier` can be "smart" by using reflection for the method name `GRPCStatus`?
// It's slow but acceptable for errors?
//
// Or, we use the error string? Unreliable.
//
// Let's stick to the "User must register it" OR "AutoClassifier is generic".
//
// If `AutoClassifier` finds `HTTPError` it uses it.
// If we want it to find `GRPCError`, we can't define the interface strictly.
//
// Wait, `status.FromError` works by type assertion.
//
// Maybe `integrations/grpc` should overwrite the `DefaultClassifier` when registered?
// `exec := retry.NewDefaultExecutor(integration.WithDefaultClassifier())`?
//
// `retry/executor.go` has `WithDefaultClassifier`.
//
// So `integration.WithClassifier()` could just set `DefaultClassifier` to one that handles gRPC AND falls back to Auto?
//
// Let's modify `integrations/grpc/grpc.go` to export a `SmartClassifier` that handles gRPC and delegates to `Auto`.
// And `WithClassifier` sets THAT as the default.
