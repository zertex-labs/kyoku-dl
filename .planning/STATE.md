# State

## Current Phase
Phase 02: Deezer Hardening — Plan 4 of 4

## Progress
[=====================         ] 6/7 plans complete (86%)

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
- [01-03] Tidal auth failure changed from fatal to warning for multi-provider support
- [01-03] Auth login/logout restructured as service-aware subcommands
- [01-03] Deezer auth loaded lazily — only init session if ARL exists
- [01-03] Artist session tracking duplicated for Deezer (simple vs shared helper)
- [02-01] Cookie jar on persistent client replaces all manual AddCookie calls
- [02-01] initSessionClient reinitializes entire http.Client for clean state
- [02-01] InitSessionFromARL for in-process session init; standalone InitSession kept for CLI auth
- [02-01] Removed output=3 query param from gwAPICall to match streamrip
- [02-03] Single retry for download failures — sufficient for transient network issues
- [02-03] 20KB threshold for small response detection — error pages always smaller than audio
- [02-03] Bounded FALLBACK recursion via SNG_ID equality check

## Blockers
None

## Performance Metrics

| Phase-Plan | Duration | Tasks | Files |
|------------|----------|-------|-------|
| 01-01      | 3min     | 2     | 3     |
| 01-02      | 5min     | 2     | 5     |
| 01-03      | 3min     | 2     | 3     |
| 02-01      | 3min     | 1     | 2     |
| 02-03      | 4min     | 2     | 2     |

## Last Session
- **Stopped at:** Completed 02-03-PLAN.md
- **Resume:** Ready for 02-04-PLAN.md
