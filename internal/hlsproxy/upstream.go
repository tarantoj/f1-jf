// Package hlsproxy fetches upstream HLS resources (playlists and segments)
// on behalf of the HTTP layer and rewrites playlists so all media is pulled
// through the proxy. It is transport-agnostic: it does not know about IPTV
// or HTTP handlers.
package hlsproxy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"f1-jf/internal/ctxlog"
)

// MaxPlaylistBytes bounds how much of a playlist we will buffer to rewrite.
const MaxPlaylistBytes = 4 << 20

// Client fetches upstream playlists and segments with caller-supplied
// headers (Referer/Origin/User-Agent) so the source's access gating is
// satisfied server-side.
type Client struct {
	http   *http.Client
	logger *slog.Logger
}

// NewClient returns a Client using hc, or a default client with a 2 minute
// timeout when hc is nil, and logger, or slog.Default() when nil.
func NewClient(hc *http.Client, logger *slog.Logger) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 2 * time.Minute}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{http: hc, logger: logger}
}

// log returns the request-scoped logger from ctx (carrying a request_id) when
// present, otherwise the client's own logger.
func (c *Client) log(ctx context.Context) *slog.Logger {
	return ctxlog.FromOr(ctx, c.logger)
}

// Response is a successful upstream resource: the raw body plus the metadata
// needed to forward or rewrite it.
type Response struct {
	Status int
	Header http.Header
	Body   io.ReadCloser
}

// IsPlaylist reports whether the response is an HLS playlist (rather than a
// media segment), based on the content type and the first bytes of the body.
func (r *Response) IsPlaylist(prefix []byte) bool {
	return looksLikePlaylist(prefix, r.Header.Get("Content-Type"))
}

// Fetch GETs an upstream URL with the given headers, forwarding any client
// Range header and requesting identity (uncompressed) responses. It returns
// an error for non-2xx statuses.
func (c *Client) Fetch(ctx context.Context, headers http.Header, upstream, rangeHeader string) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream, nil)
	if err != nil {
		return nil, err
	}
	for k, vv := range headers {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := c.http.Do(req)
	if err != nil {
		c.log(ctx).Warn("upstream fetch failed", "url", upstream, "error", err)
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		resp.Body.Close()
		c.log(ctx).Warn("upstream fetch rejected", "url", upstream, "status", resp.Status)
		return nil, fmt.Errorf("upstream %s %s", upstream, resp.Status)
	}
	return &Response{
		Status: resp.StatusCode,
		Header: resp.Header,
		Body:   resp.Body,
	}, nil
}

// SegmentContentType picks a content type for proxied segments, falling back
// to video/mp2t when the upstream did not declare one.
func SegmentContentType(upstream string) string {
	if upstream != "" {
		return upstream
	}
	return "video/mp2t"
}
