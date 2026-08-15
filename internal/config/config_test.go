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
	t.Setenv("F1IPTV_EPG_ENABLED", "")
	t.Setenv("F1IPTV_EPG_API_URL", "")
	t.Setenv("F1IPTV_EPG_TTL", "")
	t.Setenv("F1IPTV_EPG_YEAR", "")

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
	if !cfg.EPGEnabled {
		t.Error("EPGEnabled = false, want true")
	}
	if cfg.EPGAPIURL != "https://api.openf1.org/v1" {
		t.Errorf("EPGAPIURL = %q", cfg.EPGAPIURL)
	}
	if cfg.EPGTTL != 6*time.Hour {
		t.Errorf("EPGTTL = %v", cfg.EPGTTL)
	}
	if cfg.EPGYear != 0 {
		t.Errorf("EPGYear = %d, want 0", cfg.EPGYear)
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
	t.Setenv("F1IPTV_EPG_ENABLED", "false")
	t.Setenv("F1IPTV_EPG_API_URL", "https://epg.example.com/v1")
	t.Setenv("F1IPTV_EPG_TTL", "1h")
	t.Setenv("F1IPTV_EPG_YEAR", "2025")

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
	if cfg.EPGEnabled {
		t.Error("EPGEnabled = true, want false")
	}
	if cfg.EPGAPIURL != "https://epg.example.com/v1" || cfg.EPGTTL != time.Hour || cfg.EPGYear != 2025 {
		t.Errorf("unexpected epg config: %+v", cfg)
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

func TestLoadInvalidEPGYears(t *testing.T) {
	t.Setenv("F1IPTV_EPG_YEAR", "next")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid EPG year")
	}
}

func TestLoadInvalidEPGTTL(t *testing.T) {
	t.Setenv("F1IPTV_EPG_TTL", "later")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid EPG TTL")
	}
}
