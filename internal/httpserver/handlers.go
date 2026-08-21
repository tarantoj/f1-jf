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

// handleGuide serves the XMLTV electronic program guide for the channel,
// wired to the playlist's tvg-id.
func (s *Server) handleGuide(w http.ResponseWriter, r *http.Request) {
	if s.epg == nil {
		http.Error(w, "epg disabled", http.StatusServiceUnavailable)
		return
	}
	doc, err := s.epg.RenderXML(r.Context(), s.ch)
	if err != nil {
		s.logger(r.Context()).Warn("render guide", "error", err)
		http.Error(w, "guide unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(doc)
}

// handlePlaylist serves the well-formed M3U channel list consumed by
// Jellyfin's M3U Tuner. An offline channel is omitted.
func (s *Server) handlePlaylist(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	st, err := s.registry.Resolve(r.Context(), s.ch)
	if err != nil || st == nil {
		s.logger(r.Context()).Warn("channel offline", "channel", s.ch.ID, "error", err)
	} else {
		line := fmt.Sprintf(`#EXTINF:-1 tvg-id=%q tvg-name=%q group-title=%q,%s`+"\n",
			s.ch.ID, s.ch.Name, s.ch.Group, s.ch.Name)
		if logo := s.channelLogo(r.Context()); logo != "" {
			line = fmt.Sprintf(`#EXTINF:-1 tvg-id=%q tvg-name=%q tvg-logo=%q group-title=%q,%s`+"\n",
				s.ch.ID, s.ch.Name, logo, s.ch.Group, s.ch.Name)
		}
		fmt.Fprint(&b, line)
		fmt.Fprintf(&b, "%s/iptv/stream/raw.ts\n", s.publicBase(r))
	}

	w.Header().Set("Content-Type", "application/x-mpegurl; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	io.WriteString(w, b.String())
}

// handleStream serves the channel's live HLS playlist, re-fetched upstream
// with the required headers and rewritten so all segment (and nested
// playlist) requests go through the proxy. The raw MPEG-TS variant is served
// by handleStreamRawTS.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	st, err := s.registry.Resolve(r.Context(), s.ch)
	if err != nil || st == nil {
		http.Error(w, "stream offline", http.StatusServiceUnavailable)
		return
	}

	up, err := s.upstream.Fetch(r.Context(), st.Headers, st.PlaylistURL, "")
	if err != nil {
		if st2, err2 := s.registry.Refresh(r.Context(), s.ch); err2 == nil && st2 != nil {
			s.logger(r.Context()).Info("stream switched", "channel", s.ch.ID,
				"quality", st2.Quality, "error", err)
			st = st2
			up, err = s.upstream.Fetch(r.Context(), st.Headers, st.PlaylistURL, "")
		}
		if err != nil {
			s.logger(r.Context()).Warn("fetch playlist", "channel", s.ch.ID, "error", err)
			http.Error(w, "upstream unreachable", http.StatusBadGateway)
			return
		}
	}
	defer up.Body.Close()
	s.serveUpstream(w, r, up, st.PlaylistURL)
}

// handleStreamRawTS serves the channel's live stream as a continuous raw
// MPEG-TS stream (concatenated upstream segments), which makes Jellyfin pick
// its SharedHttpStream proxy path instead of handing the m3u8 and its segment
// URLs directly to clients.
func (s *Server) handleStreamRawTS(w http.ResponseWriter, r *http.Request) {
	st, err := s.registry.Resolve(r.Context(), s.ch)
	if err != nil || st == nil {
		http.Error(w, "stream offline", http.StatusServiceUnavailable)
		return
	}
	s.serveRawTS(w, r, s.ch, st)
}

// serveRawTS streams the channel's upstream HLS as continuous MPEG-TS,
// concatenating each segment's raw bytes and following the live playlist as
// new segments appear. The response is chunked and has no Content-Length.
func (s *Server) serveRawTS(w http.ResponseWriter, r *http.Request, ch *iptv.Channel, st *f1net.Stream) {
	ctx := r.Context()
	log := s.logger(ctx)
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	start := time.Now()
	log.Info("stream started", "channel", ch.ID, "quality", st.Quality)

	sent := make(map[string]bool)
	var failCount int
	var lastSwitch time.Time
	// Wrap the writer so bytes are flushed continuously during segment
	// downloads instead of only when a full segment has been buffered. This
	// gets data to the client (e.g. Jellyfin ffmpeg) as it arrives.
	bw := bufio.NewWriterSize(&flushWriter{w: w}, 32<<10)
	for {
		if err := s.streamTSWindow(ctx, st, sent, bw); err != nil {
			bw.Flush()
			if ctx.Err() != nil {
				break
			}
			log.Warn("ts stream", "channel", ch.ID, "quality", st.Quality, "error", err)
			failCount++
			if failCount > 2 && time.Since(lastSwitch) >= 10*time.Second {
				if newSt, rerr := s.registry.Refresh(ctx, ch); rerr == nil && newSt != nil {
					log.Info("stream switched", "channel", ch.ID,
						"quality", newSt.Quality, "error", err)
					st = newSt
					failCount = 0
					lastSwitch = time.Now()
					sent = make(map[string]bool)
				} else {
					log.Warn("stream refresh for ts fallback failed", "channel", ch.ID, "error", rerr)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}
		failCount = 0
		bw.Flush()
		select {
		case <-ctx.Done():
			bw.Flush()
			log.Info("stream ended", "channel", ch.ID, "quality", st.Quality,
				"duration", time.Since(start).String(), "segments", len(sent))
			return
		case <-time.After(2 * time.Second):
		}
	}
	log.Info("stream ended", "channel", ch.ID, "quality", st.Quality,
		"duration", time.Since(start).String(), "segments", len(sent))
}

// flushWriter flushes the underlying http.ResponseWriter after every Write so
// streaming responses reach the client incrementally rather than in one burst.
type flushWriter struct {
	w http.ResponseWriter
}

func (f *flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if flusher, ok := f.w.(http.Flusher); ok && n > 0 {
		flusher.Flush()
	}
	return n, err
}

// streamTSWindow fetches the current live media playlist once and streams any
// segments not yet sent, concatenated as raw MPEG-TS. Segments are shipped in
// playlist order. A single segment fetch error is logged and skipped — rather
// than aborting the whole pass and triggering a backoff retry — so a stale
// segment at the front of the sliding live window no longer stalls playback.
// The pass only fails if no segment could be fetched, so serveRawTS can retry
// after backoff.
func (s *Server) streamTSWindow(ctx context.Context, st *f1net.Stream, sent map[string]bool, w io.Writer) error {
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
	count := mp.Count()
	if count == 0 {
		return nil
	}
	shipped := 0
	for i := uint(0); i < count; i++ {
		seg := mp.Segments[i]
		if sent[seg.URI] {
			continue
		}
		segURL := hlsproxy.ResolveUpstream(st.PlaylistURL, seg.URI)
		segUp, err := s.upstream.Fetch(ctx, st.Headers, segURL, "")
		if err != nil {
			s.logger(ctx).Warn("ts segment", "error", err)
			continue
		}
		_, copyErr := io.Copy(w, segUp.Body)
		segUp.Body.Close()
		if copyErr != nil {
			return copyErr
		}
		sent[seg.URI] = true
		shipped++
	}
	if shipped == 0 {
		return fmt.Errorf("no live segments fetched")
	}
	return nil
}

// handleFetch is the generic upstream forwarder: it fetches a proxied URI
// (segment or nested playlist) with the channel's headers and streams the
// result back.
func (s *Server) handleFetch(w http.ResponseWriter, r *http.Request) {
	upstream := r.URL.Query().Get("u")
	if upstream == "" {
		http.Error(w, "missing u", http.StatusBadRequest)
		return
	}

	st, err := s.registry.Resolve(r.Context(), s.ch)
	if err != nil || st == nil {
		http.Error(w, "stream offline", http.StatusServiceUnavailable)
		return
	}

	up, err := s.upstream.Fetch(r.Context(), st.Headers, upstream, r.Header.Get("Range"))
	if err != nil {
		s.logger(r.Context()).Warn("fetch upstream", "channel", s.ch.ID, "error", err)
		http.Error(w, "upstream unreachable", http.StatusBadGateway)
		return
	}
	defer up.Body.Close()
	s.serveUpstream(w, r, up, upstream)
}

// serveUpstream forwards an upstream response to the client, rewriting it if
// it is an HLS playlist and streaming it raw (with Content-Type, Range and
// status preserved) if it is a media segment.
func (s *Server) serveUpstream(w http.ResponseWriter, r *http.Request, up *hlsproxy.Response, upstreamBase string) {
	br := bufio.NewReader(up.Body)
	prefix, _ := br.Peek(16)

	if up.IsPlaylist(prefix) {
		content, err := io.ReadAll(io.LimitReader(br, hlsproxy.MaxPlaylistBytes))
		if err != nil {
			s.logger(r.Context()).Warn("read playlist", "channel", s.ch.ID, "error", err)
			http.Error(w, "read upstream", http.StatusBadGateway)
			return
		}
		rewritten := hlsproxy.RewritePlaylist(content, upstreamBase, s.publicBase(r))
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
