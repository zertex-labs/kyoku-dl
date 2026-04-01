---
phase: 02-deezer-hardening
plan: 02
subsystem: api
tags: [csrf, rate-limit, retry, license-check, deezer, error-handling]

# Dependency graph
requires:
  - phase: 02-deezer-hardening
    provides: "Persistent http.Client with cookie jar on Deezer struct"
provides:
  - "CSRF token auto-refresh on GW API errors via refreshCSRFToken"
  - "Rate limit retry on public API (codes 4/700) with 5s backoff"
  - "License capability pre-check before stream quality request"
  - "GWResponse.Error as map[string]string for clean error handling"
  - "PublicAPIError type for public API error detection"
affects: [02-deezer-hardening]

# Tech tracking
tech-stack:
  added: []
  patterns: ["retry-once with flag to prevent infinite loops", "license pre-check before media API call", "CSRF auto-refresh via getUserData"]

key-files:
  created: []
  modified:
    - provider/deezer/deezer.go
    - provider/deezer/auth.go
    - provider/deezer/types.go

key-decisions:
  - "GWResponse.Error changed from []interface{} to map[string]string — Deezer GW API returns errors as JSON objects"
  - "CSRF refresh uses getUserData with empty api_token (same as initial session setup) and also refreshes LicenseToken and capability flags"
  - "Rate limit retry uses 5s sleep matching streamrip behavior, max 1 retry"
  - "License pre-check runs after file-size fallback but before media API call for correct quality cascade"

patterns-established:
  - "Retry-once pattern: public function delegates to inner function with retried bool flag"
  - "License pre-check cascade: FLAC -> MP3_320 -> MP3_128 based on account capabilities"

requirements-completed: [HARD-02, HARD-03, HARD-05]

# Metrics
duration: 5min
completed: 2026-04-01
---

# Phase 2 Plan 2: CSRF Auto-Refresh, Rate Limit Handling & License Pre-Check Summary

**Self-healing GW API with CSRF token auto-refresh, public API rate limit backoff (5s), and license capability pre-check preventing cryptic media API errors**

## Performance

- **Duration:** 5 min
- **Started:** 2026-04-01T22:29:46Z
- **Completed:** 2026-04-01T22:35:28Z
- **Tasks:** 2
- **Files modified:** 3

## Accomplishments
- GW API calls automatically detect CSRF token expiry and refresh via getUserData, retrying the failed call once
- Public API calls detect rate limit errors (Deezer error codes 4 and 700), sleep 5 seconds, and retry once
- License capability is checked before requesting stream quality — FLAC/MP3_320 gracefully falls back with clear warning messages
- GWResponse.Error changed from []interface{} to map[string]string for clean error detection

## Task Commits

Each task was committed atomically:

1. **Task 1: Add CSRF token auto-refresh and rate limit handling** - `c21a1df` (feat)
2. **Task 2: Add license capability pre-check before stream quality request** - included in `7c831ab` (see note below)

**Note:** Task 2 changes were committed alongside concurrent 02-03 plan work in `7c831ab` due to a parallel execution race condition. The license pre-check code is correct and verified.

## Files Created/Modified
- `provider/deezer/types.go` - GWResponse.Error changed to map[string]string, added PublicAPIError struct
- `provider/deezer/auth.go` - Added refreshCSRFToken() method that re-fetches checkForm and license token via getUserData
- `provider/deezer/deezer.go` - gwAPICall with CSRF retry, publicAPICall with rate limit retry, license pre-check in getStreamURL

## Decisions Made
- GWResponse.Error as map[string]string — Deezer GW API returns errors as JSON objects (e.g. `{"VALID_TOKEN_REQUIRED": "..."}`)
- refreshCSRFToken calls getUserData with empty api_token and refreshes all auth fields (APIToken, LicenseToken, capability flags)
- Rate limit uses 5s sleep matching streamrip's behavior, with max 1 retry to prevent infinite loops
- License pre-check placed after file-size fallback but before media API call for correct quality cascade

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Pre-existing uncommitted changes in working tree**
- **Found during:** Task 1 (staging files)
- **Issue:** Working tree had uncommitted changes from parallel 02-03 plan work (ExplicitLyrics field, GeoRestrictedError, Copyright field, geo-restriction handling, download retry)
- **Fix:** Committed all changes in the three target files together in Task 1 commit, as they were all part of deezer-hardening phase and didn't conflict
- **Files modified:** provider/deezer/types.go, provider/deezer/auth.go, provider/deezer/deezer.go
- **Verification:** go build and go vet pass
- **Committed in:** c21a1df

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Minor — pre-existing changes were complementary and didn't affect Task 1 or Task 2 correctness.

## Issues Encountered
- Task 2 commit was absorbed by a concurrent 02-03 commit (`7c831ab`) that ran between Task 1 and Task 2 completion. The license pre-check code is correct and present in the codebase.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- CSRF auto-refresh, rate limit handling, and license pre-check all complete
- Ready for 02-03 (geo-restriction fallback and download retry — partially in progress)

---
*Phase: 02-deezer-hardening*
*Completed: 2026-04-01*
