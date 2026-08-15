package f1net

import (
	"net/url"
	"strings"
	"testing"
)

func TestParseSources(t *testing.T) {
	in := `
Stream 1 | https://westreamf1.com/westreamf1.php
Full HD 1 | https://embdlol.st/embed/df03039a

  Italian  |  https://embdlol.st/embed/42838839
`
	got, err := parseSources(strings.NewReader(in))
	if err != nil {
		t.Fatalf("parseSources: %v", err)
	}
	want := []Source{
		{Name: "Stream 1", URL: "https://westreamf1.com/westreamf1.php"},
		{Name: "Full HD 1", URL: "https://embdlol.st/embed/df03039a"},
		{Name: "Italian", URL: "https://embdlol.st/embed/42838839"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d sources, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("source %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestParseSourcesInvalidLine(t *testing.T) {
	_, err := parseSources(strings.NewReader("no pipe here"))
	if err == nil {
		t.Fatal("expected error for line without '|'")
	}
}

func TestStreamKey(t *testing.T) {
	cases := map[string]string{
		"https://streamfree.top/embed/racing/skyf1?quality=720p&category=racing": "skyf1",
		"https://streamfree.top/embed/racing/skyf1":                              "skyf1",
	}
	for in, want := range cases {
		u := mustParse(t, in)
		if got := streamKey(u); got != want {
			t.Errorf("streamKey(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestPickQuality(t *testing.T) {
	avail := map[string]bool{"540p": false, "720p": true, "1080p": true, "2160p": false}

	if q, err := pickQuality("", avail); err != nil || q != "720p" {
		t.Errorf("auto = %q, %v; want 720p, nil", q, err)
	}
	if q, err := pickQuality("1080p", avail); err != nil || q != "1080p" {
		t.Errorf("explicit = %q, %v; want 1080p, nil", q, err)
	}
	if _, err := pickQuality("2160p", avail); err == nil {
		t.Error("expected error for unavailable quality")
	}
	if _, err := pickQuality("", map[string]bool{"540p": false}); err == nil {
		t.Error("expected error when nothing is live")
	}
}

func TestStreamPath(t *testing.T) {
	if got := streamPath("origin", "skyf1", "1080p"); got != "/live/skyf11080p/index.m3u8" {
		t.Errorf("origin path = %q", got)
	}
	if got := streamPath("edge", "skyf1", "1080p"); got != "/live-cdn/skyf11080p/index.m3u8" {
		t.Errorf("cdn path = %q", got)
	}
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%s): %v", raw, err)
	}
	return u
}
