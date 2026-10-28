package config_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/elliottsam/rail-timetable-mcp-uk/internal/config"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load(nil, env(nil), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Transport != config.TransportStdio || cfg.Addr != config.DefaultAddr ||
		cfg.LogLevel != slog.LevelInfo || cfg.LogFormat != "text" {
		t.Errorf("defaults = %+v", cfg)
	}
	if cfg.RDM.APIKey != "" {
		t.Error("API key should be empty by default")
	}
}

func TestLoadWithoutKeyIsValid(t *testing.T) {
	if _, err := config.Load([]string{"-transport", "http"}, env(nil), &bytes.Buffer{}); err != nil {
		t.Fatalf("a missing API key must not fail startup: %v", err)
	}
}

func TestLoadFromEnvironment(t *testing.T) {
	cfg, err := config.Load(nil, env(map[string]string{
		"RAILDATA_API_KEY":         " key1 ",
		"RAILDATA_SERVICE_API_KEY": "key2",
		"RAILDATA_BASE_URL":        "https://gw.example",
		"RAILDATA_AUTH_HEADER":     "x-custom",
		"MCP_TRANSPORT":            "http",
		"MCP_ADDR":                 "127.0.0.1:9000",
		"MCP_AUTH_TOKEN":           "tok",
		"MCP_ALLOWED_ORIGINS":      "https://a.example, https://b.example,,",
		"LOG_LEVEL":                "debug",
		"LOG_FORMAT":               "json",
	}), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RDM.APIKey != "key1" || cfg.RDM.ServiceAPIKey != "key2" ||
		cfg.RDM.BaseURL != "https://gw.example" || cfg.RDM.AuthHeader != "x-custom" {
		t.Errorf("rdm = %+v", cfg.RDM)
	}
	if cfg.Transport != "http" || cfg.Addr != "127.0.0.1:9000" || cfg.AuthToken != "tok" ||
		cfg.LogLevel != slog.LevelDebug || cfg.LogFormat != "json" {
		t.Errorf("cfg = %+v", cfg)
	}
	if want := []string{"https://a.example", "https://b.example"}; !reflect.DeepEqual(cfg.AllowedOrigins, want) {
		t.Errorf("origins = %q, want %q", cfg.AllowedOrigins, want)
	}
}

func TestFlagsOverrideEnvironment(t *testing.T) {
	cfg, err := config.Load(
		[]string{"-transport", "stdio", "-log-level", "error"},
		env(map[string]string{"MCP_TRANSPORT": "http", "LOG_LEVEL": "debug"}),
		&bytes.Buffer{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Transport != "stdio" || cfg.LogLevel != slog.LevelError {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{"bad transport", []string{"-transport", "carrier-pigeon"}, nil, "transport"},
		{"bad log level", []string{"-log-level", "loud"}, nil, "log level"},
		{"bad log format", []string{"-log-format", "xml"}, nil, "log format"},
		{"bad addr", []string{"-transport", "http", "-addr", "nonsense"}, nil, "addr"},
		{"public bind needs token", []string{"-transport", "http", "-addr", "0.0.0.0:8080"}, nil, "MCP_AUTH_TOKEN"},
		{"empty host needs token", []string{"-transport", "http", "-addr", ":8080"}, nil, "MCP_AUTH_TOKEN"},
		{"lan bind needs token", []string{"-transport", "http", "-addr", "192.168.1.5:8080"}, nil, "MCP_AUTH_TOKEN"},
		{"hostname bind needs token", []string{"-transport", "http", "-addr", "example.com:8080"}, nil, "MCP_AUTH_TOKEN"},
		{"stray argument", []string{"serve"}, nil, "unexpected argument"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(tt.args, env(tt.env), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want mention of %q", err, tt.want)
			}
		})
	}
}

func TestPublicBindWithTokenIsAllowed(t *testing.T) {
	_, err := config.Load(
		[]string{"-transport", "http", "-addr", "0.0.0.0:8080"},
		env(map[string]string{"MCP_AUTH_TOKEN": "tok"}),
		&bytes.Buffer{},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoopbackBindNeedsNoToken(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:1", "localhost:1", "[::1]:1"} {
		_, err := config.Load([]string{"-transport", "http", "-addr", addr}, env(nil), &bytes.Buffer{})
		if err != nil {
			t.Errorf("addr %s: %v", addr, err)
		}
	}
}

func TestHelpAndVersion(t *testing.T) {
	var out bytes.Buffer
	if _, err := config.Load([]string{"-h"}, env(nil), &out); !errors.Is(err, config.ErrHelp) {
		t.Errorf("-h error = %v", err)
	}
	if !strings.Contains(out.String(), "RAILDATA_API_KEY") {
		t.Errorf("usage lacks environment docs:\n%s", out.String())
	}
	if _, err := config.Load([]string{"-version"}, env(nil), &bytes.Buffer{}); !errors.Is(err, config.ErrVersion) {
		t.Errorf("-version error = %v", err)
	}
}

func TestSecretsNeverPrint(t *testing.T) {
	cfg, err := config.Load(nil, env(map[string]string{
		"RAILDATA_API_KEY": "topsecret-key", "MCP_AUTH_TOKEN": "topsecret-token",
	}), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%v %+v %#v", cfg, cfg, cfg); strings.Contains(got, "topsecret") {
		t.Errorf("config printing leaks secrets: %s", got)
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	tests := map[string]bool{
		"127.0.0.1:80":   true,
		"127.5.5.5:80":   true,
		"[::1]:80":       true,
		"localhost:80":   true,
		"LOCALHOST:80":   true,
		"0.0.0.0:80":     false,
		":80":            false,
		"[::]:80":        false,
		"10.0.0.1:80":    false,
		"example.com:80": false,
		"no-port":        false,
	}
	for addr, want := range tests {
		if got := config.IsLoopbackAddr(addr); got != want {
			t.Errorf("IsLoopbackAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}
