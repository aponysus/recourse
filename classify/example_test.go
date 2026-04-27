package classify_test

import (
	"fmt"
	"time"

	"github.com/aponysus/recourse/classify"
)

type exampleHTTPError struct {
	status int
	method string
}

func (e exampleHTTPError) Error() string { return "http error" }

func (e exampleHTTPError) HTTPStatusCode() int { return e.status }

func (e exampleHTTPError) HTTPMethod() string { return e.method }

func (e exampleHTTPError) RetryAfter() (time.Duration, bool) { return 0, false }

func ExampleHTTPClassifier() {
	classifier := classify.HTTPClassifier{}
	outcome := classifier.Classify(nil, exampleHTTPError{status: 503, method: "GET"})

	fmt.Println(outcome.Reason)
	// Output:
	// http_5xx
}
