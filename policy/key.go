package policy

import "strings"

// PolicyKey identifies a low-cardinality call site (e.g. "svc.Method").
type PolicyKey struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// ParseKey parses "namespace.name" into a PolicyKey.
// If no dot is present, the entire string is treated as Name.
func ParseKey(s string) PolicyKey {
	s = strings.TrimSpace(s)
	if s == "" {
		return PolicyKey{}
	}

	i := strings.IndexByte(s, '.')
	if i < 0 {
		return PolicyKey{Name: s}
	}

	ns := strings.TrimSpace(s[:i])
	name := strings.TrimSpace(s[i+1:])
	if name == "" {
		return PolicyKey{Name: s}
	}
	return PolicyKey{Namespace: ns, Name: name}
}

func (k PolicyKey) String() string {
	if k.Namespace == "" {
		return k.Name
	}
	if k.Name == "" {
		return k.Namespace
	}
	return k.Namespace + "." + k.Name
}
