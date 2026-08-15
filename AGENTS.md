# f1-jf

Go microservice that proxies live F1 streams from the F1Net dashboard
(`https://f1net.vercel.app`) as a Jellyfin-compatible IPTV source.

## Commands

Run inside the devenv shell (`devenv shell`) or the flake dev shell
(`nix develop`):

- Build all: `go build ./...`
- Test: `go test ./...`
- Vet: `go vet ./...`
- Format: `gofmt -l .` (report), `gofmt -w .` (write)
- Static analysis: `staticcheck ./...`
- Full test run (hooks + tests): `devenv test` (inside `devenv shell`)
- Run the IPTV service: `go run ./cmd/f1iptv` (config via `F1IPTV_*` env vars)
- Debug CLI: `go run ./cmd/f1m3u8 [-quality 1080p] [-verify]`

Nix:

- `nix build .#f1iptv`
- `nix flake check`
- `nix develop`
- NixOS module: `modules/f1iptv.nix` (exposed as `nixosModules.default`)

## Layout

- `cmd/f1iptv` — thin entrypoint: loads config, wires service, graceful shutdown.
- `cmd/f1m3u8` — dev CLI that resolves sources to m3u8 URLs.
- `internal/config` — 12-factor env configuration (`F1IPTV_*`).
- `internal/f1net` — fetches the dashboard source list and resolves embed pages into playable m3u8 streams (host-specific resolvers; `registry.go` dispatches).
- `internal/iptv` — domain: `Channel`, `Registry` (TTL-cached resolution), `ChannelsFromQualities`.
- `internal/epg` — XMLTV program guide from the OpenF1 F1 calendar (cached, `RenderXML`).
- `internal/hlsproxy` — transport-agnostic upstream fetch + HLS playlist URI rewriting.
- `internal/httpserver` — HTTP layer: routes, handlers, slog middleware.
- `package.nix` / `flake.nix` / `modules/f1iptv.nix` — Nix packaging (single derivation installing both `f1iptv` and `f1m3u8` binaries) and NixOS deployment module.
- `docs/STREAMING.md` — how the upstream m3u8 extraction works.

## Conventions

- External Go dependencies are allowed, but each must justify its use and be pinned in go.mod/go.sum. Currently: `github.com/Eyevinn/hls-m3u8` (HLS parsing), `golang.org/x/net` (HTML tokenizing), `golang.org/x/sync` (singleflight), `github.com/sherif-fanous/xmltv` (XMLTV rendering).
- `log/slog` for structured logging; services take a `*slog.Logger` via options.
- Config is env-based, read in `internal/config`; defaults documented on each field. EPG: `F1IPTV_EPG_ENABLED`, `F1IPTV_EPG_API_URL`, `F1IPTV_EPG_TTL`, `F1IPTV_EPG_YEAR`.
- External HTTP requests must send a browser-like `User-Agent` (see `internal/f1net` `defaultUA`); upstream sources are Referer/Origin gated.
- New embed hosts: implement the `resolver` interface and register it in `internal/f1net/registry.go`.
- Tests are offline (httptest mocks); never rely on the live F1Net/streamfree services in unit tests.
- Nix files are formatted with `nixfmt-rfc-style`; no dead code (`deadnix`), no lint errors (`statix`).

## Verification

Run `go test ./...` plus `nix flake check` before considering work done. A live smoke test of the service: start `go run ./cmd/f1iptv`, then curl `/healthz`, `/iptv/playlist.m3u`, `/iptv/stream/f1-1080p`, and `/iptv/guide.xml`.
