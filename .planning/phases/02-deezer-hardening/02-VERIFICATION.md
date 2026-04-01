---
phase: 02-deezer-hardening
verified: 2026-04-02T06:30:00Z
status: passed
score: 12/12 must-haves verified
re_verification: false
---

# Phase 02: Deezer Hardening Verification Report

**Phase Goal:** Make the Deezer provider production-quality by fixing all reliability, resilience, and metadata gaps identified in the streamrip comparison — persistent HTTP sessions, auto-refresh tokens, rate limit handling, download retries, geo-restriction fallback, and complete metadata extraction.
**Verified:** 2026-04-02T06:30:00Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | All Deezer API calls (GW, public, media) use the same persistent http.Client with cookie jar | ✓ VERIFIED | `cookiejar.New` in `New()` (deezer.go:66) and `initSessionClient` (deezer.go:151). `d.httpClient.Do(req)` at lines 737 (resolveMediaURL), 1080 (publicAPICall), 1152 (gwAPICall). Zero `AddCookie` calls. |
| 2 | Server-set cookies from getUserData are automatically sent on subsequent API calls | ✓ VERIFIED | `InitSessionFromARL` (auth.go:134) uses `d.httpClient.Do(req)` with jar. Cookie jar accumulates cookies. `initSessionClient` seeds ARL+sid cookies. |
| 3 | All API calls include a browser-like User-Agent header | ✓ VERIFIED | `browserUA` constant (deezer.go:36). Set in: gwAPICall (deezer.go:1150), publicAPICall (deezer.go:1078), resolveMediaURL (deezer.go:735), InitSession (auth.go:78), InitSessionFromARL (auth.go:146), refreshCSRFToken (auth.go:216) |
| 4 | GW API timeout is 30s, not 10s | ✓ VERIFIED | `DefaultTimeout = 30 * time.Second` (deezer.go:32). Used in New() (deezer.go:71) and initSessionClient (deezer.go:163). |
| 5 | If GW API returns 'invalid api token', the CSRF token is auto-refreshed and the call retried | ✓ VERIFIED | `isCSRFTokenError` (deezer.go:1199) checks `VALID_TOKEN_REQUIRED` key and `invalid csrf token`/`invalid api token` strings. `gwAPICallInner` (deezer.go:1175) calls `d.refreshCSRFToken()` and retries once. `refreshCSRFToken` (auth.go:208) re-fetches checkForm via getUserData. |
| 6 | If public API returns error code 4 or 700 (rate limit), the call sleeps 5s and retries | ✓ VERIFIED | `publicAPICallInner` (deezer.go:1100-1108) checks `apiErr.Error.Code == 4 \|\| apiErr.Error.Code == 700`, sleeps 5s, retries once. `PublicAPIError` type in types.go:125-131. |
| 7 | Before requesting FLAC, the provider checks CanStreamLossless; before MP3_320, checks CanStreamHQ | ✓ VERIFIED | `getStreamURL` (deezer.go:641-653): FLAC→MP3_320/MP3_128 fallback on `!d.auth.CanStreamLossless`, MP3_320→MP3_128 on `!d.auth.CanStreamHQ`. Warning messages printed. |
| 8 | Failed downloads are retried once before reporting error | ✓ VERIFIED | `downloadAndDecrypt` (deezer.go:929-942): first attempt, on failure removes .enc, retries once, wraps error as "download failed after retry". |
| 9 | Responses smaller than 20KB are checked for error JSON before treating as audio | ✓ VERIFIED | `downloadEncrypted` (deezer.go:1006-1008): `if totalSize > 0 && totalSize < 20*1024` reads body and returns error. |
| 10 | Geo-restricted tracks (media API error 2002) fall back to FALLBACK.SNG_ID | ✓ VERIFIED | `resolveMediaURL` (deezer.go:763-767) returns `GeoRestrictedError` on code 2002. `getStreamURL` (deezer.go:674-683) checks via `errors.As`, fetches `FALLBACK.SNG_ID` data, recursively resolves. Bounded by `SNG_ID != FALLBACK.SNG_ID` check. |
| 11 | Genre is extracted from album Genres.Data and mapped to provider.Track.Genre | ✓ VERIFIED | `GetAlbum` (deezer.go:199-201,210): `albumGenre = resp.Genres.Data[0].Name`, `track.Genre = albumGenre`. `DownloadTrack` (deezer.go:294-295): `track.Genre = albumResp.Genres.Data[0].Name`. Genre flows to tagger (tagger.go:95-96). |
| 12 | Copyright is mapped from GWSongData.COPYRIGHT to provider.Track.Copyright | ✓ VERIFIED | `StreamInfo.Copyright` field (deezer.go:239). Set from `songData.COPYRIGHT` in both media API path (deezer.go:671) and legacy CDN path (deezer.go:705). Mapped to track in `DownloadTrack` (deezer.go:312) and `DownloadAlbum` (deezer.go:397). Flows to tagger (tagger.go:87-88). |

**Score:** 12/12 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `provider/deezer/deezer.go` | Persistent session client, browser UA, all hardening logic | ✓ VERIFIED | 1341 lines. Contains: cookiejar init, browserUA constant, 30s timeout, initSessionClient, GeoRestrictedError, license pre-check, CSRF retry in gwAPICall, rate limit retry in publicAPICall, download retry, small response detection, FALLBACK handling, genre extraction, copyright mapping. |
| `provider/deezer/auth.go` | InitSessionFromARL, refreshCSRFToken | ✓ VERIFIED | 321 lines. Contains: InitSessionFromARL method (line 134), refreshCSRFToken method (line 208), standalone InitSession (line 51). All use d.httpClient and browserUA. |
| `provider/deezer/types.go` | GWResponse as map, PublicAPIError, FALLBACK on GWSongData, MediaError, ExplicitLyrics | ✓ VERIFIED | 240 lines. GWResponse.Error is `map[string]string` (line 119). PublicAPIError struct (line 125). FALLBACK struct on GWSongData (line 155). MediaError type (line 211). Errors field on MediaData (line 206). ExplicitLyrics on TrackResponse (line 61). |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| auth.go (InitSessionFromARL) | deezer.go (Deezer struct) | `d.httpClient` persistent client with cookies | ✓ WIRED | InitSessionFromARL calls d.initSessionClient, then d.httpClient.Do for getUserData (auth.go:148) |
| deezer.go (gwAPICall) | deezer.go (publicAPICall) | Both use `d.httpClient.Do` | ✓ WIRED | gwAPICall: deezer.go:1152, publicAPICall: deezer.go:1080, resolveMediaURL: deezer.go:737 |
| deezer.go (gwAPICall) | auth.go (refreshCSRFToken) | Detects CSRF error, calls refreshCSRFToken, retries | ✓ WIRED | deezer.go:1175-1182 calls d.refreshCSRFToken(), then gwAPICallInner with retried=true |
| deezer.go (getStreamURL) | deezer.go (quality check) | License capability check before media API | ✓ WIRED | deezer.go:641-653 checks CanStreamLossless/CanStreamHQ before resolveMediaURL call at 662 |
| deezer.go (downloadAndDecrypt) | deezer.go (downloadEncrypted) | Retry wrapper | ✓ WIRED | deezer.go:930-941 calls downloadEncrypted, on error retries once |
| deezer.go (getStreamURL) | deezer.go (resolveMediaURL) | On error 2002, retry with FALLBACK.SNG_ID | ✓ WIRED | deezer.go:674-683 checks GeoRestrictedError, fetches fallback data, recursively calls getStreamURL |
| deezer.go (GetAlbum) | types.go (AlbumResponse.Genres) | Extract first genre | ✓ WIRED | deezer.go:200-201 reads resp.Genres.Data[0].Name |
| deezer.go (getStreamURL) | types.go (GWSongData.COPYRIGHT) | Map to StreamInfo.Copyright | ✓ WIRED | deezer.go:671 and 705 set Copyright from songData.COPYRIGHT |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| HARD-01 | 02-01 | Persistent HTTP session — single http.Client with cookie jar | ✓ SATISFIED | `cookiejar.New` in New() and initSessionClient. All 3 API methods use d.httpClient.Do(). Zero AddCookie calls. |
| HARD-02 | 02-02 | CSRF token auto-refresh — detect "invalid api token", re-fetch checkForm, retry | ✓ SATISFIED | isCSRFTokenError() + refreshCSRFToken() + gwAPICallInner retry-once pattern |
| HARD-03 | 02-02 | Rate limit handling — detect error codes 4/700 on public API, sleep 5s, retry | ✓ SATISFIED | publicAPICallInner checks codes 4/700, sleeps 5s, retries once |
| HARD-04 | 02-01 | Browser User-Agent — set Chrome UA on all Deezer API calls | ✓ SATISFIED | browserUA constant set on all 6 HTTP request sites (3 API methods + 3 auth methods) |
| HARD-05 | 02-02 | License pre-check — verify CanStreamLossless/CanStreamHQ before requesting quality | ✓ SATISFIED | getStreamURL lines 641-653: cascade FLAC→MP3_320→MP3_128 with warning messages |
| HARD-06 | 02-03 | Download retry — 1 retry on download failure | ✓ SATISFIED | downloadAndDecrypt lines 929-941: retry once, clean up .enc on failure |
| HARD-07 | 02-03 | Small response detection — Content-Length < 20KB = check for error JSON | ✓ SATISFIED | downloadEncrypted lines 1006-1008: totalSize < 20*1024 guard |
| HARD-08 | 02-03 | FALLBACK geo-restriction support — parse FALLBACK.SNG_ID, retry on media API error 2002 | ✓ SATISFIED | GeoRestrictedError type, resolveMediaURL checks code 2002, getStreamURL uses FALLBACK.SNG_ID with bounded recursion |
| HARD-09 | 02-01 | GW API timeout increase — 10s → 30s | ✓ SATISFIED | DefaultTimeout = 30 * time.Second (was 10s) |
| HARD-10 | 02-04 | Genre extraction — map AlbumResponse.Genres to track metadata | ✓ SATISFIED | GetAlbum and DownloadTrack extract Genres.Data[0].Name → Track.Genre. Tagger writes genre tag. |
| HARD-11 | 02-04 | Copyright mapping — map GWSongData.COPYRIGHT to Track.Copyright | ✓ SATISFIED | StreamInfo.Copyright populated from songData.COPYRIGHT in both media API and legacy CDN paths. Mapped to Track.Copyright in DownloadTrack and DownloadAlbum. Tagger writes copyright tag. |

**Orphaned requirements:** None. All 11 HARD-* requirements from ROADMAP.md are claimed by plans and verified. Note: REQUIREMENTS.md file does not exist in this project — requirements are defined inline in ROADMAP.md.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | — | — | No anti-patterns found |

- No TODO/FIXME/HACK/PLACEHOLDER comments in any modified files
- No empty implementations or stub returns
- No console.log-only handlers
- `go build ./...` passes cleanly
- `go vet ./...` passes cleanly

### Human Verification Required

### 1. Persistent Session Cookies Survive Across API Calls

**Test:** Download a track with `--debug` flag and verify cookies are present on GW, public, and media API calls
**Expected:** All 3 API types show `Cookie: arl=...; sid=...` in debug output (or the server returns valid data without errors)
**Why human:** Cookie jar behavior requires a live Deezer server and valid ARL to observe in practice

### 2. CSRF Token Auto-Refresh Works in Long Sessions

**Test:** Start a long download session (multiple albums). After some time, if CSRF expires, observe debug output for "CSRF token expired, refreshing..."
**Expected:** Session self-heals without user intervention; subsequent GW calls succeed
**Why human:** CSRF expiry is time-dependent and requires a real expired token to trigger

### 3. Rate Limit Retry on Public API

**Test:** Rapidly make many album/track metadata calls to trigger rate limiting
**Expected:** Debug output shows "Rate limited (code X), retrying in 5s..." and calls succeed after retry
**Why human:** Rate limiting depends on Deezer server state and burst patterns

### 4. License Pre-Check Quality Fallback

**Test:** Use a free Deezer account (no lossless/HQ) and request FLAC quality
**Expected:** Warning printed "account cannot stream lossless, falling back to MP3_128" and track downloads at MP3_128
**Why human:** Requires testing with accounts at different subscription tiers

### 5. Geo-Restriction FALLBACK Resolution

**Test:** Attempt to download a track that is geo-restricted in the user's region but has a FALLBACK version
**Expected:** Debug output shows "Track X geo-restricted, trying FALLBACK Y" and track downloads via alternative ID
**Why human:** Requires finding a geo-restricted track and verifying FALLBACK behavior

### 6. Genre and Copyright in Downloaded Files

**Test:** Download a track from an album with genres, inspect file tags
**Expected:** `ffprobe` shows genre and copyright metadata tags populated
**Why human:** Requires running actual download and inspecting file tags

### Gaps Summary

No gaps found. All 12 observable truths verified, all 11 requirements satisfied, all artifacts substantive and wired, no anti-patterns detected. The codebase matches all PLAN must_haves.

The phase goal — production-quality Deezer provider with persistent sessions, auto-refresh tokens, rate limit handling, download retries, geo-restriction fallback, and complete metadata — is achieved at the code level. Human verification recommended for runtime behavior with live Deezer API.

---

_Verified: 2026-04-02T06:30:00Z_
_Verifier: Claude (gsd-verifier)_
