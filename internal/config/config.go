// Package config loads process configuration from flags and environment
// variables.
//
// Secrets (API keys, the HTTP bearer token) are read from the environment only,
// never from flags, because flags are visible in process listings.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"

	"github.com/elliottsam/rail-timetable-mcp-uk/internal/rdm"
)

// Transport selects how the MCP server talks to clients.
const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
)

// Defaults.
const (
	DefaultTransport = TransportStdio
	DefaultAddr      = "127.0.0.1:8080"
	DefaultLogLevel  = "info"
	DefaultLogFormat = "text"
)

var (
	// ErrInvalid is returned for a configuration that cannot be used.
	ErrInvalid = errors.New("invalid configuration")

	// ErrHelp is returned when -h was requested; usage has been written.
	ErrHelp = flag.ErrHelp

	// ErrVersion is returned when -version was requested.
	ErrVersion = errors.New("version requested")
)

// Config is the fully resolved process configuration.
type Config struct {
	// Transport is TransportStdio or TransportHTTP.
	Transport string

	// Addr is the HTTP listen address. Used only with TransportHTTP.
	Addr string

	// AuthToken is the bearer token required on the HTTP endpoint. It is
	// mandatory when Addr is not a loopback address.
	AuthToken rdm.Secret

	// AllowedOrigins are browser origins permitted to call the HTTP endpoint.
	AllowedOrigins []string

	// LogLevel and LogFormat configure logging, which always goes to stderr.
	LogLevel  slog.Level
	LogFormat string

	// Healthcheck asks the process to probe a running instance's /healthz
	// endpoint and exit, instead of serving. Used by container health checks.
	Healthcheck bool

	// RDM configures the upstream client.
	RDM rdm.Config
}

// Load resolves configuration from args (without the program name) and the
// environment accessed through getenv. Flag usage and parse errors are written
// to usageOut.
//
// Flags override environment variables, which override defaults.
func Load(args []string, getenv func(string) string, usageOut io.Writer) (Config, error) {
	var (
		cfg      Config
		level    string
		origins  string
		showVers bool
	)

	fs := flag.NewFlagSet("rail-mcp", flag.ContinueOnError)
	fs.SetOutput(usageOut)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(usageOut, "Usage: rail-mcp [flags]\n\nLive UK train times over MCP.\n\nFlags:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(usageOut, "\nEnvironment:\n"+
			"  RAILDATA_API_KEY            Rail Data Marketplace consumer key (boards)\n"+
			"  RAILDATA_SERVICE_API_KEY    key for service details (defaults to RAILDATA_API_KEY)\n"+
			"  RAILDATA_BASE_URL           API gateway origin override\n"+
			"  RAILDATA_AUTH_HEADER        header carrying the key (default %s)\n"+
			"  RAILDATA_DEPARTURES_PATH    departure board product path override\n"+
			"  RAILDATA_ARRIVALS_PATH      arrival board product path override\n"+
			"  RAILDATA_SERVICE_DETAILS_PATH  service details product path override\n"+
			"  MCP_AUTH_TOKEN              bearer token for the HTTP transport\n"+
			"  MCP_ALLOWED_ORIGINS         comma-separated browser origins for HTTP\n"+
			"  MCP_TRANSPORT, MCP_ADDR, LOG_LEVEL, LOG_FORMAT   defaults for the flags\n",
			rdm.DefaultAuthHeader)
	}

	fs.StringVar(&cfg.Transport, "transport", envOr(getenv, "MCP_TRANSPORT", DefaultTransport), "transport: stdio or http")
	fs.StringVar(&cfg.Addr, "addr", envOr(getenv, "MCP_ADDR", DefaultAddr), "HTTP listen address")
	fs.StringVar(&level, "log-level", envOr(getenv, "LOG_LEVEL", DefaultLogLevel), "log level: debug, info, warn, error")
	fs.StringVar(&cfg.LogFormat, "log-format", envOr(getenv, "LOG_FORMAT", DefaultLogFormat), "log format: text or json")
	fs.StringVar(&origins, "allowed-origins", getenv("MCP_ALLOWED_ORIGINS"), "comma-separated browser origins allowed for HTTP")
	fs.BoolVar(&showVers, "version", false, "print version and exit")
	fs.BoolVar(&cfg.Healthcheck, "healthcheck", false, "probe a running HTTP instance at -addr and exit 0 if healthy")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if showVers {
		return Config{}, ErrVersion
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("%w: unexpected argument %q", ErrInvalid, fs.Arg(0))
	}

	cfg.AuthToken = rdm.Secret(getenv("MCP_AUTH_TOKEN"))
	cfg.AllowedOrigins = splitList(origins)
	cfg.RDM = rdm.Config{
		BaseURL:            getenv("RAILDATA_BASE_URL"),
		AuthHeader:         getenv("RAILDATA_AUTH_HEADER"),
		DeparturesPath:     getenv("RAILDATA_DEPARTURES_PATH"),
		ArrivalsPath:       getenv("RAILDATA_ARRIVALS_PATH"),
		ServiceDetailsPath: getenv("RAILDATA_SERVICE_DETAILS_PATH"),
		APIKey:             rdm.Secret(strings.TrimSpace(getenv("RAILDATA_API_KEY"))),
		ServiceAPIKey:      rdm.Secret(strings.TrimSpace(getenv("RAILDATA_SERVICE_API_KEY"))),
	}

	if err := cfg.LogLevel.UnmarshalText([]byte(level)); err != nil {
		return Config{}, fmt.Errorf("%w: log level %q", ErrInvalid, level)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks cross-field rules.
func (c *Config) Validate() error {
	switch c.Transport {
	case TransportStdio:
	case TransportHTTP:
		if _, _, err := net.SplitHostPort(c.Addr); err != nil {
			return fmt.Errorf("%w: addr %q: %w", ErrInvalid, c.Addr, err)
		}
		if c.AuthToken == "" && !IsLoopbackAddr(c.Addr) {
			return fmt.Errorf("%w: MCP_AUTH_TOKEN is required when listening on non-loopback address %q", ErrInvalid, c.Addr)
		}
	default:
		return fmt.Errorf("%w: transport %q (want %s or %s)", ErrInvalid, c.Transport, TransportStdio, TransportHTTP)
	}

	switch c.LogFormat {
	case "text", "json":
	default:
		return fmt.Errorf("%w: log format %q (want text or json)", ErrInvalid, c.LogFormat)
	}
	return nil
}

// IsLoopbackAddr reports whether a host:port listen address binds only to the
// loopback interface. An empty or unspecified host binds every interface.
func IsLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func envOr(getenv func(string) string, key, def string) string {
	if v := strings.TrimSpace(getenv(key)); v != "" {
		return v
	}
	return def
}

func splitList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
