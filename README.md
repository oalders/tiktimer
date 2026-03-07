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

## Install

### From source

Requires Go and Xcode Command Line Tools.

```bash
git clone https://github.com/oalders/tiktimer.git
cd tiktimer
make build
cp tiktimer /usr/local/bin/
```

### From a release

Download the universal binary from the
[releases page](https://github.com/oalders/tiktimer/releases) and copy it
somewhere on your PATH.

> **Note:** The binary is not signed or notarized. On first launch, macOS will
> block it. Right-click the binary and choose "Open", or go to System
> Preferences > Security & Privacy and click "Open Anyway".

## Usage

Run `tiktimer` to start. It appears in the menu bar.

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
git tag v0.1.0
git push origin v0.1.0

# Build a universal binary (amd64 + arm64)
make release

# Create a GitHub release with the binary
gh release create v0.1.0 tiktimer --title "v0.1.0" --generate-notes
```
