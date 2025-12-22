package policy

import "fmt"

// NormalizeError indicates a fundamentally invalid policy configuration.
type NormalizeError struct {
	Field string
	Value string
}

func (e *NormalizeError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("recourse: invalid policy config: %s=%q", e.Field, e.Value)
}
