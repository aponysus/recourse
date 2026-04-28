// Package otelrecourse provides OpenTelemetry tracing integration for recourse.
//
// Observer implements observe.Observer and converts completed recourse calls
// into OpenTelemetry spans. The operation span includes stable recourse
// attributes such as policy key, attempt count, and policy resolution metadata.
// Attempt details can be recorded as span events, child spans, or both.
//
// This package is a separate Go module so OpenTelemetry dependencies remain
// optional for users of the core recourse module.
package otelrecourse
