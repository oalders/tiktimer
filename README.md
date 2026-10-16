# TikTimer

A lightweight macOS menu bar timer for tracking time across multiple jobs.

## Features

- Multiple named timers with independent accumulated time
- Click the menu bar to start/stop the active timer
- Option+click the menu bar to open the dropdown menu
- Switch between timers without losing accumulated time
- Tenths-of-a-second display as a visual cue that the timer is running
- Timer state persists across restarts (`~/.tiktimer.json`)
- Audible click feedback (system Pop sound)
- No dock icon — lives entirely in the menu bar

## Install

### From source

Requires Go and Xcode Command Line Tools.

```bash
git clone https://github.com/oalders/tiktimer.git
cd tiktimer
make app
cp -r TikTimer.app /Applications/
```

### From a release

Download `TikTimer.app.zip` from the
[releases page](https://github.com/oalders/tiktimer/releases), unzip it, and
drag `TikTimer.app` to your Applications folder.

> **Note:** The app is not signed or notarized. On first launch, macOS will
> block it. Right-click the app and choose "Open", or go to System
> Preferences > Security & Privacy and click "Open Anyway".

## Usage

Double-click `TikTimer.app` or run `tiktimer` from the terminal. It appears in
the menu bar.

| Action | Effect |
|---|---|
| Click | Start/stop the active timer |
| Option+click | Open the dropdown menu |

The dropdown menu opens with a **Today** summary: total hours against your
daily target, a bar split by project (solid = logged, lighter = still on a
timer), and one row per project with its entry count. It sums today's rows
from every machine's log in the iCloud `tiktimer` folder (or your custom
`logPath`), plus whatever is on your timers now. Click any of it to reveal the
log in Finder.

The dropdown menu also lets you:

- Switch between timers (click a timer name)
- Reset the active timer
- Add or remove timers
- Set the daily target (default 8h)

## Development

```bash
make init   # point git at .githooks (installs the pre-commit hook)
make test   # run the Go test suite
```

`make init` sets `core.hooksPath` to the version-controlled `.githooks`
directory, whose `pre-commit` hook runs `go test ./...` before each commit.
Bypass it for a work-in-progress commit with `git commit --no-verify`.

## Creating a release

```bash
# Tag the release
git tag v0.2.0
git push origin v0.2.0

# Build a universal .app bundle
make release

# Create a GitHub release
gh release create v0.2.0 TikTimer.app.zip --title "v0.2.0" --generate-notes
```
