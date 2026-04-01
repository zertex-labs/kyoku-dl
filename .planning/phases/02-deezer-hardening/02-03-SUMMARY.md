---
phase: 02-deezer-hardening
plan: 03
subsystem: api
tags: [retry, error-detection, geo-restriction, fallback, deezer, resilience]

# Dependency graph
requires:
  - phase: 02-deezer-hardening
    provides: "Persistent session client with cookie jar and browser UA"
provides:
  - "1-retry download in downloadAndDecrypt"
  - "Small response detection (< 20KB) to catch error JSON masquerading as audio"
  - "FALLBACK track resolution for geo-restricted content (error 2002)"
  - "GeoRestrictedError type for clean error propagation"
affects: [02-deezer-hardening]

# Tech tracking
tech-stack:
  added: []
  patterns: ["retry-once on download failure", "content-length guard for error detection", "FALLBACK SNG_ID recursive resolution with bounded recursion"]

key-files:
  created: []
  modified:
    - provider/deezer/types.go
    - provider/deezer/deezer.go

key-decisions:
  - "Single retry (not exponential backoff) — keeps it simple, one network hiccup handled"
  - "20KB threshold for small response detection — audio files are always larger"
  - "Bounded FALLBACK recursion via SNG_ID != check rather than explicit flag parameter"

patterns-established:
  - "GeoRestrictedError for type-safe geo-restriction handling with errors.As"
  - "Content-Length guard before download loop to reject non-audio responses"

requirements-completed: [HARD-06, HARD-07, HARD-08]

# Metrics
duration: 4min
completed: 2026-04-01
---

# Phase 2 Plan 3: Download Retry, Small Response Detection, and FALLBACK Geo-Restriction Summary

**1-retry download with small-response error detection and FALLBACK track resolution for geo-restricted content via media API error code 2002**

## Performance

- **Duration:** 4 min
- **Started:** 2026-04-01T22:29:51Z
- **Completed:** 2026-04-01T22:34:14Z
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments
- Downloads retry once on failure before reporting error, handling transient network hiccups
- Responses smaller than 20KB are read and checked as error text instead of being written as corrupt audio files
- Media API error code 2002 (geo-restriction) triggers automatic FALLBACK track lookup
- FALLBACK recursion bounded by SNG_ID equality check to prevent infinite loops
- GeoRestrictedError type enables clean error propagation via errors.As

## Task Commits

Each task was committed atomically:

1. **Task 1: Add FALLBACK field and media error types** - `aa3fa9e` (feat)
2. **Task 2: Add download retry, small response detection, and FALLBACK geo-restriction handling** - `7c831ab` (feat)

## Files Created/Modified
- `provider/deezer/types.go` - FALLBACK struct on GWSongData, Errors field on MediaData, MediaError type
- `provider/deezer/deezer.go` - Download retry in downloadAndDecrypt, small response detection in downloadEncrypted, GeoRestrictedError type, FALLBACK handling in getStreamURL, geo-restriction detection in resolveMediaURL

## Decisions Made
- Single retry (not exponential backoff) for download failures — sufficient for transient network issues, keeps code simple
- 20KB threshold for small response detection — all real audio files exceed this; error pages from CDN are typically much smaller
- Bounded FALLBACK recursion by checking `songData.FALLBACK.SNG_ID != songData.SNG_ID` rather than adding an explicit flag parameter — cleaner API, natural termination since fallback tracks typically have empty FALLBACK fields

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Resilience features complete: retry, error detection, geo-restriction fallback all in place
- Ready for 02-04 (remaining hardening work)

---
*Phase: 02-deezer-hardening*
*Completed: 2026-04-01*
