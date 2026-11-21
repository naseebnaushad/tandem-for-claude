---
type: feature
area: sync
title: Tandem for Claude, a desktop app for using several Claude seats side by side without losing Code-tab chats
authors: [naseebnaushad]
branch: feature/profile-manager
cr: CR-0.1.0-01, CR-0.1.0-02
after_the_fact: false
---

## What changed and why
First version of **Tandem for Claude**: a desktop app for macOS, Windows and Linux. It
solves the problem that switching Claude accounts made the other account's Code-tab
chats disappear. They weren't deleted: Claude desktop lists only the signed-in account's
chat entries, while the conversations sit in `~/.claude/projects`, shared by every
account.

What a user gets:
- **Named profiles.** Each is a separate Claude desktop window (own data folder and
  sign-in) or a Claude Code terminal (own `CLAUDE_CONFIG_DIR` and sign-in, sharing
  history, `CLAUDE.md`, settings, skills and plugins through links).
- **Chat sync.** Every profile sees every Code-tab chat and can continue it. It runs in
  the background, with "Sync now", warnings and a pause when Claude's format changes.
- **Sign-in helper.** Handles the one-time sign-in of a desktop profile. Claude sends
  sign-ins to the first open window, so the helper closes the others for a moment and
  reopens them afterwards.
- **Open brings an already-open profile to the front** instead of starting a second copy.
- **Chat retention setting:** on request, raises Claude Code's `cleanupPeriodDays`
  (30-day default) so old chats aren't deleted. Other settings are kept and the old
  file is saved.
- **Window and tray icon:** profiles, sync status, backups (restore), settings
  (interval, start at login).
- **A command line** in the same binary (`tandem status`, `sync`, `profile …`,
  `backups …`, `autostart …`).
- **Unsigned release builds** made by GitHub Actions for all three systems.

The earlier Python command-line tool from this branch was replaced. Every feature was
ported (parity checklist P1–P18 in CR-0.1.0-02). The `.app` launchers were dropped,
with approval. The tool's macOS LaunchAgent is removed automatically on the app's first
start.

## How it works (logic)
Layered per the company rules. The front-ends call one `app.Manager`.
- `internal/domain`: `PlanSync` (add missing entries, update only when Claude is
  closed, deleted markers only where live, skip chats without transcript, two-seat
  warning from per-copy state), `NextSynced`, `MakeCopy`, validation, profile rules.
- `internal/app`:
  - `Syncer.Sync`: format guard, backup before writes, write per action, re-check
    before updates, save state.
  - `Profiles`: create/rename/delete/open, sign-in helper, CLI links with a copy
    fallback, migration of existing folders.
  - `Manager`, `Settings`.
- `internal/adapters/fsstore`: atomic writes (temp file plus `link`/`rename`), capped
  reads, symlinks ignored, JSON state, profile and settings files, tar.gz backups with
  safe restore.
- `internal/adapters/platform`: per-OS code (build tags), covering:
  - Claude data folder and executable.
  - Process list (`ps`, PowerShell CIM, `/proc`).
  - Launching windows (`open -n`, exe with `--user-data-dir`).
  - Graceful quit.
  - Opening a terminal (Terminal.app, `cmd`, a list of Linux terminals).
  - Start at login (LaunchAgent, `HKCU\…\Run`, XDG autostart).
  - Links (symlinks; Windows junctions, with a file copy fallback).
- `internal/cli`, `ui` (Wails v3.1.0 window + tray, background sync loop, `app.log`),
  `frontend` (plain TypeScript, strict CSP, `textContent` only).
- `main.go`: no arguments opens the app, arguments run the CLI. On Windows it attaches
  to the parent console so CLI output shows.

## Tests added and results
See `documents/v0.1.0/alpha/v0.1.0 - Alpha Test Cases.md`.
- **Go:** 43 tests (SYNC-001…034, PROF-001…009, RET-001/002), all passing on macOS with
  `go test ./internal/...`. Synthetic data only.
- **Frontend:** `tsc --noEmit` clean, `npm audit` 0 vulnerabilities.
- **Vulnerability scan:** `govulncheck ./...` reported no vulnerabilities.
- **Cross-compile:** `GOOS=windows go build` and `GOOS=linux/windows go vet` are clean.
- **Manual on macOS:**
  - The real `tandem status` matches the Python output, and the old state carried over.
  - The app window lists profiles with the correct open/closed state.
  - The Python LaunchAgent was removed on first start.
  - Terminal profile create/rename/delete was tested with a temp home, and the shared
    `~/.claude` stayed intact.
  - Start at login on/off works.
  - The packaged `.app` starts with the new icon, and Open brings an already-open
    Claude window to the front.
- **Not run yet:**
  - Continuing a chat on the other seat (desktop and terminal, CR-02 OQ2).
  - The desktop sign-in helper end to end.
  - The tray menu.
  - Anything on Windows or Linux.

## Bugs fixed
Found and fixed during development:
- The deleted-marker copy went to seats that never had the chat. It now goes only
  where the chat is live.
- The two-seat warning fired on fresh copies and on stale copies on other seats. It
  now uses each seat copy's own last-sync time.
- The background sync used the Homebrew Python path, and its log was world-readable
  (Python tool, now replaced).
- On Windows, launched windows would have been hidden by the helper's no-window flag.
  Hiding is now used only for background commands.

## Notes for users / upgrade steps
- **Python tool users:** the app adopts existing `~/.claude-profile/profiles/*` and
  `cli/*` folders as profiles, keeps the backups and sync state, and removes the old
  LaunchAgent.
- **Restart a Claude window** to see newly synced chats.
- **Keep one chat open in one profile at a time.**
- **Unsigned builds:** right-click → Open on macOS, More info → Run anyway on Windows.

## Technical notes
- New dependencies:
  - `github.com/wailsapp/wails/v3` v3.1.0 (MIT) and its Go dependencies.
  - npm: `@wailsio/runtime` 3.1.0 (MIT), `vite` 8.3.5 (MIT, build only),
    `typescript` 6.0.3 (Apache-2.0, build only).
  - Licence scan done: everything compiled in is MIT/BSD. The build-only `lightningcss`
    (via Vite) is MPL-2.0 and was approved by the owner on 2026-11-21; it isn't
    shipped in the app.
- Configuration: `CLAUDE_PROFILE_HOME`, `CLAUDE_PROFILE_APP_DATA`,
  `CLAUDE_PROFILE_CLAUDE_DIR`, `CLAUDE_PROFILE_PROJECTS` (`CLAUDE_PROFILE_APPS_DIR`
  removed with the launchers).
- The version is stamped at build time with `APP_VERSION=<version>` (adds
  `-X main.version`).
- Go module path is `tandemforclaude` for now. Change it to the GitHub path once the
  repository exists.
- **Risks:**
  - Relies on Claude's internal chat-list format (the format guard pauses sync if it
    changes).
  - Windows and Linux are untested on real machines.
  - Linux has no official Claude desktop.

## Suggestions
- Show CLI-started chats in the desktop sidebar too.
- Signed builds if users ask for them.
