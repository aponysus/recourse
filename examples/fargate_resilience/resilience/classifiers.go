package resilience

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"strings"

	"github.com/aponysus/recourse/classify"
)

type AWSReadClassifier struct{}
type AWSWriteSafeClassifier struct{}
type AWSCallbackClassifier struct{}
type SQLConnectClassifier struct{}
type SQLQueryClassifier struct{}
type EventingClassifier struct{}

type awsAPIError interface {
	error
	ErrorCode() string
}

type sqlNumberError interface {
	error
	SQLErrorNumber() int32
}

func (AWSReadClassifier) Classify(_ any, err error) classify.Outcome {
	return classifyAWS(err, "aws_read_terminal")
}

func (AWSWriteSafeClassifier) Classify(_ any, err error) classify.Outcome {
	return classifyAWS(err, "aws_write_terminal")
}

func (AWSCallbackClassifier) Classify(_ any, err error) classify.Outcome {
	if out, ok := commonOutcome(err); ok {
		return out
	}
	if isNetRetryable(err) {
		return retryable("sfn_transport")
	}

	if code, ok := awsCode(err); ok {
		switch {
		case code == "KmsThrottlingException" || isAWSThrottle(code):
			return retryable("sfn_throttled")
		case isAWSRetryableService(code):
			return retryable("sfn_service_error")
		case code == "InvalidToken":
			return terminal("sfn_invalid_token")
		case code == "TaskTimedOut":
			return terminal("sfn_task_timed_out")
		case code == "TaskDoesNotExist":
			return terminal("sfn_task_missing")
		case code == "InvalidOutput":
			return terminal("sfn_invalid_output")
		case code == "KmsAccessDeniedException" || code == "KmsInvalidStateException":
			return terminal("sfn_kms_terminal")
		default:
			return terminal("sfn_non_retryable")
		}
	}

	return terminal("sfn_unknown")
}

func (SQLConnectClassifier) Classify(_ any, err error) classify.Outcome {
	if out, ok := commonOutcome(err); ok {
		return out
	}
	if errors.Is(err, driver.ErrBadConn) {
		return retryable("sql_bad_conn")
	}
	if isNetRetryable(err) {
		return retryable("sql_transport")
	}

	if n, ok := sqlNumber(err); ok {
		switch n {
		case 18456:
			return terminal("sql_auth")
		case 4060:
			return terminal("sql_config")
		case 40197, 40501, 40613, 10928, 10929:
			return retryable("sql_transient_engine")
		default:
			return terminal("sql_non_retryable")
		}
	}

	return terminal("sql_unknown")
}

func (SQLQueryClassifier) Classify(_ any, err error) classify.Outcome {
	if out, ok := commonOutcome(err); ok {
		return out
	}
	if errors.Is(err, driver.ErrBadConn) {
		return retryable("sql_bad_conn")
	}
	if isNetRetryable(err) {
		return retryable("sql_transport")
	}

	if n, ok := sqlNumber(err); ok {
		switch n {
		case 1205:
			return retryable("sql_deadlock")
		case 1222, -2, 40197, 40501, 40613, 10928, 10929:
			return retryable("sql_transient_engine")
		case 18456:
			return terminal("sql_auth")
		default:
			return terminal("sql_non_retryable")
		}
	}

	return terminal("sql_unknown")
}

func (EventingClassifier) Classify(_ any, err error) classify.Outcome {
	if out, ok := commonOutcome(err); ok {
		return out
	}
	if isNetRetryable(err) {
		return retryable("eventing_transport")
	}
	if code, ok := awsCode(err); ok {
		switch {
		case isAWSThrottle(code):
			return retryable("eventing_throttled")
		case isAWSRetryableService(code):
			return retryable("eventing_service_error")
		default:
			return terminal("eventing_non_retryable")
		}
	}
	return terminal("eventing_unknown")
}

func classifyAWS(err error, terminalReason string) classify.Outcome {
	if out, ok := commonOutcome(err); ok {
		return out
	}
	if isNetRetryable(err) {
		return retryable("aws_transport")
	}

	if code, ok := awsCode(err); ok {
		switch {
		case isAWSThrottle(code):
			return retryable("aws_throttled")
		case isAWSRetryableService(code):
			return retryable("aws_service_error")
		case isAWSTerminal(code):
			return terminal(terminalReason)
		default:
			return terminal("aws_non_retryable")
		}
	}

	if looksLikeThrottle(err.Error()) {
		return retryable("aws_throttled")
	}
	return terminal("aws_unknown")
}

func commonOutcome(err error) (classify.Outcome, bool) {
	switch {
	case err == nil:
		return success(), true
	case errors.Is(err, context.Canceled):
		return abort("context_canceled"), true
	case errors.Is(err, context.DeadlineExceeded):
		return retryable("context_deadline_exceeded"), true
	default:
		return classify.Outcome{}, false
	}
}

func awsCode(err error) (string, bool) {
	var ae awsAPIError
	if errors.As(err, &ae) {
		return ae.ErrorCode(), true
	}
	return "", false
}

func sqlNumber(err error) (int32, bool) {
	var se sqlNumberError
	if errors.As(err, &se) {
		return se.SQLErrorNumber(), true
	}
	return 0, false
}

func isAWSThrottle(code string) bool {
	switch code {
	case "Throttling", "ThrottlingException", "TooManyRequestsException", "SlowDown",
		"RequestLimitExceeded", "ProvisionedThroughputExceededException", "KmsThrottlingException":
		return true
	default:
		return false
	}
}

func isAWSRetryableService(code string) bool {
	switch code {
	case "InternalError", "InternalFailure", "InternalServiceError", "ServiceUnavailable",
		"RequestTimeout", "RequestTimeoutException", "PriorRequestNotComplete":
		return true
	default:
		return false
	}
}

func isAWSTerminal(code string) bool {
	switch code {
	case "AccessDenied", "AccessDeniedException", "ValidationException", "InvalidRequest",
		"InvalidParameter", "InvalidParameterValue", "ResourceNotFoundException",
		"NoSuchKey", "SecretNotFoundException":
		return true
	default:
		return false
	}
}

func isNetRetryable(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "tls handshake timeout") ||
		strings.Contains(msg, "i/o timeout")
}

func looksLikeThrottle(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "throttl") ||
		strings.Contains(msg, "rate exceeded") ||
		strings.Contains(msg, "slowdown")
}

func success() classify.Outcome {
	return classify.Outcome{Kind: classify.OutcomeSuccess, Reason: "success"}
}

func retryable(reason string) classify.Outcome {
	return classify.Outcome{Kind: classify.OutcomeRetryable, Reason: reason}
}

func terminal(reason string) classify.Outcome {
	return classify.Outcome{Kind: classify.OutcomeNonRetryable, Reason: reason}
}

func abort(reason string) classify.Outcome {
	return classify.Outcome{Kind: classify.OutcomeAbort, Reason: reason}
}
