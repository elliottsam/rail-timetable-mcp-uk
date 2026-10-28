// Command rail-mcp is an MCP server for live UK train times, backed by the
// Rail Data Marketplace (raildata.org.uk).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/elliottsam/rail-timetable-mcp-uk/internal/config"
	"github.com/elliottsam/rail-timetable-mcp-uk/internal/rdm"
	"github.com/elliottsam/rail-timetable-mcp-uk/internal/server"
	"github.com/elliottsam/rail-timetable-mcp-uk/internal/stations"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch err := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr); {
	case err == nil, errors.Is(err, config.ErrHelp):
	default:
		fmt.Fprintf(os.Stderr, "rail-mcp: %v\n", err)
		os.Exit(1)
	}
}

// run wires the dependencies and serves until ctx is cancelled. stdout is used
// only for -version: under the stdio transport it carries the protocol, so
// nothing else may be written to it.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	cfg, err := config.Load(args, getenv, stderr)
	if errors.Is(err, config.ErrVersion) {
		_, err = fmt.Fprintln(stdout, version)
		return err
	}
	if err != nil {
		return err
	}

	if cfg.Healthcheck {
		return healthcheck(ctx, cfg.Addr)
	}

	logger := newLogger(cfg, stderr)

	client, err := rdm.New(cfg.RDM)
	if err != nil {
		return err
	}
	index, err := stations.New()
	if err != nil {
		return err
	}
	srv, err := server.New(server.Config{Boards: client, Stations: index, Logger: logger, Version: version})
	if err != nil {
		return err
	}

	logger.Info("starting",
		"version", version,
		"transport", cfg.Transport,
		"stations", index.Len(),
		"boards_key", client.HasBoardCredentials(),
		"service_key", client.HasServiceCredentials(),
	)
	if !client.HasBoardCredentials() {
		logger.Warn("RAILDATA_API_KEY is not set; live board tools will return an error until it is configured")
	}

	switch cfg.Transport {
	case config.TransportHTTP:
		return serveHTTP(ctx, cfg, srv, logger)
	default:
		err := srv.Run(ctx, &mcp.StdioTransport{})
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
}

func serveHTTP(ctx context.Context, cfg config.Config, srv *mcp.Server, logger *slog.Logger) error {
	handler, err := server.Handler(srv, server.HTTPConfig{
		AuthToken:      string(cfg.AuthToken),
		AllowedOrigins: cfg.AllowedOrigins,
		Logger:         logger,
	})
	if err != nil {
		return err
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.Addr)
	if err != nil {
		return err
	}
	logger.Info("listening", "addr", ln.Addr().String(), "path", server.MCPPath, "auth", cfg.AuthToken != "")
	return server.Serve(ctx, ln, handler, logger)
}

// healthcheck probes the local /healthz endpoint. A listen address with no
// specific host (":8080", "0.0.0.0:8080") is probed on loopback.
func healthcheck(ctx context.Context, addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+server.HealthPath, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check: status %d", resp.StatusCode)
	}
	return nil
}

func newLogger(cfg config.Config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}
