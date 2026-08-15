package epg

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"f1-jf/internal/iptv"
)

// testAPI serves fake OpenF1 sessions/meetings and counts requests.
func testAPI(t *testing.T) (*httptest.Server, func() int) {
	t.Helper()
	var calls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/sessions", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `[
			{"session_key":1,"session_name":"Practice 1","session_type":"Practice",
			 "date_start":"2026-03-14T11:30:00+00:00","date_end":"2026-03-14T12:30:00+00:00",
			 "meeting_key":100,"circuit_short_name":"Bahrain Int","country_name":"Bahrain","year":2026,"is_cancelled":false},
			{"session_key":2,"session_name":"Qualifying","session_type":"Qualifying",
			 "date_start":"2026-03-14T14:00:00+00:00","date_end":"2026-03-14T15:00:00+00:00",
			 "meeting_key":100,"circuit_short_name":"Bahrain Int","country_name":"Bahrain","year":2026,"is_cancelled":false},
			{"session_key":3,"session_name":"Race","session_type":"Race",
			 "date_start":"2026-03-15T13:00:00+00:00","date_end":"2026-03-15T15:00:00+00:00",
			 "meeting_key":100,"circuit_short_name":"Bahrain Int","country_name":"Bahrain","year":2026,"is_cancelled":false},
			{"session_key":4,"session_name":"Sprint","session_type":"Sprint",
			 "date_start":"2026-04-05T12:00:00+00:00","date_end":"2026-04-05T13:00:00+00:00",
			 "meeting_key":200,"circuit_short_name":"Some Circuit","country_name":"Somewhere","year":2026,"is_cancelled":true},
			{"session_key":5,"session_name":"Day 1","session_type":"Practice",
			 "date_start":"2026-02-11T07:00:00+00:00","date_end":"2026-02-11T10:00:00+00:00",
			 "meeting_key":300,"circuit_short_name":"Sakhir","country_name":"Bahrain","year":2026,"is_cancelled":false}
		]`)
	})
	mux.HandleFunc("/meetings", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
			{"meeting_key":100,"meeting_name":"Bahrain Grand Prix & Festival","circuit_short_name":"Bahrain Int","country_name":"Bahrain"},
			{"meeting_key":200,"meeting_name":"Somewhere Sprint Weekend","circuit_short_name":"Some Circuit","country_name":"Somewhere"},
			{"meeting_key":300,"meeting_name":"Pre-Season Testing","circuit_short_name":"Sakhir","country_name":"Bahrain"}
		]`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func() int { return int(calls.Load()) }
}

func testChannels() []*iptv.Channel {
	return []*iptv.Channel{
		{ID: "f1-1080p", Name: "F1 1080p"},
		{ID: "f1-720p", Name: "F1 720p"},
	}
}

// seasonStart is a fixed clock before the 2026 test sessions, so nothing is
// filtered as past.
func seasonStart() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

func TestRenderXML(t *testing.T) {
	srv, _ := testAPI(t)
	svc := New(Options{APIURL: srv.URL, Year: 2026, TTL: time.Hour, HTTPClient: srv.Client(), Now: seasonStart})

	xml, err := svc.RenderXML(context.Background(), testChannels())
	if err != nil {
		t.Fatalf("RenderXML: %v", err)
	}
	text := string(xml)

	if !strings.HasPrefix(text, `<?xml version="1.0"`) {
		t.Errorf("missing xml header:\n%s", text)
	}
	for _, id := range []string{"f1-1080p", "f1-720p"} {
		if !strings.Contains(text, `<channel id="`+id+`">`) {
			t.Errorf("missing channel %s:\n%s", id, text)
		}
	}
	// 3 non-cancelled, non-testing sessions x 2 channels.
	if got := strings.Count(text, "<programme "); got != 6 {
		t.Errorf("programme count = %d, want 6:\n%s", got, text)
	}
	for _, want := range []string{
		`<title lang="en">2026 Bahrain Grand Prix &amp; Festival — Practice 1</title>`,
		`<title lang="en">2026 Bahrain Grand Prix &amp; Festival — Qualifying</title>`,
		`<title lang="en">2026 Bahrain Grand Prix &amp; Festival — Race</title>`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing title %q:\n%s", want, text)
		}
	}
	// XMLTV timestamp format with UTC offset.
	if !strings.Contains(text, `start="20260314113000 +0000" stop="20260314123000 +0000"`) {
		t.Errorf("wrong time format:\n%s", text)
	}
	// Cancelled and testing sessions must be excluded.
	if strings.Contains(text, "Sprint") || strings.Contains(text, "Day 1") || strings.Contains(text, "Pre-Season Testing") {
		t.Errorf("cancelled/testing sessions included:\n%s", text)
	}
}

func TestRenderXMLFiltersPast(t *testing.T) {
	srv, _ := testAPI(t)
	// "Now" is after Practice 1 (ends 12:30) but before Qualifying (starts 14:00).
	svc := New(Options{
		APIURL:     srv.URL,
		Year:       2026,
		TTL:        time.Hour,
		HTTPClient: srv.Client(),
		Now: func() time.Time {
			return time.Date(2026, 3, 14, 13, 0, 0, 0, time.UTC)
		},
	})

	xml, err := svc.RenderXML(context.Background(), testChannels())
	if err != nil {
		t.Fatalf("RenderXML: %v", err)
	}
	text := string(xml)

	// Only Qualifying and Race remain: 2 sessions x 2 channels.
	if got := strings.Count(text, "<programme "); got != 4 {
		t.Errorf("programme count = %d, want 4:\n%s", got, text)
	}
	if strings.Contains(text, "Practice 1") {
		t.Errorf("past event Practice 1 not filtered:\n%s", text)
	}
}

func TestScheduleCachesWithinTTL(t *testing.T) {
	srv, calls := testAPI(t)
	svc := New(Options{APIURL: srv.URL, Year: 2026, TTL: time.Hour, HTTPClient: srv.Client()})

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := svc.Schedule(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// One refresh fetches the sessions endpoint once; cached calls must not
	// refetch.
	if got := calls(); got != 1 {
		t.Errorf("api requests = %d, want 1 (cached)", got)
	}
	if len(svc.cached.Programmes) != 3 {
		t.Errorf("programmes = %d, want 3", len(svc.cached.Programmes))
	}
}

func TestScheduleExpiresAfterTTL(t *testing.T) {
	srv, calls := testAPI(t)
	svc := New(Options{APIURL: srv.URL, Year: 2026, TTL: time.Millisecond, HTTPClient: srv.Client()})

	ctx := context.Background()
	svc.Schedule(ctx)
	time.Sleep(5 * time.Millisecond)
	svc.Schedule(ctx)

	if got := calls(); got != 2 {
		t.Errorf("api requests = %d, want 2 (two refreshes)", got)
	}
	if len(svc.cached.Programmes) == 0 {
		t.Error("expected programmes after refresh")
	}
}

func TestScheduleLastGoodFallback(t *testing.T) {
	ok, _ := testAPI(t)
	// A failing upstream that ignores requests.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(bad.Close)

	svc := New(Options{APIURL: ok.URL, Year: 2026, TTL: time.Millisecond, HTTPClient: ok.Client()})
	ctx := context.Background()
	if _, err := svc.Schedule(ctx); err != nil {
		t.Fatal(err)
	}

	svc.apiURL = bad.URL
	time.Sleep(5 * time.Millisecond)
	got, err := svc.Schedule(ctx)
	if err != nil {
		t.Fatalf("expected last-good fallback, got error: %v", err)
	}
	if len(got.Programmes) != 3 {
		t.Errorf("fallback programmes = %d, want 3", len(got.Programmes))
	}
}

func TestEscapeXML(t *testing.T) {
	got := escapeXML(`A & B <tag> "q" 'apos'`)
	want := "A &amp; B &lt;tag&gt; &quot;q&quot; &apos;apos&apos;"
	if got != want {
		t.Errorf("escapeXML = %q, want %q", got, want)
	}
}
