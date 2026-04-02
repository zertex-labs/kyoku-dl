# Streamrip vs kyoku-dl: Deezer Implementation Comparison

Reference: [nathom/streamrip](https://github.com/nathom/streamrip) (Python, uses `deezer-py` library by RemixDev)

---

## Architecture

| Aspect | streamrip | kyoku-dl |
|--------|-----------|----------|
| Language | Python (async) | Go |
| Deezer API layer | Delegates to `deezer-py` library (synchronous `requests.Session`) | Direct HTTP calls, manual cookie handling |
| Session management | Single `requests.Session` with persistent cookie jar shared across ALL API calls | Fresh `http.Client` per GW call, separate client for media API, bare client for public API |
| Download HTTP | `aiohttp.ClientSession` (async) | `provider.NewHTTPClient()` (sync, no cookies) |

**Key difference:** streamrip uses a single persistent session for all Deezer API calls (GW, media, public). kyoku-dl creates fresh clients per call and manually attaches cookies. This means kyoku-dl misses any cookies the server sets during the session (e.g. `sid` updates).

**Recommendation:** Create a persistent `http.Client` with a `cookiejar` on the `Deezer` struct, initialized once during `InitSession`. Use it for all API calls (GW, media, public). Stop creating throwaway clients.

---

## Authentication

| Aspect | streamrip | kyoku-dl |
|--------|-----------|----------|
| Auth method | ARL cookie only | ARL cookie only |
| Storage | TOML config file (`~/.config/streamrip/config.toml`) | JSON file (`~/.config/kyoku-dl/deezer-auth.json`) |
| Session init | `login_via_arl()` → sets cookie on session → calls `getUserData` → extracts `checkForm`, `license_token`, user caps | `InitSession()` → creates temp cookie jar → calls `getUserData` → extracts same fields, saves to JSON |
| Token refresh | CSRF auto-refresh on `"invalid api token"` error (re-fetches `checkForm` and retries) | **None** — if `api_token` expires mid-session, calls fail |
| ARL expiry | Login returns `False` → `AuthenticationError` | Session init fails → prompts for new ARL |
| License check before download | Yes — checks `can_stream_lossless`/`can_stream_hq` before calling media API, raises `WrongLicense` early | **No** — sends the request regardless, finds out from error response |

**Recommendation:** 
1. Add CSRF token auto-refresh: if a GW call returns `"invalid api token"`, re-call `getUserData` to get a fresh `checkForm`, then retry the original call.
2. Add license pre-check: before requesting FLAC, verify `CanStreamLossless`; before MP3_320, verify `CanStreamHQ`. Fail fast with a clear error message.

---

## GW (Private) API

| Aspect | streamrip (`deezer-py`) | kyoku-dl |
|--------|-------------------------|----------|
| Endpoint | `http://www.deezer.com/ajax/gw-light.php` (HTTP!) | `https://www.deezer.com/ajax/gw-light.php` (HTTPS) |
| Query params | `api_version=1.0`, `api_token=<checkForm>`, `input=3`, `method=<method>` | Same + `output=3` (extra param) |
| Body | `json=args` (via requests) | Manual JSON marshal |
| Headers | Browser-like User-Agent (Chrome 79) | None set beyond Content-Type |
| api_token for getUserData | Explicit `"null"` string | Empty string (sent as `api_token=`) |
| Error: invalid CSRF | Auto-refresh token + retry | **No handling** — error propagates |
| Error: FALLBACK | Merges fallback params + retry (for geo-restricted tracks) | **No handling** — no FALLBACK field in GWSongData |
| Error: connection | Sleep 2s + infinite retry | **No retry** |
| Timeout | 30 seconds | 10 seconds |

**Differences that matter:**
1. **Missing FALLBACK support**: streamrip's GW `song.getData` response includes a `FALLBACK` field with an alternative `SNG_ID` for geo-restricted tracks. kyoku-dl doesn't parse this field and can't fall back to alternative track versions.
2. **No CSRF auto-refresh**: kyoku-dl's GW calls will fail if the session is long-lived and the `checkForm` token expires.
3. **No User-Agent header**: GW calls don't set a browser User-Agent. This could trigger bot detection.
4. **Extra `output=3` param**: kyoku-dl sends `output=3` which streamrip does not. Probably harmless but unnecessary.

**Recommendation:**
1. Add `FALLBACK` field to `GWSongData` type.
2. Add User-Agent header to GW calls.
3. Add CSRF token auto-refresh on `"invalid api token"` error.
4. Increase GW timeout to 30s.

---

## Media API (media.deezer.com/v1/get_url)

| Aspect | streamrip (`deezer-py`) | kyoku-dl |
|--------|-------------------------|----------|
| Auth | Uses the same `requests.Session` that has ARL cookie set | Manually attaches ARL + sid cookies per request |
| Request body | Identical structure: `license_token`, `media[{type: "FULL", formats: [{cipher: "BF_CBC_STRIPE", format: quality}]}]`, `track_tokens` | **Same** ✓ |
| Error parsing | Checks `data[].errors[]`, handles code 2002 (geo-restriction) | Only checks for empty `data`/`media`/`sources` arrays — **doesn't parse `errors` field** |
| Geo-restriction (code 2002) | Retries with `FALLBACK.SNG_ID` once | **No geo-restriction handling** |
| License pre-check | Checks `can_stream_lossless`/`can_stream_hq` BEFORE calling API | **No pre-check** |

**Recommendation:**
1. Parse the `errors` array in `MediaData` responses. Add an `Errors` field to `MediaData` type.
2. Handle error code 2002 with FALLBACK retry.
3. Add license pre-check before calling media API.

---

## Stream URL Resolution & Fallback

| Aspect | streamrip | kyoku-dl |
|--------|-----------|----------|
| Primary | `media.deezer.com/v1/get_url` | Same ✓ |
| Fallback | Encrypted CDN URL (`e-cdns-proxy-X.dzcdn.net/mobile/1/...`) | Same ✓ |
| Fallback quality code | Hardcoded to `1` (MP3_128) | Maps quality name to code (1/3/9) |
| CDN URL padding | Dots (`.`) | Dots (`.`) ✓ (fixed) |
| CDN URL separator | `\xa4` | `\xa4` ✓ |
| AES key | `b"jo6aey6haid2Teih"` ECB | Same ✓ |
| Quality fallback | Checks `FILESIZE_*`, steps down quality if `lower_quality_if_not_available` config is true | Always falls back (no config toggle) |

**Note:** streamrip hardcodes the legacy CDN fallback quality to `1` (MP3_128) regardless of requested quality. kyoku-dl correctly maps the quality, which is more correct behavior — but the legacy CDN is dead anyway.

---

## Download & Decryption

| Aspect | streamrip | kyoku-dl |
|--------|-----------|----------|
| Encryption detection | Regex: URL contains `/mobile/` or `/media/` → encrypted | Assumes ALL streams are encrypted (`IsEncrypted: true` always) |
| Download approach | Reads entire stream into memory `bytearray`, then decrypts | Downloads to temp `.enc` file, then decrypts to final file |
| Decryption approach | Processes in 6144-byte (3×2048) super-chunks: decrypt first 2048, pass through remaining 4096 | Reads 2048-byte chunks sequentially, decrypts every 3rd chunk (index % 3 == 0) |
| Blowfish key derivation | MD5 of track ID → XOR `md5[:16]` ⊕ `md5[16:]` ⊕ secret | **Same** ✓ |
| Blowfish IV | `b"\x00\x01\x02\x03\x04\x05\x06\x07"` | `[]byte{0, 1, 2, 3, 4, 5, 6, 7}` ✓ |
| Blowfish secret | `"g4el58wc0zvf9na1"` | Same ✓ |
| Short chunk handling | If chunk < 2048 bytes → plaintext (no decrypt) | Same ✓ (checks `n == chunkSize`) |
| Small response detection | If Content-Length < 20KB → parse as error JSON | **No small response detection** |
| Download retry | 1 retry on any exception | **No retry** |

**Both approaches are functionally equivalent** for decryption. streamrip's "3×2048 super-chunk" and kyoku-dl's "every 3rd 2048 chunk" produce identical output.

**kyoku-dl's file-based approach is better** for large FLAC files (avoids loading 30-50MB into memory).

**Recommendation:**
1. Add small response detection: if Content-Length < 20KB, read body and check for error JSON before treating as audio.
2. Add 1 retry on download failure (matching streamrip).
3. The encryption detection (regex vs always-encrypted) difference is academic — all Deezer streams from both media API and legacy CDN are encrypted.

---

## URL Handling

| Aspect | streamrip | kyoku-dl |
|--------|-----------|----------|
| Standard URLs | Regex: `deezer\.com.+(album|artist|track|playlist)/.+([\w-]+)` | Regex: `deezer\.com(?:/[a-z]{2})?/(track|album|artist)/(\d+)` |
| Share links (`deezer.page.link`) | `DeezerDynamicURL` class — follows redirect, parses HTML for standard link | `provider.ResolveURL()` — follows HTTP redirects to final URL ✓ |
| Share links (`link.deezer.com`) | Not explicitly handled (may work via redirect) | `provider.ResolveURL()` handles ✓ |
| Playlist support | Yes | **No** |

**kyoku-dl's URL regex is stricter** (requires numeric ID), which is actually better — avoids false matches. streamrip's regex could match non-numeric slugs.

**Recommendation:** Add playlist support as a future feature (not urgent).

---

## Metadata

| Aspect | streamrip | kyoku-dl |
|--------|-----------|----------|
| Source | Public API (`api.deezer.com`) | Same ✓ |
| Album enrichment | Full album metadata fetch for each track | Same (fetches album for date, album artist, total tracks) ✓ |
| Genre | Extracted from album `genres.data[].name` | **Not extracted** from Deezer (relies on MusicBrainz) |
| Copyright | Not extracted | Parsed from GW `song.getData` (`COPYRIGHT` field) but **not mapped** to `provider.Track` |
| Explicit flag | `explicit_lyrics` / `parental_warning` | **Not extracted** |
| Composer | Not available from Deezer | Not extracted |
| Cover art | Uses public API `cover_xl` (1000×1000) from album response | Uses `md5_image` to construct CDN URL at 1000×1000 |
| Quality metadata | Hardcoded 16-bit/44.1kHz | Not reported (correct — Deezer doesn't expose this) |

**Recommendation:**
1. Map `Copyright` from `GWSongData.COPYRIGHT` to `provider.Track.Copyright`.
2. Extract genre from `AlbumResponse.Genres.Data[].Name` and map to `provider.Track.Genre`.
3. Consider adding explicit lyrics flag to `provider.Track` in future.

---

## Error Handling

| Aspect | streamrip | kyoku-dl |
|--------|-----------|----------|
| Rate limiting (public API) | Sleep 5s + retry on error codes 4/700 | **No rate limit handling** |
| Rate limiting (GW API) | Sleep 2s + retry on connection error | **No retry** |
| CSRF token expiry | Auto-refresh + retry | **No handling** |
| Geo-restriction | Retry with FALLBACK SNG_ID | **No handling** |
| Download failure | 1 retry | **No retry** |
| Small/error response | Parse JSON if < 20KB | **No detection** |
| ARL expiry | Fail login → user must provide new ARL | Same ✓ |
| Track token expiry | `TRACK_TOKEN_EXPIRE` exists in data but **not explicitly checked** by streamrip either | Same — not checked ✓ |

**This is the biggest gap.** kyoku-dl has essentially no resilience — any transient failure immediately surfaces as an error to the user.

**Recommendation (priority order):**
1. **Rate limit handling on public API**: detect error codes 4 and 700, sleep 5s, retry.
2. **GW API CSRF refresh**: detect `"invalid api token"` error, re-init session, retry.
3. **Download retry**: 1 retry on failure.
4. **Small response detection**: check Content-Length before treating response as audio.
5. **FALLBACK support**: parse `FALLBACK.SNG_ID` from GW song data, retry on geo-restriction (code 2002).

---

## HTTP Headers

| Context | streamrip (`deezer-py`) | kyoku-dl |
|---------|-------------------------|----------|
| GW API | `User-Agent: Mozilla/5.0 (X11; Linux x86_64) ... Chrome/79.0.3945.130` | **No User-Agent** |
| Media API | Same session with Chrome UA | No User-Agent on media requests |
| Public API | Same session with Chrome UA | **No User-Agent** |
| Downloads | `User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:83.0) Gecko/20100101 Firefox/83.0` | `User-Agent: kyoku-dl/1.0` |

**Recommendation:** Set a browser-like User-Agent on all Deezer API calls. The `kyoku-dl/1.0` UA is fine for download streams but API calls should mimic a browser.

---

## Session Persistence

| Aspect | streamrip | kyoku-dl |
|--------|-----------|----------|
| Cookie jar lifetime | Entire session (one `requests.Session`) | Per-call (fresh jar each GW call, no jar for public/media) |
| Session reuse | Yes — cookies accumulated across calls | **No** — cookies lost after each call |
| Server-set cookies | Automatically captured and replayed | **Lost** — only manually-set ARL/sid are sent |

**This is likely a contributing factor to Bug 2.** The Deezer server may set additional cookies during the `getUserData` call that are needed for subsequent API calls. With a persistent session, these are automatically included. With per-call clients, they're lost.

**Recommendation:** Store a persistent `http.Client` with `cookiejar.Jar` on the `Deezer` struct. Initialize it during auth (set ARL cookie, call getUserData to populate session cookies). Reuse for ALL subsequent calls.

---

## Summary: Priority Fixes

### Must Fix (correctness / reliability)

1. **Persistent session client** — Stop creating throwaway HTTP clients. Use one client with a cookie jar for the lifetime of the provider instance. This likely fixes remaining media API issues.
2. **CSRF token auto-refresh** — If GW API returns `"invalid api token"`, re-call `getUserData` to refresh `checkForm`, then retry.
3. **Rate limit handling** — Sleep 5s and retry on public API error codes 4/700.
4. **Browser User-Agent** — Set Chrome/Firefox UA on all API calls.
5. **License pre-check** — Check `CanStreamLossless`/`CanStreamHQ` before requesting quality that the account can't stream.

### Should Fix (resilience)

6. **Download retry** — 1 retry on download failure.
7. **Small response detection** — If Content-Length < 20KB, check for error JSON.
8. **FALLBACK support** — Parse `FALLBACK.SNG_ID` from song data, retry geo-restricted tracks.
9. **GW API timeout** — Increase from 10s to 30s.

### Nice to Have (completeness)

10. **Genre extraction** — Map from `AlbumResponse.Genres` to track metadata.
11. **Copyright mapping** — Map `GWSongData.COPYRIGHT` to `provider.Track.Copyright`.
12. **Playlist support** — Parse and download Deezer playlists.
13. **Explicit lyrics flag** — Surface in metadata.

---

## Things kyoku-dl Does Better

1. **File-based decryption** — Downloads encrypted stream to temp file, decrypts to final. Avoids loading entire FLAC into memory (30-50MB). streamrip loads everything into a `bytearray`.
2. **Streaming progress** — Real-time TUI progress with speed/percentage during download. streamrip only has a basic callback.
3. **HTTPS for GW API** — kyoku-dl uses HTTPS, streamrip uses HTTP (!) for the GW-light endpoint.
4. **Quality code in legacy CDN** — kyoku-dl correctly maps quality to numeric code (1/3/9). streamrip hardcodes `1`.
5. **Stricter URL regex** — kyoku-dl requires numeric IDs, preventing false matches.
6. **Session resume** — kyoku-dl saves remaining URLs on interrupt for resume. streamrip has no equivalent.
7. **Artist selection UI** — Interactive TUI for album selection with release type filtering. streamrip downloads all or nothing.
