# Extracting the m3u8 stream from F1Net

F1Net (`https://f1net.vercel.app`) is a React SPA that displays a live F1 stream by
loading third-party embed player pages into full-screen iframes. It does not host
or expose an m3u8 itself — the m3u8 lives inside the embed players.

## How F1Net works

1. `https://f1net.vercel.app/source.txt` is a plain-text list of `Name | URL`
   lines. The `/stream` page fetches it with cache-busting (`?_t=Date.now()`).
2. Each URL is loaded into a full-screen `<iframe>`.
3. Each URL is a third-party embed player page that resolves its own m3u8 at
   runtime.

## The resolvable source: streamfree.top

Of the sources listed in `source.txt`, the one that is straightforward to
resolve is:

```
Stream 2 | https://streamfree.top/embed/racing/skyf1?quality=720p&category=racing
```

The embed page builds its m3u8 at runtime from two JSON endpoints plus tokens
hardcoded in its page source.

### 1. Available qualities

```bash
curl -s https://streamfree.top/api/stream-status/skyf1
```

```json
{"stream_key":"skyf1","available":true,"qualities":{"540p":false,"720p":true,"1080p":true,"2160p":false}}
```

### 2. Server origin

```bash
curl -s https://streamfree.top/get-stream-key/skyf1
```

```json
{"stream_key":"skyf1","is_external":false,"server_name":"origin","server_domain":""}
```

`server_name: "origin"` means the m3u8 is served directly from
`streamfree.top` (no CDN wrapper path).

### 3. m3u8 URL template

```
https://streamfree.top/live/skyf1{quality}/index.m3u8?_t={t}&_e={e}&_n={n}
```

The `_t` tokens are hardcoded in the embed page's JS `_0x` map
(`_e=1786778988`, `_n=a78e112785e81d85` are shared by all qualities):

| Quality | `_t` |
| --- | --- |
| 540p | `hPR1k3Ozzj2z_AgryyUFnQ` |
| 720p | `7nd4GyPtWLteWsFrcm6Xdw` |
| 1080p | `TQy82_FCjaZ9m9PxWhjd5A` |
| 2160p | `b0YhD0Vi_qA7LLblTFrGgA` |

### 4. Working m3u8 URLs (verified live)

```
https://streamfree.top/live/skyf11080p/index.m3u8?_t=TQy82_FCjaZ9m9PxWhjd5A&_e=1786778988&_n=a78e112785e81d85
https://streamfree.top/live/skyf1720p/index.m3u8?_t=7nd4GyPtWLteWsFrcm6Xdw&_e=1786778988&_n=a78e112785e81d85
```

Both return HTTP 200 with a valid `#EXTM3U` playlist. The stream is
referrer/origin-gated, so requests need these headers:

```bash
curl "https://streamfree.top/live/skyf11080p/index.m3u8?_t=TQy82_FCjaZ9m9PxWhjd5A&_e=1786778988&_n=a78e112785e81d85" \
  -H "Referer: https://streamfree.top/embed/racing/skyf1" \
  -H "Origin: https://streamfree.top" \
  -H "User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"
```

Notes:

- Playlist segments are obfuscated as `.js` files (e.g. `37327.js`), served
  relative to the playlist URL.
- To play in VLC/ffmpeg, you must forward the same headers (or proxy the
  stream). In a browser, the stream plays at `https://streamfree.top/embed/racing/skyf1`.

## In-browser shortcut

1. Open `https://f1net.vercel.app/stream`.
2. Open DevTools → Network → filter `m3u8`.
3. Reload or trigger a quality change in the iframe player — the
   `skyf1{quality}/index.m3u8` request appears with its tokenized URL and headers.

## How f1iptv resolves

The f1iptv service does not hardcode a single source. It fetches the whole
dashboard `/source.txt` list, TTL-caches it, and refreshes it lazily whenever it
goes stale (keeping the last-good list if a refresh fails). Resolution tries
the ordered quality fallback list (2160p → 1080p → 720p by default) as an outer
loop and the sources as an inner loop: every source is tried at 2160p, then
every source at 1080p, then every source at 720p, and the first success wins.
While a raw-TS session is streaming, a dead current source triggers a
re-resolution against a freshly refreshed source list so playback falls over to
another working source mid-session.

## Other sources (not worth it)

| Source | Why |
| --- | --- |
| `westreamf1.com/westreamf1.php` | Sets `streamUrl` to the literal string `" offline"` and plants fake `my-s3-bucket.s3.us-west-1.amazonaws.com/...` decoy requests — anti-scraper honeypots. Real URL not in static HTML. |
| `embdlol.st/embed/<uuid>` | Next.js app that fetches its stream from its own API by UUID; all current UUIDs returned "Failed to fetch stream" (expired/offline). |
| `logic.icelanders.st` / `videocdn-4726.website` | Not examined; likely the same "fetch JSON, build m3u8" pattern. |
