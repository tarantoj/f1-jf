package httpserver

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strings"

	"f1-jf/internal/hlsproxy"
	"f1-jf/internal/iptv"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ready")
}

// handlePlaylist serves the well-formed M3U channel list consumed by
// Jellyfin's M3U Tuner. Offline channels are omitted.
func (s *Server) handlePlaylist(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, ch := range s.channels {
		st, err := s.registry.Resolve(r.Context(), ch)
		if err != nil || st == nil {
			s.log.Warn("channel offline", "channel", ch.ID, "error", err)
			continue
		}
		fmt.Fprintf(&b, `#EXTINF:-1 tvg-id=%q tvg-name=%q group-title=%q,%s`+"\n",
			ch.ID, ch.Name, ch.Group, ch.Name)
		fmt.Fprintf(&b, "%s/iptv/stream/%s\n", s.publicBase(r), ch.ID)
	}

	w.Header().Set("Content-Type", "application/x-mpegurl; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	io.WriteString(w, b.String())
}

// handleStream serves the channel's live HLS playlist, re-fetched upstream
// with the required headers and rewritten so all segment (and nested
// playlist) requests go through the proxy.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	ch, ok := s.channel(r.PathValue("channel"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	st, err := s.registry.Resolve(r.Context(), ch)
	if err != nil || st == nil {
		http.Error(w, "stream offline", http.StatusServiceUnavailable)
		return
	}

	up, err := s.upstream.Fetch(r.Context(), st.Headers, st.PlaylistURL, "")
	if err != nil {
		s.log.Warn("fetch playlist", "channel", ch.ID, "error", err)
		http.Error(w, "upstream unreachable", http.StatusBadGateway)
		return
	}
	defer up.Body.Close()
	s.serveUpstream(w, r, ch, up, st.PlaylistURL)
}

// handleFetch is the generic upstream forwarder: it fetches a proxied URI
// (segment or nested playlist) with the channel's headers and streams the
// result back.
func (s *Server) handleFetch(w http.ResponseWriter, r *http.Request) {
	ch, ok := s.channel(r.PathValue("channel"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	upstream := r.URL.Query().Get("u")
	if upstream == "" {
		http.Error(w, "missing u", http.StatusBadRequest)
		return
	}

	st, err := s.registry.Resolve(r.Context(), ch)
	if err != nil || st == nil {
		http.Error(w, "stream offline", http.StatusServiceUnavailable)
		return
	}

	up, err := s.upstream.Fetch(r.Context(), st.Headers, upstream, r.Header.Get("Range"))
	if err != nil {
		s.log.Warn("fetch upstream", "channel", ch.ID, "error", err)
		http.Error(w, "upstream unreachable", http.StatusBadGateway)
		return
	}
	defer up.Body.Close()
	s.serveUpstream(w, r, ch, up, upstream)
}

// serveUpstream forwards an upstream response to the client, rewriting it if
// it is an HLS playlist and streaming it raw (with Content-Type, Range and
// status preserved) if it is a media segment.
func (s *Server) serveUpstream(w http.ResponseWriter, r *http.Request, ch *iptv.Channel, up *hlsproxy.Response, upstreamBase string) {
	br := bufio.NewReader(up.Body)
	prefix, _ := br.Peek(16)

	if up.IsPlaylist(prefix) {
		content, err := io.ReadAll(io.LimitReader(br, hlsproxy.MaxPlaylistBytes))
		if err != nil {
			s.log.Warn("read playlist", "channel", ch.ID, "error", err)
			http.Error(w, "read upstream", http.StatusBadGateway)
			return
		}
		rewritten := hlsproxy.RewritePlaylist(content, upstreamBase, s.publicBase(r), ch.ID)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
		io.WriteString(w, string(rewritten))
		return
	}

	// Media segment: stream the raw bytes, preserving upstream metadata.
	w.Header().Set("Content-Type", hlsproxy.SegmentContentType(up.Header.Get("Content-Type")))
	if cl := up.Header.Get("Content-Length"); cl != "" && up.Status == http.StatusOK {
		w.Header().Set("Content-Length", cl)
	}
	if cr := up.Header.Get("Content-Range"); cr != "" {
		w.Header().Set("Content-Range", cr)
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.WriteHeader(up.Status)
	io.Copy(w, br)
}

// channel returns the channel with the given ID.
func (s *Server) channel(id string) (*iptv.Channel, bool) {
	for _, ch := range s.channels {
		if ch.ID == id {
			return ch, true
		}
	}
	return nil, false
}

// publicBase builds the absolute base URL this server is reachable at.
func (s *Server) publicBase(r *http.Request) string {
	if s.base != "" {
		return s.base
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	return scheme + "://" + r.Host
}
