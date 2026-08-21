// Package epg generates an XMLTV electronic program guide for the IPTV
// channels from the OpenF1 F1 calendar (the same source the F1Net dashboard
// uses). Programmes are per-session entries (Practice, Qualifying, Race, ...)
// from the configured season.
package epg

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"f1-jf/internal/ctxlog"
	"f1-jf/internal/httpx"
	"f1-jf/internal/iptv"
)

// Programme is a single guide entry ready for XMLTV rendering.
type Programme struct {
	Start time.Time
	Stop  time.Time
	Title string
	Desc  string
}

// Schedule is a cached season schedule.
type Schedule struct {
	Year       int
	Programmes []Programme
}

// Options configures a Service.
type Options struct {
	// APIURL is the OpenF1 API base URL.
	APIURL string
	// Year is the season year to schedule.
	Year int
	// TTL is how long to cache the fetched calendar. Defaults to 6h.
	TTL time.Duration
	// HTTPClient fetches the OpenF1 API. Defaults to a 30s-timeout client.
	HTTPClient *http.Client
	// Logger receives diagnostics. Defaults to slog.Default().
	Logger *slog.Logger
	// Now returns the current time, used to drop programmes that have already
	// ended from the guide. Defaults to time.Now.
	Now func() time.Time
}

// Service fetches and caches the F1 season calendar and renders it as XMLTV.
type Service struct {
	apiURL string
	year   int
	ttl    time.Duration
	client *http.Client
	logger *slog.Logger

	mu       sync.Mutex
	cached   *Schedule
	cachedAt time.Time
	lastErr  error

	now func() time.Time
}

func New(opts Options) *Service {
	if opts.APIURL == "" {
		opts.APIURL = "https://api.openf1.org/v1"
	}
	if opts.Year == 0 {
		opts.Year = time.Now().Year()
	}
	if opts.TTL <= 0 {
		opts.TTL = 6 * time.Hour
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{
		apiURL: opts.APIURL,
		year:   opts.Year,
		ttl:    opts.TTL,
		client: opts.HTTPClient,
		logger: opts.Logger,
		now:    opts.Now,
	}
}

// log returns the request-scoped logger from ctx (carrying a request_id) when
// present, otherwise the service's own logger.
func (s *Service) log(ctx context.Context) *slog.Logger {
	return ctxlog.FromOr(ctx, s.logger)
}

// Schedule returns the season calendar, re-fetching once the cache is older
// than the TTL and falling back to the last good calendar on fetch errors.
func (s *Service) Schedule(ctx context.Context) (*Schedule, error) {
	log := s.log(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cached != nil && time.Since(s.cachedAt) < s.ttl {
		log.Debug("epg cache hit", "year", s.year)
		return s.cached, nil
	}

	log.Debug("refreshing epg calendar", "year", s.year)
	sched, err := s.fetch(ctx)
	if err != nil {
		s.lastErr = err
		if s.cached != nil {
			log.Warn("epg refresh failed, using cached calendar", "year", s.year, "error", err)
			return s.cached, nil
		}
		log.Warn("epg fetch failed", "year", s.year, "error", err)
		return nil, err
	}
	s.cached = sched
	s.cachedAt = time.Now()
	s.lastErr = nil
	log.Debug("epg calendar refreshed", "year", s.year, "programmes", len(sched.Programmes))
	return sched, nil
}

// fetch downloads and joins the season's sessions and meetings.
func (s *Service) fetch(ctx context.Context) (*Schedule, error) {
	sessions, err := fetchSessions(ctx, s.client, s.apiURL, s.year, httpx.DefaultUA)
	if err != nil {
		return nil, err
	}
	meetings, err := fetchMeetings(ctx, s.client, s.apiURL, s.year, httpx.DefaultUA)
	if err != nil {
		return nil, err
	}
	return buildSchedule(sessions, meetings, s.year)
}

// buildSchedule joins sessions to their meetings and drops testing sessions
// and cancelled events. Testing sessions are identified by their meeting
// name ("Pre-Season Testing") rather than session type, since the API labels
// them as "Practice".
func buildSchedule(sessions []Session, meetings []Meeting, year int) (*Schedule, error) {
	names := make(map[int]string, len(meetings))
	for _, m := range meetings {
		names[m.MeetingKey] = m.MeetingName
	}

	var progs []Programme
	for _, sn := range sessions {
		if sn.IsCancelled {
			continue
		}
		meeting := names[sn.MeetingKey]
		if meeting == "" {
			meeting = sn.CircuitShort
		}
		if sn.SessionType == "Testing" || strings.Contains(strings.ToLower(meeting), "testing") {
			continue
		}
		start, err := time.Parse(time.RFC3339, sn.DateStart)
		if err != nil {
			return nil, fmt.Errorf("parse start %q: %w", sn.DateStart, err)
		}
		stop, err := time.Parse(time.RFC3339, sn.DateEnd)
		if err != nil {
			return nil, fmt.Errorf("parse end %q: %w", sn.DateEnd, err)
		}
		progs = append(progs, Programme{
			Start: start,
			Stop:  stop,
			Title: fmt.Sprintf("%d %s — %s", year, meeting, sn.SessionName),
			Desc:  fmt.Sprintf("%d F1 %s — %s at %s (%s)", year, meeting, sn.SessionName, sn.CircuitShort, sn.CountryName),
		})
	}
	return &Schedule{Year: year, Programmes: progs}, nil
}

// RenderXML returns the XMLTV document for the given channel.
func (s *Service) RenderXML(ctx context.Context, ch *iptv.Channel) ([]byte, error) {
	sched, err := s.Schedule(ctx)
	if err != nil {
		return nil, err
	}

	progs := make([]Programme, 0, len(sched.Programmes))
	for _, p := range sched.Programmes {
		if !p.Stop.After(s.now()) {
			continue
		}
		progs = append(progs, p)
	}
	return xmltvDoc(ch, progs), nil
}
