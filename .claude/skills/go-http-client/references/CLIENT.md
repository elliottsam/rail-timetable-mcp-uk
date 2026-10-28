# Client Construction and Requests

## Config

```go
type Config struct {
    BaseURL    string        // default set by New; override for tests/staging
    APIKey     Secret        // may be empty; methods fail with ErrNoCredentials
    AuthHeader string        // header carrying the key
    HTTPClient *http.Client  // default: &http.Client{Timeout: Timeout}
    Timeout    time.Duration // default 10s
    MaxRetries int           // default 2
    UserAgent  string
}

func New(cfg Config) (*Client, error) // validates URL, applies defaults
```

Normalize once in `New` and keep the result immutable.

## Secrets

```go
type Secret string

func (Secret) String() string   { return "[redacted]" }
func (Secret) GoString() string { return "[redacted]" }
func (s Secret) LogValue() slog.Value { return slog.StringValue("[redacted]") }
```

Use `string(cfg.APIKey)` only where the header is set.

## The `do` helper

One place builds the request, sets headers, sends, limits the body, and maps the
status:

```go
func (c *Client) get(ctx context.Context, path string, out any) error {
    if c.cfg.APIKey == "" {
        return ErrNoCredentials
    }
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.BaseURL+path, nil)
    if err != nil {
        return fmt.Errorf("build request: %w", err)
    }
    req.Header.Set(c.cfg.AuthHeader, string(c.cfg.APIKey))
    req.Header.Set("Accept", "application/json")
    // send (with retry), then:
    // defer resp.Body.Close(); decode from io.LimitReader(resp.Body, maxBody)
}
```

Escape path segments built from user input with `url.PathEscape`; validate
codes (for example a three-letter station code) before they reach the URL.

## Error mapping

```go
var (
    ErrNoCredentials = errors.New("credentials not configured")
    ErrUnauthorized  = errors.New("upstream rejected credentials")
    ErrNotFound      = errors.New("not found")
    ErrRateLimited   = errors.New("upstream rate limit reached")
    ErrUnavailable   = errors.New("upstream unavailable")
)
```

401/403 to `ErrUnauthorized`, 404 to `ErrNotFound`, 429 to `ErrRateLimited`,
5xx to `ErrUnavailable`. Wrap with status and a short, sanitized reason:
`fmt.Errorf("get board: %w (status %d)", ErrUnauthorized, resp.StatusCode)`.
Never include the response body or request headers.

## Wire types

Decode into unexported structs shaped like the upstream payload, then convert.
Tolerate missing and null fields; parse times explicitly with a known layout
and zone (`time.LoadLocation("Europe/London")` for UK rail).
