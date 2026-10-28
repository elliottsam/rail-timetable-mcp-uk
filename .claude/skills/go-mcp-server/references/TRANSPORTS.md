# Transports

## stdio (default)

```go
if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil { ... }
```

- stdout carries JSON-RPC. Anything else written there corrupts the stream.
- Build the logger with `slog.NewTextHandler(os.Stderr, ...)` in `main` and pass
  it in. Reusable packages do not log.
- `ctx` comes from `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`.
- Secrets arrive through environment variables set in the client's MCP config,
  never as flags (flags show up in process listings).

## Streamable HTTP

```go
h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)

mux := http.NewServeMux()
mux.Handle("/mcp", authMiddleware(originCheck(h)))
mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

hs := &http.Server{
    Addr:              cfg.Addr,
    Handler:           mux,
    ReadHeaderTimeout: 10 * time.Second,
    IdleTimeout:       2 * time.Minute,
}
```

Do not set a short `WriteTimeout`; streamed responses outlive it.

### Security

- Default bind is `127.0.0.1:<port>`. Binding to a non-loopback address
  requires `MCP_AUTH_TOKEN`; refuse to start otherwise.
- Compare bearer tokens with `subtle.ConstantTimeCompare`.
- Wrap the MCP handler with `http.NewCrossOriginProtection().Handler(h)` (stdlib)
  and register permitted browser origins with `AddTrustedOrigin`. Requests with
  no `Origin` or `Sec-Fetch-Site` header (non-browser clients) pass through.
  The SDK's `StreamableHTTPOptions.CrossOriginProtection` field is deprecated.
- The SDK rejects requests that arrive on a localhost address with a
  non-localhost `Host` header (DNS rebinding). Leave
  `DisableLocalhostProtection` false.
- The server spends the operator's upstream quota on behalf of every caller, so
  treat HTTP exposure as a deployment decision, not a default.
- Never log the `Authorization` header or the upstream API key.

### Shutdown

On context cancel, call `hs.Shutdown` with a bounded timeout (for example 10s),
then return. Every goroutine started must exit on `ctx.Done()`.

## Selecting a transport

`--transport stdio|http` with an env override. `main` parses flags, builds
`Config`, and calls a `Run` function per transport; no logic in `main`.
