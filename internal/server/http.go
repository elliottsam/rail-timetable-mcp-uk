package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// MCPPath is where the streamable HTTP endpoint is mounted.
	MCPPath = "/mcp"
	// HealthPath answers 200 without authentication, for container probes.
	HealthPath = "/healthz"

	shutdownTimeout = 10 * time.Second
)

// HTTPConfig configures the streamable HTTP transport.
type HTTPConfig struct {
	// AuthToken, when set, is required as a bearer token on MCPPath.
	AuthToken string

	// AllowedOrigins are browser origins (scheme://host[:port]) permitted to
	// make cross-origin requests. Requests without an Origin header, such as
	// those from non-browser MCP clients, are always allowed.
	AllowedOrigins []string

	// Logger records panics and server lifecycle events. Default slog.Default().
	Logger *slog.Logger
}

// Handler returns an http.Handler serving srv at MCPPath, with a health probe
// at HealthPath.
func Handler(srv *mcp.Server, cfg HTTPConfig) (http.Handler, error) {
	if srv == nil {
		return nil, fmt.Errorf("%w: server is required", ErrInvalidConfig)
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	cop := http.NewCrossOriginProtection()
	for _, o := range cfg.AllowedOrigins {
		if err := cop.AddTrustedOrigin(o); err != nil {
			return nil, fmt.Errorf("%w: allowed origin %q: %w", ErrInvalidConfig, o, err)
		}
	}

	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		Logger: cfg.Logger,
	})

	mux := http.NewServeMux()
	mux.HandleFunc(HealthPath, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle(MCPPath, requireBearer(cfg.AuthToken, cop.Handler(mcpHandler)))
	return recoverPanics(cfg.Logger, mux), nil
}

// Serve serves h on ln until ctx is cancelled, then shuts down gracefully.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	hs := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	errc := make(chan error, 1)
	go func() { errc <- hs.Serve(ln) }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down HTTP server")
	shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := hs.Shutdown(shutCtx); err != nil {
		_ = hs.Close()
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// requireBearer rejects requests lacking the bearer token. An empty token
// disables the check.
func requireBearer(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		sum := sha256.Sum256([]byte(got))
		if !ok || subtle.ConstantTimeCompare(sum[:], want[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="rail-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// recoverPanics is the process boundary: it logs a handler panic and answers
// 500 instead of dropping the connection.
func recoverPanics(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(rec)
				}
				logger.Error("panic in HTTP handler", "panic", rec, "path", r.URL.Path)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
