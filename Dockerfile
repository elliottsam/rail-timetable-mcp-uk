ARG GO_VERSION=1.27

FROM golang:${GO_VERSION} AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/rail-mcp ./cmd/rail-mcp

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/rail-mcp /rail-mcp
ENV MCP_TRANSPORT=http \
    MCP_ADDR=0.0.0.0:8080 \
    LOG_FORMAT=json
EXPOSE 8080
USER nonroot:nonroot
# MCP_AUTH_TOKEN is mandatory because the container listens on a non-loopback
# address; the server refuses to start without it.
ENTRYPOINT ["/rail-mcp"]
