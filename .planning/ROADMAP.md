# Roadmap

## Phase 01: Deezer Provider

**Goal:** Add a fully-featured Deezer provider with the same capabilities as the existing Tidal provider — track/album/artist downloads, authentication, quality selection, and CLI integration.

**Requirements:** [DEE-01, DEE-02, DEE-03, DEE-04, DEE-05]

- DEE-01: Deezer API client (public + private GW-light API) with track, album, artist metadata fetching
- DEE-02: ARL cookie-based authentication with session initialization and user data retrieval
- DEE-03: Stream URL construction (media.deezer.com + legacy CDN fallback) with Blowfish CBC decryption
- DEE-04: Download track/album/artist with same UX as Tidal (TUI progress, tagging, probing, MusicBrainz)
- DEE-05: CLI integration — register Deezer provider, add `auth login deezer` command, quality mapping

**Plans:** 3/3 plans complete

Plans:
- [x] 01-01-PLAN.md — Deezer types, auth, and API client (metadata layer)
- [x] 01-02-PLAN.md — Stream URL resolution, Blowfish decryption, and download methods
- [x] 01-03-PLAN.md — CLI integration and auth commands

---

## Phase 02: Deezer Hardening

**Goal:** Make the Deezer provider production-quality by fixing all reliability, resilience, and metadata gaps identified in the streamrip comparison — persistent HTTP sessions, auto-refresh tokens, rate limit handling, download retries, geo-restriction fallback, and complete metadata extraction.

**Requirements:** [HARD-01, HARD-02, HARD-03, HARD-04, HARD-05, HARD-06, HARD-07, HARD-08, HARD-09, HARD-10, HARD-11]

- HARD-01: Persistent HTTP session — single http.Client with cookie jar for all Deezer API calls (GW, public, media)
- HARD-02: CSRF token auto-refresh — detect "invalid api token" on GW calls, re-fetch checkForm, retry
- HARD-03: Rate limit handling — detect error codes 4/700 on public API, sleep 5s, retry
- HARD-04: Browser User-Agent — set Chrome UA on all Deezer API calls
- HARD-05: License pre-check — verify CanStreamLossless/CanStreamHQ before requesting quality
- HARD-06: Download retry — 1 retry on download failure
- HARD-07: Small response detection — Content-Length < 20KB = check for error JSON
- HARD-08: FALLBACK geo-restriction support — parse FALLBACK.SNG_ID, retry on media API error 2002
- HARD-09: GW API timeout increase — 10s → 30s
- HARD-10: Genre extraction — map AlbumResponse.Genres to track metadata
- HARD-11: Copyright mapping — map GWSongData.COPYRIGHT to Track.Copyright

**Plans:** 1/4 plans executed

Plans:
- [x] 02-01-PLAN.md — Persistent HTTP session, browser UA, timeout increase
- [ ] 02-02-PLAN.md — CSRF auto-refresh, rate limiting, license pre-check
- [ ] 02-03-PLAN.md — Download retry, small response detection, FALLBACK support
- [ ] 02-04-PLAN.md — Genre extraction, copyright mapping, metadata completeness
