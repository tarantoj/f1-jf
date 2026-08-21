# Plan: Trim wrappers & constructors

**Status:** open
**Owner:** —
**Prerequisites:** none (independent of the other plans, but note interactions
with Plan 01 and Plan 02 below).

## Context / problem

Several small wrappers and dual-constructor patterns add noise without value:

1. `internal/epg/api.go:72` `parseRFC3339` - just wraps
   `time.Parse(time.RFC3339, s)`.
2. `internal/epg/xmltv.go:57,62` `xmltvChannel` / `xmltvProgramme` re-declare
   `iptv.Channel` / `epg.Programme` field-for-field, then `handleGuide` /
   `RenderXML` copy fields into them. They are redundant intermediates.
3. Dual constructors that differ only in taking a logger:
   - `internal/iptv/channel.go`: `NewRegistry(resolver, ttl)` /
     `NewRegistryLogger(resolver, ttl, logger)`.
   - `internal/hlsproxy/upstream.go`: `NewClient(hc)` /
     `NewClientLogger(hc, logger)`.
   (The `epg` package already models the nicer single-`Options`-struct pattern.)
4. `internal/hlsproxy/rewrite.go`: `ResolveUpstream` is an exported alias of
   the unexported `resolveUpstream` (export `resolveUpstream` once, drop the
   wrapper).

## Goals

1. Remove `parseRFC3339`.
2. Remove the `xmltvChannel` / `xmltvProgramme` intermediates.
3. Collapse each `NewX`/`NewXLogger` pair into one constructor.
4. Export `resolveUpstream` once as `ResolveUpstream` and drop the wrapper.

## Non-goals

- Rewriting the XMLTV rendering itself (only the type plumbing).
- Changing constructor behavior or defaults.

## Design / affected files

### 1. Remove `parseRFC3339` — `internal/epg/epg.go`

Replace the two call sites (`epg.go:173,177`):

```go
start, err := time.Parse(time.RFC3339, sn.DateStart)
```

Delete `parseRFC3339` from `api.go`. `time` is already imported in `epg.go`.

### 2. Remove `xmltvChannel` / `xmltvProgramme` — `internal/epg/xmltv.go`

`xmltvDoc(channels []xmltvChannel, programmes []xmltvProgramme)` and
`RenderXML` (`epg.go:198-208`) currently copy into these intermediate structs.
Replace with the real domain types:

- Accept `[]*iptv.Channel` (or a single channel if Plan 02 lands) and
  `[]Programme`.
- Make `xmltvDoc` read `ch.ID`, `ch.Name`, `p.Start`, `p.Stop`, `p.Title`,
  `p.Desc` directly.
- Delete `xmltvChannel` and `xmltvProgramme` structs.
- `RenderXML` then no longer builds `chs`/`progs` intermediates — pass
  `sched.Programmes` (already filtered by `now()`) and the channels through
  directly.

**Interaction with Plan 02:** if Plan 02 collapses to a single channel, change
`xmltvDoc`/`RenderXML` to a single-channel signature at that point. If this plan
runs before Plan 02, keep the slice signature but map real types (still an
improvement).

### 3. Collapse dual constructors

`internal/iptv/channel.go`:

- Replace `NewRegistry` / `NewRegistryLogger` with a single
  `NewRegistry(resolver Resolver, ttl time.Duration, opts ...Option)` using a
  functional option, OR keep a single `NewRegistry(resolver, ttl, logger)` where
  `logger` may be nil (defaulted inside). Prefer the smallest churn: one
  constructor `NewRegistry(resolver Resolver, ttl time.Duration, logger
  *slog.Logger)` that nil-defaults `logger` to `slog.Default()`.
- Update callers: `cmd/f1iptv/main.go:46` and `internal/httpserver/server_test.go`
  (`NewRegistry(resolver, 0)`).
- Remove `NewRegistryLogger`.

`internal/hlsproxy/upstream.go`:

- Replace `NewClient(hc)` / `NewClientLogger(hc, logger)` with one
  `NewClient(hc *http.Client, logger *slog.Logger)` (nil-default both), or keep
  `NewClient(hc)` and add an `Options`-style setter. Prefer:
  `NewClient(hc *http.Client, logger *slog.Logger)` and update callers
  (`httpserver/server.go:61` `NewClientLogger(nil, s.log)`,
  `internal/hlsproxy/upstream_test.go` `NewClient(srv.Client())`,
  `internal/httpserver/server_test.go` `hlsproxy.NewClient(up.Client())`).
- Remove `NewClientLogger`.

### 4. Export `ResolveUpstream` — `internal/hlsproxy/rewrite.go`

- Rename unexported `resolveUpstream` to `ResolveUpstream` (exported) everywhere
  it is used (it is used in `rewriteURI` internally and called by
  `handlers.go:224`).
- Delete the standalone wrapper `ResolveUpstream` at `rewrite.go:104`.
- `rewriteURI` calls `ResolveUpstream(...)` directly.

## Tests

- `internal/iptv/channel_test.go`: `NewRegistry(res, ttl)` calls become
  `NewRegistry(res, ttl, nil)` (or the new signature).
- `internal/hlsproxy/upstream_test.go`: `NewClient(srv.Client())` becomes
  `NewClient(srv.Client(), nil)` (or the new signature).
- `internal/hlsproxy/rewrite_test.go`: `TestResolveUpstream` already calls
  `ResolveUpstream` (now the real exported name) — unchanged.
- `internal/epg/epg_test.go`: unchanged in behavior; only internal signature
  changes propagate.
- Existing tests must pass unchanged in behavior.

## Verification

```sh
cd /home/james/Documents/f1-jf
gofmt -w .
go vet ./...
staticcheck ./...
go test ./...
nix flake check
```

## Acceptance criteria

- `rg -n "parseRFC3339|xmltvChannel|xmltvProgramme" internal/` returns nothing.
- `rg -n "func NewRegistryLogger|func NewClientLogger" internal/` returns nothing.
- `rg -n "func resolveUpstream" internal/` returns nothing (only `ResolveUpstream`).
- No manual field-for-field copies between `epg`/`iptv` domain types and XMLTV
  intermediates.
