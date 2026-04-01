---
phase: 01-deezer-provider
plan: 02
subsystem: provider
tags: [blowfish, cbc, decryption, deezer, streaming, tui, bubbletea, golang-x-crypto]

# Dependency graph
requires:
  - phase: 01-deezer-provider/01-01
    provides: "Deezer types, auth, GW-light API client, metadata methods"
provides:
  - "Blowfish CBC stream decryption (DeriveBlowfishKey, DecryptStream)"
  - "Legacy CDN URL construction with AES-ECB encryption"
  - "Stream URL resolution via media.deezer.com with legacy CDN fallback"
  - "Quality fallback chain: FLAC → MP3_320 → MP3_128"
  - "Complete download methods: DownloadTrack, DownloadAlbum, DownloadArtist"
  - "Custom TUI download loop with decrypt step between download and tagging"
affects: [01-deezer-provider/01-03]

# Tech tracking
tech-stack:
  added: [golang.org/x/crypto/blowfish]
  patterns: [custom-tui-download-loop, download-decrypt-tag-probe-pipeline, quality-fallback-chain]

key-files:
  created:
    - provider/deezer/crypto.go
    - provider/deezer/crypto_test.go
  modified:
    - provider/deezer/deezer.go
    - go.mod
    - go.sum

key-decisions:
  - "Custom TUI download loop instead of ui.DownloadManager.Run() — Deezer needs decrypt step between download and tagging"
  - "Download encrypted to temp .enc file, decrypt to final path — simpler than streaming decryption with progress"
  - "All Deezer streams treated as Blowfish encrypted (both modern media API and legacy CDN)"
  - "Quality fallback based on GW song data FILESIZE fields (FLAC→MP3_320→MP3_128)"

patterns-established:
  - "Custom download loop pattern: create tea.Program manually, run downloadAllDeezer goroutine"
  - "Download-decrypt-tag-probe pipeline for encrypted providers"
  - "StreamInfo struct pattern for carrying stream metadata through download flow"

requirements-completed: [DEE-03, DEE-04]

# Metrics
duration: 5min
completed: 2026-04-01
---

# Phase 01 Plan 02: Stream Decryption and Download Methods Summary

**Blowfish CBC stream decryption with custom TUI download loop for track/album/artist downloads**

## Performance

- **Duration:** 5 min
- **Started:** 2026-04-01T21:10:28Z
- **Completed:** 2026-04-01T21:16:11Z
- **Tasks:** 2
- **Files modified:** 5

## Accomplishments
- Blowfish CBC chunk-based decryption (every 3rd 2048-byte chunk) with MD5-XOR key derivation
- Legacy CDN URL construction using AES-128-ECB encryption
- Stream URL resolution via modern media.deezer.com API with legacy CDN fallback
- Custom Deezer TUI download loop that handles download→decrypt→tag→probe pipeline
- Full DownloadTrack, DownloadAlbum, DownloadArtist implementations matching Tidal UX
- Artist download flow with release type filter, album selector, and sequential album downloads

## Task Commits

Each task was committed atomically:

1. **Task 1: Create Blowfish crypto module** - `8f71bae` (feat)
2. **Task 2: Implement download methods with TUI** - `98d19cd` (feat)

## Files Created/Modified
- `provider/deezer/crypto.go` - Blowfish key derivation, stream decryption, legacy CDN URL construction
- `provider/deezer/crypto_test.go` - Unit tests for crypto operations (key derivation length, decryption passthrough)
- `provider/deezer/deezer.go` - Complete download methods: stream URL resolution, custom TUI download loop, track/album/artist downloads
- `go.mod` / `go.sum` - Added golang.org/x/crypto dependency

## Decisions Made
- **Custom TUI download loop:** Standard `ui.DownloadManager.Run()` does download→tag→probe, but Deezer needs download→decrypt→tag→probe. Created `runDeezerDownload` / `downloadAllDeezer` that create the tea.Program manually and insert the decrypt step.
- **Temp file decryption:** Download encrypted stream to `.enc` temp file, then decrypt to final path. Simpler than streaming decryption and allows accurate progress tracking during download phase.
- **All streams encrypted:** Both modern media API and legacy CDN streams are Blowfish encrypted — `IsEncrypted` is always true.
- **Quality fallback via file sizes:** Check GW song data `FILESIZE_FLAC` / `FILESIZE_MP3_320` fields to determine if quality is available before requesting.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Deezer provider fully implements the `provider.Provider` interface
- Both Tidal and Deezer providers coexist and compile together
- Ready for Plan 03: CLI integration and auth commands (register Deezer provider, add `auth login deezer` command)

## Self-Check: PASSED

- All files exist: crypto.go, crypto_test.go, deezer.go, 01-02-SUMMARY.md
- All commits exist: 8f71bae (Task 1), 98d19cd (Task 2)
- `go build ./...` passes
- `go vet ./...` passes

---
*Phase: 01-deezer-provider*
*Completed: 2026-04-01*
