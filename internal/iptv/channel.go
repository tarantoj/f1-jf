// Package iptv models the IPTV channel catalog and caches each channel's
// resolved stream so the HTTP layer can serve a Jellyfin-compatible M3U
// playlist and proxy the underlying HLS without re-resolving constantly.
package iptv

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	f1net "f1-jf/internal/f1net"
)

// Resolver resolves an embed source into a playable stream. *f1net.Client
// satisfies it; tests provide fakes.
type Resolver interface {
	ResolveStream(ctx context.Context, src f1net.Source, quality string) (*f1net.Stream, error)
}

// Channel is one IPTV channel backed by a resolved F1 stream.
type Channel struct {
	ID      string
	Name    string
	Group   string
	Quality string
	Source  f1net.Source
}

// ChannelsFromQualities builds one channel per comma-separated quality
// ("1080p,720p", or "auto" for best-available). Empty list elements are
// skipped; an entirely empty list or an unknown quality is an error.
func ChannelsFromQualities(qs, group string, src f1net.Source) ([]*Channel, error) {
	if strings.TrimSpace(qs) == "" {
		return nil, fmt.Errorf("no qualities given")
	}
	var out []*Channel
	for _, q := range strings.Split(qs, ",") {
		q = strings.TrimSpace(q)
		switch {
		case q == "":
			continue
		case q == "auto":
			out = append(out, &Channel{
				ID:      "f1-auto",
				Name:    "F1 Auto",
				Quality: "",
				Group:   group,
				Source:  src,
			})
		case q == "540p" || q == "720p" || q == "1080p" || q == "2160p":
			out = append(out, &Channel{
				ID:      "f1-" + q,
				Name:    "F1 " + q,
				Quality: q,
				Group:   group,
				Source:  src,
			})
		default:
			return nil, fmt.Errorf("unknown quality %q", q)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no qualities given")
	}
	return out, nil
}

// Registry caches each channel's resolved stream for a short TTL so playlist
// requests pick up fresh auth tokens without hammering the source.
type Registry struct {
	resolver Resolver
	ttl      time.Duration
	mu       sync.RWMutex
	cache    map[string]cachedStream
}

type cachedStream struct {
	stream *f1net.Stream
	err    error
	at     time.Time
}

func NewRegistry(resolver Resolver, ttl time.Duration) *Registry {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &Registry{
		resolver: resolver,
		ttl:      ttl,
		cache:    make(map[string]cachedStream),
	}
}

// Resolve returns the channel's stream, re-resolving once the cached entry
// is older than the TTL. If a fresh resolve fails, the last good stream is
// returned as a fallback; if there is none, the error is returned.
func (r *Registry) Resolve(ctx context.Context, ch *Channel) (*f1net.Stream, error) {
	r.mu.RLock()
	c := r.cache[ch.ID]
	r.mu.RUnlock()

	if c.stream != nil && time.Since(c.at) < r.ttl {
		return c.stream, nil
	}

	st, err := r.resolver.ResolveStream(ctx, ch.Source, ch.Quality)

	r.mu.Lock()
	r.cache[ch.ID] = cachedStream{stream: st, err: err, at: time.Now()}
	r.mu.Unlock()

	if err != nil {
		if c.stream != nil {
			return c.stream, nil
		}
		return nil, err
	}
	return st, nil
}
