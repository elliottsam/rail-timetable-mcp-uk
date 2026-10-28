package rdm_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elliottsam/rail-timetable-mcp-uk/internal/rdm"
)

const testKey = "test-key-123"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// newClient returns a Client pointed at srv with fast retries and a key.
func newClient(t *testing.T, srv *httptest.Server, mutate ...func(*rdm.Config)) *rdm.Client {
	t.Helper()
	cfg := rdm.Config{
		BaseURL:      srv.URL,
		APIKey:       testKey,
		RetryBackoff: time.Millisecond,
		CacheTTL:     -1,
	}
	for _, m := range mutate {
		m(&cfg)
	}
	c, err := rdm.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestDepartures(t *testing.T) {
	var gotPath, gotQuery, gotKey, gotAccept string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		gotKey, gotAccept = r.Header.Get("x-apikey"), r.Header.Get("Accept")
		_, _ = w.Write(fixture(t, "departures.json"))
	})

	board, err := newClient(t, srv).Departures(context.Background(), rdm.BoardRequest{
		CRS: "kgx", FilterCRS: "edb", Rows: 3, TimeOffset: 10, TimeWindow: 60,
	})
	if err != nil {
		t.Fatal(err)
	}

	if want := rdm.DefaultDeparturesPath + "/GetDepBoardWithDetails/KGX"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	for _, want := range []string{"numRows=3", "filterCrs=EDB", "filterType=to", "timeOffset=10", "timeWindow=60"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
	if gotKey != testKey || gotAccept != "application/json" {
		t.Errorf("headers: key=%q accept=%q", gotKey, gotAccept)
	}

	if board.Station.CRS != "KGX" || len(board.Services) != 3 {
		t.Fatalf("board = %+v", board)
	}
	first := board.Services[0]
	if first.Scheduled != "08:33" || first.Expected != "On time" || first.Platform != "4" ||
		first.Operator != "LNER" || first.Coaches != 9 || first.ID != "SVC-LNER-0833" {
		t.Errorf("first service = %+v", first)
	}
	if len(first.CallingPoints) != 2 || first.CallingPoints[1].CRS != "YRK" {
		t.Errorf("calling points = %+v", first.CallingPoints)
	}
	if d := board.Services[1]; d.Expected != "08:52" || d.DelayReason == "" || d.Platform != "" {
		t.Errorf("delayed service = %+v", d)
	}
	if c := board.Services[2]; !c.Cancelled || c.CancelReason == "" {
		t.Errorf("cancelled service = %+v", c)
	}
	if len(board.Messages) != 1 || strings.ContainsAny(board.Messages[0], "<>") {
		t.Errorf("messages not sanitised: %q", board.Messages)
	}
}

func TestArrivals(t *testing.T) {
	var gotPath, gotQuery string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write(fixture(t, "arrivals.json"))
	})

	board, err := newClient(t, srv).Arrivals(context.Background(), rdm.BoardRequest{CRS: "YRK", FilterCRS: "EDB"})
	if err != nil {
		t.Fatal(err)
	}
	if want := rdm.DefaultArrivalsPath + "/GetArrBoardWithDetails/YRK"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if !strings.Contains(gotQuery, "filterType=from") {
		t.Errorf("query = %q, want filterType=from", gotQuery)
	}
	s := board.Services[0]
	if s.Scheduled != "08:41" || s.Expected != "08:44" {
		t.Errorf("times = %q / %q", s.Scheduled, s.Expected)
	}
	if len(s.CallingPoints) != 1 || s.CallingPoints[0].Expected != "07:51" {
		t.Errorf("previous calling points = %+v", s.CallingPoints)
	}
}

func TestServiceDetails(t *testing.T) {
	var gotPath, gotKey string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey = r.URL.EscapedPath(), r.Header.Get("x-apikey")
		_, _ = w.Write(fixture(t, "service.json"))
	})
	c := newClient(t, srv, func(c *rdm.Config) { c.ServiceAPIKey = "service-key" })

	d, err := c.ServiceDetails(context.Background(), "a/b c")
	if err != nil {
		t.Fatal(err)
	}
	if want := rdm.DefaultServiceDetailsPath + "/GetServiceDetailsByRID/a%2Fb%20c"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotKey != "service-key" {
		t.Errorf("key = %q, want the service key", gotKey)
	}
	if d.ID != "202610288000001" || len(d.Previous) != 2 || len(d.Subsequent) != 2 {
		t.Errorf("detail = %+v", d)
	}
	if last := d.Subsequent[1]; !last.Cancelled {
		t.Errorf("last stop = %+v, want cancelled", last)
	}
}

func TestServiceDetailsFallsBackToBoardKey(t *testing.T) {
	var gotKey string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-apikey")
		_, _ = w.Write(fixture(t, "service.json"))
	})
	if _, err := newClient(t, srv).ServiceDetails(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if gotKey != testKey {
		t.Errorf("key = %q, want board key", gotKey)
	}
}

func TestInputValidation(t *testing.T) {
	var calls atomic.Int32
	srv := serve(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	c := newClient(t, srv)
	ctx := context.Background()

	tests := map[string]error{
		"short crs":      callDep(ctx, c, rdm.BoardRequest{CRS: "KG"}),
		"digit crs":      callDep(ctx, c, rdm.BoardRequest{CRS: "K1X"}),
		"path injection": callDep(ctx, c, rdm.BoardRequest{CRS: "../"}),
		"bad filter":     callDep(ctx, c, rdm.BoardRequest{CRS: "KGX", FilterCRS: "EDBX"}),
		"too many rows":  callDep(ctx, c, rdm.BoardRequest{CRS: "KGX", Rows: 1000}),
		"negative rows":  callDep(ctx, c, rdm.BoardRequest{CRS: "KGX", Rows: -1}),
	}
	_, err := c.ServiceDetails(ctx, "  ")
	tests["blank service id"] = err
	_, err = c.ServiceDetails(ctx, strings.Repeat("x", 129))
	tests["long service id"] = err

	for name, err := range tests {
		if !errors.Is(err, rdm.ErrInvalidInput) {
			t.Errorf("%s: error = %v, want ErrInvalidInput", name, err)
		}
	}
	if calls.Load() != 0 {
		t.Errorf("server received %d calls for invalid input", calls.Load())
	}
}

func callDep(ctx context.Context, c *rdm.Client, r rdm.BoardRequest) error {
	_, err := c.Departures(ctx, r)
	return err
}

func TestNoCredentials(t *testing.T) {
	var calls atomic.Int32
	srv := serve(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	c, err := rdm.New(rdm.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if c.HasBoardCredentials() || c.HasServiceCredentials() {
		t.Error("client without keys reports credentials")
	}
	if err := callDep(context.Background(), c, rdm.BoardRequest{CRS: "KGX"}); !errors.Is(err, rdm.ErrNoCredentials) {
		t.Errorf("error = %v, want ErrNoCredentials", err)
	}
	if _, err := c.ServiceDetails(context.Background(), "x"); !errors.Is(err, rdm.ErrNoCredentials) {
		t.Errorf("service error = %v, want ErrNoCredentials", err)
	}
	if calls.Load() != 0 {
		t.Error("request sent without credentials")
	}
}

func TestStatusMapping(t *testing.T) {
	tests := []struct {
		status   int
		want     error
		attempts int32
	}{
		{http.StatusUnauthorized, rdm.ErrUnauthorized, 1},
		{http.StatusForbidden, rdm.ErrUnauthorized, 1},
		{http.StatusNotFound, rdm.ErrNotFound, 1},
		{http.StatusBadRequest, rdm.ErrInvalidInput, 1},
		{http.StatusTeapot, rdm.ErrBadResponse, 1},
		{http.StatusTooManyRequests, rdm.ErrRateLimited, 3},
		{http.StatusBadGateway, rdm.ErrUnavailable, 3},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.status), func(t *testing.T) {
			var calls atomic.Int32
			srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				http.Error(w, "secret body "+testKey, tt.status)
			})

			err := callDep(context.Background(), newClient(t, srv), rdm.BoardRequest{CRS: "KGX"})
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if strings.Contains(err.Error(), testKey) || strings.Contains(err.Error(), "secret body") {
				t.Errorf("error leaks response body or key: %v", err)
			}
			if got := calls.Load(); got != tt.attempts {
				t.Errorf("attempts = %d, want %d", got, tt.attempts)
			}
		})
	}
}

func TestRetryThenSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(fixture(t, "departures.json"))
	})

	board, err := newClient(t, srv).Departures(context.Background(), rdm.BoardRequest{CRS: "KGX"})
	if err != nil || len(board.Services) != 3 {
		t.Fatalf("board=%v err=%v", board, err)
	}
	if calls.Load() != 3 {
		t.Errorf("attempts = %d, want 3", calls.Load())
	}
}

func TestRetriesDisabled(t *testing.T) {
	var calls atomic.Int32
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	c := newClient(t, srv, func(c *rdm.Config) { c.MaxRetries = -1 })

	if err := callDep(context.Background(), c, rdm.BoardRequest{CRS: "KGX"}); !errors.Is(err, rdm.ErrUnavailable) {
		t.Fatalf("error = %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("attempts = %d, want 1", calls.Load())
	}
}

func TestRetryAfterTooLongGivesUp(t *testing.T) {
	var calls atomic.Int32
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	err := callDep(context.Background(), newClient(t, srv), rdm.BoardRequest{CRS: "KGX"})
	if !errors.Is(err, rdm.ErrRateLimited) {
		t.Fatalf("error = %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("attempts = %d, want 1", calls.Load())
	}
}

func TestContextCancelledDuringBackoff(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	c := newClient(t, srv, func(c *rdm.Config) { c.RetryBackoff = time.Hour })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := callDep(ctx, c, rdm.BoardRequest{CRS: "KGX"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("cancellation did not interrupt backoff")
	}
}

func TestSlowServerTimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := serve(t, func(http.ResponseWriter, *http.Request) { <-release })
	t.Cleanup(func() { close(release) })
	c := newClient(t, srv, func(c *rdm.Config) {
		c.Timeout = 20 * time.Millisecond
		c.MaxRetries = -1
	})

	if err := callDep(context.Background(), c, rdm.BoardRequest{CRS: "KGX"}); !errors.Is(err, rdm.ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestBadBodies(t *testing.T) {
	tests := map[string]string{
		"not json":      "<html>gateway</html>",
		"wrong shape":   `{"trainServices": "nope"}`,
		"wrong element": `{"trainServices": [{"length": "many"}]}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			srv := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
			err := callDep(context.Background(), newClient(t, srv), rdm.BoardRequest{CRS: "KGX"})
			if !errors.Is(err, rdm.ErrBadResponse) {
				t.Fatalf("error = %v, want ErrBadResponse", err)
			}
		})
	}
}

func TestOversizedBodyRejected(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"x":"` + strings.Repeat("a", 5<<20) + `"}`))
	})
	err := callDep(context.Background(), newClient(t, srv), rdm.BoardRequest{CRS: "KGX"})
	if !errors.Is(err, rdm.ErrBadResponse) {
		t.Fatalf("error = %v, want ErrBadResponse", err)
	}
}

func TestEmptyBoardIsNotNil(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"crs":"KGX"}`)) })
	board, err := newClient(t, srv).Departures(context.Background(), rdm.BoardRequest{CRS: "KGX"})
	if err != nil || board.Services == nil || len(board.Services) != 0 {
		t.Fatalf("board=%+v err=%v", board, err)
	}
}

func TestCache(t *testing.T) {
	var calls atomic.Int32
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write(fixture(t, "departures.json"))
	})
	now := time.Unix(1_000_000, 0)
	c := newClient(t, srv, func(c *rdm.Config) {
		c.CacheTTL = 30 * time.Second
		c.Now = func() time.Time { return now }
	})
	ctx := context.Background()

	for range 3 {
		if err := callDep(ctx, c, rdm.BoardRequest{CRS: "KGX"}); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1 (cached)", calls.Load())
	}

	if err := callDep(ctx, c, rdm.BoardRequest{CRS: "PAD"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("different station should miss cache; calls = %d", calls.Load())
	}

	now = now.Add(31 * time.Second)
	if err := callDep(ctx, c, rdm.BoardRequest{CRS: "KGX"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Errorf("expired entry should refetch; calls = %d", calls.Load())
	}
}

func TestErrorsAreNotCached(t *testing.T) {
	var calls atomic.Int32
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(fixture(t, "departures.json"))
	})
	c := newClient(t, srv, func(c *rdm.Config) { c.CacheTTL = time.Minute })

	if err := callDep(context.Background(), c, rdm.BoardRequest{CRS: "KGX"}); !errors.Is(err, rdm.ErrNotFound) {
		t.Fatalf("first error = %v", err)
	}
	if err := callDep(context.Background(), c, rdm.BoardRequest{CRS: "KGX"}); err != nil {
		t.Fatalf("second call should reach the server: %v", err)
	}
}

func TestConcurrentUse(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(fixture(t, "departures.json")) })
	c := newClient(t, srv, func(c *rdm.Config) { c.CacheTTL = time.Minute })

	errs := make(chan error, 20)
	for i := range 20 {
		go func() {
			crs := []string{"KGX", "PAD", "EUS", "VIC"}[i%4]
			errs <- callDep(context.Background(), c, rdm.BoardRequest{CRS: crs})
		}()
	}
	for range 20 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}

func TestNewValidation(t *testing.T) {
	tests := map[string]rdm.Config{
		"relative base url": {BaseURL: "/just/a/path"},
		"wrong scheme":      {BaseURL: "ftp://example.invalid"},
		"path without /":    {DeparturesPath: "no-slash"},
		"blank header":      {AuthHeader: " "},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := rdm.New(cfg); !errors.Is(err, rdm.ErrInvalidConfig) {
				t.Fatalf("error = %v, want ErrInvalidConfig", err)
			}
		})
	}
	if _, err := rdm.New(rdm.Config{}); err != nil {
		t.Errorf("zero config should be valid: %v", err)
	}
}

func TestSecretIsRedacted(t *testing.T) {
	s := rdm.Secret("hunter2")
	for _, got := range []string{fmt.Sprint(s), fmt.Sprintf("%v %+v %#v %s", s, s, s, s)} {
		if strings.Contains(got, "hunter2") {
			t.Errorf("secret leaked: %q", got)
		}
	}
	cfgStr := fmt.Sprintf("%+v", rdm.Config{APIKey: "hunter2", ServiceAPIKey: "hunter3"})
	if strings.Contains(cfgStr, "hunter") {
		t.Errorf("config printing leaks key: %s", cfgStr)
	}
}

func FuzzDecodeBoard(f *testing.F) {
	f.Add(string(mustRead("testdata/departures.json")))
	f.Add(`{}`)
	f.Add(`{"trainServices":[null]}`)
	f.Add(`{"nrccMessages":["a",{"value":"<b>x"},null,5]}`)

	f.Fuzz(func(t *testing.T, body string) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		defer srv.Close()
		c, err := rdm.New(rdm.Config{BaseURL: srv.URL, APIKey: "k", CacheTTL: -1, MaxRetries: -1})
		if err != nil {
			t.Fatal(err)
		}
		// Must never panic; errors are fine.
		_, _ = c.Departures(context.Background(), rdm.BoardRequest{CRS: "KGX"})
	})
}

func mustRead(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return b
}
