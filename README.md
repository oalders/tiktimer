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

TikTimer depends on features that are not in upstream
[`caseymrm/menuet`](https://github.com/caseymrm/menuet) — specifically the
`StatusItemClicked` handler (left-click to start/stop) and prefilled alert
inputs (`InputValues`, used when editing a timer's elapsed time). These live on
the `olaf/status-item-click-handler` branch of the
[`oalders/menuet`](https://github.com/oalders/menuet) fork, which `go.mod`
wires in with a `replace` directive pointing at a **local checkout**:

```
replace github.com/caseymrm/menuet => /Users/olaf/github/oalders/menuet
```

So you must clone the fork yourself and check out that branch before building:

```bash
# 1. Clone the menuet fork somewhere and check out the required branch
git clone https://github.com/oalders/menuet.git
cd menuet
git checkout olaf/status-item-click-handler
cd ..

# 2. Clone TikTimer
git clone https://github.com/oalders/tiktimer.git
cd tiktimer

# 3. Point the replace directive at wherever you cloned menuet, if it
#    differs from the path already in go.mod, then build
make app
cp -r TikTimer.app /Applications/
```

If `go build` fails with `unknown field InputValues` or `replacement directory
... does not exist`, the `replace` path in `go.mod` isn't pointing at a menuet
checkout on the `olaf/status-item-click-handler` branch. Fix the path (or check
out the branch) and rebuild.

Other things worth knowing:

- **The `replace` path is an absolute local path**, so it's machine-specific
  and won't be correct out of the box on another machine (or after you move the
  menuet checkout). Expect to adjust it, and note that editing it shows up as an
  uncommitted change to `go.mod`.
- **cgo is required.** menuet is Objective-C/Cocoa, so builds need
  `CGO_ENABLED=1` (the Makefile sets this) and won't cross-compile off macOS.
- **Deprecation warnings are silenced on purpose.** menuet calls the deprecated
  `NSUserNotification` API; the Makefile sets
  `CGO_CFLAGS=-Wno-deprecated-declarations` so those clang warnings don't drown
  out real ones. That's expected, not a problem with your setup.
- **The menuet branch is the source of truth for these features.** If a build
  breaks after a menuet update, make sure your local menuet checkout is on
  `olaf/status-item-click-handler` and up to date (`git pull`) before
  investigating tiktimer itself.

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
