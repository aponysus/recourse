package controlplane

import "errors"

var (
	// ErrProviderUnavailable indicates the provider could not be reached or used.
	ErrProviderUnavailable = errors.New("recourse: policy provider unavailable")
	// ErrPolicyNotFound indicates the provider has no policy for the requested key.
	ErrPolicyNotFound = errors.New("recourse: policy not found")
	// ErrPolicyFetchFailed indicates a provider failure other than unavailability.
	ErrPolicyFetchFailed = errors.New("recourse: policy fetch failed")
)
