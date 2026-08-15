// Package f1net resolves live F1 stream playlists from the F1Net
// dashboard (https://f1net.vercel.app). F1Net itself only iframes a list of
// third-party embed player pages (see /source.txt); this package fetches that
// list and resolves each embed into a playable m3u8 URL.
package f1net

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://f1net.vercel.app"
	// defaultUA mimics a real Chrome browser so upstream servers treat the
	// requests as coming from a user rather than an API client.
	defaultUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

// Errors returned by the package.
var (
	ErrNoSources       = errors.New("no streams found")
	ErrUnsupportedHost = errors.New("unsupported embed host")
	ErrStreamOffline   = errors.New("stream offline")
)

// Client fetches and resolves F1 streams.
type Client struct {
	// HTTPClient used for all requests. If nil, a default client with a
	// 15s timeout is used.
	HTTPClient *http.Client

	// BaseURL is the dashboard origin the source list is fetched from.
	// Defaults to https://f1net.vercel.app.
	BaseURL string

	// UserAgent sent with every request. If empty, a default is used.
	UserAgent string

	// VerifyPlaylist performs a GET on the resolved m3u8 (with the
	// required headers) to confirm it is reachable before returning it.
	VerifyPlaylist bool
}

func (c *Client) http() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return defaultBaseURL
}

func (c *Client) userAgent() string {
	if c.UserAgent != "" {
		return c.UserAgent
	}
	return defaultUA
}

// Source is a single entry from the dashboard source list.
type Source struct {
	Name string // display name, e.g. "Full HD 1"
	URL  string // embed player URL
}

func (s Source) String() string { return s.Name }

// Stream is a resolved, playable m3u8 playlist.
type Stream struct {
	Name        string      // display name from the source list
	Source      string      // embed URL the stream was resolved from
	PlaylistURL string      // m3u8 playlist URL
	Quality     string      // resolved quality, e.g. "1080p"
	Headers     http.Header // headers required to fetch the playlist and segments
}

func (s *Stream) String() string {
	return fmt.Sprintf("%s (%s): %s", s.Name, s.Quality, s.PlaylistURL)
}

// ListSources fetches and parses the dashboard's stream list.
func (c *Client) ListSources(ctx context.Context) ([]Source, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL()+"/source.txt", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent())

	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("source list: unexpected status %s", resp.Status)
	}
	return parseSources(resp.Body)
}

// ResolveStream resolves a single embed source into an m3u8 playlist.
//
// quality selects a specific quality (e.g. "1080p"); an empty string selects
// the best available quality automatically. Streams that are offline or on
// unsupported hosts are reported through ErrUnsupportedHost / ErrStreamOffline.
func (c *Client) ResolveStream(ctx context.Context, src Source, quality string) (*Stream, error) {
	u, err := url.Parse(src.URL)
	if err != nil {
		return nil, fmt.Errorf("parse source url %q: %w", src.URL, err)
	}
	res, ok := resolvers[u.Hostname()]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedHost, u.Hostname())
	}
	stream, err := res.resolve(ctx, c, src, u, quality)
	if err != nil {
		return nil, err
	}
	if c.VerifyPlaylist && stream.PlaylistURL != "" {
		if err := c.checkPlaylist(ctx, stream); err != nil {
			return nil, err
		}
	}
	return stream, nil
}

// ResolveAll resolves every source from the list. Supported hosts are resolved
// concurrently; unsupported hosts and offline streams are skipped. It returns
// an error only if no stream could be resolved.
func (c *Client) ResolveAll(ctx context.Context, quality string) ([]*Stream, error) {
	sources, err := c.ListSources(ctx)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, ErrNoSources
	}

	streams := make([]*Stream, 0, len(sources))
	errs := make([]error, 0, len(sources))
	for _, src := range sources {
		st, err := c.ResolveStream(ctx, src, quality)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", src.Name, err))
			continue
		}
		streams = append(streams, st)
	}
	if len(streams) == 0 {
		return nil, errors.Join(errs...)
	}
	return streams, nil
}

// checkPlaylist issues a GET against the playlist URL with the stream's
// headers and verifies it returns HTTP 200.
func (c *Client) checkPlaylist(ctx context.Context, st *Stream) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, st.PlaylistURL, nil)
	if err != nil {
		return err
	}
	for k, vv := range st.Headers {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s", ErrStreamOffline, resp.Status)
	}
	return nil
}
