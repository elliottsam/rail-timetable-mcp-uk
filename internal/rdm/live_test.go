//go:build live

package rdm_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/elliottsam/rail-timetable-mcp-uk/internal/rdm"
)

// These tests call the real Rail Data Marketplace. Run them with:
//
//	RAILDATA_API_KEY=... go test -tags live -count=1 -v ./internal/rdm/...
//
// They are how the default endpoint paths, API version and auth header in
// config.go get confirmed. If one fails with ErrNotFound or ErrUnauthorized,
// compare the Specification tab of the subscribed product with the defaults
// and override them through the RAILDATA_* variables.
func liveClient(t *testing.T) *rdm.Client {
	t.Helper()
	key := os.Getenv("RAILDATA_API_KEY")
	if key == "" {
		t.Skip("RAILDATA_API_KEY not set")
	}
	c, err := rdm.New(rdm.Config{
		BaseURL:            os.Getenv("RAILDATA_BASE_URL"),
		AuthHeader:         os.Getenv("RAILDATA_AUTH_HEADER"),
		DeparturesPath:     os.Getenv("RAILDATA_DEPARTURES_PATH"),
		ArrivalsPath:       os.Getenv("RAILDATA_ARRIVALS_PATH"),
		ServiceDetailsPath: os.Getenv("RAILDATA_SERVICE_DETAILS_PATH"),
		APIKey:             rdm.Secret(key),
		ServiceAPIKey:      rdm.Secret(os.Getenv("RAILDATA_SERVICE_API_KEY")),
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLiveDepartures(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	board, err := c.Departures(ctx, rdm.BoardRequest{CRS: "KGX", Rows: 5})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s (%s): %d services", board.Station.Name, board.Station.CRS, len(board.Services))
	for _, s := range board.Services {
		t.Logf("  %s %v %s platform=%q id=%q", s.Scheduled, s.Destinations, s.Expected, s.Platform, s.ID)
	}
	if board.Station.CRS == "" {
		t.Error("station CRS empty: the response shape may differ from the wire types in wire.go")
	}
}

func TestLiveArrivals(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	board, err := c.Arrivals(ctx, rdm.BoardRequest{CRS: "YRK", Rows: 5})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d services", board.Station.Name, len(board.Services))
}

func TestLiveServiceDetails(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	board, err := c.Departures(ctx, rdm.BoardRequest{CRS: "KGX", Rows: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range board.Services {
		if s.ID == "" {
			continue
		}
		d, err := c.ServiceDetails(ctx, s.ID)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("service %s: %d previous, %d subsequent stops", d.ID, len(d.Previous), len(d.Subsequent))
		return
	}
	t.Skip("no service with an id on the board")
}
