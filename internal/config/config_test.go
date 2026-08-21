package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("F1IPTV_HOST", "")
	t.Setenv("F1IPTV_PORT", "")
	t.Setenv("F1IPTV_QUALITIES", "")
	t.Setenv("F1IPTV_DASHBOARD_URL", "")
	t.Setenv("F1IPTV_BASE_URL", "")
	t.Setenv("F1IPTV_GROUP", "")
	t.Setenv("F1IPTV_CHANNEL_LOGO", "")
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
	if cfg.Host != "" {
		t.Errorf("Host = %q", cfg.Host)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.Addr() != ":8080" {
		t.Errorf("Addr() = %q, want :8080", cfg.Addr())
	}
	if cfg.Qualities != "2160p,1080p,720p" {
		t.Errorf("Qualities = %q", cfg.Qualities)
	}
	if cfg.DashboardURL != "https://f1net.vercel.app" {
		t.Errorf("DashboardURL = %q", cfg.DashboardURL)
	}
	if cfg.BaseURL != "" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Group != "Sports" {
		t.Errorf("Group = %q", cfg.Group)
	}
	if cfg.ChannelLogo != "" {
		t.Errorf("ChannelLogo = %q, want empty", cfg.ChannelLogo)
	}
	if cfg.ResolutionTTL != 5*time.Minute {
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
	t.Setenv("F1IPTV_HOST", "")
	t.Setenv("F1IPTV_PORT", "9999")
	t.Setenv("F1IPTV_QUALITIES", "720p")
	t.Setenv("F1IPTV_DASHBOARD_URL", "https://example.com")
	t.Setenv("F1IPTV_BASE_URL", "https://f1.example.com")
	t.Setenv("F1IPTV_GROUP", "Racing")
	t.Setenv("F1IPTV_CHANNEL_LOGO", "https://img.example/f1.png")
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
	if cfg.Addr() != ":9999" || cfg.Port != 9999 || cfg.Qualities != "720p" ||
		cfg.DashboardURL != "https://example.com" ||
		cfg.BaseURL != "https://f1.example.com" ||
		cfg.Group != "Racing" ||
		cfg.ChannelLogo != "https://img.example/f1.png" ||
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

func TestLoadHostPort(t *testing.T) {
	t.Setenv("F1IPTV_HOST", "127.0.0.1")
	t.Setenv("F1IPTV_PORT", "9000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr() != "127.0.0.1:9000" {
		t.Errorf("Addr() = %q, want 127.0.0.1:9000", cfg.Addr())
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 9000 {
		t.Errorf("Host/Port = %q/%d", cfg.Host, cfg.Port)
	}
}

func TestLoadInvalidPort(t *testing.T) {
	t.Setenv("F1IPTV_PORT", "http")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid port")
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
