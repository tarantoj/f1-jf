package f1net

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testCDXServer serves an embed page whose obfuscated script decodes to a
// player script carrying a distinct signed m3u8 URL, so the tests prove the
// page is actually scraped and decoded rather than hardcoded.
func testCDXServer(t *testing.T) *httptest.Server {
	t.Helper()
	playlist := "https://volder.example/main/secure/%s/%d/skysportsf1-uk.m3u8"
	decoded := fmt.Sprintf(`
function init() {
  var ID = document.getElementById('player').getAttribute('data-id');
  var SIGNED_URL = %q;
  player.setup({ file: SIGNED_URL, type: "hls" });
}`,
		fmt.Sprintf(playlist, strings.Repeat("ab", 32), 1787346789))

	obfuscated := obfuscate(t, decoded, 238, 72)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!doctype html><html><body>
<div id="player" data-id="skysportsf1-uk"></div>
<script>(function(){var _oq4=%s,_qj8=238,_tc5=72,_wu7="",_tv4;for(_tv4=0;_tv4<_oq4.length;_tv4++){_wu7+=String.fromCharCode(((_oq4[_tv4]^_qj8)-_tc5+256)%%256);}window["ev"+"al"](_wu7);})();</script>
</body></html>`, obfuscated)
	}))
}

// obfuscate encodes each byte of src into the script literal that decodeCDX
// reverses. decodeCDX computes ((n ^ xorKey) - subKey + 256) % 256, so the
// stored value is ((b + subKey) % 256) ^ xorKey.
func obfuscate(t *testing.T, src string, xorKey, subKey int) string {
	t.Helper()
	var nums []string
	for i := 0; i < len(src); i++ {
		b := int(src[i])
		nums = append(nums, fmt.Sprintf("%d", ((b+subKey)%256)^xorKey))
	}
	return "[" + strings.Join(nums, ",") + "]"
}

func TestCDXResolve(t *testing.T) {
	srv := testCDXServer(t)
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client()}
	u := mustParse(t, srv.URL+"/embed/skysportsf1-uk")
	src := Source{Name: "Full HD 1", URL: srv.URL + "/embed/skysportsf1-uk"}

	st, err := (cdxResolver{}).resolve(context.Background(), c, src, u, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := fmt.Sprintf("https://volder.example/main/secure/%s/1787346789/skysportsf1-uk.m3u8", strings.Repeat("ab", 32))
	if st.PlaylistURL != want {
		t.Errorf("playlist = %q, want %q", st.PlaylistURL, want)
	}
	if st.Quality != "auto" {
		t.Errorf("quality = %q, want auto", st.Quality)
	}
	if st.Headers.Get("Referer") != src.URL {
		t.Errorf("Referer = %q", st.Headers.Get("Referer"))
	}
	if st.Headers.Get("Origin") != srv.URL {
		t.Errorf("Origin = %q", st.Headers.Get("Origin"))
	}
}

func TestDecodeCDXMissingScript(t *testing.T) {
	if _, err := decodeCDX([]byte("<html><body>no script here</body></html>")); err == nil {
		t.Fatal("expected error when script is missing")
	}
}

func TestDecodeCDXNoM3U8(t *testing.T) {
	// Script decodes but produces no m3u8 URL.
	decoded := "function init() { console.log('no stream'); }"
	obf := obfuscate(t, decoded, 238, 72)
	body := fmt.Sprintf(`<html><script>(function(){var _oq4=%s,_qj8=238,_tc5=72,_wu7="",_tv4;for(_tv4=0;_tv4<_oq4.length;_tv4++){_wu7+=String.fromCharCode(((_oq4[_tv4]^_qj8)-_tc5+256)%%256);}window["ev"+"al"](_wu7);})();</script></html>`, obf)
	if _, err := decodeCDX([]byte(body)); err == nil {
		t.Fatal("expected error when no m3u8 is decoded")
	}
}

func TestCDXResolveEmbedFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusGone)
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client()}
	u := mustParse(t, srv.URL+"/embed/skysportsf1-uk")
	src := Source{Name: "Fail", URL: srv.URL + "/embed/skysportsf1-uk"}

	_, err := (cdxResolver{}).resolve(context.Background(), c, src, u, "")
	if err == nil {
		t.Fatal("expected error on embed fetch failure")
	}
	if !strings.Contains(err.Error(), ErrStreamOffline.Error()) {
		t.Fatalf("error = %v, want ErrStreamOffline", err)
	}
}

func TestParseByteArray(t *testing.T) {
	got, err := parseByteArray([]byte("1, 2,3, 4"))
	if err != nil {
		t.Fatalf("parseByteArray: %v", err)
	}
	want := []int{1, 2, 3, 4}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("byte %d = %d, want %d", i, got[i], want[i])
		}
	}
}
