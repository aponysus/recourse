# Comparison with other libraries

This page is a decision aid, not an exhaustive evaluation. It groups common approaches and lists example projects. For exact behavior and guarantees, consult upstream docs.

## Quick decision questions

- Do you need centralized policy control, bounded envelopes, and per-attempt observability? -> recourse.
- Do you only need a simple backoff helper at a few call sites? -> a backoff helper library.
- Do you only need circuit breaking without retries or hedging? -> a focused circuit breaker library.
- Do you want a Hystrix-style command abstraction? -> a Hystrix-style library.

## Comparison table

| Approach | Example projects | Best for | Tradeoffs |
|---|---|---|---|
| Policy-driven envelopes | recourse | Multi-service consistency, operational visibility, and governance | Requires key conventions, policy ownership, and rollout discipline |
| Backoff helpers | cenkalti/backoff, avast/retry-go | Small apps or isolated call sites | Configuration lives at call sites; consistency and observability are up to you |
| Circuit breaker only | sony/gobreaker | Adding circuit breaking without a retry framework | You still need retry logic, backoff, and observability elsewhere |
| Command pattern | hystrix-go | Teams that want a Hystrix-style command abstraction | Heavier integration and a different API shape |

## How to choose

1. Start with the simplest tool that meets your needs.
2. If retries are a platform concern, centralize policies rather than duplicating loops.
3. If on-call needs to explain behavior, prioritize structured observability.
4. If load amplification is a risk, use budgets and explicit backpressure.
5. If you only need one resilience mechanism, pick a focused library.
