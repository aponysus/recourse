package observe_test

import (
	"context"
	"fmt"

	"github.com/aponysus/recourse/observe"
)

func ExampleRecordTimeline() {
	ctx, capture := observe.RecordTimeline(context.Background())

	// Pass ctx to recourse.Do, recourse.DoValue, or retry.Executor.Do.
	// The capture is populated after the call returns.
	_ = ctx

	if capture.Timeline() == nil {
		fmt.Println("no attempts yet")
	}
	// Output:
	// no attempts yet
}
