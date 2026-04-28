package observe_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/recourse"
)

func ExampleRecordTimeline() {
	ctx, capture := observe.RecordTimeline(context.Background())
	attempts := 0

	err := recourse.Do(ctx, "svc.GetUser", func(ctx context.Context) error {
		attempts++
		if attempts == 1 {
			return errors.New("temporary upstream error")
		}
		return nil
	})

	tl := capture.Timeline()
	fmt.Println("err:", err)
	fmt.Println("policy_source:", tl.Attributes["policy_source"])
	for _, attempt := range tl.Attempts {
		fmt.Printf("attempt=%d reason=%s err=%v\n", attempt.Attempt, attempt.Outcome.Reason, attempt.Err)
	}

	// Output:
	// err: <nil>
	// policy_source: default
	// attempt=0 reason=retryable_error err=temporary upstream error
	// attempt=1 reason=success err=<nil>
}
