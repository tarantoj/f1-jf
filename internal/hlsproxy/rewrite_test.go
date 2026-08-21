package hlsproxy

import (
	"net/url"
	"strings"
	"testing"
)

const testPlaylist = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-MEDIA-SEQUENCE:10
#EXT-X-TARGETDURATION:6
#EXTINF:5.760,
10.js
#EXTINF:5.760,
https://cdn.example/seg/11.ts
#EXTINF:5.000,
/tmp/12.js
`

func TestRewritePlaylistRelative(t *testing.T) {
	got := string(RewritePlaylist([]byte(testPlaylist),
		"https://streamfree.top/live/skyf11080p/index.m3u8",
		"http://h:8080", "f1"))

	wantSeg := "http://h:8080/iptv/f/f1/stream.ts?u=" + url.QueryEscape("https://streamfree.top/live/skyf11080p/10.js")
	if !strings.Contains(got, wantSeg) {
		t.Errorf("relative segment not rewritten:\n%s", got)
	}

	wantAbs := "http://h:8080/iptv/f/f1/stream.ts?u=" + url.QueryEscape("https://cdn.example/seg/11.ts")
	if !strings.Contains(got, wantAbs) {
		t.Errorf("absolute segment not rewritten:\n%s", got)
	}

	wantRoot := "http://h:8080/iptv/f/f1/stream.ts?u=" + url.QueryEscape("https://streamfree.top/tmp/12.js")
	if !strings.Contains(got, wantRoot) {
		t.Errorf("root-relative segment not rewritten:\n%s", got)
	}

	if strings.Contains(got, "\n10.js\n") || strings.Contains(got, "\n11.ts\n") {
		t.Errorf("raw segment lines still present:\n%s", got)
	}
}

func TestRewritePlaylistKey(t *testing.T) {
	in := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"/keys/k1.bin\",IV=0x1\n#EXTINF:4,\n1.js\n"
	got := string(RewritePlaylist([]byte(in),
		"https://streamfree.top/live/skyf11080p/index.m3u8",
		"http://h:8080", "f1"))

	want := `URI="http://h:8080/iptv/f/f1/stream.ts?u=` + url.QueryEscape("https://streamfree.top/keys/k1.bin") + `"`
	if !strings.Contains(got, want) {
		t.Errorf("EXT-X-KEY URI not rewritten:\n%s", got)
	}
}

func TestRewritePlaylistExtensionAllowed(t *testing.T) {
	got := string(RewritePlaylist([]byte(testPlaylist),
		"https://streamfree.top/live/skyf11080p/index.m3u8",
		"http://h:8080", "f1"))

	// Every proxied segment must end with an allowed extension (.ts) in its
	// path so ffmpeg's HLS demuxer does not reject the obfuscated upstream
	// extension (.js).
	wantRel := "/iptv/f/f1/stream.ts?u=" + url.QueryEscape("https://streamfree.top/live/skyf11080p/10.js")
	wantRoot := "/iptv/f/f1/stream.ts?u=" + url.QueryEscape("https://streamfree.top/tmp/12.js")
	for _, want := range []string{wantRel, wantRoot} {
		if !strings.Contains(got, want) {
			t.Errorf("segment not proxied through .ts path:\n%s", got)
		}
	}
}

func TestResolveUpstream(t *testing.T) {
	const playlist = "https://streamfree.top/live-cdn/skyf11080p/index.m3u8"

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "relative",
			raw:  "18261.js",
			want: "https://streamfree.top/live-cdn/skyf11080p/18261.js",
		},
		{
			name: "root-relative",
			raw:  "/live/skyf11080p/18261.js",
			want: "https://streamfree.top/live/skyf11080p/18261.js",
		},
		{
			name: "absolute cdn url",
			raw:  "https://cdn1.streamfree.top/live/skyf11080p/18261.js",
			want: "https://cdn1.streamfree.top/live/skyf11080p/18261.js",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveUpstream(playlist, c.raw); got != c.want {
				t.Errorf("ResolveUpstream(%q, %q) = %q, want %q", playlist, c.raw, got, c.want)
			}
		})
	}
}

func TestRewritePlaylistNonPlaylist(t *testing.T) {
	in := []byte("not a playlist at all, just data")
	got := RewritePlaylist(in, "https://streamfree.top/live/x/index.m3u8", "http://h:8080", "c1")
	if string(got) != string(in) {
		t.Errorf("non-playlist content was rewritten: %q", got)
	}
}

func TestLooksLikePlaylist(t *testing.T) {
	cases := []struct {
		prefix []byte
		ct     string
		want   bool
	}{
		{[]byte("#EXTM3U"), "", true},
		{[]byte("#EXTM3U\n#EXTINF"), "text/plain", true},
		{[]byte("PK\x03\x04data"), "", false},
		{[]byte(""), "application/vnd.apple.mpegurl", true},
		{[]byte("x"), "application/x-mpegurl", true},
		{[]byte("x"), "application/javascript", false},
	}
	for _, c := range cases {
		if got := looksLikePlaylist(c.prefix, c.ct); got != c.want {
			t.Errorf("looksLikePlaylist(%q, %q) = %v, want %v", c.prefix, c.ct, got, c.want)
		}
	}
}
