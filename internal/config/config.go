// Package config loads the f1iptv service configuration from environment
// variables, following 12-factor conventions.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds the service configuration.
type Config struct {
	Listen         string        // F1IPTV_LISTEN
	Qualities      string        // F1IPTV_QUALITIES (comma-separated)
	SourceURL      string        // F1IPTV_SOURCE_URL
	BaseURL        string        // F1IPTV_BASE_URL (empty = derive from request Host)
	Group          string        // F1IPTV_GROUP (IPTV group-title)
	ResolutionTTL  time.Duration // F1IPTV_TTL (how long to cache a resolved stream)
	VerifyPlaylist bool          // F1IPTV_VERIFY_PLAYLIST
	LogLevel       string        // F1IPTV_LOG_LEVEL (debug/info/warn/error)
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
	return &Config{
		Listen:         str("F1IPTV_LISTEN", ":8080"),
		Qualities:      str("F1IPTV_QUALITIES", "1080p,720p"),
		SourceURL:      str("F1IPTV_SOURCE_URL", "https://streamfree.top/embed/racing/skyf1"),
		BaseURL:        str("F1IPTV_BASE_URL", ""),
		Group:          str("F1IPTV_GROUP", "Sports"),
		ResolutionTTL:  ttl,
		VerifyPlaylist: verify,
		LogLevel:       str("F1IPTV_LOG_LEVEL", "info"),
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
