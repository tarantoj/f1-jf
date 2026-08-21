# Plan: Dedupe logging & HTTP helpers

**Status:** open
**Owner:** —
**Prerequisites:** none

## Context / problem

Two kinds of duplication across packages:

1. The request-logger boilerplate `func (x *T) log(ctx) *slog.Logger { if lg :=
   ctxlog.From(ctx); lg != nil { return lg }; return x.logger }` is repeated in
   five (six) places:
   - `internal/f1net/f1net.go:58`
   - `internal/iptv/channel.go:110` and `:212` (two receivers)
   - `internal/hlsproxy/upstream.go:49`
   - `internal/epg/epg.go:101`
   - `internal/httpserver/server.go:81`

2. HTTP plumbing duplication:
   - `getJSON` is byte-identical in `internal/f1net/streamfree.go:250` and
     `internal/epg/api.go:53`.
   - The `defaultUA` const is duplicated in `internal/f1net/f1net.go:24` and
     `internal/epg/epg.go:21`.
   - The "GET with UA, capped at 1 MiB" logic is duplicated between
     `internal/f1net/cdx.go:62` (`fetchEmbed`) and
     `internal/f1net/streamfree.go:199` (`fetchTokens`).

## Goals

1. Add `ctxlog.FromOr(ctx, fallback)` and replace all five `log()` bodies.
2. Add a tiny shared `internal/httpx` package holding:
   - the shared `defaultUA` constant,
   - `getJSON(ctx, hc, endpoint, ua, keyLib? , dst)`,
   - a capped GET helper (`Get(ctx, hc, url, ua, limit)` or similar).
3. Replace the duplicated `getJSON` and GET code with the shared helpers.
4. No behavior change; pure refactor.

## Non-goals

- Changing logging semantics, request-ID behavior, or timeouts.
- Introducing a full HTTP client abstraction beyond the helpers listed.

## Design / affected files

### 1. `internal/ctxlog/ctxlog.go`

Add:

```go
// FromOr returns the logger stored in ctx by With, or fb if none is present.
func FromOr(ctx context.Context, fb *slog.Logger) *slog.Logger {
    if lg := From(ctx); lg != nil {
        return lg
    }
    return fb
}
```

Meet the existing `defaultUA` / timeouts:

### 2. New package `internal/httpx` (file `internal/httpx/httpx.go`)

```go
// Package httpx holds small shared HTTP helpers used by the upstream-facing
// packages (f1net, epg): a browser-like default User-Agent and JSON/GET
// utilities that set it consistently.
package httpx

const DefaultUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) ..."

// Get fetches url with the given UA and returns the response body, capped at
// limit bytes. Non-2xx statuses are errors. Caller must not keep the body open
// beyond the call (it is read fully here) — or return an io.ReadCloser if
// streaming is needed.
func Get(ctx context.Context, hc *http.Client, url, ua string, limit int64) ([]byte, error)

// GetJSON fetches url, sets the UA, and decodes the JSON body into dst.
func GetJSON(ctx context.Context, hc *http.Client, url, ua string, dst any) error
```

Decisions:

- Put `DefaultUA` here and reference it from both `f1net` and `epg` so the one
  constant is shared.
- `GetJSON` is the shared replacement for the two `getJSON` copies.
- `Get` is the shared replacement for the capped-GET logic in `cdx.go`
  `fetchEmbed` and `streamfree.go` `fetchTokens`. Note `fetchTokens` needs the
  raw body to scan scripts, and `fetchEmbed` needs the raw body too — a
  `Get(...) ([]byte, error)` works for both. It also needs to NOT read from an
  already-consuming reader; using `Get` returning the full capped body is fine.

### 3. `internal/f1net/f1net.go`

- Delete the local `defaultUA` const; reference `httpx.DefaultUA` (keep a
  package-level alias `const defaultUA = httpx.DefaultUA` if referenced by the
  resolvers, or update references).
- Replace the `log(ctx)` body with `return ctxlog.FromOr(ctx, c.Logger)` —
  note the existing fallback is `c.Logger` (may be nil -> then returns nil?).
  Keep parity: `FromOr` returns the fallback which may be nil, but today the
  code falls back to `slog.Default()` too. Standardize: construct/replace all
  `log()` helpers to use `ctxlog.FromOr(ctx, logger)` where `logger` is the
  pre-defaulted field (already non-nil after constructors), so `FromOr` never
  returns nil.

### 4. `internal/f1net/streamfree.go`

- Replace `getJSON(ctx, c, ...)` calls and the local `getJSON` func with
  `httpx.GetJSON(ctx, c.http(), endpoint, c.userAgent(), &out)`.
- Replace the manual GET + capped read in `fetchTokens` with `httpx.Get(...)`;
  note `fetchTokens` currently takes `(ctx, c, hc, ua, embedURL)` — simplify to
  `(ctx, c, embedURL)` and use `c` fields, OR keep signature but call
  `httpx.Get(ctx, hc, embedURL, ua, 1<<20)`.

### 5. `internal/f1net/cdx.go`

- Replace the GET + capped read in `fetchEmbed` with `httpx.Get(...)` (keep the
  same 1 MiB cap and status handling). `fetchEmbed` can call
  `httpx.Get(ctx, c.http(), embedURL, c.userAgent(), 1<<20)` and map errors as
  today.

### 6. `internal/epg/api.go` / `epg.go`

- `internal/epg/api.go` `getJSON`: replace with `httpx.GetJSON(...)`.
- `internal/epg/epg.go`: delete the local `defaultUA` const; reference
  `httpx.DefaultUA`.
- Replace the `log(ctx)` body with `ctxlog.FromOr(ctx, s.logger)`.
- If Plan 01 (EPG API key) lands, `httpx.GetJSON` gains an optional key header
  or a headers param — coordinate: if the EPG needs `x-api-key`, extend
  `httpx` with a `Headers http.Header` optional arg or add a `GetJSONWithHeaders`.
  **Order:** do this plan's dedupe first with plain UA; add the key param when
  Plan 01 is applied.

### 7. `internal/iptv/channel.go`, `internal/hlsproxy/upstream.go`, `internal/httpserver/server.go`

- Replace each `log()` body with `ctxlog.FromOr(ctx, <receiver field>)`.
  - `iptv`: `fallbackResolver.log` -> `FromOr(ctx, f.logger)`; `Registry.log`
    -> `FromOr(ctx, r.logger)`.
  - `hlsproxy`: `Client.log` -> `FromOr(ctx, c.logger)`.
  - `httpserver.Server.logger` -> `FromOr(ctx, s.log)`.
- Ensure the receiver logger field is never nil when the method runs (the
  constructors already default nil -> `slog.Default()`).

## Tests

- Add a unit test for `ctxlog.FromOr` (non-nil ctx logger wins, nil falls back
  to provided fallback).
- Existing package tests already exercise these paths; no new test coverage
  needed for the HTTP helpers beyond compiling and the package tests passing.
- `internal/f1net` and `internal/epg` tests must remain green (they use their
  own clients via HTTPClient, which the helpers take as an argument, so the mock
  servers still work).

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

- `rg -n "func \(.*\) log\(ctx" internal/` returns **zero** hand-written
  `log()` methods (all use `ctxlog.FromOr`).
- `rg -n "defaultUA" internal/epg/` returns no local const (uses `httpx.DefaultUA`).
- `rg -n "func getJSON" internal/` returns no local `getJSON` (only
  `httpx.GetJSON`).
- No duplicate GET-with-UA-and-cap code remains.
