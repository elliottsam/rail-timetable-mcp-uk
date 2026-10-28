---
name: go-mcp-server
description: >
  Guidance for building Model Context Protocol (MCP) servers in Go with the
  official modelcontextprotocol/go-sdk. Use whenever the task involves defining
  MCP tools, resources, or prompts, wiring stdio or streamable HTTP transports,
  mapping errors to tool results, or testing an MCP server. Complements
  go-style-guide; follow that skill for general Go conventions.
license: Apache-2.0
metadata:
  version: "0.1.0"
---

# Go MCP Server Skill

Project conventions for MCP servers written in Go. General Go style (config,
errors, interfaces, testing, layout) comes from `go-style-guide`; this skill
only covers what is specific to MCP.

---

## TL;DR

- Use `github.com/modelcontextprotocol/go-sdk/mcp`. Pin the version in `go.mod`
  and check signatures against that version, since the SDK is still evolving.
- Tool handlers are thin: decode typed input, call a domain package, shape the
  output. No upstream HTTP, parsing, or business logic in handlers.
- Define tools with typed input and output structs. Describe every field with a
  `jsonschema` tag; the model reads those descriptions.
- Over stdio, stdout is the protocol channel. Log to stderr only. Never use
  `fmt.Println` or the default `log` package output.
- A missing optional credential must not stop the server starting. `tools/list`
  works, and the affected tools return a clear tool error.
- Tool failures that the model can act on are tool errors. Reserve protocol
  errors for malformed requests.
- The SDK does not recover tool-handler panics; guard every handler or one bug
  kills the server.
- Test handlers through the SDK's in-memory transport, not by calling the
  handler function alone.

---

## Execution Protocol

1. Inspect existing tools before adding one; match naming and output shape.
2. Define the contract first: tool name (`snake_case`, verb first), input
   struct, output struct, and the error cases the model should see.
3. Write the handler test with a fake of the consumer-side interface.
4. Implement the handler as glue only.
5. Run `make check`, then exercise the server through an MCP client
   (Inspector or the in-memory test client) before calling it done.
6. Confirm tool descriptions and field docs match actual behavior.

---

## Quick Rules Table

| Topic | Rule | Reference |
| --- | --- | --- |
| Tool design | Typed in/out structs, `jsonschema` descriptions, narrow single-purpose tools | `references/TOOLS.md` |
| Errors | Actionable tool errors, no secrets or upstream bodies in messages | `references/TOOLS.md` |
| Transports | stdio default, streamable HTTP opt-in, loopback by default | `references/TRANSPORTS.md` |
| HTTP security | Bearer auth off-loopback, validate `Origin`, server-side timeouts | `references/TRANSPORTS.md` |
| Testing | In-memory client/server pair, fakes for domain deps | `references/TESTING-MCP.md` |

---

## Common Pitfalls

- Writing logs or debug output to stdout under the stdio transport.
- Failing startup because an optional API key is unset.
- Returning raw upstream payloads instead of a compact, model-friendly shape.
- Putting retries, caching, or parsing in the handler instead of the client.
- Exposing a remote HTTP server that spends the operator's upstream quota with
  no authentication.
- Tool descriptions that say what the tool is, not when to use it.
- Unbounded result lists; always cap and say when results were truncated.
- Assuming the SDK recovers a panicking tool handler. It does not.

---

## Reference Loading

Load only what the task needs:

- Read [references/TOOLS.md](references/TOOLS.md) for defining tools, input and
  output structs, schema tags, result shaping, and error mapping.
- Read [references/TRANSPORTS.md](references/TRANSPORTS.md) for stdio, streamable
  HTTP, auth, `Origin` checks, and graceful shutdown.
- Read [references/TESTING-MCP.md](references/TESTING-MCP.md) for in-memory
  client/server tests and manual verification with Inspector.
