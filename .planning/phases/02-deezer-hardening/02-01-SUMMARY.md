---
phase: 02-deezer-hardening
plan: 01
subsystem: api
tags: [http, cookies, session, user-agent, deezer]

# Dependency graph
requires:
  - phase: 01-deezer-provider
    provides: "Deezer provider with GW, public, and media API calls"
provides:
  - "Persistent http.Client with cookie jar on Deezer struct"
  - "Browser User-Agent on all Deezer API requests"
  - "initSessionClient() and InitSessionFromARL() for session management"
affects: [02-deezer-hardening]

# Tech tracking
tech-stack:
  added: []
  patterns: ["persistent session client with cookie jar", "browser UA on all requests"]

key-files:
  created: []
  modified:
    - provider/deezer/deezer.go
    - provider/deezer/auth.go

key-decisions:
  - "Cookie jar on persistent client replaces all manual AddCookie calls"
  - "initSessionClient reinitializes the entire http.Client (not just the jar) to ensure clean state"
  - "InitSessionFromARL as method on Deezer struct for in-process session init; standalone InitSession kept for CLI auth"
  - "Removed output=3 query param from gwAPICall to match streamrip behavior"

patterns-established:
  - "All Deezer API methods use d.httpClient.Do(req) with browser UA header"
  - "Auth cookies managed via cookie jar, never via manual AddCookie"

requirements-completed: [HARD-01, HARD-04, HARD-09]

# Metrics
duration: 3min
completed: 2026-04-01
---

# Phase 2 Plan 1: Persistent Session Client Summary

**Single persistent http.Client with cookie jar replacing all throwaway clients, browser UA on all API calls, 30s timeout**

## Performance

- **Duration:** 3 min
- **Started:** 2026-04-01T22:23:58Z
- **Completed:** 2026-04-01T22:27:11Z
- **Tasks:** 1
- **Files modified:** 2

## Accomplishments
- Replaced throwaway http.Client creation in gwAPICall, resolveMediaURL with single persistent d.httpClient
- All API calls (GW, public, media) now share one cookie jar that accumulates server-set cookies
- Browser User-Agent (Chrome 124) on every outgoing request
- GW/media timeout increased from 10s to 30s matching streamrip
- Zero manual AddCookie calls remaining in API methods

## Task Commits

Each task was committed atomically:

1. **Task 1: Create persistent session client with cookie jar and browser UA** - `272cc44` (feat)

## Files Created/Modified
- `provider/deezer/deezer.go` - Persistent httpClient with cookie jar, browserUA constant, refactored gwAPICall/publicAPICall/resolveMediaURL, initSessionClient method, updated New()/SetAuth()
- `provider/deezer/auth.go` - InitSessionFromARL method on Deezer struct, InitSession now uses http.NewRequest with browser UA

## Decisions Made
- Cookie jar on persistent client replaces all manual AddCookie calls — cookies accumulated during getUserData (ARL, sid, any server-set) are automatically sent on subsequent calls
- initSessionClient creates a fresh http.Client with new jar rather than modifying existing jar, ensuring clean state
- Standalone InitSession kept for CLI `auth login` command (no Deezer struct available); InitSessionFromARL added for in-process session initialization
- Removed `output=3` query param from gwAPICall to match streamrip's behavior

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Persistent session client complete, ready for 02-02 (retry logic and error handling)
- All API methods use d.httpClient — future changes only need to modify one client

---
*Phase: 02-deezer-hardening*
*Completed: 2026-04-01*
