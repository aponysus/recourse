package retry

import (
	"context"
	"testing"

	"github.com/aponysus/rego/policy"
)

func TestExecutor_Do_Trivial(t *testing.T) {
	exec := &Executor{}
	called := false
	err := exec.Do(context.Background(), policy.PolicyKey{}, func(context.Context) error {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("unexpected result: err=%v called=%v", err, called)
	}
}
