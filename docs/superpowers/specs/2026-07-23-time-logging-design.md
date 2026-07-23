# TikTimer — Time Logging for Invoicing

**Date:** 2026-07-23
**Status:** Approved design

## Problem

TikTimer runs on two Macs. The user wants to reset a timer and log how the
elapsed time was spent, into a store common to both machines, so the records
can be used later for invoicing. Live-syncing of running timers is explicitly
*not* the goal — capturing completed time entries is.

## Constraints

- Must keep running on a very old Mac **and** the latest macOS.
- **No** Go version bump, **no** menuet or other dependency changes, **no**
  gratuitous API updates. Changes are strictly additive and use only the Go
  standard library (`encoding/csv`, `os`, `time`, `path/filepath`).

## Behavior

The active-timer menu item **"Reset Current Timer"** is renamed to
**"Log & Reset…"**. Clicking it:

1. Snapshots the current elapsed time for the active timer, including any
   in-flight running time (i.e. `Accumulated + time.Since(lastTick)` when
   running). The snapshot is taken *before* the modal prompt so time spent
   in the dialog is not counted.
2. Opens the existing menuet `Alert` with a single note input
   ("How was this time spent?") and buttons `[Log & Reset]` `[Cancel]`.
3. **Cancel** → nothing changes; the timer keeps its accumulated time and
   keeps running if it was running.
4. **Log & Reset** → append one row to the CSV, then zero the active timer's
   accumulated time and set `lastTick = now`.
5. If the snapshot rounds to `00:00:00`, no CSV row is written (avoids junk
   zero-duration rows); the reset is a harmless no-op.

The prompt handler must **not** hold the app mutex while the modal `Alert` is
open — it follows the existing "Rename Current Timer" pattern (lock to
snapshot, unlock, prompt, lock again to mutate). Holding the lock during the
blocking Alert would freeze the tick/updateTitle goroutine.

Everything else (switch timer, rename, new/remove timer, odometer display) is
unchanged.

## CSV format

**Default location:**
`~/Library/Mobile Documents/com~apple~CloudDocs/tiktimer-log.csv`

**Override:** a new optional `logPath` field in the existing `~/.tiktimer.json`
save file. When present and non-empty it is used verbatim; otherwise the
default iCloud path is used. The field is loaded into the `App` and written
back on every `save()` so a hand-edited value is preserved.

**Columns / header:**

```
timestamp,name,duration,hours,note
```

**Example row:**

```
2026-07-23T14:30:00-04:00,Acme redesign,01:23:45,1.40,Fixed login bug
```

- `timestamp` — RFC3339 with timezone offset (`time.Now().Format(time.RFC3339)`).
- `name` — the active timer's name.
- `duration` — always `HH:MM:SS`, all fields zero-padded to two digits so the
  column is uniform in a spreadsheet, e.g. `01:23:45` or `12:05:03`. This is a
  dedicated format (`"%02d:%02d:%02d"`), **not** the existing `formatDuration`,
  which omits the hours field under one hour and does not zero-pad hours.
- `hours` — decimal hours, 2 decimal places (`"%.2f"`), for rate math.
- `note` — free text from the prompt; may be empty.

Rows are written with `encoding/csv` so notes containing commas, quotes, or
newlines are escaped correctly. The header is written only when the file is
first created.

## Cross-machine sync

The CSV lives in iCloud Drive and is **append-only**. Each Mac appends its own
rows; nothing is edited in place, so slow iCloud propagation is fine.
Simultaneous appends from both Macs at the same instant could in theory
produce an iCloud conflict copy, but a manual reset action makes this
vanishingly rare and it is accepted as out of scope.

## Error handling

Losing billable time silently is unacceptable. If the CSV write fails (e.g.
the iCloud Drive folder does not exist, or a permissions error):

- TikTimer shows a menuet `Alert` describing the error.
- The timer is **not** reset, so the elapsed time is preserved and can be
  logged again after the problem is fixed.

The parent directory of the log path is created (`os.MkdirAll`) if it does not
already exist before writing.

## Code structure

Add a new `logentry.go` containing pure, independently testable functions:

- `resolveLogPath(configPath string) (string, error)` — returns `configPath`
  if non-empty, else the default iCloud path built from `os.UserHomeDir()`.
- `formatEntryFields(d time.Duration) (duration string, hours string)` —
  builds the `HH:MM:SS` and decimal-hours strings from a `time.Duration`.
- `appendLogRow(path, timestamp, name, duration, hours, note string) error` —
  ensures the parent dir and header exist, then appends one CSV row using
  `encoding/csv`.

The menu handler in `main.go` stays thin: snapshot → prompt → call
`appendLogRow` → on success, reset the timer; on error, show an Alert.

## Testing

Unit tests (standard `testing` package) cover the pure functions:

- `formatEntryFields`: durations of 0, sub-minute, sub-hour, multi-hour,
  and a case exercising the decimal-hours rounding (e.g. 1h23m45s → `01:23:45`
  / `1.40`, and a duration ≥ 100h to confirm three-plus-digit hours still
  format).
- `resolveLogPath`: empty config → default iCloud path; non-empty config →
  returned verbatim.
- `appendLogRow`: against a temp file — creates the file with a header on
  first call, appends without re-writing the header on subsequent calls, and
  correctly escapes a note containing a comma and a quote.

The GUI `Alert` interaction is not unit-tested.

## Out of scope

- Live-syncing of running timers across machines.
- Editing or deleting past log entries from within the app.
- Any invoicing / reporting UI (the CSV is consumed in a spreadsheet).
