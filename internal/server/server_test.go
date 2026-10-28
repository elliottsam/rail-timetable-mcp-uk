package server_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/elliottsam/rail-timetable-mcp-uk/internal/rdm"
	"github.com/elliottsam/rail-timetable-mcp-uk/internal/server"
	"github.com/elliottsam/rail-timetable-mcp-uk/internal/stations"
)

// fakeBoards is a hand-written Boards double that records the last request.
type fakeBoards struct {
	board   *rdm.Board
	detail  *rdm.ServiceDetail
	err     error
	lastReq rdm.BoardRequest
	lastID  string
}

func (f *fakeBoards) Departures(_ context.Context, r rdm.BoardRequest) (*rdm.Board, error) {
	f.lastReq = r
	return f.board, f.err
}

func (f *fakeBoards) Arrivals(_ context.Context, r rdm.BoardRequest) (*rdm.Board, error) {
	f.lastReq = r
	return f.board, f.err
}

func (f *fakeBoards) ServiceDetails(_ context.Context, id string) (*rdm.ServiceDetail, error) {
	f.lastID = id
	return f.detail, f.err
}

func sampleBoard(n int) *rdm.Board {
	b := &rdm.Board{Station: rdm.Location{Name: "London Kings Cross", CRS: "KGX"}, Services: []rdm.Service{}}
	for i := range n {
		b.Services = append(b.Services, rdm.Service{
			ID:           "svc-" + string(rune('a'+i)),
			Scheduled:    "08:33",
			Expected:     "On time",
			Platform:     "4",
			Operator:     "LNER",
			Destinations: []rdm.Location{{Name: "Edinburgh", CRS: "EDB"}},
		})
	}
	return b
}

func newSession(t *testing.T, b server.Boards) *mcp.ClientSession {
	t.Helper()
	idx, err := stations.New()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(server.Config{
		Boards:   b,
		Stations: idx,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", tool, err)
	}
	return res
}

func text(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range res.Content {
		tc, ok := c.(*mcp.TextContent)
		if !ok {
			t.Fatalf("content is %T, want text", c)
		}
		sb.WriteString(tc.Text)
	}
	return sb.String()
}

func TestNewRequiresDependencies(t *testing.T) {
	idx, _ := stations.New()
	for name, cfg := range map[string]server.Config{
		"no boards":   {Stations: idx},
		"no stations": {Boards: &fakeBoards{}},
	} {
		if _, err := server.New(cfg); !errors.Is(err, server.ErrInvalidConfig) {
			t.Errorf("%s: error = %v, want ErrInvalidConfig", name, err)
		}
	}
}

func TestListTools(t *testing.T) {
	cs := newSession(t, &fakeBoards{})
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		got[tool.Name] = tool
	}
	for _, name := range []string{"search_stations", "get_departures", "get_arrivals", "get_service_details"} {
		tool, ok := got[name]
		if !ok {
			t.Errorf("tool %s not listed", name)
			continue
		}
		if tool.Description == "" || tool.InputSchema == nil {
			t.Errorf("tool %s lacks description or schema", name)
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %s should be annotated read-only", name)
		}
	}
}

func TestSearchStations(t *testing.T) {
	cs := newSession(t, &fakeBoards{})

	res := call(t, cs, "search_stations", map[string]any{"query": "kings cross"})
	if res.IsError || !strings.Contains(text(t, res), "KGX: London King's Cross") {
		t.Fatalf("result = %q (error=%v)", text(t, res), res.IsError)
	}

	res = call(t, cs, "search_stations", map[string]any{"query": "atlantis"})
	if res.IsError || !strings.Contains(text(t, res), "No stations match") {
		t.Errorf("no-match result = %q (error=%v)", text(t, res), res.IsError)
	}

	res = call(t, cs, "search_stations", map[string]any{"query": "london", "limit": 2})
	if n := strings.Count(text(t, res), "\n") + 1; n != 2 {
		t.Errorf("limit 2 returned %d lines: %q", n, text(t, res))
	}

	if res = call(t, cs, "search_stations", map[string]any{"query": "  "}); !res.IsError {
		t.Error("blank query should be a tool error")
	}
}

func TestGetDepartures(t *testing.T) {
	fake := &fakeBoards{board: sampleBoard(2)}
	cs := newSession(t, fake)

	res := call(t, cs, "get_departures", map[string]any{
		"station": "kings cross", "destination": "edinburgh waverley", "limit": 5,
		"time_offset_minutes": 15, "time_window_minutes": 90,
	})
	if res.IsError {
		t.Fatalf("tool error: %s", text(t, res))
	}

	want := rdm.BoardRequest{CRS: "KGX", FilterCRS: "EDB", Rows: 6, TimeOffset: 15, TimeWindow: 90}
	if fake.lastReq != want {
		t.Errorf("request = %+v, want %+v", fake.lastReq, want)
	}

	got := text(t, res)
	for _, frag := range []string{"Departures from London Kings Cross (KGX)", "08:33 to Edinburgh, platform 4, LNER - on time", "[id svc-a]"} {
		if !strings.Contains(got, frag) {
			t.Errorf("text missing %q:\n%s", frag, got)
		}
	}
	if res.StructuredContent == nil {
		t.Error("no structured content")
	}
}

func TestGetArrivalsUsesOrigin(t *testing.T) {
	fake := &fakeBoards{board: sampleBoard(1)}
	cs := newSession(t, fake)

	res := call(t, cs, "get_arrivals", map[string]any{"station": "YRK", "origin": "EDB"})
	if res.IsError {
		t.Fatalf("tool error: %s", text(t, res))
	}
	if fake.lastReq.CRS != "YRK" || fake.lastReq.FilterCRS != "EDB" || fake.lastReq.Rows != 11 {
		t.Errorf("request = %+v", fake.lastReq)
	}
	if !strings.HasPrefix(text(t, res), "Arrivals at ") {
		t.Errorf("text = %q", text(t, res))
	}
}

func TestBoardLimitsAndTruncation(t *testing.T) {
	tests := []struct {
		name      string
		limit     any
		available int
		wantRows  int
		wantShown int
		wantTrunc bool
	}{
		{"default limit", nil, 20, 11, 10, true},
		{"explicit", 3, 4, 4, 3, true},
		{"exactly full", 3, 3, 4, 3, false},
		{"cap", 1000, 30, 26, 25, true},
		{"negative uses default", -4, 2, 11, 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeBoards{board: sampleBoard(tt.available)}
			args := map[string]any{"station": "KGX"}
			if tt.limit != nil {
				args["limit"] = tt.limit
			}
			res := call(t, newSession(t, fake), "get_departures", args)
			if res.IsError {
				t.Fatalf("tool error: %s", text(t, res))
			}
			if fake.lastReq.Rows != tt.wantRows {
				t.Errorf("rows requested = %d, want %d", fake.lastReq.Rows, tt.wantRows)
			}
			if n := strings.Count(text(t, res), "[id "); n != tt.wantShown {
				t.Errorf("services shown = %d, want %d", n, tt.wantShown)
			}
			if got := strings.Contains(text(t, res), "More services are available"); got != tt.wantTrunc {
				t.Errorf("truncation notice = %v, want %v", got, tt.wantTrunc)
			}
		})
	}
}

func TestEmptyBoard(t *testing.T) {
	fake := &fakeBoards{board: &rdm.Board{}}
	res := call(t, newSession(t, fake), "get_departures", map[string]any{"station": "ZZZ"})
	if res.IsError {
		t.Fatalf("tool error: %s", text(t, res))
	}
	if got := text(t, res); !strings.Contains(got, "ZZZ") || !strings.Contains(got, "0 services") {
		t.Errorf("text = %q", got)
	}
}

func TestCancelledAndDelayedText(t *testing.T) {
	fake := &fakeBoards{board: &rdm.Board{
		Station: rdm.Location{Name: "York", CRS: "YRK"},
		Services: []rdm.Service{
			{ID: "1", Scheduled: "09:00", Expected: "Cancelled", Cancelled: true, CancelReason: "a broken rail",
				Destinations: []rdm.Location{{Name: "Leeds"}}},
			{ID: "2", Scheduled: "09:10", Expected: "09:25", DelayReason: "congestion",
				Destinations: []rdm.Location{{Name: "Hull"}}},
		},
		Messages: []string{"Lifts out of service."},
	}}
	got := text(t, call(t, newSession(t, fake), "get_departures", map[string]any{"station": "YRK"}))

	for _, frag := range []string{"CANCELLED (a broken rail)", "expected 09:25 (congestion)", "Notice: Lifts out of service."} {
		if !strings.Contains(got, frag) {
			t.Errorf("text missing %q:\n%s", frag, got)
		}
	}
}

func TestGetServiceDetails(t *testing.T) {
	fake := &fakeBoards{detail: &rdm.ServiceDetail{
		ID: "abc", Operator: "LNER",
		Location: rdm.Location{Name: "York", CRS: "YRK"}, Platform: "9",
		Previous:   []rdm.CallingPoint{{Location: rdm.Location{Name: "Newcastle"}, Scheduled: "07:50", Expected: "07:51"}},
		Subsequent: []rdm.CallingPoint{{Location: rdm.Location{Name: "Doncaster"}, Scheduled: "09:15", Expected: "On time"}},
	}}
	res := call(t, newSession(t, fake), "get_service_details", map[string]any{"service_id": "abc"})
	if res.IsError {
		t.Fatalf("tool error: %s", text(t, res))
	}
	if fake.lastID != "abc" {
		t.Errorf("service id = %q", fake.lastID)
	}
	got := text(t, res)
	for _, frag := range []string{"Service abc (LNER)", "Previous stops:", "07:50 Newcastle", "Now at: York (YRK), platform 9", "09:15 Doncaster - on time"} {
		if !strings.Contains(got, frag) {
			t.Errorf("text missing %q:\n%s", frag, got)
		}
	}
}

func TestToolErrors(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		args    map[string]any
		upstrm  error
		wantMsg string
	}{
		{"unknown station", "get_departures", map[string]any{"station": "atlantis"}, nil, "search_stations"},
		{"ambiguous station", "get_departures", map[string]any{"station": "london"}, nil, "more specific"},
		{"unknown filter", "get_departures", map[string]any{"station": "KGX", "destination": "atlantis"}, nil, "search_stations"},
		{"no key", "get_departures", map[string]any{"station": "KGX"}, rdm.ErrNoCredentials, "RAILDATA_API_KEY"},
		{"bad key", "get_arrivals", map[string]any{"station": "KGX"}, rdm.ErrUnauthorized, "rejected"},
		{"not found", "get_departures", map[string]any{"station": "ZZZ"}, rdm.ErrNotFound, "no such station"},
		{"rate limited", "get_departures", map[string]any{"station": "KGX"}, rdm.ErrRateLimited, "rate limit"},
		{"down", "get_departures", map[string]any{"station": "KGX"}, rdm.ErrUnavailable, "unavailable"},
		{"service no key", "get_service_details", map[string]any{"service_id": "x"}, rdm.ErrNoCredentials, "RAILDATA_API_KEY"},
		{"unexpected", "get_departures", map[string]any{"station": "KGX"}, errors.New("db password=hunter2"), "unexpected error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeBoards{board: sampleBoard(1), detail: &rdm.ServiceDetail{}, err: tt.upstrm}
			res := call(t, newSession(t, fake), tt.tool, tt.args)
			if !res.IsError {
				t.Fatalf("expected tool error, got %q", text(t, res))
			}
			got := text(t, res)
			if !strings.Contains(got, tt.wantMsg) {
				t.Errorf("message %q lacks %q", got, tt.wantMsg)
			}
			if strings.Contains(got, "hunter2") {
				t.Errorf("message leaks internal detail: %q", got)
			}
		})
	}
}

func TestStationCodePassesThroughWithoutNameLookup(t *testing.T) {
	fake := &fakeBoards{board: &rdm.Board{}}
	res := call(t, newSession(t, fake), "get_departures", map[string]any{"station": "qqq"})
	if res.IsError {
		t.Fatalf("tool error: %s", text(t, res))
	}
	if fake.lastReq.CRS != "QQQ" {
		t.Errorf("CRS = %q, want QQQ", fake.lastReq.CRS)
	}
}
