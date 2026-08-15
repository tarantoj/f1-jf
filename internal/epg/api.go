package epg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Session is a single F1 session from the OpenF1 API.
type Session struct {
	SessionKey   int    `json:"session_key"`
	SessionName  string `json:"session_name"`
	SessionType  string `json:"session_type"`
	DateStart    string `json:"date_start"`
	DateEnd      string `json:"date_end"`
	MeetingKey   int    `json:"meeting_key"`
	CircuitShort string `json:"circuit_short_name"`
	CountryName  string `json:"country_name"`
	Year         int    `json:"year"`
	IsCancelled  bool   `json:"is_cancelled"`
}

// Meeting is a single race meeting from the OpenF1 API.
type Meeting struct {
	MeetingKey   int    `json:"meeting_key"`
	MeetingName  string `json:"meeting_name"`
	CircuitShort string `json:"circuit_short_name"`
	CountryName  string `json:"country_name"`
}

// fetchSessions fetches every session of a year.
func fetchSessions(ctx context.Context, c *http.Client, baseURL string, year int, ua string) ([]Session, error) {
	var out []Session
	if err := getJSON(ctx, c, baseURL+"/sessions?year="+url.QueryEscape(fmt.Sprintf("%d", year)), ua, &out); err != nil {
		return nil, fmt.Errorf("sessions: %w", err)
	}
	return out, nil
}

// fetchMeetings fetches every meeting of a year.
func fetchMeetings(ctx context.Context, c *http.Client, baseURL string, year int, ua string) ([]Meeting, error) {
	var out []Meeting
	if err := getJSON(ctx, c, baseURL+"/meetings?year="+url.QueryEscape(fmt.Sprintf("%d", year)), ua, &out); err != nil {
		return nil, fmt.Errorf("meetings: %w", err)
	}
	return out, nil
}

// getJSON fetches and decodes a JSON endpoint with a browser-like UA.
func getJSON(ctx context.Context, c *http.Client, endpoint, ua string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", ua)

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: unexpected status %s", endpoint, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// parseRFC3339 parses an ISO-8601 timestamp from the API.
func parseRFC3339(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}
