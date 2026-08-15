// Package config loads the f1iptv service configuration from environment
// variables, following 12-factor conventions.
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"
)

// Config holds the service configuration.
type Config struct {
	Host           string        // F1IPTV_HOST (bind address; empty = all interfaces)
	Port           int           // F1IPTV_PORT (default 8080)
	Qualities      string        // F1IPTV_QUALITIES (comma-separated)
	SourceURL      string        // F1IPTV_SOURCE_URL
	BaseURL        string        // F1IPTV_BASE_URL (empty = derive from request Host)
	Group          string        // F1IPTV_GROUP (IPTV group-title)
	ResolutionTTL  time.Duration // F1IPTV_TTL (how long to cache a resolved stream)
	VerifyPlaylist bool          // F1IPTV_VERIFY_PLAYLIST
	LogLevel       string        // F1IPTV_LOG_LEVEL (debug/info/warn/error)

	EPGEnabled bool          // F1IPTV_EPG_ENABLED
	EPGAPIURL  string        // F1IPTV_EPG_API_URL
	EPGTTL     time.Duration // F1IPTV_EPG_TTL (how long to cache the season calendar)
	EPGYear    int           // F1IPTV_EPG_YEAR (0 = current year)
}

// Addr returns the full listen address host:port.
func (c *Config) Addr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

// Load reads configuration from the environment, applying defaults for any
// unset variable. It returns an error for malformed values.
func Load() (*Config, error) {
	ttl, err := duration("F1IPTV_TTL", 30*time.Second)
	if err != nil {
		return nil, err
	}
	verify, err := boolVar("F1IPTV_VERIFY_PLAYLIST", false)
	if err != nil {
		return nil, err
	}
	epgEnabled, err := boolVar("F1IPTV_EPG_ENABLED", true)
	if err != nil {
		return nil, err
	}
	epgTTL, err := duration("F1IPTV_EPG_TTL", 6*time.Hour)
	if err != nil {
		return nil, err
	}
	epgYear, err := intVar("F1IPTV_EPG_YEAR", 0)
	if err != nil {
		return nil, err
	}
	port, err := intVar("F1IPTV_PORT", 8080)
	if err != nil {
		return nil, err
	}
	return &Config{
		Host:           str("F1IPTV_HOST", ""),
		Port:           port,
		Qualities:      str("F1IPTV_QUALITIES", "1080p,720p"),
		SourceURL:      str("F1IPTV_SOURCE_URL", "https://streamfree.top/embed/racing/skyf1"),
		BaseURL:        str("F1IPTV_BASE_URL", ""),
		Group:          str("F1IPTV_GROUP", "Sports"),
		ResolutionTTL:  ttl,
		VerifyPlaylist: verify,
		LogLevel:       str("F1IPTV_LOG_LEVEL", "info"),

		EPGEnabled: epgEnabled,
		EPGAPIURL:  str("F1IPTV_EPG_API_URL", "https://api.openf1.org/v1"),
		EPGTTL:     epgTTL,
		EPGYear:    epgYear,
	}, nil
}

func str(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func duration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}

func boolVar(key string, def bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return b, nil
}

func intVar(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}
