# rail-timetable-mcp-uk

An [MCP](https://modelcontextprotocol.io) server for live UK train times, backed
by the [Rail Data Marketplace](https://raildata.org.uk) (RDM). Ask an assistant
"what's the next train from King's Cross to Edinburgh?" and it can answer from
live departure boards.

## Status

> **Unverified against the live API.** The server was built before an RDM API
> key was available. The gateway paths, API version, auth header name and JSON
> field names are best guesses from third-party projects, and the test fixtures
> are synthetic. Everything is tested against fakes, but the first run with a
> real key may need a path or header tweak. See [Verifying the live API](#verifying-the-live-api).

Without a key the server still starts, lists its tools, and `search_stations`
works; the board tools return a clear "no API key configured" error.

## Tools

| Tool | Needs key | Purpose |
| --- | --- | --- |
| `search_stations` | no | Find a station's three-letter CRS code by name. |
| `get_departures` | yes | Live departure board; optional `destination`, `limit`, time window. |
| `get_arrivals` | yes | Live arrival board; optional `origin`, `limit`, time window. |
| `get_service_details` | yes | Full route and running of one train, by service `id` from a board. |

Station arguments accept a CRS code (`KGX`) or a name (`kings cross`). An
ambiguous name returns the candidates rather than guessing. The built-in station
list is a seed of about 160 major stations; any valid three-letter code works
even if it is not listed.

## Getting an API key

1. Register at <https://raildata.org.uk> (choose the individual option if you
   are not a company).
2. Subscribe to the **Live Departure Board** product (and, if you want
   `get_service_details`, the service details product).
3. Open the subscription's **Specification** tab and copy the **Consumer key**.
4. Export it: `export RAILDATA_API_KEY=...`

Treat the key as a password. Never commit it or paste it into a chat.

## Quick start (stdio)

```sh
make build            # produces bin/rail-mcp
RAILDATA_API_KEY=... ./bin/rail-mcp
```

### Claude Code

```sh
claude mcp add rail-timetable --env RAILDATA_API_KEY=... -- /path/to/bin/rail-mcp
```

### Claude Desktop

```json
{
  "mcpServers": {
    "rail-timetable": {
      "command": "/path/to/bin/rail-mcp",
      "env": { "RAILDATA_API_KEY": "..." }
    }
  }
}
```

## Configuration

Flags override environment variables. Secrets are environment-only.

| Variable | Flag | Default | Purpose |
| --- | --- | --- | --- |
| `RAILDATA_API_KEY` | | | RDM consumer key for boards. |
| `RAILDATA_SERVICE_API_KEY` | | `RAILDATA_API_KEY` | Key for service details, if separate. |
| `RAILDATA_BASE_URL` | | `https://api1.raildata.org.uk` | API gateway origin. |
| `RAILDATA_AUTH_HEADER` | | `x-apikey` | Header that carries the key. |
| `RAILDATA_DEPARTURES_PATH` | | see `internal/rdm/config.go` | Departure product path. |
| `RAILDATA_ARRIVALS_PATH` | | see `internal/rdm/config.go` | Arrival product path. |
| `RAILDATA_SERVICE_DETAILS_PATH` | | see `internal/rdm/config.go` | Service details product path. |
| `MCP_TRANSPORT` | `-transport` | `stdio` | `stdio` or `http`. |
| `MCP_ADDR` | `-addr` | `127.0.0.1:8080` | HTTP listen address. |
| `MCP_AUTH_TOKEN` | | | Bearer token for HTTP. **Required** off loopback. |
| `MCP_ALLOWED_ORIGINS` | `-allowed-origins` | | Comma-separated browser origins for HTTP. |
| `LOG_LEVEL` | `-log-level` | `info` | `debug`, `info`, `warn`, `error`. |
| `LOG_FORMAT` | `-log-format` | `text` | `text` or `json`. Logs go to stderr. |

## HTTP transport

```sh
MCP_AUTH_TOKEN=$(openssl rand -base64 32) ./bin/rail-mcp -transport http
```

The MCP endpoint is `/mcp` (streamable HTTP); `/healthz` is an unauthenticated
probe. The server spends your RDM quota on behalf of every caller, so:

- it binds to loopback by default;
- listening on any other address **refuses to start** without `MCP_AUTH_TOKEN`;
- cross-origin browser requests and DNS-rebinding hosts are rejected;
- terminate TLS in front of it (a reverse proxy) before exposing it beyond
  localhost.

## Docker

```sh
cp .env.example .env     # set MCP_AUTH_TOKEN and RAILDATA_API_KEY
docker compose up --build
```

The image is distroless, runs as non-root, and listens on port 8080 (published
on `127.0.0.1` by the compose file). The container health check is the binary's
own `-healthcheck` flag.

## Verifying the live API

Once you have a key:

```sh
RAILDATA_API_KEY=... go test -tags live -count=1 -v ./internal/rdm/...
```

If a test fails with `not found` or `rejected credentials`, compare the product's
Specification tab with the defaults in `internal/rdm/config.go` and override them
with the `RAILDATA_*` variables above (then update the defaults). If decoding
fails, adjust the wire types in `internal/rdm/wire.go` and record a scrubbed real
response under `internal/rdm/testdata/`.

## Development

```sh
make check    # gofmt, vet, golangci-lint, go test -race; needs no key or network
make fuzz     # short fuzz run over the station search and board decoder
make docker
```

Layout:

```text
cmd/rail-mcp/        wiring only
internal/config/     flags and environment
internal/rdm/        Rail Data Marketplace client (retries, cache, typed errors)
internal/stations/   embedded CRS list and name search
internal/server/     MCP tools and the HTTP transport
.claude/skills/      Go style guidance for coding agents
```

The skills in `.claude/skills/` guide coding agents working in this repo:
`go-style-guide` is vendored from
[madflojo/go-style-agent-skill](https://github.com/madflojo/go-style-agent-skill)
(Apache-2.0); `go-mcp-server` and `go-http-client` are project-specific.

## Station data

`internal/stations/stations.csv` is a hand-curated seed list. To cover every
station, replace it with a full CRS list (columns `crs,name`) from a source whose
licence permits redistribution; the package validates the file at startup and in
tests.
