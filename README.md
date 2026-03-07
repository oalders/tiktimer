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

The dropdown menu lets you:

- Switch between timers (click a timer name)
- Reset the active timer
- Add or remove timers

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
