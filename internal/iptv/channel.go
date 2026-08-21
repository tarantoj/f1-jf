// Package iptv models the IPTV channel catalog and caches each channel's
// resolved stream so the HTTP layer can serve a Jellyfin-compatible M3U
// playlist and proxy the underlying HLS without re-resolving constantly.
//
// Resolution follows an ordered quality fallback across every embed source in
// the dashboard source list: for each quality in the channel's list every
// source is tried in turn, and the first success wins. The source list itself
// is TTL-cached and refreshed lazily, keeping the last-good list on failure.
package iptv

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"f1-jf/internal/ctxlog"
	f1net "f1-jf/internal/f1net"
)

// StreamResolver resolves a single embed source into a playable stream at a
// requested quality. *f1net.Client satisfies it; tests provide fakes.
type StreamResolver interface {
	ResolveStream(ctx context.Context, src f1net.Source, quality string) (*f1net.Stream, error)
}

// Resolver resolves a channel into a playable stream, applying whatever
// fallback strategy it wants.
type Resolver interface {
	Resolve(ctx context.Context, ch *Channel) (*f1net.Stream, error)
}

// SourceLister lists the dashboard's embed sources. *f1net.Client satisfies it.
type SourceLister interface {
	ListSources(ctx context.Context) ([]f1net.Source, error)
}

// Channel is one IPTV channel backed by a resolved F1 stream.
type Channel struct {
	ID        string
	Name      string
	Group     string
	Qualities []string
}

// validQualities are the resolutions a channel may try, tried in order.
var validQualities = map[string]bool{
	"540p":  true,
	"720p":  true,
	"1080p": true,
	"2160p": true,
}

// NewChannel builds the single F1 channel. It errors on an empty quality list
// or a quality that is not one of 540p/720p/1080p/2160p.
func NewChannel(group string, qualities []string) (*Channel, error) {
	if len(qualities) == 0 {
		return nil, errors.New("no qualities given")
	}
	for _, q := range qualities {
		if !validQualities[q] {
			return nil, fmt.Errorf("unknown quality %q", q)
		}
	}
	return &Channel{ID: "f1", Name: "F1", Group: group, Qualities: qualities}, nil
}

// fallbackResolver resolves a channel by trying every source from the
// dashboard source list at each quality in the channel's ordered quality list.
// The source list is TTL-cached; a stale list is kept when a refresh fails so
// resolution keeps working through upstream hiccups.
type fallbackResolver struct {
	inner  StreamResolver
	lister SourceLister
	ttl    time.Duration

	mu     sync.Mutex
	cached []f1net.Source
	at     time.Time

	logger *slog.Logger
}

// NewFallbackResolver returns a Resolver that tries each of the channel's
// qualities across every source returned by lister (qualities outer loop,
// sources inner loop), returning the first successful stream. inner resolves
// a single source+quality pair. ttl <= 0 defaults to 30s and a nil logger to
// slog.Default().
func NewFallbackResolver(inner StreamResolver, lister SourceLister, ttl time.Duration, logger *slog.Logger) *fallbackResolver {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &fallbackResolver{
		inner:  inner,
		lister: lister,
		ttl:    ttl,
		logger: logger,
	}
}

// log returns the request-scoped logger from ctx (carrying a request_id) when
// present, otherwise the resolver's own logger.
func (f *fallbackResolver) log(ctx context.Context) *slog.Logger {
	return ctxlog.FromOr(ctx, f.logger)
}

// sources returns the dashboard source list, cached while fresh. On a refetch
// failure with a cached list the stale list is returned (with a warning); if
// nothing is cached the error is propagated.
func (f *fallbackResolver) sources(ctx context.Context) ([]f1net.Source, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.cached) > 0 && time.Since(f.at) < f.ttl {
		return f.cached, nil
	}
	list, err := f.lister.ListSources(ctx)
	if err != nil {
		if len(f.cached) > 0 {
			f.log(ctx).Warn("refresh source list failed, using stale list",
				"error", err, "age", time.Since(f.at).String())
			return f.cached, nil
		}
		return nil, err
	}
	if len(list) == 0 && len(f.cached) > 0 {
		f.log(ctx).Warn("refresh source list empty, using stale list")
		return f.cached, nil
	}
	f.cached = list
	f.at = time.Now()
	return list, nil
}

// Resolve tries the channel's qualities in order, each across every source.
// The first successful resolution wins; failures are logged and aggregated.
func (f *fallbackResolver) Resolve(ctx context.Context, ch *Channel) (*f1net.Stream, error) {
	log := f.log(ctx)
	sources, err := f.sources(ctx)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, errors.New("no sources to resolve")
	}
	var errs []error
	for _, q := range ch.Qualities {
		for _, src := range sources {
			st, err := f.inner.ResolveStream(ctx, src, q)
			if err != nil {
				log.Warn("resolve fallback attempt failed",
					"source", src.Name, "quality", q, "error", err)
				errs = append(errs, fmt.Errorf("%s: %w", src.Name, err))
				continue
			}
			return st, nil
		}
	}
	return nil, errors.Join(errs...)
}

// Registry caches each channel's resolved stream for a short TTL so playlist
// requests pick up fresh auth tokens without hammering the source.
type Registry struct {
	resolver Resolver
	ttl      time.Duration
	mu       sync.RWMutex
	cache    map[string]cachedStream
	group    singleflight.Group
	logger   *slog.Logger
}

type cachedStream struct {
	stream *f1net.Stream
	err    error
	at     time.Time
}

func NewRegistry(resolver Resolver, ttl time.Duration, logger *slog.Logger) *Registry {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Registry{
		resolver: resolver,
		ttl:      ttl,
		cache:    make(map[string]cachedStream),
		logger:   logger,
	}
}

// log returns the request-scoped logger from ctx (carrying a request_id) when
// present, otherwise the registry's own logger.
func (r *Registry) log(ctx context.Context) *slog.Logger {
	return ctxlog.FromOr(ctx, r.logger)
}

// Resolve returns the channel's stream, re-resolving once the cached entry
// is older than the TTL. If a fresh resolve fails, the last good stream is
// returned as a fallback; if there is none, the error is returned. Concurrent
// resolves of the same channel are coalesced into a single upstream call.
func (r *Registry) Resolve(ctx context.Context, ch *Channel) (*f1net.Stream, error) {
	log := r.log(ctx)
	r.mu.RLock()
	c := r.cache[ch.ID]
	r.mu.RUnlock()

	if c.stream != nil && time.Since(c.at) < r.ttl {
		log.Debug("resolution cache hit", "channel", ch.ID)
		return c.stream, nil
	}

	log.Debug("resolving channel", "channel", ch.ID)
	v, err, _ := r.group.Do(ch.ID, func() (any, error) {
		return r.resolver.Resolve(ctx, ch)
	})
	st, _ := v.(*f1net.Stream)

	r.mu.Lock()
	r.cache[ch.ID] = cachedStream{stream: st, err: err, at: time.Now()}
	r.mu.Unlock()

	if err != nil {
		if c.stream != nil {
			log.Warn("resolution failed, using cached stream",
				"channel", ch.ID, "error", err, "age", time.Since(c.at).String())
			return c.stream, nil
		}
		log.Warn("resolution failed", "channel", ch.ID, "error", err)
		return nil, err
	}
	log.Debug("resolved channel", "channel", ch.ID, "quality", st.Quality)
	return st, nil
}

// Refresh drops the cached stream for the channel and re-resolves it,
// bypassing the TTL. It returns the freshly resolved stream or its error.
func (r *Registry) Refresh(ctx context.Context, ch *Channel) (*f1net.Stream, error) {
	r.mu.Lock()
	delete(r.cache, ch.ID)
	r.mu.Unlock()
	return r.Resolve(ctx, ch)
}
