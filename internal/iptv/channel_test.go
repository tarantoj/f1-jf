package iptv

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	f1net "f1-jf/internal/f1net"
)

func TestNewChannel(t *testing.T) {
	ch, err := NewChannel("Sports", []string{"2160p", "1080p", "720p"})
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	if ch.ID != "f1" || ch.Name != "F1" || ch.Group != "Sports" {
		t.Errorf("channel = %#v", ch)
	}
	if len(ch.Qualities) != 3 || ch.Qualities[0] != "2160p" || ch.Qualities[1] != "1080p" || ch.Qualities[2] != "720p" {
		t.Errorf("qualities = %v", ch.Qualities)
	}
}

func TestNewChannelEmpty(t *testing.T) {
	if _, err := NewChannel("Sports", nil); err == nil {
		t.Fatal("expected error for empty quality list")
	}
	if _, err := NewChannel("Sports", []string{}); err == nil {
		t.Fatal("expected error for empty quality list")
	}
}

func TestNewChannelUnknownQuality(t *testing.T) {
	if _, err := NewChannel("Sports", []string{"4k"}); err == nil {
		t.Fatal("expected error for unknown quality")
	}
	if _, err := NewChannel("Sports", []string{"1080p", "4k"}); err == nil {
		t.Fatal("expected error for unknown quality in list")
	}
}

// countingResolver is a StreamResolver that resolves any source at any
// quality, counting calls so cache behaviour can be observed.
type countingResolver struct {
	calls atomic.Int32
	ok    bool
}

func (c *countingResolver) ResolveStream(_ context.Context, _ f1net.Source, quality string) (*f1net.Stream, error) {
	c.calls.Add(1)
	if !c.ok {
		return nil, errors.New("offline")
	}
	return &f1net.Stream{
		Name:        "F1 " + quality,
		PlaylistURL: "https://upstream.test/live/skyf1" + quality + "/index.m3u8",
		Quality:     quality,
	}, nil
}

// Resolve makes countingResolver a full Resolver for registry tests: it
// resolves the channel at its first quality against a single source.
func (c *countingResolver) Resolve(ctx context.Context, ch *Channel) (*f1net.Stream, error) {
	for _, q := range ch.Qualities {
		if st, err := c.ResolveStream(ctx, f1net.Source{Name: "src", URL: "s"}, q); err == nil {
			return st, nil
		}
	}
	return nil, errors.New("offline")
}

// staticSources is a SourceLister returning a fixed list.
type staticSources struct {
	sources []f1net.Source
	err     error
}

func (s *staticSources) ListSources(_ context.Context) ([]f1net.Source, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.sources, nil
}

// mapResolver resolves streams from a map keyed "quality|sourceURL".
type mapResolver struct {
	streams map[string]*f1net.Stream
	order   []string
}

func (m *mapResolver) ResolveStream(_ context.Context, src f1net.Source, quality string) (*f1net.Stream, error) {
	key := quality + "|" + src.URL
	m.order = append(m.order, key)
	if st := m.streams[key]; st != nil {
		return st, nil
	}
	return nil, errors.New("no stream at " + key)
}

func TestFallbackQualityOrder(t *testing.T) {
	A := f1net.Source{Name: "A", URL: "a"}
	B := f1net.Source{Name: "B", URL: "b"}
	mr := &mapResolver{streams: map[string]*f1net.Stream{
		"1080p|a": {Name: "A 1080p", Quality: "1080p", PlaylistURL: "https://a/1080"},
		"720p|b":  {Name: "B 720p", Quality: "720p", PlaylistURL: "https://b/720"},
	}}
	fr := NewFallbackResolver(mr, &staticSources{sources: []f1net.Source{A, B}}, time.Hour, nil)
	ch := &Channel{ID: "f1", Qualities: []string{"2160p", "1080p", "720p"}}

	st, err := fr.Resolve(context.Background(), ch)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if st.PlaylistURL != "https://a/1080" {
		t.Errorf("stream = %q, want A@1080p", st.PlaylistURL)
	}
	// A@1080p wins at the first source that serves it. B@1080p must NOT be
	// attempted because A already succeeded at that quality tier.
	wantOrder := []string{
		"2160p|a", "2160p|b",
		"1080p|a",
	}
	if len(mr.order) != len(wantOrder) {
		t.Fatalf("attempt order = %v, want %v", mr.order, wantOrder)
	}
	for i := range wantOrder {
		if mr.order[i] != wantOrder[i] {
			t.Errorf("attempt[%d] = %q, want %q (order %v)", i, mr.order[i], wantOrder[i], mr.order)
		}
	}
}

func TestFallbackAllFail(t *testing.T) {
	src := f1net.Source{Name: "A", URL: "a"}
	mr := &mapResolver{streams: map[string]*f1net.Stream{}}
	fr := NewFallbackResolver(mr, &staticSources{sources: []f1net.Source{src}}, time.Hour, nil)
	ch := &Channel{ID: "f1", Qualities: []string{"1080p", "720p"}}

	if _, err := fr.Resolve(context.Background(), ch); err == nil {
		t.Fatal("expected joined error when all fallback attempts fail")
	}
	if len(mr.order) != 2 {
		t.Errorf("attempts = %v, want 2 (1080p then 720p)", mr.order)
	}
}

func TestFallbackSourceOrder(t *testing.T) {
	A := f1net.Source{Name: "A", URL: "a"}
	B := f1net.Source{Name: "B", URL: "b"}
	mr := &mapResolver{streams: map[string]*f1net.Stream{
		"1080p|b": {Name: "B 1080p", Quality: "1080p", PlaylistURL: "https://b/1080"},
	}}
	fr := NewFallbackResolver(mr, &staticSources{sources: []f1net.Source{A, B}}, time.Hour, nil)
	ch := &Channel{ID: "f1", Qualities: []string{"1080p"}}

	st, err := fr.Resolve(context.Background(), ch)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if st.PlaylistURL != "https://b/1080" {
		t.Errorf("stream = %q, want B@1080p", st.PlaylistURL)
	}
	// Within the 1080p tier sources are tried in list order; A fails, B wins.
	wantOrder := []string{"1080p|a", "1080p|b"}
	for i := range wantOrder {
		if mr.order[i] != wantOrder[i] {
			t.Errorf("attempt[%d] = %q, want %q (order %v)", i, mr.order[i], wantOrder[i], mr.order)
		}
	}
}

func TestFallbackListerTTL(t *testing.T) {
	var listCalls atomic.Int32
	lister := &countingLister{calls: &listCalls, sources: []f1net.Source{{Name: "A", URL: "a"}}}
	mr := &mapResolver{streams: map[string]*f1net.Stream{
		"1080p|a": {Name: "A 1080p", Quality: "1080p", PlaylistURL: "https://a/1080"},
	}}
	fr := NewFallbackResolver(mr, lister, time.Hour, nil)
	ch := &Channel{ID: "f1", Qualities: []string{"1080p"}}

	for i := 0; i < 3; i++ {
		if _, err := fr.Resolve(context.Background(), ch); err != nil {
			t.Fatal(err)
		}
	}
	if got := listCalls.Load(); got != 1 {
		t.Errorf("lister calls = %d, want 1 (cached within TTL)", got)
	}
}

type countingLister struct {
	calls   *atomic.Int32
	sources []f1net.Source
	err     error
}

func (c *countingLister) ListSources(_ context.Context) ([]f1net.Source, error) {
	c.calls.Add(1)
	if c.err != nil {
		return nil, c.err
	}
	return c.sources, nil
}

func TestFallbackListerLastGood(t *testing.T) {
	ok := &countingLister{calls: &atomic.Int32{}, sources: []f1net.Source{{Name: "A", URL: "a"}}}
	mr := &mapResolver{streams: map[string]*f1net.Stream{
		"1080p|a": {Name: "A 1080p", Quality: "1080p", PlaylistURL: "https://a/1080"},
	}}
	fr := NewFallbackResolver(mr, ok, time.Millisecond, nil)
	ch := &Channel{ID: "f1", Qualities: []string{"1080p"}}

	st, err := fr.Resolve(context.Background(), ch)
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	ok.err = errors.New("lister down")
	time.Sleep(5 * time.Millisecond)

	got, err := fr.Resolve(context.Background(), ch)
	if err != nil {
		t.Fatalf("expected stale-list resolve, got error: %v", err)
	}
	if got.PlaylistURL != st.PlaylistURL {
		t.Errorf("stream = %q, want stale %q", got.PlaylistURL, st.PlaylistURL)
	}
}

func TestFallbackRefreshedSources(t *testing.T) {
	var listCalls atomic.Int32
	lister := &countingLister{calls: &listCalls, sources: []f1net.Source{{Name: "A", URL: "a"}}}
	mr := &mapResolver{streams: map[string]*f1net.Stream{
		"1080p|b": {Name: "B 1080p", Quality: "1080p", PlaylistURL: "https://b/1080"},
	}}
	fr := NewFallbackResolver(mr, lister, time.Millisecond, nil)
	ch := &Channel{ID: "f1", Qualities: []string{"1080p"}}

	if _, err := fr.Resolve(context.Background(), ch); err == nil {
		t.Fatal("expected failure: only A listed, only B works")
	}
	// After TTL the lister returns both; B is now reachable.
	lister.sources = []f1net.Source{{Name: "A", URL: "a"}, {Name: "B", URL: "b"}}
	time.Sleep(5 * time.Millisecond)

	st, err := fr.Resolve(context.Background(), ch)
	if err != nil {
		t.Fatalf("second resolve after refresh: %v", err)
	}
	if st.PlaylistURL != "https://b/1080" {
		t.Errorf("stream = %q, want B@1080p", st.PlaylistURL)
	}
}

func TestRegistryCachesWithinTTL(t *testing.T) {
	res := &countingResolver{ok: true}
	reg := NewRegistry(res, time.Hour, nil)
	ch := &Channel{ID: "f1", Qualities: []string{"1080p"}}

	if _, err := reg.Resolve(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Resolve(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	if got := res.calls.Load(); got != 1 {
		t.Errorf("resolve calls = %d, want 1 (cached)", got)
	}
}

func TestRegistryExpiresAfterTTL(t *testing.T) {
	res := &countingResolver{ok: true}
	reg := NewRegistry(res, time.Millisecond, nil)
	ch := &Channel{ID: "f1", Qualities: []string{"1080p"}}

	reg.Resolve(context.Background(), ch)
	time.Sleep(5 * time.Millisecond)
	reg.Resolve(context.Background(), ch)

	if got := res.calls.Load(); got != 2 {
		t.Errorf("resolve calls = %d, want 2 (expired)", got)
	}
}

func TestRegistryLastGoodFallback(t *testing.T) {
	res := &countingResolver{ok: true}
	reg := NewRegistry(res, time.Millisecond, nil)
	ch := &Channel{ID: "f1", Qualities: []string{"1080p"}}

	st, err := reg.Resolve(context.Background(), ch)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	res.ok = false // upstream goes down

	got, err := reg.Resolve(context.Background(), ch)
	if err != nil {
		t.Fatalf("expected last-good fallback, got error: %v", err)
	}
	if got.PlaylistURL != st.PlaylistURL {
		t.Errorf("fallback stream = %q, want %q", got.PlaylistURL, st.PlaylistURL)
	}
}

func TestRegistryNoFallbackWithoutCache(t *testing.T) {
	res := &countingResolver{ok: false}
	reg := NewRegistry(res, time.Hour, nil)
	ch := &Channel{ID: "f1", Qualities: []string{"1080p"}}

	if _, err := reg.Resolve(context.Background(), ch); err == nil {
		t.Fatal("expected error when offline and no cached stream")
	}
}

func TestRegistryRefresh(t *testing.T) {
	res := &countingResolver{ok: true}
	reg := NewRegistry(res, time.Hour, nil)
	ch := &Channel{ID: "f1", Qualities: []string{"1080p"}}

	if _, err := reg.Resolve(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	if got := res.calls.Load(); got != 1 {
		t.Fatalf("resolve calls = %d, want 1", got)
	}
	// Refresh must force a new resolve even though the cache is fresh (within TTL).
	st, err := reg.Refresh(context.Background(), ch)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if st == nil {
		t.Fatal("Refresh returned nil stream")
	}
	if got := res.calls.Load(); got != 2 {
		t.Errorf("resolve calls = %d, want 2 (Refresh bypasses cache)", got)
	}
}

func TestFallbackResolverDefaults(t *testing.T) {
	// ttl <= 0 should default to a workable TTL and a nil logger should not
	// panic.
	fr := NewFallbackResolver(&mapResolver{streams: map[string]*f1net.Stream{}}, &staticSources{sources: []f1net.Source{{Name: "A", URL: "a"}}}, 0, nil)
	if fr.ttl <= 0 {
		t.Errorf("default ttl = %v, want > 0", fr.ttl)
	}
	if fr.logger == nil {
		t.Error("default logger is nil")
	}
	if _, err := fr.Resolve(context.Background(), &Channel{ID: "f1", Qualities: []string{"1080p"}}); err == nil {
		t.Fatal("expected join error")
	}
}
