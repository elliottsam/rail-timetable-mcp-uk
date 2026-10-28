BIN     := bin/rail-mcp
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
IMAGE   ?= rail-timetable-mcp-uk

.PHONY: all build test race fmt vet lint check fuzz live docker clean

all: check build

build:
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(BIN) ./cmd/rail-mcp

test:
	go test ./...

race:
	go test -race ./...

fmt:
	gofmt -l -w .

vet:
	go vet ./...

lint:
	golangci-lint run

# check must pass with no API key and no network access.
check:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)
	go vet ./...
	golangci-lint run
	go test -race ./...

# Short fuzz pass over the parsers.
fuzz:
	go test ./internal/stations -run '^$$' -fuzz FuzzResolve -fuzztime 20s
	go test ./internal/rdm -run '^$$' -fuzz FuzzDecodeBoard -fuzztime 20s

# Opt-in tests against the real API; need RAILDATA_API_KEY.
live:
	go test -tags live -count=1 ./internal/rdm/...

docker:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) .

clean:
	rm -rf bin
