package httpserver

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	m3u8 "github.com/Eyevinn/hls-m3u8/m3u8"

	f1net "f1-jf/internal/f1net"
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

// handleGuide serves the XMLTV electronic program guide for the configured
// channels, wired to the playlist's tvg-ids.
func (s *Server) handleGuide(w http.ResponseWriter, r *http.Request) {
	if s.epg == nil {
		http.Error(w, "epg disabled", http.StatusServiceUnavailable)
		return
	}
	doc, err := s.epg.RenderXML(r.Context(), s.channels)
	if err != nil {
		s.log.Warn("render guide", "error", err)
		http.Error(w, "guide unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(doc)
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
		fmt.Fprintf(&b, "%s/iptv/stream/%s.ts\n", s.publicBase(r), ch.ID)
	}

	w.Header().Set("Content-Type", "application/x-mpegurl; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	io.WriteString(w, b.String())
}

// handleStream serves the channel's live stream. When the route is requested
// with a .ts suffix it returns a continuous raw MPEG-TS stream (concatenated
// upstream segments), which makes Jellyfin pick its SharedHttpStream proxy
// path instead of handing the m3u8 and its segment URLs directly to clients.
// Otherwise it serves the channel's live HLS playlist, re-fetched upstream
// with the required headers and rewritten so all segment (and nested
// playlist) requests go through the proxy.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	chName := r.PathValue("channel")
	rawTS := strings.HasSuffix(chName, ".ts")
	chID := strings.TrimSuffix(chName, ".ts")
	ch, ok := s.channel(chID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	st, err := s.registry.Resolve(r.Context(), ch)
	if err != nil || st == nil {
		http.Error(w, "stream offline", http.StatusServiceUnavailable)
		return
	}
	if rawTS {
		s.serveRawTS(w, r, ch, st)
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

// serveRawTS streams the channel's upstream HLS as continuous MPEG-TS,
// concatenating each segment's raw bytes and following the live playlist as
// new segments appear. The response is chunked and has no Content-Length.
func (s *Server) serveRawTS(w http.ResponseWriter, r *http.Request, ch *iptv.Channel, st *f1net.Stream) {
	ctx := r.Context()
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	sent := make(map[string]bool)
	flusher, _ := w.(http.Flusher)
	for {
		if err := s.streamTSWindow(ctx, st, sent, w, flusher); err != nil {
			if ctx.Err() != nil {
				return
			}
			s.log.Warn("ts stream", "channel", ch.ID, "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// streamTSWindow fetches the current live media playlist once and streams any
// segments not yet sent. It returns without error when a full playlist pass
// completed (so serveRawTS can wait and re-poll); a non-nil error means the
// pass should be retried after backoff.
func (s *Server) streamTSWindow(ctx context.Context, st *f1net.Stream, sent map[string]bool, w io.Writer, flusher http.Flusher) error {
	up, err := s.upstream.Fetch(ctx, st.Headers, st.PlaylistURL, "")
	if err != nil {
		return err
	}
	defer up.Body.Close()
	content, err := io.ReadAll(io.LimitReader(up.Body, hlsproxy.MaxPlaylistBytes))
	if err != nil {
		return err
	}
	pl, _, err := m3u8.DecodeFrom(bytes.NewReader(content), false)
	if err != nil {
		return err
	}
	mp, ok := pl.(*m3u8.MediaPlaylist)
	if !ok {
		return fmt.Errorf("not a media playlist")
	}
	finished := false
	for i := uint(0); i < mp.Count(); i++ {
		seg := mp.Segments[i]
		if sent[seg.URI] {
			finished = true
			continue
		}
		segURL := hlsproxy.ResolveUpstream(st.PlaylistURL, seg.URI)
		segUp, err := s.upstream.Fetch(ctx, st.Headers, segURL, "")
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, segUp.Body)
		segUp.Body.Close()
		if copyErr != nil {
			return copyErr
		}
		sent[seg.URI] = true
		if flusher != nil {
			flusher.Flush()
		}
	}
	if finished {
		return nil
	}
	return nil
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
		w.Write(rewritten)
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
