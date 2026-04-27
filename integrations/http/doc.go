// Package http provides opt-in net/http integrations for recourse.
//
// DoHTTP runs an http.Client request through a recourse retry executor. It
// clones the request for each attempt, replays the body through Request.GetBody
// when present, drains and closes failed response bodies for connection reuse,
// and returns both the successful response and a captured observe.Timeline.
//
// StatusError implements classify.HTTPError so HTTP-aware classifiers can make
// retry decisions from status code, method, and Retry-After. Request bodies must
// be replayable when retries are possible; non-replayable bodies are rejected
// before the first attempt.
package http
