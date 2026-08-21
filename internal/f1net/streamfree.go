package f1net

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"time"

	"golang.org/x/net/html"
)

// streamfreeResolver resolves https://streamfree.top/embed/{...}/{key}
// embeds. The player page builds its m3u8 from two JSON endpoints plus a
// per-quality token map (_0x) embedded in the page source:
//
//	GET /api/stream-status/{key}      -> {"qualities":{"720p":true,"1080p":true,...}}
//	GET /get-stream-key/{key}         -> {"server_name":"origin","is_external":false,...}
//	GET /live/skyf1{quality}/index.m3u8?_t={t}&_e={e}&_n={n}   (Referer/Origin gated)
type streamfreeResolver struct{}

var (
	// tokensRe matches the _0x token map, e.g.
	// const _0x = {"1080p": {"_e": 1786778988, "_n": "...", "_t": "..."}, ...};
	tokensRe = regexp.MustCompile(`(?s)const\s+_0x\s*=\s*(\{.*?\})\s*;`)
	// qualityPref matches the player's own auto-quality preference order.
	qualityPref = []string{"720p", "1080p", "2160p", "540p"}
)

// qualityToken is a single entry of the embed's _0x map.
type qualityToken struct {
	Expires int64  `json:"_e"`
	Nonce   string `json:"_n"`
	Token   string `json:"_t"`
}

// streamStatus is the /api/stream-status/{key} payload.
type streamStatus struct {
	StreamKey string          `json:"stream_key"`
	Available bool            `json:"available"`
	Qualities map[string]bool `json:"qualities"`
}

// streamKeyResponse is the /get-stream-key/{key} payload.
type streamKeyResponse struct {
	StreamKey    string `json:"stream_key"`
	IsExternal   bool   `json:"is_external"`
	ExternalURL  string `json:"external_url"`
	ServerName   string `json:"server_name"`
	ServerDomain string `json:"server_domain"`
}

func (streamfreeResolver) resolve(ctx context.Context, c *Client, src Source, u *url.URL, quality string) (*Stream, error) {
	origin := u.Scheme + "://" + u.Host
	key := streamKey(u)

	// External streams point straight at a URL and need no token building.
	sk, err := fetchStreamKey(ctx, c, origin, key)
	if err != nil {
		return nil, err
	}
	if sk.IsExternal && sk.ExternalURL != "" {
		return &Stream{
			Name:        src.Name,
			Source:      src.URL,
			PlaylistURL: sk.ExternalURL,
			Quality:     "external",
			Headers:     playbackHeaders(c, src.URL, origin),
		}, nil
	}

	status, err := fetchStreamStatus(ctx, c, origin, key)
	if err != nil {
		return nil, err
	}
	q, err := pickQuality(quality, status.Qualities)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrStreamOffline, err)
	}

	tokens, err := fetchTokens(ctx, c, c.http(), c.userAgent(), src.URL)
	if err != nil {
		return nil, fmt.Errorf("%w: extract tokens: %v", ErrStreamOffline, err)
	}
	t, ok := tokens[q]
	if !ok {
		return nil, fmt.Errorf("%w: no token for quality %s", ErrStreamOffline, q)
	}
	if tokenExpired(t) {
		return nil, fmt.Errorf("%w: %w: token for quality %s expired at %s",
			ErrStreamOffline, ErrTokenExpired, q, time.Unix(t.Expires, 0).UTC())
	}

	playlist := origin + streamPath(sk.ServerName, key, q)
	if t.Token != "" {
		playlist += fmt.Sprintf("?_t=%s&_e=%d&_n=%s", t.Token, t.Expires, t.Nonce)
	}

	return &Stream{
		Name:        src.Name,
		Source:      src.URL,
		PlaylistURL: playlist,
		Quality:     q,
		Headers:     playbackHeaders(c, src.URL, origin),
	}, nil
}

// tokenExpired reports whether the token's epoch expiry has passed. Tokens
// with no expiry (_e = 0) are never considered expired.
func tokenExpired(t qualityToken) bool {
	return t.Expires != 0 && time.Now().Unix() >= t.Expires
}

// streamKey extracts the stream key from an embed URL like
// /embed/racing/skyf1 -> "skyf1".
func streamKey(u *url.URL) string {
	return path.Base(u.Path)
}

// streamPath builds the m3u8 path. The origin serves playlists from /live;
// a CDN front (server_name != "origin") rewrites segment URLs in the
// playlist, so /live-cdn is used instead.
func streamPath(serverName, key, quality string) string {
	dir := "/live"
	if serverName != "" && serverName != "origin" {
		dir = "/live-cdn"
	}
	return dir + "/" + key + quality + "/index.m3u8"
}

// pickQuality resolves the requested quality against the ones that are live.
func pickQuality(requested string, available map[string]bool) (string, error) {
	if len(available) == 0 {
		return "", fmt.Errorf("no qualities reported")
	}
	if requested != "" {
		if available[requested] {
			return requested, nil
		}
		return "", fmt.Errorf("quality %s unavailable", requested)
	}
	for _, q := range qualityPref {
		if available[q] {
			return q, nil
		}
	}
	// Fall back to whatever the status reports as on.
	for q, on := range available {
		if on {
			return q, nil
		}
	}
	return "", fmt.Errorf("no live quality")
}

// playbackHeaders returns the headers required to fetch the playlist and its
// segments: the embed page as Referer, the streamfree origin, and a UA.
func playbackHeaders(c *Client, referer, origin string) http.Header {
	h := http.Header{}
	h.Set("Referer", referer)
	h.Set("Origin", origin)
	h.Set("User-Agent", c.userAgent())
	return h
}

func fetchStreamKey(ctx context.Context, c *Client, origin, key string) (*streamKeyResponse, error) {
	var out streamKeyResponse
	if err := getJSON(ctx, c, origin+"/get-stream-key/"+url.PathEscape(key), &out); err != nil {
		return nil, fmt.Errorf("get-stream-key: %w", err)
	}
	return &out, nil
}

func fetchStreamStatus(ctx context.Context, c *Client, origin, key string) (*streamStatus, error) {
	var out streamStatus
	if err := getJSON(ctx, c, origin+"/api/stream-status/"+url.PathEscape(key), &out); err != nil {
		return nil, fmt.Errorf("stream-status: %w", err)
	}
	return &out, nil
}

// fetchTokens extracts the _0x token map from the embed page's <script>
// blocks. It returns an error if the page cannot be fetched or does not
// contain the map (e.g. page changed); callers must not fall back to
// hardcoded tokens, which silently rot and get rejected upstream.
func fetchTokens(ctx context.Context, c *Client, hc *http.Client, ua, embedURL string) (map[string]qualityToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, embedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)

	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	for _, script := range scriptBodies(body) {
		m := tokensRe.FindSubmatch(script)
		if m == nil {
			continue
		}
		var out map[string]qualityToken
		if err := json.Unmarshal(m[1], &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	return nil, fmt.Errorf("token map not found in embed page")
}

// scriptBodies returns the text content of every <script> element in the page.
func scriptBodies(body []byte) [][]byte {
	z := html.NewTokenizer(bytes.NewReader(body))
	var out [][]byte
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return out
		case html.StartTagToken, html.SelfClosingTagToken:
			if name, _ := z.TagName(); string(name) == "script" {
				tt := z.Next()
				if tt == html.TextToken {
					out = append(out, z.Text())
				}
			}
		}
	}
}

// getJSON fetches and decodes a JSON endpoint using the client's UA.
func getJSON(ctx context.Context, c *Client, endpoint string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent())

	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: unexpected status %s", endpoint, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
