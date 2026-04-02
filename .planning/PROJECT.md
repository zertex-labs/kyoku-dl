# kyoku-dl

A command-line tool to download music from various streaming services. Built in Go with a Bubble Tea TUI.

## Stack
- **Language:** Go 1.25
- **CLI:** spf13/cobra
- **TUI:** charmbracelet/bubbletea, bubbles, lipgloss
- **Audio:** ffmpeg/ffprobe for tagging and probing

## Architecture
- `provider/` — shared interfaces (`Provider`), types (`Track`, `Album`, `Config`), utilities (download, tagging, MusicBrainz, probe, filename formatting)
- `provider/tidal/` — Tidal provider implementation (auth, API, types, streaming)
- `cmd/` — CLI commands (root, download, auth, version)
- `ui/` — TUI components (download progress, album selector, release filter, loading spinner, styles)

## Patterns
- Provider interface: `Name()`, `MatchesURL()`, `URLType()`, `IsAuthenticated()`, `GetTrack()`, `GetAlbum()`, `GetStreamURL()`, `DownloadTrack()`, `DownloadAlbum()`, `DownloadArtist()`, `SetDebug()`
- Registry-based URL routing: `registry.GetProviderForURL(url)` dispatches to the right provider
- Config-based quality override with friendly name mapping
- Session persistence for crash recovery (artist download sessions, failed album retry)
- MusicBrainz enrichment via ISRC lookup (optional `--musicbrainz` flag)
- ffmpeg-based metadata tagging (no re-encoding, codec copy)
