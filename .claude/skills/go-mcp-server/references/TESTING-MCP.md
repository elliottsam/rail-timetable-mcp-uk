# Testing MCP Servers

## In-memory client/server

Exercise tools through a real MCP session so schemas, argument decoding, and
error mapping are covered, not just the handler body.

```go
func connect(t *testing.T, srv *mcp.Server) *mcp.ClientSession {
    t.Helper()
    ctx := t.Context()

    st, ct := mcp.NewInMemoryTransports()
    if _, err := srv.Connect(ctx, st, nil); err != nil {
        t.Fatal(err)
    }
    client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
    cs, err := client.Connect(ctx, ct, nil)
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { _ = cs.Close() })
    return cs
}
```

Then `cs.CallTool(ctx, &mcp.CallToolParams{Name: "...", Arguments: map[string]any{...}})`
and assert on `IsError`, text content, and structured content.

## What to cover

- `tools/list` returns every tool with a description and input schema, even
  with no credentials configured.
- Each tool: happy path, invalid input, unknown entity, upstream unauthorized,
  rate limited, upstream down, context cancelled.
- Missing-credential tools return the documented tool error, not a crash.
- Truncation and default-limit behavior.

## Fakes

Fake the consumer-side interface by hand; no mocking framework. Keep fixtures in
`testdata/`. Tests must pass offline with no API key (`make check`). Anything
needing a real key sits behind `//go:build live` and skips when the variable is
unset.

## Manual verification

```sh
npx @modelcontextprotocol/inspector ./bin/rail-mcp
```

Check `tools/list`, call each tool, and confirm nothing but JSON-RPC reaches
stdout.
