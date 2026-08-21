// Package httpx holds small shared HTTP helpers used by the upstream-facing
// packages (f1net, epg): a browser-like default User-Agent and JSON/GET
// utilities that set it consistently.
package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// DefaultUA mimics a real Chrome browser so upstream servers treat the
// requests as coming from a user rather than an API client.
const DefaultUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// maxJSONBytes bounds GetJSON bodies. The cap is generous enough that no
// JSON payload is truncated in practice.
const maxJSONBytes = 1 << 28

// Get fetches url with the given UA and returns the response body, capped at
// limit bytes. Non-2xx statuses are errors. The body is fully read here; the
// caller must not keep it open beyond the call.
func Get(ctx context.Context, hc *http.Client, url, ua string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)

	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: unexpected status %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// GetJSON fetches url with the given UA and decodes the JSON body into dst.
func GetJSON(ctx context.Context, hc *http.Client, url, ua string, dst any) error {
	body, err := Get(ctx, hc, url, ua, maxJSONBytes)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dst)
}
