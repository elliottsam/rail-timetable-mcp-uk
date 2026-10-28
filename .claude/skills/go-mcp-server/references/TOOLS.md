# Defining Tools

Verify names and signatures against the pinned `go-sdk` version before copying
these examples.

## Shape

```go
type DeparturesInput struct {
    Station     string `json:"station" jsonschema:"CRS code (e.g. KGX) or station name"`
    Destination string `json:"destination,omitempty" jsonschema:"only services calling at this station"`
    Limit       int    `json:"limit,omitempty" jsonschema:"max services to return, default 10"`
}

type DeparturesOutput struct {
    Station  string    `json:"station"`
    Services []Service `json:"services"`
    Truncated bool     `json:"truncated,omitempty"`
}

func (s *Server) getDepartures(
    ctx context.Context,
    _ *mcp.CallToolRequest,
    in DeparturesInput,
) (*mcp.CallToolResult, DeparturesOutput, error) {
    // resolve station, call s.boards, shape output
}

mcp.AddTool(srv, &mcp.Tool{
    Name:        "get_departures",
    Description: "Live departures from a UK station. Use for 'next trains from X'.",
}, s.getDepartures)
```

## Rules

- One job per tool. Prefer `get_departures` and `get_arrivals` over a single
  `get_board` with a mode flag.
- Descriptions say when to use the tool and what it returns. Mention
  prerequisites (for example, "requires RAILDATA_API_KEY").
- Every input field has a `jsonschema` description. Optional fields use
  `omitempty`. Apply defaults and caps in the handler, and document them.
- Outputs are compact and stable. Drop upstream fields the model does not need.
  Use ISO 8601 times with an explicit zone.
- Cap list sizes and set `Truncated` rather than returning everything.
- Handlers depend on a small consumer-side interface, not the concrete client,
  so tests can fake it.

## Errors

- Return a Go `error` from the handler for failures the model should see (bad
  station, key not set, upstream unavailable). The SDK reports it as a tool
  result with `isError` set, so the model can recover or tell the user.
- Messages are actionable and free of secrets, URLs with keys, or raw upstream
  bodies. Example: `"RAILDATA_API_KEY is not set; see README"`.
- Map domain sentinels (`ErrUnauthorized`, `ErrNotFound`, `ErrRateLimited`) to
  fixed user-facing messages in one place, using `errors.Is`.
- Protocol-level errors are for malformed requests the SDK itself rejects. Do
  not use them for business failures.
- Check `ctx.Err()` after long calls; respect cancellation.

## Panics

The SDK runs each tool call on its own goroutine and does **not** recover
panics, so one panicking handler terminates the whole server process. Wrap every
handler in a recover guard at registration time (this is the application
boundary, where `recover` belongs). The guard logs the panic and stack, and
returns a generic tool error that does not echo the panic value:

```go
mcp.AddTool(srv, tool, guard(logger, "get_departures", h.getDepartures))
```

Test it with a fake dependency that panics and assert the call returns
`IsError` and the server still answers afterwards.
