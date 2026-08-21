// Command f1iptv runs the F1 IPTV service: it proxies live F1 streams from
// the F1Net dashboard (resolved by internal/f1net) and exposes them as a
// well-formed IPTV M3U playlist consumable by Jellyfin's M3U Tuner.
//
// Add the playlist URL (e.g. http://localhost:8080/iptv/playlist.m3u) as an
// M3U Tuner source in Jellyfin; all upstream header/auth requirements are
// handled by the proxy, so the source is never contacted directly.
//
// Configuration is read from the environment (see internal/config).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"f1-jf/internal/config"
	"f1-jf/internal/epg"
	f1net "f1-jf/internal/f1net"
	"f1-jf/internal/httpserver"
	"f1-jf/internal/iptv"
)

func main() {
	if err := run(); err != nil {
		slog.Error("f1iptv", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	f1 := &f1net.Client{VerifyPlaylist: cfg.VerifyPlaylist, Logger: logger}
	registry := iptv.NewRegistryLogger(f1, cfg.ResolutionTTL, logger)

	channels, err := iptv.ChannelsFromQualities(cfg.Qualities, cfg.Group, f1net.Source{Name: "F1", URL: cfg.SourceURL})
	if err != nil {
		return err
	}

	prewarm(registry, channels, logger)

	var epgSvc *epg.Service
	if cfg.EPGEnabled {
		year := cfg.EPGYear
		if year == 0 {
			year = time.Now().Year()
		}
		epgSvc = epg.New(epg.Options{
			APIURL: cfg.EPGAPIURL,
			Year:   year,
			TTL:    cfg.EPGTTL,
			Logger: logger,
		})
	}

	server := httpserver.New(registry, channels, httpserver.Options{
		Base:   cfg.BaseURL,
		Logger: logger,
		EPG:    epgSvc,
	})

	addr := cfg.Addr()
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening",
			"addr", addr,
			"playlist", playlistURL(cfg.BaseURL, addr),
			"channels", len(channels))
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// prewarm resolves every channel concurrently in the background so the first
// stream start finds a warm Registry cache instead of paying the full
// multi-request source resolution inline. It never blocks startup or fails the
// service; failures are logged and the cache is populated lazily later.
func prewarm(registry *iptv.Registry, channels []*iptv.Channel, logger *slog.Logger) {
	for _, ch := range channels {
		go func(ch *iptv.Channel) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if _, err := registry.Resolve(ctx, ch); err != nil {
				logger.Warn("prewarm failed", "channel", ch.ID, "error", err)
			}
		}(ch)
	}
}

// newLogger builds a structured logger at the requested level.
func newLogger(level string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}

// playlistURL builds the playlist URL shown at startup.
func playlistURL(base, listen string) string {
	if base != "" {
		return strings.TrimRight(base, "/") + "/iptv/playlist.m3u"
	}
	if strings.HasPrefix(listen, ":") {
		return "http://localhost" + listen + "/iptv/playlist.m3u"
	}
	return "http://" + listen + "/iptv/playlist.m3u"
}
