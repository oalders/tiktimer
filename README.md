# TikTimer

A lightweight macOS menu bar timer for tracking time across multiple jobs.

## Features

- Multiple named timers with independent accumulated time
- Click the menu bar to open the dropdown menu
- Option+click the menu bar (or use the menu's first item) to start/stop the
  active timer
- Switch between timers without losing accumulated time, using Switch To in
  a timer's submenu (clicking a timer row only opens the submenu; the bold
  item at the top starts/stops the active timer)
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
| Click | Open the dropdown menu |
| Option+click | Start/stop the active timer |

The dropdown menu opens with a Start/Stop item for the active timer, then a
**Today** summary: total hours against your daily target, a bar split by
project (solid = logged, lighter = still on a timer), and one row per project
with its entry count. A day runs from 4 am to 4 am, so late-night work counts
toward the evening it belongs to. It sums today's rows from every machine's
log in the iCloud `tiktimer` folder (or your custom `logPath`), plus whatever
is on your timers now.

Time left on a timer from an earlier day doesn't count toward today. When you
Log & Reset such a timer, the row is dated when the timer last ran, so
clearing out yesterday's timers in the morning files that work under
yesterday. A timer that also ran today is dated now, unless you press **Log &
Reset as Yesterday** in the dialog (useful after starting it by accident for a few
seconds), which dates the row 23:59 on the previous day.

The dropdown menu also lets you:

- Act on any timer from its submenu (hover a timer name): Switch To, Rename,
  Set Time, Log & Reset, Pause on Sleep, Colour (its Today bar color), or
  Remove it
- Log & Reset any timer: write a CSV row with a note, then take that time off
  the timer, without pausing or switching away from the timer that's
  running. A name or note starting with `=`, `+`, `-` or `@` is written with
  a leading `'` so a spreadsheet won't run it as a formula
- Add timers
- Change settings from the **Settings** submenu: Odometer Display, the
  Daily Target (default 8h), Copy Log File Path, and Show Log in Finder

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
