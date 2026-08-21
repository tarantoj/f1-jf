# Plan: EPG — fix the live-window gap (keep the guide working)

**Status:** open
**Owner:** —
**Prerequisites:** none

## Context / problem

The XMLTV guide (`/iptv/guide.xml`) served by `internal/epg` pulls the season
calendar from OpenF1 (`https://api.openf1.org/v1`). This works for *historical*
data but OpenF1 classifies data as *real-time* from 30 minutes before a session
until 30 minutes after it ends, and the real-time tier requires a paid
subscription (`x-api-key`). During that window the proxy's anonymous requests
get 401, the fetch fails, and if there is no cached calendar yet, the handler
returns 503.

This is **not** "the EPG is dead" — the guide works whenever no race is live.
The plan keeps the EPG and makes it resilient.

## Goals

1. Support an optional `F1IPTV_EPG_API_KEY` that is sent to OpenF1 so the
   living/real-time tier works when the operator has a subscription.
2. Serve a stale (last-good) calendar instead of 503 during transient/live
   fetch failures.
3. Prewarm the calendar at startup so the first `/guide.xml` request hits a
   warm cache rather than fetching inline during a live window.
4. Record the corrected understanding (the real cause is the live-window 401,
   not "OpenF1 now requires auth") so an operator can decide whether to
   configure `F1IPTV_EPG_API_KEY`.

## Non-goals

- Switching to a different calendar provider.
- Making the guide work through the paid live window without a key (not
  possible; OpenF1 gates it).

## Design / affected files

### 1. Optional API key

`internal/config/config.go`:

- Add field `EPGAPIKey string` with doc comment `// F1IPTV_EPG_API_KEY`.
- In `Load()` add: `EPGAPIKey: str("F1IPTV_EPG_API_KEY", "")`.

`modules/f1iptv.nix`:

- Add option `epgAPIKey = lib.mkOption { type = lib.types.str; default = ""; ... }`
  (empty default).
- Add `F1IPTV_EPG_API_KEY = cfg.epgAPIKey;` to the systemd `environment`.

`internal/epg/api.go` — `getJSON`:

- Add an `apiKey string` parameter (or an `Options`-carried header). When
  non-empty, set `req.Header.Set("x-api-key", apiKey)`.

`internal/epg/epg.go`:

- Add `apiKey string` to `Service`.
- Thread it through `Options` -> `New` -> `fetch` -> `fetchSessions` /
  `fetchMeetings`.

`cmd/f1iptv/main.go`:

- Pass `APIKey: cfg.EPGAPIKey` into `epg.Options`.

**Note:** sending the key is harmless when the data is historical (it is
ignored); it only matters during live windows.

### 2. Stale-guide fallback

`internal/epg/epg.go` — `Schedule` already returns the cached calendar on a
refresh failure (`epg.go:121-127`). Verify this path:

- When `s.cached != nil` and refresh fails, we already return `s.cached`.
- The 503 only happens on the very first fetch (no cache yet).

Improvement (optional but recommended): if a fetch fails and there is no cache,
retry once with a short delay before giving up, to ride out transient blips.
Keep it simple — do not add exponential backoff.

### 3. Prewarm at startup

`cmd/f1iptv/main.go`:

- Add a `prewarmEPG(ctx, epgSvc, channels, logger)` that calls
  `epgSvc.Schedule(ctx)` once (respecting the existing shutdown context) and
  logs failures at warn level, mirroring the existing stream `prewarm` at
  `main.go:123`.
- Call it (as a goroutine) next to the existing `prewarm(registry, ...)` call in
  `run()`.

This means the calendar is fetched once at boot (offline-ok), so by the time a
session goes live the cache is already warm and the stale fallback keeps serving.

### 4. Record the corrected cause

The old `TODO.md` (now removed) claimed "OpenF1 now requires auth on every
request". That is inaccurate and the note should not be re-introduced. The
correct understanding to preserve (e.g. in a doc comment or this plan):

- Historical data: free, no auth, works.
- Real-time/live window: requires Sponsor tier + `x-api-key`.
- Options: set `F1IPTV_EPG_API_KEY` if you have a subscription; otherwise the
  guide degrades to a stale calendar and eventually 503 only on the very first
  cold fetch during a live window (mitigated by the prewarm step above).

## Tests

- `internal/config/config_test.go`: assert `EPGAPIKey` default `""` and that a
  set env var is read.
- `internal/epg/epg_test.go` (`testAPI`): add a case where the mock requires an
  `x-api-key` header; assert the request fails without it and succeeds when the
  option is set.
- Add a test: first fetch fails (no cache) -> `Schedule` returns error; after a
  successful fetch, subsequent failures return the cached schedule (already
  covered by `TestScheduleLastGoodFallback`, extend if needed).

## Verification

```sh
cd /home/james/Documents/f1-jf
gofmt -w .
go vet ./...
staticcheck ./...
go test ./...
nix flake check
```

Manual smoke: run the service, then `curl -s http://127.0.0.1:8090/iptv/guide.xml`
should return `200` XMLTV when no race is live.
