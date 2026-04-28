package circuit_test

import (
	"context"
	"fmt"
	"time"

	"github.com/aponysus/recourse/circuit"
)

type exampleClock struct {
	now time.Time
}

func (c *exampleClock) Now() time.Time {
	return c.now
}

func (c *exampleClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}

func ExampleConsecutiveFailureBreaker() {
	ctx := context.Background()
	clock := &exampleClock{now: time.Unix(0, 0)}
	breaker := circuit.NewConsecutiveFailureBreaker(2, time.Second)
	breaker.SetClock(clock.Now)

	fmt.Println(breaker.Allow(ctx).State)

	breaker.RecordFailure(ctx)
	breaker.RecordFailure(ctx)

	denied := breaker.Allow(ctx)
	fmt.Println(denied.Allowed, denied.State, denied.Reason)

	clock.Advance(time.Second)
	probe := breaker.Allow(ctx)
	fmt.Println(probe.Allowed, probe.State)

	breaker.RecordSuccess(ctx)
	fmt.Println(breaker.State())

	// Output:
	// closed
	// false open circuit_open
	// true half-open
	// closed
}
