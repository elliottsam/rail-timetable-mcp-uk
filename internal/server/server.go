// Package server exposes live UK train times as an MCP server.
//
// It adapts the rdm client and the stations index to MCP tools; it holds no
// business logic of its own.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/elliottsam/rail-timetable-mcp-uk/internal/rdm"
	"github.com/elliottsam/rail-timetable-mcp-uk/internal/stations"
)

// ErrInvalidConfig is returned by New when a dependency is missing.
var ErrInvalidConfig = errors.New("invalid config")

// Boards is the upstream data source the tools depend on. *rdm.Client
// implements it.
type Boards interface {
	Departures(ctx context.Context, req rdm.BoardRequest) (*rdm.Board, error)
	Arrivals(ctx context.Context, req rdm.BoardRequest) (*rdm.Board, error)
	ServiceDetails(ctx context.Context, serviceID string) (*rdm.ServiceDetail, error)
}

// Config configures the MCP server.
type Config struct {
	// Boards fetches live data. Required.
	Boards Boards

	// Stations resolves station names. Required.
	Stations *stations.Index

	// Logger records unexpected failures. Default slog.Default().
	Logger *slog.Logger

	// Version is reported to MCP clients. Default "dev".
	Version string
}

type handlers struct {
	boards   Boards
	stations *stations.Index
	logger   *slog.Logger
}

const instructions = `Live UK National Rail train times.
Use search_stations to find a station's three-letter CRS code, then get_departures or get_arrivals for the live board. ` +
	`Each service on a board has an id that get_service_details accepts to show its full route. ` +
	`Times are UK local time. "expected" is a time, or text such as "On time", "Delayed" or "Cancelled".`

// New builds an MCP server with the train-times tools registered.
//
// Tools that need the upstream API key are registered even when no key is
// configured; they return a clear tool error when called.
func New(cfg Config) (*mcp.Server, error) {
	if cfg.Boards == nil {
		return nil, fmt.Errorf("%w: Boards is required", ErrInvalidConfig)
	}
	if cfg.Stations == nil {
		return nil, fmt.Errorf("%w: Stations is required", ErrInvalidConfig)
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}

	h := &handlers{boards: cfg.Boards, stations: cfg.Stations, logger: cfg.Logger}

	srv := mcp.NewServer(
		&mcp.Implementation{Name: "rail-timetable-mcp-uk", Version: cfg.Version},
		&mcp.ServerOptions{Instructions: instructions, Logger: cfg.Logger},
	)
	h.register(srv)
	return srv, nil
}
