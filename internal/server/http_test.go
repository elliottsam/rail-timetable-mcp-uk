package server_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/elliottsam/rail-timetable-mcp-uk/internal/rdm"
	"github.com/elliottsam/rail-timetable-mcp-uk/internal/server"
	"github.com/elliottsam/rail-timetable-mcp-uk/internal/stations"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newMCPServer(t *testing.T) *mcp.Server {
	t.Helper()
	idx, err := stations.New()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(server.Config{
		Boards:   &fakeBoards{board: sampleBoard(1)},
		Stations: idx,
		Logger:   quietLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func newHTTPServer(t *testing.T, cfg server.HTTPConfig) *httptest.Server {
	t.Helper()
	cfg.Logger = quietLogger()
	h, err := server.Handler(newMCPServer(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

const initBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`

// post sends an MCP initialize request and returns the status and headers.
func post(t *testing.T, url string, headers map[string]string) (int, http.Header) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(initBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, resp.Header
}

// get requests url and returns the status code.
func get(t *testing.T, url string) (int, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}

func TestHealthzNeedsNoAuth(t *testing.T) {
	ts := newHTTPServer(t, server.HTTPConfig{AuthToken: "s3cret"})
	if status, err := get(t, ts.URL+server.HealthPath); err != nil || status != http.StatusOK {
		t.Errorf("status = %d, err = %v", status, err)
	}
}

func TestBearerAuth(t *testing.T) {
	ts := newHTTPServer(t, server.HTTPConfig{AuthToken: "s3cret"})

	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"wrong token", "Bearer nope", http.StatusUnauthorized},
		{"wrong scheme", "Basic s3cret", http.StatusUnauthorized},
		{"bare token", "s3cret", http.StatusUnauthorized},
		{"prefix of token", "Bearer s3cre", http.StatusUnauthorized},
		{"correct", "Bearer s3cret", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{}
			if tt.header != "" {
				headers["Authorization"] = tt.header
			}
			status, hdr := post(t, ts.URL+server.MCPPath, headers)
			if status != tt.want {
				t.Errorf("status = %d, want %d", status, tt.want)
			}
			if tt.want == http.StatusUnauthorized && hdr.Get("WWW-Authenticate") == "" {
				t.Error("401 without WWW-Authenticate")
			}
		})
	}
}

func TestNoTokenMeansOpen(t *testing.T) {
	ts := newHTTPServer(t, server.HTTPConfig{})
	if status, _ := post(t, ts.URL+server.MCPPath, nil); status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
}

func TestCrossOriginRequestsBlocked(t *testing.T) {
	ts := newHTTPServer(t, server.HTTPConfig{AllowedOrigins: []string{"https://app.example.com"}})

	tests := []struct {
		name   string
		origin string
		want   int
	}{
		{"hostile origin", "https://evil.example", http.StatusForbidden},
		{"allowed origin", "https://app.example.com", http.StatusOK},
		{"no origin", "", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{}
			if tt.origin != "" {
				headers["Origin"] = tt.origin
			}
			if status, _ := post(t, ts.URL+server.MCPPath, headers); status != tt.want {
				t.Errorf("status = %d, want %d", status, tt.want)
			}
		})
	}
}

func TestHandlerRejectsBadConfig(t *testing.T) {
	if _, err := server.Handler(nil, server.HTTPConfig{}); err == nil {
		t.Error("nil server accepted")
	}
	if _, err := server.Handler(newMCPServer(t), server.HTTPConfig{AllowedOrigins: []string{"not an origin"}}); err == nil {
		t.Error("malformed origin accepted")
	}
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func TestToolCallOverStreamableHTTP(t *testing.T) {
	ts := newHTTPServer(t, server.HTTPConfig{AuthToken: "s3cret"})

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:   ts.URL + server.MCPPath,
		HTTPClient: &http.Client{Transport: bearerTransport{"s3cret"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.Close() }()

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "get_departures",
		Arguments: map[string]any{"station": "KGX"},
	})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if got := text(t, res); !strings.Contains(got, "London Kings Cross (KGX)") {
		t.Errorf("text = %q", got)
	}
}

func TestServeShutsDownOnCancel(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h, err := server.Handler(newMCPServer(t), server.HTTPConfig{Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, ln, h, quietLogger()) }()

	url := "http://" + ln.Addr().String() + server.HealthPath
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := get(t, url); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never came up: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve returned %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}
	if _, err := get(t, url); err == nil {
		t.Error("server still accepting connections")
	}
}

func TestToolPanicBecomesToolError(t *testing.T) {
	idx, _ := stations.New()
	srv, err := server.New(server.Config{Boards: panicBoards{}, Stations: idx, Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mustHandler(t, srv))
	defer ts.Close()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil).
		Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: ts.URL + server.MCPPath}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.Close() }()

	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"get_departures", map[string]any{"station": "KGX"}},
		{"get_arrivals", map[string]any{"station": "KGX"}},
		{"get_service_details", map[string]any{"service_id": "x"}},
	} {
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tc.tool, Arguments: tc.args})
		if err != nil || !res.IsError {
			t.Fatalf("%s: err=%v res=%+v, want tool error", tc.tool, err, res)
		}
		if got := text(t, res); strings.Contains(got, "boom") {
			t.Errorf("%s: panic value leaked to client: %q", tc.tool, got)
		}
	}

	if status, err := get(t, ts.URL+server.HealthPath); err != nil || status != http.StatusOK {
		t.Fatalf("server unhealthy after panic: status=%d err=%v", status, err)
	}
}

func mustHandler(t *testing.T, srv *mcp.Server) http.Handler {
	t.Helper()
	h, err := server.Handler(srv, server.HTTPConfig{Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

type panicBoards struct{}

func (panicBoards) Departures(context.Context, rdm.BoardRequest) (*rdm.Board, error) {
	panic("boom")
}
func (panicBoards) Arrivals(context.Context, rdm.BoardRequest) (*rdm.Board, error) { panic("boom") }
func (panicBoards) ServiceDetails(context.Context, string) (*rdm.ServiceDetail, error) {
	panic("boom")
}
