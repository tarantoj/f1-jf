package hlsproxy

import (
	"bytes"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var uriAttrRe = regexp.MustCompile(`URI="([^"]*)"`)

// looksLikePlaylist reports whether a fetched resource is an HLS playlist
// rather than a media segment, based on the upstream content type and the
// first bytes of the body.
func looksLikePlaylist(prefix []byte, contentType string) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "mpegurl") || strings.Contains(ct, "m3u8") {
		return true
	}
	return bytes.HasPrefix(bytes.TrimSpace(prefix), []byte("#EXTM3U"))
}

// RewritePlaylist rewrites every URI reference in an HLS playlist so that it
// is fetched through the proxy at pubBase+/iptv/f/{channel}. Relative URIs
// resolve against upstreamBase (the URL the playlist was fetched from).
// Non-playlist content is returned unchanged.
func RewritePlaylist(content []byte, upstreamBase, pubBase, channel string) []byte {
	if !looksLikePlaylist(content[:min(len(content), 16)], "") {
		return content
	}
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case t == "":
			continue
		case strings.HasPrefix(t, "#"):
			if strings.Contains(line, "URI=") {
				lines[i] = rewriteURIAttr(line, upstreamBase, pubBase, channel)
			}
		default:
			lines[i] = rewriteURI(upstreamBase, pubBase, channel, t)
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// rewriteURIAttr rewrites URI="..." attributes inside playlist tags such as
// #EXT-X-KEY and #EXT-X-MEDIA.
func rewriteURIAttr(line, upstreamBase, pubBase, channel string) string {
	return uriAttrRe.ReplaceAllStringFunc(line, func(m string) string {
		inner := m[len(`URI="`) : len(m)-1]
		return `URI="` + rewriteURI(upstreamBase, pubBase, channel, inner) + `"`
	})
}

// rewriteURI turns an HLS URI reference into an absolute URL on the proxy.
func rewriteURI(upstreamBase, pubBase, channel, raw string) string {
	up := raw
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		up = resolveUpstream(upstreamBase, raw)
	}
	return pubBase + "/iptv/f/" + channel + "?u=" + url.QueryEscape(up)
}

// resolveUpstream resolves a possibly-relative URI against a playlist URL.
// HLS segments resolve against the playlist's directory, not the playlist
// file itself, and never inherit the playlist's query tokens.
func resolveUpstream(playlistURL, raw string) string {
	u, err := url.Parse(playlistURL)
	if err != nil {
		return strings.TrimSuffix(playlistURL, "/") + "/" + raw
	}
	if strings.HasPrefix(raw, "/") {
		u.Path = raw
		u.RawQuery = ""
		return u.String()
	}
	dir := path.Dir(u.Path)
	if dir == "." {
		dir = ""
	}
	u.Path = strings.TrimSuffix(dir, "/") + "/" + raw
	u.RawQuery = ""
	return u.String()
}
