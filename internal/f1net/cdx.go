package f1net

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"f1-jf/internal/httpx"
)

// cdxResolver resolves https://cdx-08192.website/embed/{channel} embeds.
//
// The embed page hides its stream inside a single obfuscated <script>. The
// script is a byte array that is XOR'd with one constant and shifted by a
// second, then eval'd; the decoded source calls jwplayer.setup({file:
// "<signed-m3u8>", ...}). The signed m3u8 lives on a CDN origin
// (volder.timst.cfd) and carries its own expiry in the path, so the page is
// re-scraped each resolve rather than caching the URL.
type cdxResolver struct{}

// cdxScriptRe matches the obfuscated payload:
//
//	var _ARR=[...],_XK=<n>,_SK=<n>,_S="",_V;for(_V=0;...){_S+=String.fromCharCode(((_ARR[_V]^_XK)-_SK+256)%256);}
//
// Group 1 is the array variable name, group 2 the byte array, group 3 the XOR
// key, group 4 the subtract key, and group 5 the array reference used inside
// the loop (used to sanity check the variable names line up).
var cdxScriptRe = regexp.MustCompile(
	`var\s+(\w+)=\[(.*?)\],\w+=(\d+),\w+=(\d+),\w+="",\w+;for\(\w+=0;` +
		`\w+<\w+\.length;\w+\+\+\)\{\w+\+=String\.fromCharCode\(\(\((\w+)\[\w+\]\^\w+\)-\w+\+256\)%256\)`)

// m3u8Re matches the signed playlist URL in the decoded player source.
var m3u8Re = regexp.MustCompile(`https?://[^"'\s]+\.m3u8`)

func (cdxResolver) resolve(ctx context.Context, c *Client, src Source, u *url.URL, _ string) (*Stream, error) {
	body, err := c.fetchEmbed(ctx, src.URL)
	if err != nil {
		c.log(ctx).Warn("cdx embed fetch failed", "source", src.Name, "error", err)
		return nil, fmt.Errorf("%w: %v", ErrStreamOffline, err)
	}

	playlist, err := decodeCDX(body)
	if err != nil {
		c.log(ctx).Warn("cdx decode failed", "source", src.Name, "error", err)
		return nil, fmt.Errorf("%w: %v", ErrStreamOffline, err)
	}

	return &Stream{
		Name:        src.Name,
		Source:      src.URL,
		PlaylistURL: playlist,
		Quality:     "auto",
		Headers:     playbackHeaders(c, src.URL, u.Scheme+"://"+u.Host),
	}, nil
}

// fetchEmbed GETs the embed page and returns its body, capped at the same
// limit the streamfree resolver uses.
func (c *Client) fetchEmbed(ctx context.Context, embedURL string) ([]byte, error) {
	return httpx.Get(ctx, c.http(), embedURL, c.userAgent(), 1<<20)
}

// decodeCDX extracts the signed m3u8 URL from the embed page's obfuscated
// script. It returns an error if the script is missing or does not decode to
// a playable URL; callers must not fall back to hardcoded URLs, which rot
// when the embedded expiry passes.
func decodeCDX(body []byte) (string, error) {
	m := cdxScriptRe.FindSubmatch(body)
	if m == nil {
		return "", fmt.Errorf("obfuscated script not found in embed page")
	}
	arrayName := string(m[1])
	loopName := string(m[5])
	if loopName != arrayName {
		return "", fmt.Errorf("script variable mismatch: %q vs %q", arrayName, loopName)
	}
	xorKey, err := strconv.Atoi(string(m[3]))
	if err != nil {
		return "", fmt.Errorf("bad xor key: %w", err)
	}
	subKey, err := strconv.Atoi(string(m[4]))
	if err != nil {
		return "", fmt.Errorf("bad subtract key: %w", err)
	}

	nums, err := parseByteArray(m[2])
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, n := range nums {
		sb.WriteByte(byte(((n ^ xorKey) - subKey + 256) % 256))
	}

	p := m3u8Re.FindString(sb.String())
	if p == "" {
		return "", fmt.Errorf("no m3u8 found in decoded player source")
	}
	return p, nil
}

// parseByteArray decodes a comma/space separated list of byte literals.
func parseByteArray(b []byte) ([]int, error) {
	parts := strings.FieldsFunc(string(b), func(r rune) bool { return r == ',' || r == ' ' })
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("bad byte literal %q: %w", p, err)
		}
		out = append(out, n)
	}
	return out, nil
}
