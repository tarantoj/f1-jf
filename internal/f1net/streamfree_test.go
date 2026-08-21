package f1net

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// farFutureExpiry is an epoch well beyond "now" so fixture tokens are never
// treated as expired by the resolver.
const farFutureExpiry = int64(4102444800) // 2100-01-01

// testServer serves a fake streamfree embed with a distinct _0x token map so
// the tests prove the page is actually scraped rather than using a fallback.
func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/embed/racing/skyf1", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `
<!doctype html><html><body>
<script>
const _0x = {"540p": {"_e": %d, "_n": "n540", "_t": "t540"},
             "720p": {"_e": %d, "_n": "n720", "_t": "t720"},
             "1080p": {"_e": %d, "_n": "n1080", "_t": "t1080"},
             "2160p": {"_e": %d, "_n": "n2160", "_t": "t2160"}};
</script>
</body></html>`, farFutureExpiry, farFutureExpiry, farFutureExpiry, farFutureExpiry)
	})

	mux.HandleFunc("/api/stream-status/skyf1", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"stream_key":"skyf1","available":true,"qualities":{"540p":false,"720p":true,"1080p":true,"2160p":false}}`)
	})

	mux.HandleFunc("/get-stream-key/skyf1", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"stream_key":"skyf1","is_external":false,"server_name":"origin","server_domain":""}`)
	})

	mux.HandleFunc("/live/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("_t") != "t1080" {
			http.Error(w, "bad token", http.StatusForbidden)
			return
		}
		if r.Header.Get("Referer") == "" || r.Header.Get("Origin") == "" {
			http.Error(w, "missing referrer", http.StatusForbidden)
			return
		}
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXTINF:5.760,\nseg.js\n")
	})

	return httptest.NewServer(mux)
}

func TestStreamfreeResolve(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client()}
	u := mustParse(t, srv.URL+"/embed/racing/skyf1")
	src := Source{Name: "Stream 2", URL: srv.URL + "/embed/racing/skyf1?quality=720p&category=racing"}

	st, err := (streamfreeResolver{}).resolve(context.Background(), c, src, u, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := fmt.Sprintf("%s/live/skyf1720p/index.m3u8?_t=t720&_e=%d&_n=n720", srv.URL, farFutureExpiry)
	if st.PlaylistURL != want {
		t.Errorf("playlist = %q, want %q", st.PlaylistURL, want)
	}
	if st.Quality != "720p" {
		t.Errorf("quality = %q, want 720p", st.Quality)
	}
	if st.Headers.Get("Referer") != src.URL {
		t.Errorf("Referer = %q", st.Headers.Get("Referer"))
	}
	if st.Headers.Get("Origin") != srv.URL {
		t.Errorf("Origin = %q", st.Headers.Get("Origin"))
	}
}

func TestStreamfreeResolveExplicitQuality(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client()}
	u := mustParse(t, srv.URL+"/embed/racing/skyf1")
	src := Source{Name: "Full HD", URL: srv.URL + "/embed/racing/skyf1"}

	st, err := (streamfreeResolver{}).resolve(context.Background(), c, src, u, "1080p")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := fmt.Sprintf("%s/live/skyf11080p/index.m3u8?_t=t1080&_e=%d&_n=n1080", srv.URL, farFutureExpiry)
	if st.PlaylistURL != want {
		t.Errorf("playlist = %q, want %q", st.PlaylistURL, want)
	}
}

func TestStreamfreeResolveExternal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get-stream-key/skyf1":
			fmt.Fprint(w, `{"stream_key":"skyf1","is_external":true,"external_url":"https://cdn.example/live.m3u8","server_name":"origin","server_domain":""}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client()}
	u := mustParse(t, srv.URL+"/embed/racing/skyf1")
	src := Source{Name: "Ext", URL: srv.URL + "/embed/racing/skyf1"}

	st, err := (streamfreeResolver{}).resolve(context.Background(), c, src, u, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if st.PlaylistURL != "https://cdn.example/live.m3u8" {
		t.Errorf("playlist = %q", st.PlaylistURL)
	}
}

func TestStreamfreeResolveUnavailableQuality(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get-stream-key/skyf1":
			fmt.Fprint(w, `{"stream_key":"skyf1","is_external":false,"server_name":"origin"}`)
		case "/api/stream-status/skyf1":
			fmt.Fprint(w, `{"qualities":{"540p":false,"720p":false,"1080p":false,"2160p":false}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client()}
	u := mustParse(t, srv.URL+"/embed/racing/skyf1")
	src := Source{Name: "Offline", URL: srv.URL + "/embed/racing/skyf1"}

	_, err := (streamfreeResolver{}).resolve(context.Background(), c, src, u, "")
	if err == nil {
		t.Fatal("expected ErrStreamOffline")
	}
	if !strings.Contains(err.Error(), ErrStreamOffline.Error()) {
		t.Fatalf("error = %v, want ErrStreamOffline", err)
	}
}

func TestStreamfreeResolveExpiredTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get-stream-key/skyf1":
			fmt.Fprint(w, `{"stream_key":"skyf1","is_external":false,"server_name":"origin"}`)
		case "/api/stream-status/skyf1":
			fmt.Fprint(w, `{"qualities":{"720p":true,"1080p":true}}`)
		case "/embed/racing/skyf1":
			// The _0x map is present but every token is already expired.
			fmt.Fprint(w, `
<script>
const _0x = {"720p": {"_e": 1, "_n": "n720", "_t": "t720"},
             "1080p": {"_e": 1, "_n": "n1080", "_t": "t1080"}};
</script>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client()}
	u := mustParse(t, srv.URL+"/embed/racing/skyf1")
	src := Source{Name: "Expired", URL: srv.URL + "/embed/racing/skyf1"}

	_, err := (streamfreeResolver{}).resolve(context.Background(), c, src, u, "")
	if err == nil {
		t.Fatal("expected error for expired tokens")
	}
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("error = %v, want ErrTokenExpired", err)
	}
}

func TestStreamfreeResolveScrapeFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get-stream-key/skyf1":
			fmt.Fprint(w, `{"stream_key":"skyf1","is_external":false,"server_name":"origin"}`)
		case "/api/stream-status/skyf1":
			fmt.Fprint(w, `{"qualities":{"720p":true,"1080p":true}}`)
		case "/embed/racing/skyf1":
			// Page present but token map missing: extraction must fail loudly
			// rather than fall back to hardcoded tokens.
			http.Error(w, "gone", http.StatusGone)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client()}
	u := mustParse(t, srv.URL+"/embed/racing/skyf1")
	src := Source{Name: "ScrapeFail", URL: srv.URL + "/embed/racing/skyf1"}

	_, err := (streamfreeResolver{}).resolve(context.Background(), c, src, u, "")
	if err == nil {
		t.Fatal("expected error when token scrape fails")
	}
	if !strings.Contains(err.Error(), ErrStreamOffline.Error()) {
		t.Fatalf("error = %v, want ErrStreamOffline", err)
	}
}

func TestResolveStreamUnsupportedHost(t *testing.T) {
	c := &Client{}
	src := Source{Name: "Bad", URL: "https://embdlol.st/embed/abc"}
	_, err := c.ResolveStream(context.Background(), src, "")
	if err == nil {
		t.Fatal("expected error for unsupported host")
	}
	if err != ErrUnsupportedHost && !strings.Contains(err.Error(), ErrUnsupportedHost.Error()) {
		t.Fatalf("error = %v, want ErrUnsupportedHost", err)
	}
}
