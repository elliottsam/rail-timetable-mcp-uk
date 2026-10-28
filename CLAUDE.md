# rail-timetable-mcp-uk

A Go MCP server returning live UK train times from the Rail Data Marketplace
(raildata.org.uk). The module path is in `go.mod`.

## Skills

Project skills live in `.claude/skills/`. Use them for all Go work here:

- `go-style-guide`: general Go conventions (vendored from
  madflojo/go-style-agent-skill; do not edit, see its `NOTICE.md`).
- `go-mcp-server`: MCP tools, transports, and testing with the official SDK.
- `go-http-client`: wrapping the upstream REST API.

If a rule here conflicts with a skill, this file wins.

## Conventions

- `make check` (gofmt, vet, golangci-lint, `go test -race ./...`) must pass
  with no API key and no network.
- Upstream credentials come from `RAILDATA_API_KEY` (and optionally
  `RAILDATA_SERVICE_API_KEY`). A missing key must never stop the server
  starting; affected tools return a clear tool error.
- Under stdio, stdout is the protocol channel. Log to stderr only.
- The RDM endpoint path, API version, and auth header name are unverified
  until a key is available. Keep them in `Config`, not constants.
- Tests needing a real key use `//go:build live` and skip when the key is unset.
