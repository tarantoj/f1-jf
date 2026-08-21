# Plan: Collapse single-channel machinery

**Status:** open
**Owner:** —
**Prerequisites:** none

## Context / problem

`iptv.Channel` is hardcoded to a single F1 channel — `NewChannel`
(`internal/iptv/channel.go:60`) always sets `{ID: "f1", Name: "F1"}` and only
takes a `group` and `qualities` list. Despite this, the codebase threads a
`[]*iptv.Channel` slice, per-channel-ID caching + `singleflight`, a `channel()`
lookup loop, and a `{channel}` URL path segment through every layer.

F1Net only ever carries F1, so the multi-channel abstraction is not earning its
keep. This plan collapses it to a single implicit channel. Decision confirmed:
**single channel is fine**.

## Goals

1. Remove per-channel-ID machinery (cache map, singleflight, lookups).
2. Remove the `{channel}` path segments from routes.
3. Simplify `Channel` to remove the redundant `ID`/`Name` fields (or fold them
   into a single `Channel` value that is always used wholesale).
4. Simplify `handlePlaylist`, `handleGuide`/`RenderXML`, `prewarm`, and
   `httpserver.New` signature (drop the slice parameter).
5. Keep behavior identical (same M3U `tvg-id`, same URLs except no channel
   segment — see note below).

## Non-goals

- Changing the streaming/TS logic (`serveRawTS`, `streamTSWindow`).
- Removing quality fallback.
- Adding the ability to ever have more than one channel.

## Design / affected files

### 1. `internal/iptv/channel.go`

- `Registry`: replace the `cache map[string]cachedStream` + `group
  singleflight.Group` with a single-slot cache:
  ```go
  type Registry struct {
      resolver Resolver
      ttl      time.Duration
      mu       sync.Mutex
      stream   *f1net.Stream
      err      error
      at       time.Time
      logger   *slog.Logger
  }
  ```
  Drop the `singleflight` import (no longer used) — unless we want to keep
  coalescing, in which case keep `singleflight` but it becomes trivially
  orthogonal; simplest is to drop it since concurrent playlist requests are
  cheap and the cache is fast.
- `Resolve(ctx, ch *Channel)`:
  - Cache-hit check against the single slot.
  - On miss, resolve, store, return.
  - Fall back to last-good slot on error (same behavior as today).
- `Refresh(ctx, ch)`: clear the slot and re-resolve (keep, it is used by
  `handleStream` and `serveRawTS` for mid-session switches).
- Consider whether `Channel` still needs `ID`:
  - The M3U `tvg-id` and URLs currently use `ch.ID == "f1"`.
  - Keep `ID`/`Name`/`Group` on `Channel` (they are data, not structure) but
    drop the slice; the `Registry.Resolve` and handlers can use the one channel
    directly. This is the low-risk option.
  - **Decide:** keep `ID`/`Name`/`Group` fields (simplest, tests keep passing);
    do NOT remove them, to avoid churn. Only remove the *per-ID* machinery.

### 2. `internal/httpserver/server.go`

- `New` signature: drop `channels []*iptv.Channel`, take a single
  `*iptv.Channel` (e.g. `New(registry *iptv.Registry, ch *iptv.Channel, opts Options)`).
- `Handler()` routes: change
  - `GET /iptv/stream/{channel}` -> `GET /iptv/stream` and
    `GET /iptv/stream/f1.ts` handling
  - `GET /iptv/f/{channel}/{path...}` -> `GET /iptv/f/{path...}`
- Store the single channel on `Server` instead of the slice.

### 3. `internal/httpserver/handlers.go`

- `handleStream`:
  - Remove `r.PathValue("channel")` / `channel()` lookup; use the stored
    single channel.
  - Keep the `.ts` suffix behavior: either a dedicated route
    `GET /iptv/stream/f1.ts` or inspect the path suffix on `/iptv/stream`.
    Simplest: keep one route `/iptv/stream` and also
    `/iptv/stream/f1.ts`? Cleaner: keep `/iptv/stream/{channel}`-free by
    registering `/iptv/stream` and `/iptv/stream/raw.ts` (or keep suffix
    detection against the fixed channel id). **Decide during execution** —
    keep the constant `"f1"` as the segment name that the playlist already
    advertises (`/iptv/stream/f1.ts`).
- `handleFetch`:
  - `r.PathValue("channel")` -> fixed channel.
  - `/iptv/f/{path...}` rewritten segment URLs from `rewriteURI` must match.
- `handlePlaylist`: iterate the single channel (or just emit one entry).
- `channel(id)` helper: delete.
- `handleGuide`: call `RenderXML(ctx, singleChannel)`.

### 4. `internal/hlsproxy/rewrite.go`

- `RewritePlaylist(content, upstreamBase, pubBase, channel string)` keeps the
  `channel` param as a plain string (it just needs the segment to build
  `/iptv/f/{channel}/stream.ts?...`). Keep it as-is; only the **caller** now
  passes a constant `"f1"`.

### 5. `cmd/f1iptv/main.go`

- `channels := []*iptv.Channel{ch}` -> pass `ch` directly.
- `prewarm(registry, ch, logger)` instead of the slice.
- `httpserver.New(registry, ch, opts)`.

### 6. `internal/epg/epg.go` / `xmltv.go`

- `RenderXML(ctx, channels []*iptv.Channel)`:
  - Either keep the slice signature (called with a single-element slice), or
    change to accept a single `*iptv.Channel`.
  - **Recommend:** change to `RenderXML(ctx, ch *iptv.Channel)` and update
    `xmltvDoc` to build one channel (no channels×programmes cross product;
    one programme set per the one channel).

### 7. Tests

- `internal/iptv/channel_test.go`: registry tests become single-slot aware
  (still pass — the countingResolver and single-channel set are unchanged).
- `internal/httpserver/server_test.go`:
  - `newServerFrom`/`newServerWith`/etc.: pass a single channel instead of
    `testChannels`.
  - URL assertions: update `/iptv/stream/f1.ts`, `/iptv/f/f1/stream.ts?...`,
    `/iptv/stream/nope` -> adapt to the new route shapes.
- `internal/epg/epg_test.go`: `testChannels()` returns a single channel; adjust
  `RenderXML` call sites.

## Compatibility note

The playlist currently advertises `{base}/iptv/stream/f1.ts` and proxied
segments use `/iptv/f/f1/stream.ts?u=...`. If we keep the `"f1"` literal path
segment (recommended), Jellyfin playlists and already-started streams keep
working with **zero** migration. Do NOT remove the literal `f1` from the URL
shape unless a full redeploy + re-add of the M3U tuner source is acceptable.

## Verification

```sh
cd /home/james/Documents/f1-jf
gofmt -w .
go vet ./...
staticcheck ./...
go test ./...
nix flake check
```

Smoke: run the service; `curl /iptv/playlist.m3u`, `/iptv/stream/f1`,
`/iptv/stream/f1.ts`, `/iptv/f/f1/stream.ts?u=<upstream>`, `/iptv/guide.xml`.

## Acceptance criteria

- `rg -n "\[\]\*iptv.Channel|map\[string\]cachedStream|singleflight|channel\(" internal/`
  returns no structural per-channel machinery (only a single channel value).
- All existing tests pass unchanged in behavior (URLs preserved via the `f1`
  literal).
- `go test ./...` + `nix flake check` green.
