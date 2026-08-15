package hlsproxy

import (
	"bytes"
	"net/url"
	"path"
	"strings"

	m3u8 "github.com/Eyevinn/hls-m3u8/m3u8"
)

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
	pl, _, err := m3u8.DecodeFrom(bytes.NewReader(content), false)
	if err != nil {
		return content
	}
	switch p := pl.(type) {
	case *m3u8.MediaPlaylist:
		for i := uint(0); i < p.Count(); i++ {
			s := p.Segments[i]
			s.URI = rewriteURI(upstreamBase, pubBase, channel, s.URI)
			for j := range s.Keys {
				if s.Keys[j].URI != "" {
					s.Keys[j].URI = rewriteURI(upstreamBase, pubBase, channel, s.Keys[j].URI)
				}
			}
			if s.Map != nil && s.Map.URI != "" {
				s.Map.URI = rewriteURI(upstreamBase, pubBase, channel, s.Map.URI)
			}
		}
	case *m3u8.MasterPlaylist:
		for _, v := range p.Variants {
			if v.URI != "" {
				v.URI = rewriteURI(upstreamBase, pubBase, channel, v.URI)
			}
			for _, alt := range v.Alternatives {
				if alt.URI != "" {
					alt.URI = rewriteURI(upstreamBase, pubBase, channel, alt.URI)
				}
			}
		}
	}
	return pl.Encode().Bytes()
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
