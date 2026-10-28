# Resilience and Testing

## Retries

- Retry only idempotent requests (GET). Retry on transport errors, 429, and
  5xx. Do not retry other 4xx.
- Capped exponential backoff with jitter; honor `Retry-After` when present.
- Wait with a timer and `ctx`, never `time.Sleep`:

```go
select {
case <-ctx.Done():
    return ctx.Err()
case <-time.After(delay):
}
```

- Make the backoff base a `Config` field so tests can set it to near zero.
- Rebuild or rewind the request on each attempt; drain and close the previous
  body.

## Caching

A small in-memory TTL cache (about 30s for live boards) protects the upstream
quota when several callers ask for the same thing.

- Key by the full request path.
- Guard with a `sync.Mutex`; run tests with `-race`.
- Bound the size or sweep expired entries so memory cannot grow without limit.
- Do not cache errors.
- Take a clock function in `Config` so expiry is testable without sleeping.

## Testing

- Use `httptest.NewServer` and point `BaseURL` at it. Assert the path, the auth
  header, and `Accept`.
- Table-driven cases for each status class, a malformed body, an oversized body,
  a slow server (timeout), and context cancellation mid-retry.
- A retry test asserts attempt counts: 503, 503, 200 succeeds; 401 is attempted
  once.
- Fixtures live in `testdata/*.json`. Scrub any real recorded response of keys,
  tokens, and personal data before committing.
- Fuzz the wire-to-domain mapper with `go test -fuzz` for crash safety.
- Anything that needs a real key sits behind `//go:build live` and skips when
  the key variable is unset. `make check` never needs network or credentials.
