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
- [ ] 01-03-PLAN.md — CLI integration and auth commands
