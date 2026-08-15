package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("F1IPTV_LISTEN", "")
	t.Setenv("F1IPTV_QUALITIES", "")
	t.Setenv("F1IPTV_SOURCE_URL", "")
	t.Setenv("F1IPTV_BASE_URL", "")
	t.Setenv("F1IPTV_GROUP", "")
	t.Setenv("F1IPTV_TTL", "")
	t.Setenv("F1IPTV_VERIFY_PLAYLIST", "")
	t.Setenv("F1IPTV_LOG_LEVEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen != ":8080" {
		t.Errorf("Listen = %q", cfg.Listen)
	}
	if cfg.Qualities != "1080p,720p" {
		t.Errorf("Qualities = %q", cfg.Qualities)
	}
	if cfg.SourceURL != "https://streamfree.top/embed/racing/skyf1" {
		t.Errorf("SourceURL = %q", cfg.SourceURL)
	}
	if cfg.BaseURL != "" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Group != "Sports" {
		t.Errorf("Group = %q", cfg.Group)
	}
	if cfg.ResolutionTTL != 30*time.Second {
		t.Errorf("ResolutionTTL = %v", cfg.ResolutionTTL)
	}
	if cfg.VerifyPlaylist {
		t.Error("VerifyPlaylist = true, want false")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("F1IPTV_LISTEN", ":9999")
	t.Setenv("F1IPTV_QUALITIES", "720p")
	t.Setenv("F1IPTV_SOURCE_URL", "https://example.com/embed")
	t.Setenv("F1IPTV_BASE_URL", "https://f1.example.com")
	t.Setenv("F1IPTV_GROUP", "Racing")
	t.Setenv("F1IPTV_TTL", "5s")
	t.Setenv("F1IPTV_VERIFY_PLAYLIST", "true")
	t.Setenv("F1IPTV_LOG_LEVEL", "debug")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen != ":9999" || cfg.Qualities != "720p" ||
		cfg.SourceURL != "https://example.com/embed" ||
		cfg.BaseURL != "https://f1.example.com" ||
		cfg.Group != "Racing" ||
		cfg.ResolutionTTL != 5*time.Second ||
		!cfg.VerifyPlaylist || cfg.LogLevel != "debug" {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

func TestLoadInvalidTTL(t *testing.T) {
	t.Setenv("F1IPTV_TTL", "nonsense")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid TTL")
	}
}

func TestLoadInvalidBool(t *testing.T) {
	t.Setenv("F1IPTV_VERIFY_PLAYLIST", "maybe")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid boolean")
	}
}
