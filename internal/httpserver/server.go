// Package httpserver exposes the F1 IPTV service over HTTP: a Jellyfin M3U
// playlist plus a proxy for each channel's live HLS stream. It depends on the
// iptv domain (channel catalog + resolution cache) and the hlsproxy client
// (upstream fetching + playlist rewriting).
package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"f1-jf/internal/hlsproxy"
	"f1-jf/internal/iptv"
)

// Options configures a Server.
type Options struct {
	// Base is the external base URL (scheme://host[:port]) this server is
	// reachable at, used to build absolute URLs in playlists. If empty, the
	// request Host (and X-Forwarded-Proto, when present) is used.
	Base string
	// Logger receives request diagnostics. Defaults to slog.Default().
	Logger *slog.Logger
	// Upstream fetches upstream playlists and segments. Defaults to a
	// hlsproxy.Client with a 2 minute timeout.
	Upstream *hlsproxy.Client
	// EPG renders the XMLTV guide; when nil, /iptv/guide.xml is disabled.
	EPG EPGRenderer
}

// EPGRenderer renders an XMLTV document for the given channels.
type EPGRenderer interface {
	RenderXML(ctx context.Context, channels []*iptv.Channel) ([]byte, error)
}

// Server proxies resolved F1 streams and exposes them as an IPTV playlist.
type Server struct {
	registry *iptv.Registry
	channels []*iptv.Channel
	base     string
	log      *slog.Logger
	upstream *hlsproxy.Client
	epg      EPGRenderer
}

func New(registry *iptv.Registry, channels []*iptv.Channel, opts Options) *Server {
	s := &Server{
		registry: registry,
		channels: channels,
		base:     strings.TrimRight(opts.Base, "/"),
		log:      opts.Logger,
		upstream: opts.Upstream,
		epg:      opts.EPG,
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.upstream == nil {
		s.upstream = hlsproxy.NewClient(nil)
	}
	return s
}

// Handler returns the HTTP handler exposing the service's endpoints, wrapped
// in panic recovery and request logging middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /iptv/playlist.m3u", s.handlePlaylist)
	mux.HandleFunc("GET /iptv/stream/{channel}", s.handleStream)
	mux.HandleFunc("GET /iptv/f/{channel}", s.handleFetch)
	mux.HandleFunc("GET /iptv/guide.xml", s.handleGuide)
	return withMiddleware(s.log, mux)
}
