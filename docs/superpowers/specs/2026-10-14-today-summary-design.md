# TikTimer — "Today" Summary in the Dropdown

**Date:** 2026-10-14
**Status:** Approved after subagent review (see Revisions)

## Problem

The user logs several entries per project per day (via Log & Reset) and works
several jobs, often 2–3 in one day. Before wrapping up they want to see, at a
glance, how many hours each project has today and whether the day's total is
close to their daily target (usually 8h, but configurable). The CSV already
goes to a spreadsheet later. This feature only gives a quick in-app view.

## Constraints (same as the time-logging design)

- No Go version bump, no dependency changes. Standard library only (`image`,
  `image/png`, `encoding/csv`, …). Text cannot be drawn into images, because
  `golang.org/x/image/font` would be a new dependency, so all text stays in menu item titles.
- menuet facts that shape the design:
  - `MenuItem` has `Text`, `Image` (supports `data:image/png;base64,` URIs,
    already used by the odometer), `FontSize`, `FontWeight`. Menu-item images
    taller than 16pt are scaled down to 16pt high.
  - The title font is `monospacedDigitSystemFont`: digits line up, letters
    don't.
  - An item with no `Clicked` and no children is disabled. AppKit greys its
    text and dims its image. Summary items therefore get a `Clicked` handler
    (see below) so they render at full strength.
  - `Children` (i.e. `menuItems`) runs only when the menu opens, on the main
    thread, so the summary is computed fresh at each open.

## What it looks like

Added at the top of the dropdown (opened by a plain click), below the
Start/Stop item and above the timer list:

```
  Today  7h 08m of 8h · 52m to go          ← text, bold
  [██████████▒▒▒███████░░░░░░░░░░░░]        ← rendered PNG bar
  ● Acme        4h 50m · 5 logged + 23m on timer
  ● Globex      1h 48m · 2 logged
  ● Initech     0h 30m · 1 logged
  ─────────────────────────────────
  Acme  0:23:10   ✓
  ...
```

### Headline text item
- `Today  <total> of <target> · <remaining> to go`, where total = logged today
  + unlogged timer time (see Data). Durations are shown as `Xh YYm`, truncated
  to minutes. Once total ≥ target: `· target reached` if within one minute of
  it, otherwise `· <over> over`.
- `FontWeight` bold.

### Headline bar image
- One fixed-width stacked bar, rendered at 2× pixel density
  (e.g. 520×32 px → displayed ~260×16pt). The bar itself is a rounded
  rectangle ~14 px tall, vertically centered, on a translucent grey track.
- Scale is `max(target, total)`. Segments are drawn left to right in project
  order (see Ordering). For each project: a **solid** segment for logged time,
  then a **lighter shade** segment of the same hue for unlogged
  timer time.
- When total exceeds target, a thin vertical tick marks the target position
  so overtime is visible.
- Colors come from a fixed palette of saturated mid-tone hues that read on
  both the light and dark menu backgrounds (blue, orange, green, purple, pink,
  teal), cycling if there are more projects. The image is non-template.

### Project rows (plain text)
- One row per project that has logged time today **or** a non-zero timer.
- `Image`: a small colored dot (rendered PNG) matching that project's bar
  color, so it doubles as the bar's legend.
- Text: `<name>  <total>  (<n> entries)`, or `(<n> entries, <unlogged> unlogged)`
  when that project's timer is non-zero. `<total>` includes the unlogged time. `n` is the count of today's entries, so
  "five logs" can be checked at a glance.

### Clicking
All summary items (headline, bar, rows) share one action: **reveal the log in
Finder** (`open -R <file>` for a custom `logPath`, `open <dir>` for the default
iCloud folder). This is useful, and it keeps the items enabled so they are not
greyed. Nothing is changed by clicking.

### Empty day
When nothing is logged and every timer is zero, the section is just the
headline `Today  0h 00m of 8h` plus the (empty-track) bar.

## Data

### Logged time
- **Files:** with a custom `logPath`, just that file. Otherwise every `*.csv`
  in the default iCloud `tiktimer/` folder (`filepath.Dir(defaultLogPath())`),
  which includes the other Mac's file, so both machines' work is summed.
  Evicted iCloud placeholders (`.Name.csv.icloud`) don't match the glob and
  are skipped. That can undercount until iCloud downloads them, which is accepted.
- **Rows:** skip the header (first field `timestamp`). Accept both the
  5-column and 6-column formats. A row counts toward "today" when its RFC3339
  `timestamp`, converted to local time, falls in the current work day (since
  04:00 local; see Revisions). The amount is taken from `duration` via `parseHMS`, the exact
  logged (possibly rounded) value. There is no fallback to `hours`. Malformed
  rows are skipped.
- **Project key:** the `name` column, `strings.TrimSpace`d, compared exactly.
- An unreadable file is skipped. The headline then appends ` · log unreadable`
  so a missing total is never silent.

### Unlogged time
- For each timer, its current accumulated time (plus in-flight running time
  for the active running timer), taken under the mutex like the existing menu
  rows. It's keyed by timer name. Time left over from an earlier work day is
  excluded (see Revisions: leftover timer time).

### Ordering and colors
Projects are ordered by timer order first, then CSV-only names (renamed or
removed timers) in order of first appearance in the files. Palette color = position
in that order. Colors therefore stay stable through the day as long as the timers don't
change.

## Daily target

- New `dailyTargetHours` (float64, `omitempty`) in `~/.tiktimer.json`,
  round-tripped through `saveData` exactly like `logPath`. 0/absent → 8.
- New menu item **"Daily Target…"** (next to "Odometer Display") opens an
  Alert prefilled with the current value (e.g. `8` or `7.5`). The input must parse as a
  number with 0 < h ≤ 24, otherwise an "Invalid target" alert is shown and nothing changes.
  Setting it in the app avoids the "edit JSON only while the app is stopped"
  caveat.

## Code structure

- `today.go`, pure and testable:
  - `logFilesToRead(configPath string) []string`
  - `readTodayLogged(paths []string, now time.Time) (logged map[string]time.Duration, counts map[string]int, order []string, unreadable bool)`
  - `type projectTotal struct { Name string; Logged, Unlogged time.Duration; Entries int }`
  - `buildDaySummary(timerNames []string, timerElapsed []time.Duration, logged…, counts…, csvOrder…) []projectTotal`
    (filters empty projects, applies ordering)
  - `formatHM(d time.Duration) string` → `7h 08m`
  - `headlineText(total, target time.Duration, unreadable bool) string`
- `todaybar.go`: `renderDayBar(projects []projectTotal, target time.Duration) string`
  and `renderSwatch(i int) string` (data URIs), plus the palette. Rounded ends via
  a per-pixel coverage check, lighter shade by blending toward the track.
- `main.go`: `todayItems()` builds the section. It's called from `menuItems`, with
  the timer snapshot taken under the mutex and the CSV reads done **after**
  unlocking so file I/O never holds the app lock. Plus the Daily Target handler.

`menuItems` currently holds the mutex for its whole body via `defer`. It will
be restructured as: snapshot timers (lock/unlock), read logs, then build the
rest of the menu as today.

## Testing

- `readTodayLogged`: temp dir with two CSVs (5- and 6-column), rows from
  yesterday/today/tomorrow around the 04:00 day boundary, a malformed row, a quoted
  note containing a comma and newline; asserts sums, counts and first-seen order;
  an unreadable path sets `unreadable`.
- `buildDaySummary`: timer-only project, CSV-only project, both, both empty
  (omitted), and ordering.
- `formatHM` / `headlineText`: under, exactly at (±1 min), and over target;
  the unreadable suffix.
- `renderDayBar`: decodes the PNG; checks dimensions, that a solid segment
  pixel has the project color, and that the track shows past total < target;
  the over-target case draws the tick.
- `dailyTargetHours` save/load round trip and the default of 8.

## Out of scope

- Per-project targets, weekly views, charts of history.
- Watching iCloud for changes while the menu is open.

## Revisions after review

- **Old-format files:** a file created before the `tracked` column has a
  5-column header followed by 6-column rows. Read with `FieldsPerRecord = -1`
  and use columns 0–2 by position. Tested with one mixed file.
- **Crisp 2× images:** Go's PNG encoder writes no DPI, so a 32 px-tall image
  would be treated as 32pt and resampled by menuet's `imageWithHeight:`.
  Insert a `pHYs` chunk (144 dpi, 5669 px/m) after IHDR so AppKit sees a
  ≤16pt image and uses the 2× bitmap directly. Tested by decoding the output and
  checking the chunk is present.
- **Dataless iCloud files:** on Sonoma and later, evicted files keep their `.csv` name, and
  opening one blocks while it downloads, on the main thread. Skip files whose
  `Stat_t.Flags` has `SF_DATALESS` (0x40000000). The headline then appends
  ` · some logs not downloaded`.
- **Missing ≠ unreadable:** a missing folder, no matching files, or a not-yet-created
  custom `logPath` all count as zero, with no warning.
- **Today is a half-open window** (originally midnight to midnight; now
  starting at 04:00, see below), built with `time.Date` and `AddDate(0,0,1)` so
  DST days are handled. Rows dated in the future are excluded.
- **Double counting after a skipped reset:** originally the reset was skipped
  if another timer became active mid-dialog; it now always applies (see "Log &
  Reset works on any timer" below), warning only if the timer was removed.
- **Locking:** lock, copy `logPath` plus the timer snapshot, unlock, read the files,
  then lock again for the rest of `menuItems`. The summary and the timer rows
  may differ by the instant between the two locks.
- **Headline math:** remaining time is computed from the truncated total.
  "target reached" applies when `|target − total| < 1m`. The target is shown
  compactly (`8h`, `7h 30m`). The target dialog accepts `7,5` as well as `7.5`.
- **Accepted:** summary items get the accent highlight on hover. Renaming a
  timer mid-day splits its time into two rows.
- **Sub-minute timer time is hidden:** a row mentions unlogged time only at
  ≥ 1m, and a project with no entries and under a minute on its timer is
  left out. It still counts toward the headline total and the bar.
- **The day starts at 4 am, not midnight:** the window is
  `[04:00 local, 04:00 next day)` (`dayStartHour`), so late-night work logged
  after midnight still counts toward the evening it belongs to.
- **Leftover timer time belongs to its own day:** each timer saves `lastRun`
  (when time last accrued: `flushActive`, or Set Time raising the value). A
  stopped timer whose `lastRun` is before the current work day contributes
  nothing to Today, and Log & Reset dates its row at `lastRun` instead of now,
  so the CSV and the spreadsheet file it under the day it was worked. The
  "Logged" notification says when a row was backdated. A timer that ran on
  both days is dated now (accepted). Timers saved before `lastRun` existed
  count as today.
- **Log & Reset works on any timer:** it is a submenu of all timers (those
  under a second are disabled), so logging one never pauses or switches the
  running timer. The reset subtracts the logged snapshot from the timer
  instead of zeroing it, so time a running timer gains while the dialog is
  open is kept, and switching timers mid-dialog no longer skips the reset.
- **Log & Reset as Yesterday:** a dialog button, shown unless the time is already from
  an earlier day, dates the row at `lastRun` if that is before today's work
  day, otherwise 23:59 on the previous work day. It covers a timer started by
  accident for a few seconds this morning.
- **Dialog edge cases:** the dating is judged at the moment the dialog opened,
  so its hint and the row agree if it stays open past 4 am. The row uses the
  timer's name at write time, in case it was renamed mid-dialog. Two Log &
  Reset dialogs cannot be open at once (menuet refuses a second Alert). A
  timer restarted this morning while still holding yesterday's leftover
  counts and logs entirely as today (use Log & Reset as Yesterday, or log the
  leftover before starting it).
