package recourse_test

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/aponysus/recourse/recourse"
)

func ExampleDoValue() {
	var attempts int32

	user, err := recourse.DoValue[string](context.Background(), "svc.GetUser", func(ctx context.Context) (string, error) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			return "", fmt.Errorf("transient error")
		}
		return "alice", nil
	})

	fmt.Println(user, err, attempts)
	// Output:
	// alice <nil> 2
}
