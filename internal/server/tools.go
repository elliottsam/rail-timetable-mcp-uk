package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/elliottsam/rail-timetable-mcp-uk/internal/rdm"
	"github.com/elliottsam/rail-timetable-mcp-uk/internal/stations"
)

const (
	defaultLimit = 10
	maxLimit     = 25

	defaultSearchLimit = 5
	maxSearchLimit     = 20
)

// SearchStationsInput is the input of search_stations.
type SearchStationsInput struct {
	Query string `json:"query" jsonschema:"Station name or part of one, e.g. 'kings cross' or 'edinburgh'"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum matches to return (default 5, max 20)"`
}

// SearchStationsOutput is the output of search_stations.
type SearchStationsOutput struct {
	Stations []stations.Station `json:"stations"`
}

// DeparturesInput is the input of get_departures.
type DeparturesInput struct {
	Station           string `json:"station" jsonschema:"Station to show departures from: a three-letter CRS code (e.g. KGX) or a station name"`
	Destination       string `json:"destination,omitempty" jsonschema:"Only show trains calling at this station (CRS code or name)"`
	Limit             int    `json:"limit,omitempty" jsonschema:"Maximum services to return (default 10, max 25)"`
	TimeOffsetMinutes int    `json:"time_offset_minutes,omitempty" jsonschema:"Start the window this many minutes from now (negative for the past)"`
	TimeWindowMinutes int    `json:"time_window_minutes,omitempty" jsonschema:"Length of the time window in minutes"`
}

// ArrivalsInput is the input of get_arrivals.
type ArrivalsInput struct {
	Station           string `json:"station" jsonschema:"Station to show arrivals at: a three-letter CRS code (e.g. YRK) or a station name"`
	Origin            string `json:"origin,omitempty" jsonschema:"Only show trains that called at this station earlier (CRS code or name)"`
	Limit             int    `json:"limit,omitempty" jsonschema:"Maximum services to return (default 10, max 25)"`
	TimeOffsetMinutes int    `json:"time_offset_minutes,omitempty" jsonschema:"Start the window this many minutes from now (negative for the past)"`
	TimeWindowMinutes int    `json:"time_window_minutes,omitempty" jsonschema:"Length of the time window in minutes"`
}

// BoardOutput is the output of get_departures and get_arrivals.
type BoardOutput struct {
	Station     rdm.Location  `json:"station"`
	GeneratedAt string        `json:"generatedAt,omitempty"`
	Services    []rdm.Service `json:"services"`
	Messages    []string      `json:"messages,omitempty"`
	// Truncated is set when more services were available than Limit.
	Truncated bool `json:"truncated,omitempty"`
}

// ServiceDetailsInput is the input of get_service_details.
type ServiceDetailsInput struct {
	ServiceID string `json:"service_id" jsonschema:"Service id from a departure or arrival board"`
}

func (h *handlers) register(srv *mcp.Server) {
	openWorld := true
	readOnly := func(title string) *mcp.ToolAnnotations {
		return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &openWorld}
	}

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_stations",
		Description: "Find UK rail stations by name and get their three-letter CRS codes. Works offline. Use before get_departures when you only know a name.",
		Annotations: readOnly("Search stations"),
	}, guard(h.logger, "search_stations", h.searchStations))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_departures",
		Description: "Live departure board for a UK station: next trains with scheduled and expected times, platform, operator, delays, cancellations and calling points. Use for 'next trains from X' or 'trains from X to Y'. Requires the server's Rail Data Marketplace API key.",
		Annotations: readOnly("Live departures"),
	}, guard(h.logger, "get_departures", h.getDepartures))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_arrivals",
		Description: "Live arrival board for a UK station: trains due to arrive with scheduled and expected times, platform, operator and delays. Use for 'what is arriving at X' or 'is my train into X on time'. Requires the server's Rail Data Marketplace API key.",
		Annotations: readOnly("Live arrivals"),
	}, guard(h.logger, "get_arrivals", h.getArrivals))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_service_details",
		Description: "Full route and live running of one train, using a service id from get_departures or get_arrivals. Shows every previous and subsequent stop with times and cancellations. Requires the server's Rail Data Marketplace API key.",
		Annotations: readOnly("Service details"),
	}, guard(h.logger, "get_service_details", h.getServiceDetails))
}

func (h *handlers) searchStations(_ context.Context, _ *mcp.CallToolRequest, in SearchStationsInput) (*mcp.CallToolResult, SearchStationsOutput, error) {
	if strings.TrimSpace(in.Query) == "" {
		return nil, SearchStationsOutput{}, errors.New("query must not be empty")
	}
	limit := clamp(in.Limit, defaultSearchLimit, maxSearchLimit)

	found := h.stations.Search(in.Query, limit)
	if found == nil {
		found = []stations.Station{}
	}

	var b strings.Builder
	if len(found) == 0 {
		fmt.Fprintf(&b, "No stations match %q.", in.Query)
	}
	for _, s := range found {
		fmt.Fprintf(&b, "%s: %s\n", s.CRS, s.Name)
	}
	return textResult(b.String()), SearchStationsOutput{Stations: found}, nil
}

func (h *handlers) getDepartures(ctx context.Context, _ *mcp.CallToolRequest, in DeparturesInput) (*mcp.CallToolResult, BoardOutput, error) {
	return h.board(ctx, "Departures from", "to", in.Station, in.Destination, in.Limit, in.TimeOffsetMinutes, in.TimeWindowMinutes, h.boards.Departures)
}

func (h *handlers) getArrivals(ctx context.Context, _ *mcp.CallToolRequest, in ArrivalsInput) (*mcp.CallToolResult, BoardOutput, error) {
	return h.board(ctx, "Arrivals at", "from", in.Station, in.Origin, in.Limit, in.TimeOffsetMinutes, in.TimeWindowMinutes, h.boards.Arrivals)
}

type boardFunc func(context.Context, rdm.BoardRequest) (*rdm.Board, error)

func (h *handlers) board(ctx context.Context, heading, direction, station, filter string, limit, offset, window int, fetch boardFunc) (*mcp.CallToolResult, BoardOutput, error) {
	st, err := h.stations.Resolve(station)
	if err != nil {
		return nil, BoardOutput{}, h.userError(err)
	}
	req := rdm.BoardRequest{CRS: st.CRS, TimeOffset: offset, TimeWindow: window}
	if strings.TrimSpace(filter) != "" {
		f, err := h.stations.Resolve(filter)
		if err != nil {
			return nil, BoardOutput{}, h.userError(err)
		}
		req.FilterCRS = f.CRS
	}

	limit = clamp(limit, defaultLimit, maxLimit)
	req.Rows = limit + 1 // one extra row tells us whether the list was cut

	board, err := fetch(ctx, req)
	if err != nil {
		return nil, BoardOutput{}, h.userError(err)
	}

	out := BoardOutput{
		Station:     board.Station,
		GeneratedAt: board.GeneratedAt,
		Services:    board.Services,
		Messages:    board.Messages,
	}
	if out.Station.Name == "" {
		out.Station.Name = st.Name
	}
	if out.Station.CRS == "" {
		out.Station.CRS = st.CRS
	}
	if len(out.Services) > limit {
		out.Services, out.Truncated = out.Services[:limit], true
	}
	if out.Services == nil {
		out.Services = []rdm.Service{}
	}
	return textResult(boardText(heading, direction, req.FilterCRS, out)), out, nil
}

func (h *handlers) getServiceDetails(ctx context.Context, _ *mcp.CallToolRequest, in ServiceDetailsInput) (*mcp.CallToolResult, rdm.ServiceDetail, error) {
	d, err := h.boards.ServiceDetails(ctx, in.ServiceID)
	if err != nil {
		return nil, rdm.ServiceDetail{}, h.userError(err)
	}
	return textResult(detailText(d)), *d, nil
}

// guard is the process boundary for tool handlers. The SDK runs each call on
// its own goroutine and does not recover panics, so an unguarded panic would
// take down the whole server. The panic is logged and reported as a tool error.
func guard[In, Out any](logger *slog.Logger, tool string, h mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (res *mcp.CallToolResult, out Out, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("panic in tool handler", "tool", tool, "panic", rec, "stack", string(debug.Stack()))
				var zero Out
				res, out, err = nil, zero, errors.New("internal error running the tool; see the server log")
			}
		}()
		return h(ctx, req, in)
	}
}

func clamp(v, def, upper int) int {
	switch {
	case v <= 0:
		return def
	case v > upper:
		return upper
	}
	return v
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.TrimRight(text, "\n")}}}
}

// userError turns a failure into a message the model can act on. Messages
// never carry credentials or upstream response bodies.
func (h *handlers) userError(err error) error {
	var amb *stations.AmbiguousError
	switch {
	case errors.As(err, &amb):
		return fmt.Errorf("%w; use a CRS code or a more specific name", err)
	case errors.Is(err, stations.ErrNotFound):
		return fmt.Errorf("%w; try search_stations, or pass a three-letter CRS code", err)
	case errors.Is(err, rdm.ErrNoCredentials):
		return errors.New("this server has no Rail Data Marketplace API key configured; the operator must set RAILDATA_API_KEY")
	case errors.Is(err, rdm.ErrUnauthorized):
		return errors.New("the Rail Data Marketplace rejected this server's API key; the operator should check RAILDATA_API_KEY and the product subscription")
	case errors.Is(err, rdm.ErrNotFound):
		return errors.New("the upstream service has no such station or service; check the station code or service id (service ids expire quickly)")
	case errors.Is(err, rdm.ErrInvalidInput),
		errors.Is(err, rdm.ErrRateLimited),
		errors.Is(err, rdm.ErrUnavailable),
		errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		h.logger.Error("unexpected tool failure", "error", err)
		return errors.New("unexpected error fetching train data; see the server log")
	}
}
