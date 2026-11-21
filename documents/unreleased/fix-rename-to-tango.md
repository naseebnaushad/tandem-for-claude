---
type: breaking
area: prof
title: The app is renamed from "Tandem for Claude" to "Tango for Claude", and its command from `tandem` to `tango`
authors: [naseebnaushad]
branch: fix/rename-to-tango
cr: none
after_the_fact: false
---

## What changed and why
Another GitHub project already uses the name "Tandem for Claude" for the same purpose.
To avoid confusion the app is now **Tango for Claude** ("it takes two to tango"). The
release owner chose the name, the matching repo name and to keep the published
0.1.0-beta.1 release as history. This was decided directly with the release owner, so
there is no separate Change Request.

Renamed everywhere outside the frozen 0.1.0 documents:
- App name in the window, tray, menus, messages and installers: **Tango for Claude**.
- Command: `tango` (was `tandem`), program file `tango` / `tango.exe` / `tango.app`.
- App ID `io.github.naseebnaushad.tangoforclaude`, Go module
  `github.com/naseebnaushad/tango-for-claude`.
- Start-at-login entries: LaunchAgent `local.tango-for-claude` (macOS), Run value
  `TangoForClaude` (Windows), `tango-for-claude.desktop` (Linux).
- Download names: `tango-for-claude-<version>-…`.
- Small files the app writes: `.tango-` temp files, `.tango-copied`, and the Claude Code
  settings backup `settings.json.before-tango`.

Not renamed: the data folder `~/.claude-profile`, so profiles, sign-ins, backups and
sync state carry over untouched.

## How it works (logic)
Upgrading from 0.1.0-beta.1 keeps working without any user steps beyond installing:
- **Start at login** moves over on the first start:
  `MigrateOldAutostart` on each platform (`internal/adapters/platform/darwin.go`,
  `linux.go`, `windows.go`) removes the old "Tandem" entry if it exists and creates the
  Tango one for the current program. Called from `ui/run.go` next to the older
  Python-tool clean-up, through the `wiring.OldAutostartMigrator` interface.
- **Terminal profiles on Windows without links**: `RefreshCLI`
  (`internal/app/profiles.go`) reads both `.tango-copied` and the old `.tandem-copied`
  record, so files copied by beta.1 keep being refreshed, and writes the record under
  the new name.
- **Backups** skip leftover `.tandem-` temp files as well as `.tango-` ones
  (`internal/adapters/fsstore/backup.go`).

The old and new app have different single-instance IDs, so both can run at once if the
old one isn't quit. Both would sync; writes are atomic so nothing breaks, but the README
tells users to quit and delete the old app.

## Tests added and results
| ID | Case | Result |
|---|---|---|
| PROF-010 | A terminal profile made by beta.1 (old `.tandem-copied` record) still gets its copied files refreshed, and the record moves to the new name | ✅ |
| PROF-011 | Start at login set by beta.1 moves to Tango's entry and points at the new program (macOS; Linux test runs in CI on Ubuntu) | ✅ macOS, ⏳ Linux in CI |
| PROF-012 | Windows Run entry moves from `TandemForClaude` to `TangoForClaude` | ⏭ needs a real Windows PC |
| PROF-013 | Built app: `tango version`, `tango help`, bundle name and ID show Tango; existing profiles listed unchanged | ✅ (macOS package built locally) |

All 45 Go tests pass on macOS. `gofmt` and `go vet` (macOS, Windows; Linux for the core
packages) are clean. `govulncheck` wasn't installed locally; CI runs it. No dependencies
changed.

## Bugs fixed
- The name clashed with another GitHub project for the same purpose. Fixed by renaming.

## Notes for users / upgrade steps
**Breaking for scripts:** the command is now `tango`, not `tandem`. Inside the macOS app
it's at `/Applications/Tango for Claude.app/Contents/MacOS/tango`.

Coming from 0.1.0-beta.1:
1. Quit Tandem for Claude (tray icon → **Quit**) and delete it.
2. Install Tango for Claude and open it. Profiles, sign-ins, backups and settings are
   kept. Start at login, if it was on, moves over automatically.

## Technical notes
- Old GitHub links (`naseebnaushad/tandem-for-claude`) redirect after the repo rename.
- The 0.1.0 Beta documents and CHANGELOG still say "Tandem for Claude"; `/promote beta`
  updates them for beta.2.
- Pre-1.0, so this breaking change doesn't need a major version: the release owner can
  keep the 0.1.0 target at `/promote`.

## Suggestions
None.
