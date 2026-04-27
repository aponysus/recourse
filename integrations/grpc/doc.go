// Package grpc provides opt-in gRPC integrations for recourse.
//
// UnaryClientInterceptor wraps unary client calls with a recourse retry
// executor. DefaultKeyFunc maps stable gRPC method names to policy keys, and
// Classifier maps gRPC status codes to retry outcomes for use as an executor
// classifier.
//
// This package is a separate Go module so gRPC dependencies remain optional for
// users of the core recourse module. It intentionally covers unary RPCs only;
// streaming RPCs require application-specific replay or resume semantics.
package grpc
