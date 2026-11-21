---
type: fix
area: release
title: Release downloads get clean file names and Beta builds are marked as pre-releases
authors: [naseebnaushad]
branch: fix/release-asset-names
cr: none
after_the_fact: false
---

## What changed and why
The first Beta release (0.1.0-beta.1) had two problems:
- Its downloads were named `dist-tandem-for-claude-…`, while the README promises
  `tandem-for-claude-…`.
- It wasn't marked as a pre-release.

Both were fixed by hand on the draft before it was published. This change fixes the
workflow so later tags get it right on their own.

## How it works (logic)
`.github/workflows/release.yml`:
- Packages are written into a `dist/` folder under their final names
  (`tandem-for-claude-<version>-…`) and uploaded from there.
- The release job checksums `tandem-for-claude-*` and creates the draft with the title
  `Tandem for Claude <version>`.
- `--prerelease` is added when the tag has a hyphen (for example `v0.1.0-beta.2`).

`.gitignore` ignores the top-level `/dist/` folder.

## Tests added and results
| ID | Case | Result |
|---|---|---|
| REL-001 | Feature-branch release run names artifacts `tandem-for-claude-*` | ⏳ runs on push of this branch |
| REL-002 | Next beta tag creates a pre-release draft with clean names | ⏳ at the next tag |

The 0.1.0-beta.1 draft was corrected by hand. Its re-uploaded files were checked to be
byte-identical to the CI builds (SHA-256).

## Bugs fixed
- Release downloads had a `dist-` prefix. Cause: the packaging step used it to tell
  files apart for upload.
- Beta releases weren't marked as pre-releases. Cause: there was no flag in
  `gh release create`.

## Notes for users / upgrade steps
None.

## Technical notes
CI and release workflow only. No app code changes.

## Suggestions
None.
