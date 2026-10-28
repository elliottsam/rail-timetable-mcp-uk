---
name: go-http-client
description: >
  Guidance for writing Go clients that wrap an upstream HTTP/REST API. Use
  whenever the task involves calling an external API, API keys or auth headers,
  timeouts, retries and backoff, caching, mapping upstream failures to typed
  errors, or testing a client with httptest and fixtures. Complements
  go-style-guide; follow that skill for general Go conventions.
license: Apache-2.0
metadata:
  version: "0.1.0"
---

# Go HTTP Client Skill

Project conventions for packages that wrap a third-party HTTP API. General Go
style comes from `go-style-guide`; this skill covers what is specific to
upstream API clients.

---

## TL;DR

- `New(cfg Config) (*Client, error)`: validate, apply defaults, return a
  concrete struct. Base URL, API version, auth header name, timeout, and
  retry policy are `Config` fields, not constants.
- Inject `*http.Client` through `Config` (default has a timeout). Never use
  `http.DefaultClient`.
- Every method takes `context.Context` first and builds requests with
  `http.NewRequestWithContext`.
- Decode into small private wire structs, then map to the package's public
  domain types. Upstream shapes must not leak out.
- Typed sentinel errors for durable branching (`ErrUnauthorized`,
  `ErrNotFound`, `ErrRateLimited`); wrap upstream detail with `%w`.
- The client does not log. Secrets never appear in errors, `String()`, or
  `%v` output.
- Retries only for idempotent requests, only on network errors, 429, and 5xx,
  with capped exponential backoff and jitter, honoring `Retry-After` and `ctx`.
- Tests use `httptest.Server` and fixtures. No network, no real key.

---

## Execution Protocol

1. Read the upstream docs and, if possible, a real response. Record unverified
   assumptions (paths, header names, versions) as `Config` defaults and note
   them in the README until confirmed.
2. Define the public types, `Config`, and sentinel errors first.
3. Write `httptest` tests for success, each error class, retry, and cancel.
4. Implement request building, one `do` helper, decode, and mapping.
5. Check that errors and logs cannot contain the key.

---

## Quick Rules Table

| Topic | Rule | Reference |
| --- | --- | --- |
| Construction | `Config` in, concrete `*Client` out, secrets redacted | `references/CLIENT.md` |
| Requests | One `do` helper; context, auth header, `Accept`, body limit | `references/CLIENT.md` |
| Resilience | Capped backoff + jitter, `Retry-After`, small TTL cache | `references/RESILIENCE.md` |
| Errors | Status to sentinel mapping; no bodies or keys in messages | `references/CLIENT.md` |
| Testing | `httptest`, fixtures in `testdata/`, opt-in `live` build tag | `references/RESILIENCE.md` |

---

## Common Pitfalls

- Hard-coding the base URL or API version so a path change means a code change.
- Reading an unbounded response body; wrap it with `io.LimitReader`.
- Forgetting to close (and drain) `resp.Body` on every path.
- Retrying non-idempotent calls, or retrying 4xx other than 429.
- Sleeping with `time.Sleep` instead of a `select` on `ctx.Done()` and a timer.
- Including the request URL or headers in errors when they carry credentials.
- Sharing a mutable cache without synchronization.

---

## Reference Loading

- Read [references/CLIENT.md](references/CLIENT.md) for `Config`, the `do`
  helper, decoding, and error mapping.
- Read [references/RESILIENCE.md](references/RESILIENCE.md) for retries,
  backoff, caching, and test strategy.
