package f1net

import (
	"context"
	"net/url"
)

// resolver turns an embed page into a playable m3u8 stream. Implementations
// live in host-specific files (e.g. streamfree.go) and are registered in
// the resolvers map below.
type resolver interface {
	resolve(ctx context.Context, c *Client, src Source, u *url.URL, quality string) (*Stream, error)
}

// resolvers dispatches embed URLs by hostname to their resolver.
var resolvers = map[string]resolver{
	"streamfree.top": streamfreeResolver{},
}
