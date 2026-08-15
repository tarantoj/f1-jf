package hlsproxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchSendsHeadersAndRange(t *testing.T) {
	var gotHeaders http.Header
	var gotRange string
	var gotAcceptEncoding string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		gotRange = r.Header.Get("Range")
		gotAcceptEncoding = r.Header.Get("Accept-Encoding")
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Content-Length", "4")
		io.WriteString(w, "SEG1")
	}))
	defer srv.Close()

	headers := http.Header{}
	headers.Set("Referer", "https://upstream.test/embed/racing/skyf1")
	headers.Set("Origin", "https://upstream.test")
	headers.Set("User-Agent", "test-agent")

	resp, err := NewClient(srv.Client()).Fetch(context.Background(), headers, srv.URL+"/seg.js", "bytes=0-2")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer resp.Body.Close()

	if gotHeaders.Get("Referer") != "https://upstream.test/embed/racing/skyf1" {
		t.Errorf("Referer = %q", gotHeaders.Get("Referer"))
	}
	if gotHeaders.Get("Origin") != "https://upstream.test" {
		t.Errorf("Origin = %q", gotHeaders.Get("Origin"))
	}
	if gotHeaders.Get("User-Agent") != "test-agent" {
		t.Errorf("User-Agent = %q", gotHeaders.Get("User-Agent"))
	}
	if gotRange != "bytes=0-2" {
		t.Errorf("Range = %q", gotRange)
	}
	if gotAcceptEncoding != "identity" {
		t.Errorf("Accept-Encoding = %q", gotAcceptEncoding)
	}
	if resp.Status != http.StatusOK {
		t.Errorf("status = %d", resp.Status)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "SEG1" {
		t.Errorf("body = %q", body)
	}
}

func TestFetchRejectsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "offline", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := NewClient(srv.Client()).Fetch(context.Background(), http.Header{}, srv.URL, "")
	if err == nil {
		t.Fatal("expected error for 503")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error = %v, want status mention", err)
	}
}

func TestResponseIsPlaylist(t *testing.T) {
	ok := &Response{Status: 200, Header: http.Header{"Content-Type": {"application/vnd.apple.mpegurl"}}}
	if !ok.IsPlaylist([]byte("anything")) {
		t.Error("mpegurl content-type should be a playlist")
	}
	sniffed := &Response{Status: 200, Header: http.Header{}}
	if !sniffed.IsPlaylist([]byte("#EXTM3U\n#EXTINF:4,\n1.js")) {
		t.Error("#EXTM3U prefix should be a playlist")
	}
	seg := &Response{Status: 200, Header: http.Header{"Content-Type": {"application/javascript"}}}
	if seg.IsPlaylist([]byte("SEG1")) {
		t.Error("javascript segment should not be a playlist")
	}
}

func TestSegmentContentType(t *testing.T) {
	if got := SegmentContentType("application/javascript"); got != "application/javascript" {
		t.Errorf("got %q", got)
	}
	if got := SegmentContentType(""); got != "video/mp2t" {
		t.Errorf("fallback = %q", got)
	}
}
