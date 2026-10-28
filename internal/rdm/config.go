package rdm

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Defaults applied by New.
//
// The endpoint paths and header name below are UNVERIFIED: they come from
// third-party projects and must be confirmed against the Specification tab of
// the subscribed product on raildata.org.uk once an API key is available.
// Override them through Config rather than editing code.
const (
	DefaultBaseURL            = "https://api1.raildata.org.uk"
	DefaultDeparturesPath     = "/1010-live-departure-board-dep/LDBWS/api/20220120"
	DefaultArrivalsPath       = "/1010-live-arrival-board-arr/LDBWS/api/20220120"
	DefaultServiceDetailsPath = "/1010-service-details/LDBSVWS/api/20220120"
	DefaultAuthHeader         = "x-apikey"
	DefaultTimeout            = 10 * time.Second
	DefaultMaxRetries         = 2
	DefaultRetryBackoff       = 250 * time.Millisecond
	DefaultCacheTTL           = 30 * time.Second

	maxBackoff   = 5 * time.Second
	maxBodyBytes = 4 << 20
	maxCacheSize = 256
)

// Secret is a credential that never prints its value.
type Secret string

// String implements fmt.Stringer.
func (Secret) String() string { return "[redacted]" }

// GoString implements fmt.GoStringer.
func (Secret) GoString() string { return "[redacted]" }

// LogValue implements slog.LogValuer.
func (Secret) LogValue() slog.Value { return slog.StringValue("[redacted]") }

// Config configures a Client. The zero value is valid: it yields a client
// with no credentials, whose methods return ErrNoCredentials.
type Config struct {
	// BaseURL is the API gateway origin. Default DefaultBaseURL.
	BaseURL string

	// DeparturesPath, ArrivalsPath and ServiceDetailsPath are the product
	// path prefixes appended to BaseURL. Each must start with "/". Defaults
	// are the Default*Path constants.
	DeparturesPath     string
	ArrivalsPath       string
	ServiceDetailsPath string

	// AuthHeader names the request header carrying the key.
	// Default DefaultAuthHeader.
	AuthHeader string

	// APIKey authenticates board requests. Empty disables them.
	APIKey Secret

	// ServiceAPIKey authenticates service-detail requests. When empty,
	// APIKey is used.
	ServiceAPIKey Secret

	// HTTPClient sends requests. Default is a client using Timeout.
	HTTPClient *http.Client

	// Timeout bounds each attempt when HTTPClient is nil.
	// Default DefaultTimeout.
	Timeout time.Duration

	// MaxRetries is how many times a failed idempotent request is retried.
	// Zero uses DefaultMaxRetries; a negative value disables retries.
	MaxRetries int

	// RetryBackoff is the base delay, doubled each retry, capped at 5s and
	// jittered. Default DefaultRetryBackoff.
	RetryBackoff time.Duration

	// CacheTTL is how long a successful response is reused. Zero uses
	// DefaultCacheTTL; a negative value disables caching.
	CacheTTL time.Duration

	// UserAgent is sent on every request. Default "rail-timetable-mcp-uk".
	UserAgent string

	// Now returns the current time, for cache expiry. Default time.Now.
	Now func() time.Time
}

// Validate reports whether the config is usable after defaults are applied.
func (c *Config) Validate() error {
	u, err := url.Parse(c.BaseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%w: base URL must be an absolute http(s) URL", ErrInvalidConfig)
	}
	for name, p := range map[string]string{
		"departures path":      c.DeparturesPath,
		"arrivals path":        c.ArrivalsPath,
		"service details path": c.ServiceDetailsPath,
	} {
		if !strings.HasPrefix(p, "/") {
			return fmt.Errorf("%w: %s must start with /", ErrInvalidConfig, name)
		}
	}
	if strings.TrimSpace(c.AuthHeader) == "" {
		return fmt.Errorf("%w: auth header is empty", ErrInvalidConfig)
	}
	return nil
}

func (c *Config) applyDefaults() {
	set := func(dst *string, def string) {
		if *dst == "" {
			*dst = def
		}
	}
	set(&c.BaseURL, DefaultBaseURL)
	set(&c.DeparturesPath, DefaultDeparturesPath)
	set(&c.ArrivalsPath, DefaultArrivalsPath)
	set(&c.ServiceDetailsPath, DefaultServiceDetailsPath)
	set(&c.AuthHeader, DefaultAuthHeader)
	set(&c.UserAgent, "rail-timetable-mcp-uk")

	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	if c.Timeout == 0 {
		c.Timeout = DefaultTimeout
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = DefaultMaxRetries
	}
	if c.RetryBackoff == 0 {
		c.RetryBackoff = DefaultRetryBackoff
	}
	if c.CacheTTL == 0 {
		c.CacheTTL = DefaultCacheTTL
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: c.Timeout}
	}
	if c.Now == nil {
		c.Now = time.Now
	}
}
