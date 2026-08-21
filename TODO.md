# TODO

## EPG is broken when an event is on (OpenF1 now requires auth)

**Status:** open
**Impact:** `/iptv/guide.xml` always returns `503 guide unavailable`, so no
programme guide is served to Jellyfin while a race/event is on.

**Cause:** The EPG pulls the season calendar from `https://api.openf1.org/v1`
(`internal/epg`, config `F1IPTV_EPG_API_URL`). As of Aug 2026 the OpenF1 API
returns **401 Unauthorized on every request** (verified directly for the
`sessions` and `meetings` endpoints, with and without a browser `User-Agent`).
It now requires authentication that the proxy does not provide. The service
logs:

```
epg fetch failed year=2026 error="sessions: https://api.openf1.org/v1/sessions?year=2026: unexpected status 401 Unauthorized"
render guide ... error="sessions: ... 401 Unauthorized"
```

`F1IPTV_EPG_ENABLED=true` is set in the deployed `f1iptv.service`.

**Options:**
- Add an API key/credential path (e.g. `F1IPTV_EPG_API_KEY`) and send it to
  OpenF1, once upstream auth details are known.
- Point `F1IPTV_EPG_API_URL` at an alternate free calendar source that still
  works anonymously.
- Disable the EPG (`F1IPTV_EPG_ENABLED=false`) until a working source exists,
  to avoid the log spam and dead 503 endpoint.

**Verification:** `curl -s http://127.0.0.1:8090/iptv/guide.xml` should return
`200` and valid XMLTV, not `503`.
