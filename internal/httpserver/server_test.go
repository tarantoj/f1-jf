package httpserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	f1net "f1-jf/internal/f1net"
	"f1-jf/internal/hlsproxy"
	"f1-jf/internal/iptv"
)

// fakeResolver returns predetermined streams/errors per quality.
type fakeResolver struct {
	streams map[string]*f1net.Stream
	errs    map[string]error
}

func (f *fakeResolver) ResolveStream(_ context.Context, _ f1net.Source, quality string) (*f1net.Stream, error) {
	if err := f.errs[quality]; err != nil {
		return nil, err
	}
	if st := f.streams[quality]; st != nil {
		return st, nil
	}
	return nil, errors.New("no stream")
}

// gateHeaders is what the upstream requires; mirroring the real streamfree
// embed (Referer + Origin + UA).
func gateHeaders() http.Header {
	h := http.Header{}
	h.Set("Referer", "https://streamfree.top/embed/racing/skyf1")
	h.Set("Origin", "https://streamfree.top")
	h.Set("User-Agent", "f1-test-agent")
	return h
}

func hasGate(r *http.Request) bool {
	return r.Header.Get("Referer") != "" &&
		r.Header.Get("Origin") != "" &&
		r.Header.Get("User-Agent") == "f1-test-agent"
}

// upstreamOpts configures the mock upstream's segment behaviour.
type upstreamOpts struct {
	// failSegment makes the named segment (e.g. "1.js") return a 404.
	failSegment string
	// slowSegments makes each segment stream in chunks with a delay between
	// them, exercising incremental flushing.
	slowSegments bool
}

// newUpstream serves a mock streamfree-like source and records whether the
// required gate headers were seen on playlist and segment requests. An
// optional upstreamOpts tweaks segment behaviour for streaming tests.
func newUpstream(t *testing.T, opts ...upstreamOpts) *httptest.Server {
	t.Helper()
	var o upstreamOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	mux := http.NewServeMux()
	seg := []byte("SEG1-AAAAAAAAAA-BYTES")
	slowChunk := []byte(strings.Repeat("S", 64<<10))

	playlist := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-MEDIA-SEQUENCE:10
#EXT-X-TARGETDURATION:6
#EXTINF:5.760,
1.js
#EXTINF:5.760,
2.js
`
	mux.HandleFunc("/live/skyf11080p/index.m3u8", func(w http.ResponseWriter, r *http.Request) {
		if !hasGate(r) {
			http.Error(w, "missing gate headers", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		io.WriteString(w, playlist)
	})
	mux.HandleFunc("/live/skyf1720p/index.m3u8", func(w http.ResponseWriter, r *http.Request) {
		if !hasGate(r) {
			http.Error(w, "missing gate headers", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		io.WriteString(w, playlist)
	})
	segHandler := func(w http.ResponseWriter, r *http.Request) {
		if !hasGate(r) {
			http.Error(w, "missing gate headers", http.StatusForbidden)
			return
		}
		if o.failSegment != "" && strings.HasSuffix(r.URL.Path, o.failSegment) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		if rg := r.Header.Get("Range"); rg != "" {
			start, end, ok := parseRange(t, rg, len(seg))
			if !ok {
				http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(seg)))
			w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(seg[start : end+1])
			return
		}
		if o.slowSegments {
			// Stream in many chunks with a small delay between each so the
			// client can observe bytes arriving before the segment completes.
			flusher, _ := w.(http.Flusher)
			for i := 0; i < 4; i++ {
				if _, err := w.Write(slowChunk); err != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
				time.Sleep(100 * time.Millisecond)
			}
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(seg)))
		w.Write(seg)
	}
	mux.HandleFunc("/live/skyf11080p/1.js", segHandler)
	mux.HandleFunc("/live/skyf11080p/2.js", segHandler)
	mux.HandleFunc("/live/skyf1720p/1.js", segHandler)
	mux.HandleFunc("/live/skyf1720p/2.js", segHandler)
	return httptest.NewServer(mux)
}

func parseRange(t *testing.T, rg string, size int) (int, int, bool) {
	t.Helper()
	const p = "bytes="
	if !strings.HasPrefix(rg, p) {
		return 0, 0, false
	}
	parts := strings.SplitN(strings.TrimPrefix(rg, p), "-", 2)
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	end := size - 1
	if parts[1] != "" {
		end, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, false
		}
	}
	if start < 0 || end >= size || end < start {
		return 0, 0, false
	}
	return start, end, true
}

// newServer wires the service to a fake resolver and returns both the test
// HTTP server and the upstream mock. build receives the upstream mock so
// streams can be wired to it; errs makes the given qualities fail to resolve.
// An optional EPGRenderer enables the guide endpoint.
func newServer(t *testing.T, build func(up *httptest.Server) map[string]*f1net.Stream, errs map[string]error, epg ...EPGRenderer) (*httptest.Server, *httptest.Server) {
	return newServerWith(t, build, errs, upstreamOpts{}, epg...)
}

// newServerWith is newServer but with control over the mock upstream's
// segment behaviour (see upstreamOpts).
func newServerWith(t *testing.T, build func(up *httptest.Server) map[string]*f1net.Stream, errs map[string]error, uo upstreamOpts, epg ...EPGRenderer) (*httptest.Server, *httptest.Server) {
	return newServerWithLogger(t, slog.New(slog.NewTextHandler(io.Discard, nil)), build, errs, uo, epg...)
}

// newServerWithLogger is newServerWith but with an explicit application logger
// so tests can capture emitted log lines.
func newServerWithLogger(t *testing.T, logger *slog.Logger, build func(up *httptest.Server) map[string]*f1net.Stream, errs map[string]error, uo upstreamOpts, epg ...EPGRenderer) (*httptest.Server, *httptest.Server) {
	t.Helper()
	up := newUpstream(t, uo)
	var streams map[string]*f1net.Stream
	if build != nil {
		streams = build(up)
	}
	reg := iptv.NewRegistry(&fakeResolver{streams: streams, errs: errs}, 0)
	channels := []*iptv.Channel{
		{ID: "f1-1080p", Name: "F1 1080p", Group: "Sports", Quality: "1080p"},
		{ID: "f1-720p", Name: "F1 720p", Group: "Sports", Quality: "720p"},
	}
	opts := Options{Logger: logger, Upstream: hlsproxy.NewClient(up.Client())}
	if len(epg) > 0 {
		opts.EPG = epg[0]
	}
	srv := New(reg, channels, opts)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	t.Cleanup(up.Close)
	return ts, up
}

// fakeEPG is a canned EPGRenderer.
type fakeEPG struct {
	doc []byte
	err error
}

func (f *fakeEPG) RenderXML(_ context.Context, _ []*iptv.Channel) ([]byte, error) {
	return f.doc, f.err
}

func streamFor(up *httptest.Server, quality string) *f1net.Stream {
	return &f1net.Stream{
		Name:        "F1 " + quality,
		Source:      "https://streamfree.top/embed/racing/skyf1",
		PlaylistURL: up.URL + "/live/skyf1" + quality + "/index.m3u8",
		Quality:     quality,
		Headers:     gateHeaders(),
	}
}

func TestHealth(t *testing.T) {
	ts, _ := newServer(t, nil, nil)
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestReady(t *testing.T) {
	ts, _ := newServer(t, nil, nil)
	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestPlaylist(t *testing.T) {
	ts, _ := newServer(t, func(up *httptest.Server) map[string]*f1net.Stream {
		return map[string]*f1net.Stream{"1080p": streamFor(up, "1080p"), "720p": streamFor(up, "720p")}
	}, nil)

	resp, err := http.Get(ts.URL + "/iptv/playlist.m3u")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "mpegurl") {
		t.Errorf("content-type = %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !strings.HasPrefix(text, "#EXTM3U") {
		t.Fatalf("not an m3u:\n%s", text)
	}
	for _, want := range []string{
		`tvg-id="f1-1080p" tvg-name="F1 1080p" group-title="Sports",F1 1080p`,
		ts.URL + "/iptv/stream/f1-1080p.ts",
		`tvg-id="f1-720p" tvg-name="F1 720p" group-title="Sports",F1 720p`,
		ts.URL + "/iptv/stream/f1-720p.ts",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("playlist missing %q:\n%s", want, text)
		}
	}
}

func TestPlaylistOmitsOffline(t *testing.T) {
	ts, _ := newServer(t, func(up *httptest.Server) map[string]*f1net.Stream {
		return map[string]*f1net.Stream{"1080p": streamFor(up, "1080p")}
	}, map[string]error{"720p": errors.New("offline")})

	resp, err := http.Get(ts.URL + "/iptv/playlist.m3u")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "f1-720p") {
		t.Errorf("offline channel listed:\n%s", body)
	}
	if !strings.Contains(string(body), "f1-1080p") {
		t.Errorf("live channel missing:\n%s", body)
	}
}

func TestStreamPlaylistRewrite(t *testing.T) {
	ts, up := newServer(t, func(up *httptest.Server) map[string]*f1net.Stream {
		return map[string]*f1net.Stream{"1080p": streamFor(up, "1080p")}
	}, nil)

	resp, err := http.Get(ts.URL + "/iptv/stream/f1-1080p")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "mpegurl") {
		t.Errorf("content-type = %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	wantSeg := ts.URL + "/iptv/f/f1-1080p/stream.ts?u=" + url.QueryEscape(up.URL+"/live/skyf11080p/1.js")
	if !strings.Contains(text, wantSeg) {
		t.Errorf("segment 1 not rewritten to proxy:\n%s", text)
	}
	if strings.Contains(text, "\n1.js\n") {
		t.Errorf("raw segment line present:\n%s", text)
	}
}

func TestSegmentPassthrough(t *testing.T) {
	ts, up := newServer(t, func(up *httptest.Server) map[string]*f1net.Stream {
		return map[string]*f1net.Stream{"1080p": streamFor(up, "1080p")}
	}, nil)

	segURL := ts.URL + "/iptv/f/f1-1080p/stream.ts?u=" + url.QueryEscape(up.URL+"/live/skyf11080p/2.js")
	resp, err := http.Get(segURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/javascript" {
		t.Errorf("content-type = %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.HasPrefix(string(body), "SEG1-") {
		t.Errorf("segment body = %q", body)
	}
}

func TestSegmentRange(t *testing.T) {
	ts, up := newServer(t, func(up *httptest.Server) map[string]*f1net.Stream {
		return map[string]*f1net.Stream{"1080p": streamFor(up, "1080p")}
	}, nil)

	segURL := ts.URL + "/iptv/f/f1-1080p/stream.ts?u=" + url.QueryEscape(up.URL+"/live/skyf11080p/1.js")
	req, _ := http.NewRequest(http.MethodGet, segURL, nil)
	req.Header.Set("Range", "bytes=0-3")
	resp, err := up.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "SEG1" {
		t.Errorf("range body = %q", body)
	}
	if resp.Header.Get("Content-Range") == "" {
		t.Error("missing Content-Range")
	}
}

func TestUnknownChannel(t *testing.T) {
	ts, _ := newServer(t, nil, nil)
	resp, err := http.Get(ts.URL + "/iptv/stream/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestOfflineChannelStream(t *testing.T) {
	ts, _ := newServer(t, nil, map[string]error{"1080p": errors.New("offline")})
	resp, err := http.Get(ts.URL + "/iptv/stream/f1-1080p")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestGuide(t *testing.T) {
	doc := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<tv generator-info-name="f1-jf"><channel id="f1-1080p"/></tv>
`)
	ts, _ := newServer(t, nil, nil, &fakeEPG{doc: doc})

	resp, err := http.Get(ts.URL + "/iptv/guide.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/xml") {
		t.Errorf("content-type = %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `<channel id="f1-1080p"/>`) {
		t.Errorf("guide body = %q", body)
	}
}

func TestGuideDisabled(t *testing.T) {
	ts, _ := newServer(t, nil, nil)
	resp, err := http.Get(ts.URL + "/iptv/guide.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestGuideError(t *testing.T) {
	ts, _ := newServer(t, nil, nil, &fakeEPG{err: errors.New("unreachable")})
	resp, err := http.Get(ts.URL + "/iptv/guide.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestRawTSPassthrough(t *testing.T) {
	ts, up := newServer(t, func(up *httptest.Server) map[string]*f1net.Stream {
		return map[string]*f1net.Stream{"1080p": streamFor(up, "1080p")}
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/iptv/stream/f1-1080p.ts", nil)
	resp, err := up.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/mp2t" {
		t.Errorf("content-type = %q, want video/mp2t", ct)
	}
	// The mock serves two identical segments; expect both, concatenated.
	want := strings.Repeat("SEG1-AAAAAAAAAA-BYTES", 2)
	buf := make([]byte, len(want))
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatalf("read ts stream: %v", err)
	}
	if string(buf) != want {
		t.Errorf("ts body = %q, want %q", buf, want)
	}
}

// TestRawTSFlushesIncrementally verifies a .ts stream delivers bytes as they
// arrive rather than waiting for a whole (slow) segment to download first.
// The mock streams each segment in 10 chunks over ~2.5s; the client must
// observe its first bytes well before the segment completes.
func TestRawTSFlushesIncrementally(t *testing.T) {
	ts, up := newServerWith(t, func(up *httptest.Server) map[string]*f1net.Stream {
		return map[string]*f1net.Stream{"1080p": streamFor(up, "1080p")}
	}, nil, upstreamOpts{slowSegments: true})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/iptv/stream/f1-1080p.ts", nil)
	resp, err := up.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}

	// A full segment takes ~2.5s to stream. The first chunk must arrive
	// promptly; if the proxy buffered the entire segment before flushing the
	// first read would block for the full duration. Allow generous margin.
	start := time.Now()
	first := make([]byte, 1)
	if _, err := resp.Body.Read(first); err != nil {
		t.Fatalf("first read: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Errorf("first bytes took %v, want < 1.5s (incremental flush)", elapsed)
	}
}

// TestRawTSSkipsStaleSegment verifies a stale segment at the front of the
// live window (which 404s upstream) no longer stalls playback: the fresher
// segments must still be delivered.
func TestRawTSSkipsStaleSegment(t *testing.T) {
	ts, up := newServerWith(t, func(up *httptest.Server) map[string]*f1net.Stream {
		return map[string]*f1net.Stream{"1080p": streamFor(up, "1080p")}
	}, nil, upstreamOpts{failSegment: "1.js"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/iptv/stream/f1-1080p.ts", nil)
	resp, err := up.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}

	// Segment 1 (1.js) is stale and 404s; segment 2 (2.js) must still be
	// streamed. Read enough bytes to confirm the surviving segment arrived.
	buf := make([]byte, len("SEG1-AAAAAAAAAA-BYTES"))
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatalf("read ts stream: %v", err)
	}
	if string(buf) != "SEG1-AAAAAAAAAA-BYTES" {
		t.Errorf("ts body = %q", buf)
	}
}

func TestRawTSUnknownOffline(t *testing.T) {
	ts, _ := newServer(t, nil, map[string]error{"1080p": errors.New("offline")})
	resp, err := http.Get(ts.URL + "/iptv/stream/f1-1080p.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestRawTSUnknownChannel(t *testing.T) {
	ts, _ := newServer(t, nil, nil)
	resp, err := http.Get(ts.URL + "/iptv/stream/nope.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// bufferSink is a thread-safe log sink used to capture structured output.
type bufferSink struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *bufferSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *bufferSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestRequestIDHonorsInboundHeader verifies that an inbound X-Request-ID is
// honored and attached to the request's structured log output.
func TestRequestIDHonorsInboundHeader(t *testing.T) {
	sink := &bufferSink{}
	logger := slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ts, _ := newServerWithLogger(t, logger, nil, nil, upstreamOpts{})

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/healthz", nil)
	req.Header.Set("X-Request-ID", "test-request-123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	// The request logger line may be written after the response completes;
	// poll briefly for it.
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(sink.String(), "request_id=test-request-123") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(sink.String(), "request_id=test-request-123") {
		t.Errorf("log output missing correlated request_id:\n%s", sink.String())
	}
}

// TestStreamStartEndLogged verifies the raw TS streaming route emits structured
// stream started/ended log lines (since the request middleware cannot log an
// indefinite stream on completion).
func TestStreamStartEndLogged(t *testing.T) {
	sink := &bufferSink{}
	logger := slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ts, up := newServerWithLogger(t, logger, func(up *httptest.Server) map[string]*f1net.Stream {
		return map[string]*f1net.Stream{"1080p": streamFor(up, "1080p")}
	}, nil, upstreamOpts{slowSegments: true})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/iptv/stream/f1-1080p.ts", nil)
	resp, err := up.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	// Read a little then abort so the stream ends.
	buf := make([]byte, 64)
	if _, err := resp.Body.Read(buf); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	cancel()

	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(sink.String(), "stream ended") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	out := sink.String()
	if !strings.Contains(out, "level=INFO") || !strings.Contains(out, "stream started") {
		t.Errorf("missing stream started log:\n%s", out)
	}
	if !strings.Contains(out, "stream ended") {
		t.Errorf("missing stream ended log:\n%s", out)
	}
	if !strings.Contains(out, "channel=f1-1080p") {
		t.Errorf("stream logs missing channel:\n%s", out)
	}
}
