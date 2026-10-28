// Package rdm is a client for the Rail Data Marketplace (raildata.org.uk)
// live departure board, arrival board and service detail products.
//
// The package does not log. Failures are reported as the package's sentinel
// errors, wrapped with context; use errors.Is to branch on them.
package rdm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	opDepartures     = "GetDepBoardWithDetails"
	opArrivals       = "GetArrBoardWithDetails"
	opServiceDetails = "GetServiceDetailsByRID"

	maxRows = 150
)

// Client queries the Rail Data Marketplace. It is safe for concurrent use.
type Client struct {
	cfg   Config
	cache *cache
}

// New validates cfg, applies defaults, and returns a Client.
func New(cfg Config) (*Client, error) {
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Client{cfg: cfg, cache: newCache(cfg.CacheTTL, cfg.Now)}, nil
}

// HasBoardCredentials reports whether departure and arrival boards can be queried.
func (c *Client) HasBoardCredentials() bool { return c.cfg.APIKey != "" }

// HasServiceCredentials reports whether service details can be queried.
func (c *Client) HasServiceCredentials() bool { return c.serviceKey() != "" }

func (c *Client) serviceKey() Secret {
	if c.cfg.ServiceAPIKey != "" {
		return c.cfg.ServiceAPIKey
	}
	return c.cfg.APIKey
}

// Departures returns the live departure board for a station.
func (c *Client) Departures(ctx context.Context, req BoardRequest) (*Board, error) {
	return c.board(ctx, c.cfg.DeparturesPath, opDepartures, "to", req)
}

// Arrivals returns the live arrival board for a station.
func (c *Client) Arrivals(ctx context.Context, req BoardRequest) (*Board, error) {
	return c.board(ctx, c.cfg.ArrivalsPath, opArrivals, "from", req)
}

// ServiceDetails returns the full route of one service, identified by the ID
// from a board.
func (c *Client) ServiceDetails(ctx context.Context, serviceID string) (*ServiceDetail, error) {
	if serviceID = strings.TrimSpace(serviceID); serviceID == "" || len(serviceID) > 128 {
		return nil, fmt.Errorf("%w: service ID must be 1-128 characters", ErrInvalidInput)
	}
	var w wireDetail
	path := c.cfg.ServiceDetailsPath + "/" + opServiceDetails + "/" + url.PathEscape(serviceID)
	if err := c.fetch(ctx, c.serviceKey(), path, nil, &w); err != nil {
		return nil, fmt.Errorf("service details: %w", err)
	}
	return w.toDetail(serviceID), nil
}

func (c *Client) board(ctx context.Context, prefix, op, filterType string, req BoardRequest) (*Board, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	q := url.Values{}
	if req.Rows > 0 {
		q.Set("numRows", strconv.Itoa(req.Rows))
	}
	if req.FilterCRS != "" {
		q.Set("filterCrs", strings.ToUpper(req.FilterCRS))
		q.Set("filterType", filterType)
	}
	if req.TimeOffset != 0 {
		q.Set("timeOffset", strconv.Itoa(req.TimeOffset))
	}
	if req.TimeWindow != 0 {
		q.Set("timeWindow", strconv.Itoa(req.TimeWindow))
	}

	var w wireBoard
	path := prefix + "/" + op + "/" + strings.ToUpper(req.CRS)
	if err := c.fetch(ctx, c.cfg.APIKey, path, q, &w); err != nil {
		return nil, fmt.Errorf("%s %s: %w", op, strings.ToUpper(req.CRS), err)
	}
	return w.toBoard(), nil
}

func (r BoardRequest) validate() error {
	if !isCRS(r.CRS) {
		return fmt.Errorf("%w: station code %q is not three letters", ErrInvalidInput, r.CRS)
	}
	if r.FilterCRS != "" && !isCRS(r.FilterCRS) {
		return fmt.Errorf("%w: filter code %q is not three letters", ErrInvalidInput, r.FilterCRS)
	}
	if r.Rows < 0 || r.Rows > maxRows {
		return fmt.Errorf("%w: rows must be 0-%d", ErrInvalidInput, maxRows)
	}
	return nil
}

func isCRS(s string) bool {
	if len(s) != 3 {
		return false
	}
	for i := range len(s) {
		if c := s[i] | 0x20; c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

// fetch GETs path with query q using key, and decodes the JSON reply into out.
func (c *Client) fetch(ctx context.Context, key Secret, path string, q url.Values, out any) error {
	if key == "" {
		return ErrNoCredentials
	}
	target := c.cfg.BaseURL + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}

	body, ok := c.cache.get(target)
	if !ok {
		var err error
		if body, err = c.send(ctx, key, target); err != nil {
			return err
		}
		c.cache.put(target, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: %w", ErrBadResponse, err)
	}
	return nil
}

// send performs the request, retrying transient failures.
func (c *Client) send(ctx context.Context, key Secret, target string) ([]byte, error) {
	retries := max(c.cfg.MaxRetries, 0)
	for attempt := 0; ; attempt++ {
		body, retryAfter, err := c.attempt(ctx, key, target)
		if err == nil {
			return body, nil
		}
		if !errors.Is(err, ErrRateLimited) && !errors.Is(err, ErrUnavailable) || attempt >= retries {
			return nil, err
		}
		delay, ok := c.backoff(attempt, retryAfter)
		if !ok {
			return nil, err
		}
		if werr := sleep(ctx, delay); werr != nil {
			return nil, werr
		}
	}
}

// attempt makes one request. It returns the server's Retry-After hint, if any.
func (c *Client) attempt(ctx context.Context, key Secret, target string) ([]byte, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: build request: %w", ErrInvalidInput, err)
	}
	req.Header.Set(c.cfg.AuthHeader, string(key))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.cfg.UserAgent)

	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil, retryAfter(resp), statusError(resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	switch {
	case err != nil && ctx.Err() != nil:
		return nil, 0, ctx.Err()
	case err != nil:
		return nil, 0, fmt.Errorf("%w: read body: %w", ErrUnavailable, err)
	case len(body) > maxBodyBytes:
		return nil, 0, fmt.Errorf("%w: response exceeds %d bytes", ErrBadResponse, maxBodyBytes)
	}
	return body, 0, nil
}

// statusError maps a non-2xx status to a sentinel. It never includes the
// response body, which could echo credentials.
func statusError(code int) error {
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return fmt.Errorf("%w (status %d)", ErrUnauthorized, code)
	case code == http.StatusNotFound:
		return fmt.Errorf("%w (status %d)", ErrNotFound, code)
	case code == http.StatusBadRequest:
		return fmt.Errorf("%w: upstream rejected the request (status %d)", ErrInvalidInput, code)
	case code == http.StatusTooManyRequests:
		return fmt.Errorf("%w (status %d)", ErrRateLimited, code)
	case code >= 500:
		return fmt.Errorf("%w (status %d)", ErrUnavailable, code)
	default:
		return fmt.Errorf("%w: status %d", ErrBadResponse, code)
	}
}

func retryAfter(resp *http.Response) time.Duration {
	secs, err := strconv.Atoi(resp.Header.Get("Retry-After"))
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// backoff returns how long to wait before the next attempt. It reports false
// when the server asked for a wait longer than we are willing to make.
func (c *Client) backoff(attempt int, hint time.Duration) (time.Duration, bool) {
	if hint > maxBackoff {
		return 0, false
	}
	if hint > 0 {
		return hint, true
	}
	d := c.cfg.RetryBackoff << min(attempt, 10)
	d = min(d, maxBackoff)
	return d/2 + rand.N(d/2+1), true //nolint:gosec // retry jitter, not security-sensitive
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
