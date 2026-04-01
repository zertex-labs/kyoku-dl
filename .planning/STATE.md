# State

## Current Phase
Phase 01: Deezer Provider — Plan 3 of 3

## Progress
[====================..........] 2/3 plans complete (67%)

## Decisions
- Use ARL cookie auth (same as deemix/d-fi, no OAuth)
- Use media.deezer.com modern API for stream URLs, with legacy CDN fallback
- Blowfish CBC decryption with golang.org/x/crypto/blowfish
- Quality mapping: normal→MP3_128, high→MP3_320, lossless→FLAC
- Same file structure as Tidal provider: provider/deezer/{auth.go, types.go, deezer.go, crypto.go}
- [01-01] Followed Tidal package structure for consistency (types.go, auth.go, deezer.go)
- [01-01] Used cookiejar for GW-light API calls for transparent ARL cookie handling
- [01-01] Private API types use exact Deezer field names as JSON tags for 1:1 mapping
- [01-02] Custom TUI download loop instead of ui.DownloadManager.Run() — Deezer needs decrypt step between download and tagging
- [01-02] Download encrypted to temp .enc file, decrypt to final path — simpler than streaming decryption
- [01-02] All Deezer streams treated as Blowfish encrypted (both modern media API and legacy CDN)
- [01-02] Quality fallback based on GW song data FILESIZE fields

## Blockers
None

## Performance Metrics

| Phase-Plan | Duration | Tasks | Files |
|------------|----------|-------|-------|
| 01-01      | 3min     | 2     | 3     |
| 01-02      | 5min     | 2     | 5     |

## Last Session
- **Stopped at:** Completed 01-02-PLAN.md
- **Resume:** 01-03-PLAN.md (CLI integration and auth commands)
