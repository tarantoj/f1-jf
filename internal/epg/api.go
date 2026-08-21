package epg

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"f1-jf/internal/httpx"
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
	if err := httpx.GetJSON(ctx, c, baseURL+"/sessions?year="+url.QueryEscape(fmt.Sprintf("%d", year)), ua, &out); err != nil {
		return nil, fmt.Errorf("sessions: %w", err)
	}
	return out, nil
}

// fetchMeetings fetches every meeting of a year.
func fetchMeetings(ctx context.Context, c *http.Client, baseURL string, year int, ua string) ([]Meeting, error) {
	var out []Meeting
	if err := httpx.GetJSON(ctx, c, baseURL+"/meetings?year="+url.QueryEscape(fmt.Sprintf("%d", year)), ua, &out); err != nil {
		return nil, fmt.Errorf("meetings: %w", err)
	}
	return out, nil
}
