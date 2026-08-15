package iptv

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	f1net "f1-jf/internal/f1net"
)

func TestChannelsFromQualities(t *testing.T) {
	src := f1net.Source{Name: "Stream 2", URL: "https://streamfree.top/embed/racing/skyf1"}
	chans, err := ChannelsFromQualities("1080p,720p", "Sports", src)
	if err != nil {
		t.Fatalf("ChannelsFromQualities: %v", err)
	}
	if len(chans) != 2 {
		t.Fatalf("got %d channels, want 2", len(chans))
	}
	want := []Channel{
		{ID: "f1-1080p", Name: "F1 1080p", Group: "Sports", Quality: "1080p", Source: src},
		{ID: "f1-720p", Name: "F1 720p", Group: "Sports", Quality: "720p", Source: src},
	}
	for i := range want {
		if chans[i].ID != want[i].ID || chans[i].Name != want[i].Name ||
			chans[i].Quality != want[i].Quality || chans[i].Group != want[i].Group {
			t.Errorf("channel %d = %#v, want %#v", i, chans[i], want[i])
		}
	}
}

func TestChannelsFromQualitiesAuto(t *testing.T) {
	chans, err := ChannelsFromQualities("auto", "Sports", f1net.Source{})
	if err != nil {
		t.Fatalf("ChannelsFromQualities: %v", err)
	}
	if len(chans) != 1 || chans[0].ID != "f1-auto" || chans[0].Quality != "" {
		t.Errorf("auto channel = %#v", chans)
	}
}

func TestChannelsFromQualitiesInvalid(t *testing.T) {
	if _, err := ChannelsFromQualities("4k", "Sports", f1net.Source{}); err == nil {
		t.Fatal("expected error for unknown quality")
	}
	if _, err := ChannelsFromQualities("", "Sports", f1net.Source{}); err == nil {
		t.Fatal("expected error for empty quality list")
	}
}

// countingResolver counts ResolveStream calls to observe cache behaviour.
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

func TestRegistryCachesWithinTTL(t *testing.T) {
	res := &countingResolver{ok: true}
	reg := NewRegistry(res, time.Hour)
	ch := &Channel{ID: "f1-1080p", Quality: "1080p"}

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
	reg := NewRegistry(res, time.Millisecond)
	ch := &Channel{ID: "f1-1080p", Quality: "1080p"}

	reg.Resolve(context.Background(), ch)
	time.Sleep(5 * time.Millisecond)
	reg.Resolve(context.Background(), ch)

	if got := res.calls.Load(); got != 2 {
		t.Errorf("resolve calls = %d, want 2 (expired)", got)
	}
}

func TestRegistryLastGoodFallback(t *testing.T) {
	res := &countingResolver{ok: true}
	reg := NewRegistry(res, time.Millisecond)
	ch := &Channel{ID: "f1-1080p", Quality: "1080p"}

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
	reg := NewRegistry(res, time.Hour)
	ch := &Channel{ID: "f1-1080p", Quality: "1080p"}

	if _, err := reg.Resolve(context.Background(), ch); err == nil {
		t.Fatal("expected error when offline and no cached stream")
	}
}
