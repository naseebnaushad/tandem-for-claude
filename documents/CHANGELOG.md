# Changelog

All notable changes, in user terms. Written by `/promote` only.

## 0.1.0-beta.2 (2026-11-21)
The app is renamed to **Tango for Claude** (it was "Tandem for Claude" in beta.1).

### Changed
- New name, **Tango for Claude**, because another project already used "Tandem for Claude" for the same purpose. The GitHub repository is now `naseebnaushad/tango-for-claude`; old links redirect.
- **Breaking for scripts:** the command-line tool is now `tango` instead of `tandem`. Commands and options are unchanged.

### Upgrading from beta.1
- Quit and delete Tandem for Claude, then install Tango for Claude. Profiles, sign-ins, backups and settings are kept.
- Start at login, if it was on, moves to Tango automatically.

### Fixed
- Release downloads now get clean names (`tango-for-claude-<version>-…`) and Beta releases are marked as pre-releases.

### Known issues
- Same as beta.1. Also: if the old Tandem app is still running, both apps sync; quit the old one.

## 0.1.0-beta.1 (2026-11-21)
First public Beta of **Tandem for Claude**.

### New
- Use several Claude seats side by side: each **profile** is its own Claude desktop window or Claude Code terminal with its own sign-in.
- Every Code-tab chat is visible in every profile and can be continued there; chats sync in the background and on **Sync now**.
- Guided first sign-in for desktop profiles.
- Window and tray / menu bar icon; command line `tandem` in the same program.
- Automatic backups of the chat lists, with restore.
- Optional setting to keep Claude Code chats longer than its 30-day default.
- Start at login.
- Downloads for macOS, Windows and Linux (unsigned).

### Known issues
- Continuing a chat on another seat isn't confirmed end to end yet.
- Windows and Linux haven't been tried on a real machine; Linux needs a community Claude desktop build for desktop profiles.
- New chats appear in a Claude window after it restarts.
