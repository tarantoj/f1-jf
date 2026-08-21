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
// is fetched through the proxy at pubBase+/iptv/f/stream.ts. Relative URIs
// resolve against upstreamBase (the URL the playlist was fetched from).
// Non-playlist content is returned unchanged.
func RewritePlaylist(content []byte, upstreamBase, pubBase string) []byte {
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
			s.URI = rewriteURI(upstreamBase, pubBase, s.URI)
			for j := range s.Keys {
				if s.Keys[j].URI != "" {
					s.Keys[j].URI = rewriteURI(upstreamBase, pubBase, s.Keys[j].URI)
				}
			}
			if s.Map != nil && s.Map.URI != "" {
				s.Map.URI = rewriteURI(upstreamBase, pubBase, s.Map.URI)
			}
		}
	case *m3u8.MasterPlaylist:
		for _, v := range p.Variants {
			if v.URI != "" {
				v.URI = rewriteURI(upstreamBase, pubBase, v.URI)
			}
			for _, alt := range v.Alternatives {
				if alt.URI != "" {
					alt.URI = rewriteURI(upstreamBase, pubBase, alt.URI)
				}
			}
		}
	}
	return pl.Encode().Bytes()
}

// rewriteURI turns an HLS URI reference into an absolute URL on the proxy.
// The path ends in /stream.ts so ffmpeg's HLS demuxer accepts the segment
// extension (upstream segments may carry obfuscated extensions like .js).
func rewriteURI(upstreamBase, pubBase, raw string) string {
	up := raw
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		up = ResolveUpstream(upstreamBase, raw)
	}
	return pubBase + "/iptv/f/stream.ts?u=" + url.QueryEscape(up)
}

// ResolveUpstream resolves a possibly-relative URI against a playlist URL.
// Absolute (scheme-qualified) URIs are returned unchanged; host-relative
// URIs resolve against the playlist host; other relative URIs resolve against
// the playlist's directory (not the playlist file itself) and never inherit
// the playlist's query tokens.
func ResolveUpstream(playlistURL, raw string) string {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
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
