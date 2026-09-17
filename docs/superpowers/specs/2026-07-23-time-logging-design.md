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

1. Under the mutex, captures a **snapshot** of: the active index, the active
   timer's `*Timer` pointer, its name, and its current elapsed time including
   in-flight running time (i.e. `Accumulated + time.Since(lastTick)` when
   running). The snapshot is taken *before* the modal prompt so time spent in
   the dialog is not counted. Then it unlocks.
2. If the snapshotted duration is `< 1 second` (covers zero, sub-second, and
   negative durations from clock skew), the handler does nothing at all — no
   prompt, no row, no reset — and returns.
3. Opens the existing menuet `Alert` with a single note input
   ("How was this time spent?") and buttons `[Log & Reset]` `[Cancel]`.
4. **Cancel** → nothing changes; the timer keeps its accumulated time and
   keeps running if it was running.
5. **Log & Reset** → append one row to the CSV built entirely from the
   *snapshot* (captured name + duration), independent of current app state.
   Only on a successful append does the handler re-lock and reset the timer.
6. **Reset targets the snapshotted timer, not whoever is active at confirm
   time.** menuet dispatches each `Clicked`/`StatusItemClicked` handler on its
   own goroutine, and the blocking `Alert` blocks only this goroutine — the
   status-item click (`toggleActive`) and the menu (`switchTo`, `removeTimer`)
   stay live and can change `a.active` or remove the timer while the dialog is
   open. So after re-locking, reset **only if** `a.active == snapIdx` **and**
   `a.timers[snapIdx] == snapPtr`; otherwise skip the reset. The CSV row is
   still written correctly from the snapshot either way. When the reset does
   apply, it zeros that timer's accumulated time and sets `lastTick = now`.

The prompt handler must **not** hold the app mutex while the modal `Alert` is
open — it follows the existing "Rename Current Timer" pattern (lock to
snapshot, unlock, prompt, lock again to mutate). Holding the lock during the
blocking Alert would freeze the tick/updateTitle goroutine.

**Known, accepted tradeoffs:**
- Time that elapses *while the note dialog is open* is discarded on confirm
  (the snapshot predates it and the reset zeros the timer). This is fine for a
  quick note; a dialog left open for many minutes loses that unlogged time.
- If the app receives SIGTERM (quit) while the modal is open, the in-progress
  entry is never written to the CSV. No time is lost — the SIGTERM handler
  still flushes accumulated time to `~/.tiktimer.json` — it is simply unlogged.

Everything else (switch timer, rename, new/remove timer, odometer display) is
unchanged.

## CSV format

**Default location:**
`~/Library/Mobile Documents/com~apple~CloudDocs/tiktimer-log.csv`

**Override:** a new optional `logPath` field in the existing `~/.tiktimer.json`
save file. When present and non-empty it is used verbatim; otherwise the
default iCloud path is used. To make this round-trip correctly, `logPath` is
added to the `saveData` struct, `load()` reads it into an `App` field, and
`save()` writes that field back on every save. Because `save()` serializes the
in-memory `App` roughly every 30 s (and on toggle/quit), a `logPath` value must
be **edited while the app is not running** — an edit made while it runs is
overwritten by the in-memory value within ~30 s.

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
newlines are escaped correctly. The header is written when the file does not
yet exist **or** exists but is zero bytes (an empty iCloud placeholder, or a
prior run that created the file but failed before writing the header) — checked
via `os.Stat` (missing OR size 0). The file is opened
`O_APPEND|O_CREATE|O_WRONLY`.

Note on precision: `duration` (`HH:MM:SS`) truncates to whole seconds while
`hours` rounds to two decimals, so a short entry can read `00:00:10` /
`hours=0.00`. That is intentional — any entry ≥ 1 second is logged; only
sub-second/zero/negative durations are skipped (see Behavior step 2).

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

`appendLogRow` must surface the *real* error: check the errors from
`OpenFile`, from `csv.Writer.Flush()` / `w.Error()`, and from `Close()` — not
just the open. Any of them aborts the reset and triggers the Alert.

**Directory creation is deliberately asymmetric to avoid silent divergence:**

- For a **user-supplied `logPath`**, `os.MkdirAll` the parent directory if
  absent (the user chose the location; creating it is expected).
- For the **default iCloud path**, do **not** `MkdirAll` the
  `com~apple~CloudDocs` root. If that base directory is missing, iCloud Drive
  is disabled/not set up; `MkdirAll` would create a plain *local* folder,
  writes would succeed, and rows would never sync to the other Mac — a silent
  divergence the error handling above cannot catch. Instead, if the iCloud
  base directory does not exist, show an Alert telling the user to enable
  iCloud Drive (or set a `logPath`) and do not write or reset.

## Code structure

Add a new `logentry.go` containing pure, independently testable functions:

- `resolveLogPath(configPath string) string` — returns `configPath` if
  non-empty, else the default iCloud path built from `os.UserHomeDir()`.
- `defaultLogDir() string` — the iCloud base directory
  (`~/Library/Mobile Documents/com~apple~CloudDocs`); used by the handler to
  check existence before writing to the default path.
- `formatEntryFields(d time.Duration) (duration string, hours string)` —
  builds the `HH:MM:SS` (`"%02d:%02d:%02d"`) and decimal-hours (`"%.2f"`)
  strings from a `time.Duration`.
- `appendLogRow(path, timestamp, name, duration, hours, note string) error` —
  writes the header when `os.Stat(path)` reports missing or size 0, opens
  `O_APPEND|O_CREATE|O_WRONLY`, appends one CSV row via `encoding/csv`, and
  returns the first non-nil error among write/flush/close. It does **not**
  create the iCloud root (that check lives in the handler, per Error handling);
  it may `MkdirAll` a user `logPath`'s parent.

The menu handler in `main.go` stays thin: snapshot (idx, ptr, name, duration)
→ skip if `< 1s` → prompt → on confirm, verify the default-path iCloud dir
exists (Alert if not) → call `appendLogRow` → on success reset the snapshotted
timer *iff* it is still active and identical → on error show an Alert.

## Testing

Unit tests (standard `testing` package) cover the pure functions:

- `formatEntryFields`: durations of 0, sub-minute, sub-hour, multi-hour,
  and a case exercising the decimal-hours rounding (e.g. 1h23m45s → `01:23:45`
  / `1.40`, and a duration ≥ 100h to confirm three-plus-digit hours still
  format).
- `resolveLogPath`: empty config → default iCloud path; non-empty config →
  returned verbatim.
- `appendLogRow`: against a temp file —
  - creates a new file with a header on first call, then appends without
    re-writing the header on subsequent calls;
  - given a pre-existing **zero-byte** file, still writes the header before the
    first data row;
  - correctly escapes a note containing a comma and a quote.

The GUI `Alert` interaction and the goroutine-race reset guard (active-timer
identity check) are not unit-tested; the identity check is verified by reading
the code, since it depends on menuet's live handlers.

## Out of scope

- Live-syncing of running timers across machines.
- Editing or deleting past log entries from within the app.
- Any invoicing / reporting UI (the CSV is consumed in a spreadsheet).
