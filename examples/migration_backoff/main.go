package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aponysus/recourse/classify"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
	"github.com/aponysus/recourse/retry"
)

var errInvalidCard = errors.New("invalid card")

type Receipt struct {
	ID string
}

type paymentGateway struct {
	attempts int
}

func (g *paymentGateway) Charge(ctx context.Context, accountID string, cents int) (Receipt, error) {
	select {
	case <-ctx.Done():
		return Receipt{}, ctx.Err()
	default:
	}

	if accountID == "" || cents <= 0 {
		return Receipt{}, errInvalidCard
	}

	g.attempts++
	if g.attempts == 1 {
		return Receipt{}, errors.New("gateway timeout")
	}

	return Receipt{ID: "ch_123"}, nil
}

type paymentClassifier struct{}

func (paymentClassifier) Classify(_ any, err error) classify.Outcome {
	switch {
	case err == nil:
		return classify.Outcome{Kind: classify.OutcomeSuccess, Reason: "success"}
	case errors.Is(err, context.Canceled):
		return classify.Outcome{Kind: classify.OutcomeAbort, Reason: "context_canceled"}
	case errors.Is(err, errInvalidCard):
		return classify.Outcome{Kind: classify.OutcomeNonRetryable, Reason: "payment_invalid_card"}
	default:
		return classify.Outcome{Kind: classify.OutcomeRetryable, Reason: "payment_transient"}
	}
}

func newExecutor() *retry.Executor {
	return retry.NewDefaultExecutor(
		retry.WithClassifier("payment", paymentClassifier{}),
		retry.WithPolicy("payments.Charge",
			policy.MaxAttempts(3),
			policy.ExponentialBackoff(10*time.Millisecond, 100*time.Millisecond),
			policy.Classifier("payment"),
			policy.Budget("unlimited"),
		),
	)
}

func chargeWithExecutor(ctx context.Context, exec *retry.Executor, gateway *paymentGateway) (Receipt, error) {
	key := policy.ParseKey("payments.Charge")
	return retry.DoValue[Receipt](ctx, exec, key, func(ctx context.Context) (Receipt, error) {
		return gateway.Charge(ctx, "acct_123", 2500)
	})
}

func main() {
	ctx, capture := observe.RecordTimeline(context.Background())
	gateway := &paymentGateway{}

	receipt, err := chargeWithExecutor(ctx, newExecutor(), gateway)
	fmt.Printf("receipt=%s err=%v attempts=%d\n", receipt.ID, err, gateway.attempts)

	if tl := capture.Timeline(); tl != nil {
		for _, attempt := range tl.Attempts {
			fmt.Printf("attempt=%d reason=%s err=%v\n", attempt.Attempt, attempt.Outcome.Reason, attempt.Err)
		}
	}
}
