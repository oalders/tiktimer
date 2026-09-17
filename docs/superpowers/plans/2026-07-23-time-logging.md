# TikTimer Time Logging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Log & Reset…" action that records the active timer's elapsed time (timestamp, name, HH:MM:SS, decimal hours, note) as a row in an append-only CSV in iCloud Drive, then zeros the timer, so time can be pulled into invoices from either Mac.

**Architecture:** All I/O and formatting logic lives in a new, dependency-free `logentry.go` with small pure functions that are unit-tested against temp files. `main.go` gains one thin menu handler (`logAndReset`) that snapshots the active timer under the mutex, prompts for a note *without* holding the lock, appends via the tested helper, and resets only if the snapshotted timer is still active. The CSV location defaults to iCloud Drive and is overridable via a new `logPath` field in the existing `~/.tiktimer.json`.

**Tech Stack:** Go (standard library only: `encoding/csv`, `os`, `time`, `path/filepath`, `fmt`), `github.com/caseymrm/menuet` (unchanged) for the menu-bar UI.

## Global Constraints

- **Go directive stays `go 1.24.4`** — do not bump the version in `go.mod`.
- **menuet stays `v1.0.3`** with its existing local `replace` directive — do not change, upgrade, or add dependencies. Standard library only.
- **Additive only** — must keep running on a very old Mac and the latest macOS; introduce no new external APIs.
- **Package is `main`**; all new Go files declare `package main`.
- Default CSV path: `~/Library/Mobile Documents/com~apple~CloudDocs/tiktimer-log.csv`.
- CSV header (exact order): `timestamp,name,duration,hours,note`.
- `duration` format is always `%02d:%02d:%02d`; `hours` format is `%.2f`; `timestamp` is `time.RFC3339`.

---

### Task 1: `formatEntryFields` — duration/hours formatting

**Files:**
- Create: `logentry.go`
- Test: `logentry_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `formatEntryFields(d time.Duration) (duration string, hours string)` — returns the always-`HH:MM:SS` string and the two-decimal hours string. Negative `d` is clamped to zero.

- [ ] **Step 1: Write the failing test**

Create `logentry_test.go`:

```go
package main

import (
	"testing"
	"time"
)

func TestFormatEntryFields(t *testing.T) {
	cases := []struct {
		name     string
		d        time.Duration
		duration string
		hours    string
	}{
		{"zero", 0, "00:00:00", "0.00"},
		{"sub-minute", 45 * time.Second, "00:00:45", "0.01"},
		{"sub-hour", 23*time.Minute + 45*time.Second, "00:23:45", "0.40"},
		{"multi-hour", time.Hour + 23*time.Minute + 45*time.Second, "01:23:45", "1.40"},
		{"three-digit-hours", 100*time.Hour + 5*time.Minute + 3*time.Second, "100:05:03", "100.08"},
		{"negative-clamped", -5 * time.Second, "00:00:00", "0.00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotDur, gotHours := formatEntryFields(c.d)
			if gotDur != c.duration {
				t.Errorf("duration: got %q want %q", gotDur, c.duration)
			}
			if gotHours != c.hours {
				t.Errorf("hours: got %q want %q", gotHours, c.hours)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestFormatEntryFields -v`
Expected: FAIL — build error `undefined: formatEntryFields`.

- [ ] **Step 3: Write minimal implementation**

Create `logentry.go`:

```go
package main

import (
	"fmt"
	"time"
)

// formatEntryFields renders a duration as a uniform HH:MM:SS string (hours,
// minutes, and seconds each zero-padded to at least two digits) and as decimal
// hours with two places. Negative durations are treated as zero.
func formatEntryFields(d time.Duration) (string, string) {
	if d < 0 {
		d = 0
	}
	total := int(d / time.Second)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	duration := fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	hours := fmt.Sprintf("%.2f", d.Hours())
	return duration, hours
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -run TestFormatEntryFields -v`
Expected: PASS (all sub-tests).

- [ ] **Step 5: Commit**

```bash
git add logentry.go logentry_test.go
git commit -m "feat: add formatEntryFields for CSV duration/hours columns"
```

---

### Task 2: Log-path resolution

**Files:**
- Modify: `logentry.go`
- Test: `logentry_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `defaultLogDir() string` — the iCloud Drive base directory (`~/Library/Mobile Documents/com~apple~CloudDocs`).
  - `defaultLogPath() string` — `defaultLogDir()` joined with `tiktimer-log.csv`.
  - `resolveLogPath(configPath string) string` — returns `configPath` when non-empty, else `defaultLogPath()`.

- [ ] **Step 1: Write the failing test**

Append to `logentry_test.go`:

```go
import (
	"path/filepath"
	// (keep existing imports: testing, time)
)

func TestResolveLogPath(t *testing.T) {
	if got := resolveLogPath("/tmp/custom.csv"); got != "/tmp/custom.csv" {
		t.Errorf("non-empty config: got %q want %q", got, "/tmp/custom.csv")
	}
	got := resolveLogPath("")
	want := defaultLogPath()
	if got != want {
		t.Errorf("empty config: got %q want %q", got, want)
	}
	if filepath.Base(want) != "tiktimer-log.csv" {
		t.Errorf("default filename: got %q", filepath.Base(want))
	}
	if filepath.Base(defaultLogDir()) != "com~apple~CloudDocs" {
		t.Errorf("default dir: got %q", defaultLogDir())
	}
}
```

Note: merge the `path/filepath` import into the existing `import (...)` block rather than adding a second block.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestResolveLogPath -v`
Expected: FAIL — build error `undefined: resolveLogPath` (and `defaultLogPath`, `defaultLogDir`).

- [ ] **Step 3: Write minimal implementation**

Add to `logentry.go` (and add `"os"` and `"path/filepath"` to its import block):

```go
// defaultLogDir returns the macOS iCloud Drive base directory.
func defaultLogDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Mobile Documents", "com~apple~CloudDocs")
}

// defaultLogPath returns the default CSV location inside iCloud Drive.
func defaultLogPath() string {
	return filepath.Join(defaultLogDir(), "tiktimer-log.csv")
}

// resolveLogPath returns configPath if set, otherwise the iCloud default.
func resolveLogPath(configPath string) string {
	if configPath != "" {
		return configPath
	}
	return defaultLogPath()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -run TestResolveLogPath -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add logentry.go logentry_test.go
git commit -m "feat: add log-path resolution with iCloud default"
```

---

### Task 3: `appendLogRow` — CSV writer

**Files:**
- Modify: `logentry.go`
- Test: `logentry_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `appendLogRow(path, timestamp, name, duration, hours, note string) error` — creates the parent directory if missing, writes the header row when the file is missing or zero bytes, appends one CSV record, and returns the first non-nil error among write/flush/close.

- [ ] **Step 1: Write the failing test**

Append to `logentry_test.go` (add `"encoding/csv"`, `"os"` to the import block):

```go
func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return rows
}

func TestAppendLogRowCreatesHeaderThenAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.csv")

	if err := appendLogRow(path, "2026-07-23T14:30:00-04:00", "Acme", "01:23:45", "1.40", "first"); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := appendLogRow(path, "2026-07-23T15:00:00-04:00", "Acme", "00:10:00", "0.17", "second"); err != nil {
		t.Fatalf("second append: %v", err)
	}

	rows := readCSV(t, path)
	if len(rows) != 3 {
		t.Fatalf("want 3 rows (header + 2), got %d: %v", len(rows), rows)
	}
	wantHeader := []string{"timestamp", "name", "duration", "hours", "note"}
	for i, h := range wantHeader {
		if rows[0][i] != h {
			t.Errorf("header col %d: got %q want %q", i, rows[0][i], h)
		}
	}
	if rows[1][4] != "first" || rows[2][4] != "second" {
		t.Errorf("data rows wrong: %v", rows)
	}
}

func TestAppendLogRowWritesHeaderForZeroByteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.csv")
	// Pre-create an empty file, simulating an iCloud placeholder.
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatalf("precreate: %v", err)
	}
	if err := appendLogRow(path, "2026-07-23T14:30:00-04:00", "Acme", "01:00:00", "1.00", "x"); err != nil {
		t.Fatalf("append: %v", err)
	}
	rows := readCSV(t, path)
	if len(rows) != 2 || rows[0][0] != "timestamp" {
		t.Fatalf("expected header + 1 row, got %v", rows)
	}
}

func TestAppendLogRowEscapesNote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.csv")
	note := `fixed "login", again`
	if err := appendLogRow(path, "2026-07-23T14:30:00-04:00", "Acme", "01:00:00", "1.00", note); err != nil {
		t.Fatalf("append: %v", err)
	}
	rows := readCSV(t, path)
	if rows[1][4] != note {
		t.Errorf("note not round-tripped: got %q want %q", rows[1][4], note)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestAppendLogRow -v`
Expected: FAIL — build error `undefined: appendLogRow`.

- [ ] **Step 3: Write minimal implementation**

Add to `logentry.go` (add `"encoding/csv"` to the import block):

```go
// appendLogRow appends one time-entry row to the CSV at path, creating the
// parent directory and a header row when needed. It returns the first error
// encountered while writing, flushing, or closing the file so callers can
// avoid resetting a timer whose time was never persisted.
func appendLogRow(path, timestamp, name, duration, hours, note string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	needHeader := true
	if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
		needHeader = false
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	w := csv.NewWriter(f)
	if needHeader {
		if err := w.Write([]string{"timestamp", "name", "duration", "hours", "note"}); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.Write([]string{timestamp, name, duration, hours, note}); err != nil {
		f.Close()
		return err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -run TestAppendLogRow -v`
Expected: PASS (all three tests).

- [ ] **Step 5: Commit**

```bash
git add logentry.go logentry_test.go
git commit -m "feat: add appendLogRow CSV writer with header + escaping"
```

---

### Task 4: Persist `logPath` in the config file

**Files:**
- Modify: `main.go` (`App` struct ~line 29, `saveData` ~line 38, `save()` ~line 53, `load()` ~line 65)

**Interfaces:**
- Consumes: nothing new.
- Produces: an `App.logPath string` field, populated from `~/.tiktimer.json` on load and written back on save, that Task 5 passes to `resolveLogPath`.

This task is struct plumbing with no meaningful unit test (it reads/writes a fixed path in the user's home directory); it is verified by `go vet` + `go build` and a manual round-trip smoke test.

- [ ] **Step 1: Add the `logPath` field to `App`**

In `main.go`, add a field to the `App` struct (after `display DisplayMode`):

```go
type App struct {
	mu       sync.Mutex
	timers   []*Timer
	active   int  // index of active timer, -1 if none
	running  bool // whether the active timer is ticking
	lastTick time.Time
	display  DisplayMode
	logPath  string // CSV destination; empty means the iCloud default
}
```

- [ ] **Step 2: Add `LogPath` to `saveData`**

Replace the `saveData` struct with:

```go
type saveData struct {
	Timers  []Timer     `json:"timers"`
	Active  int         `json:"active"`
	Display DisplayMode `json:"display,omitempty"`
	LogPath string      `json:"logPath,omitempty"`
}
```

- [ ] **Step 3: Write `logPath` in `save()`**

In `save()`, change the `data` initialization to include the field:

```go
	data := saveData{Active: a.active, Display: a.display, LogPath: a.logPath}
```

- [ ] **Step 4: Read `logPath` in `load()`**

In `load()`, after the existing `if data.Display != "" { a.display = data.Display }` block, add:

```go
	a.logPath = data.LogPath
```

- [ ] **Step 5: Verify build and vet pass**

Run: `go build ./... && go vet ./...`
Expected: no output (success).

- [ ] **Step 6: Manual round-trip smoke test**

Run:
```bash
go build -o /tmp/tiktimer-smoke . && \
printf '{"timers":[{"name":"Job 1","accumulated":0}],"active":-1,"logPath":"/tmp/mine.csv"}' > "$HOME/.tiktimer.json.bak-check"
```
Then confirm the field survives a load/save by inspecting the code path is wired (the automated proof comes in Task 5). Expected: build succeeds, binary produced. Remove `/tmp/tiktimer-smoke` and the temp file afterward.

- [ ] **Step 7: Commit**

```bash
git add main.go
git commit -m "feat: persist logPath override in ~/.tiktimer.json"
```

---

### Task 5: "Log & Reset…" menu handler

**Files:**
- Modify: `main.go` (remove `resetActive` ~lines 206-216; add `logAndReset`; change the menu item ~lines 270-277)

**Interfaces:**
- Consumes: `resolveLogPath`, `defaultLogDir`, `formatEntryFields`, `appendLogRow` (Tasks 1–3); `App.logPath` (Task 4).
- Produces: `App.logAndReset()` — snapshots the active timer, prompts for a note, appends a CSV row, and resets the timer only if it is still the active one.

This is menu-bar UI code that cannot be unit-tested; it is verified by `go vet`, `go build`, and a manual UI checklist.

- [ ] **Step 1: Remove the now-unused `resetActive` method**

Delete this entire method from `main.go`:

```go
func (a *App) resetActive() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.active >= 0 && a.active < len(a.timers) {
		a.timers[a.active].Accumulated = 0
		a.lastTick = time.Now()
	}

	go a.save()
}
```

- [ ] **Step 2: Add the `logAndReset` method**

Add this method to `main.go` (place it where `resetActive` was):

```go
// logAndReset snapshots the active timer's elapsed time, asks the user how the
// time was spent, appends a CSV row for invoicing, and then zeros the timer.
// The note prompt is modal, so the mutex is released around it (matching the
// rename handler). The reset applies only if the snapshotted timer is still the
// active one, because menuet dispatches other click handlers on their own
// goroutines while this Alert is open.
func (a *App) logAndReset() {
	a.mu.Lock()
	if a.active < 0 || a.active >= len(a.timers) {
		a.mu.Unlock()
		return
	}
	snapIdx := a.active
	snapPtr := a.timers[snapIdx]
	snapName := snapPtr.Name
	snapDur := snapPtr.Accumulated
	if a.running {
		snapDur += time.Since(a.lastTick)
	}
	a.mu.Unlock()

	// Skip zero, sub-second, and clock-skew-negative durations entirely.
	if snapDur < time.Second {
		return
	}

	response := menuet.App().Alert(menuet.Alert{
		MessageText:     "Log & Reset",
		InformativeText: "How was this time spent?",
		Buttons:         []string{"Log & Reset", "Cancel"},
		Inputs:          []string{"Note"},
	})
	if response.Button != 0 {
		return // Cancel or dismissed.
	}
	note := ""
	if len(response.Inputs) > 0 {
		note = response.Inputs[0]
	}

	// For the default iCloud path, refuse to write when iCloud Drive is not set
	// up: MkdirAll would create a plain local folder that never syncs.
	if a.logPath == "" {
		if _, err := os.Stat(defaultLogDir()); err != nil {
			menuet.App().Alert(menuet.Alert{
				MessageText:     "iCloud Drive not found",
				InformativeText: "Enable iCloud Drive or set \"logPath\" in ~/.tiktimer.json. Nothing was logged.",
				Buttons:         []string{"OK"},
			})
			return
		}
	}

	durStr, hoursStr := formatEntryFields(snapDur)
	timestamp := time.Now().Format(time.RFC3339)
	if err := appendLogRow(resolveLogPath(a.logPath), timestamp, snapName, durStr, hoursStr, note); err != nil {
		menuet.App().Alert(menuet.Alert{
			MessageText:     "Could not write log",
			InformativeText: fmt.Sprintf("%v\n\nThe timer was not reset.", err),
			Buttons:         []string{"OK"},
		})
		return
	}

	// Reset only if the snapshotted timer is still active and identical.
	a.mu.Lock()
	if a.active == snapIdx && snapIdx < len(a.timers) && a.timers[snapIdx] == snapPtr {
		a.timers[snapIdx].Accumulated = 0
		a.lastTick = time.Now()
	}
	a.mu.Unlock()

	go a.save()
}
```

- [ ] **Step 3: Repoint the menu item**

In `menuItems()`, replace the existing "Reset Current Timer" item:

```go
		items = append(items, menuet.MenuItem{
			Text: "Reset Current Timer",
			Clicked: func() {
				a.resetActive()
			},
		})
```

with:

```go
		items = append(items, menuet.MenuItem{
			Text: "Log & Reset…",
			Clicked: func() {
				a.logAndReset()
			},
		})
```

- [ ] **Step 4: Verify build and vet pass**

Run: `go build ./... && go vet ./...`
Expected: no output (success). In particular, no "declared and not used" or "undefined" errors — confirms `resetActive` has no remaining callers and `logAndReset`'s helpers resolve.

- [ ] **Step 5: Manual UI verification**

Build and run, then walk the checklist:

```bash
go build -o /tmp/tiktimer-manual . && /tmp/tiktimer-manual
```

- [ ] Start the active timer (click the menu-bar item), let it run a few seconds, stop it.
- [ ] Option+click → menu shows **"Log & Reset…"** (not "Reset Current Timer").
- [ ] Click it → a note dialog appears with a "Note" input and `[Log & Reset]` `[Cancel]`.
- [ ] Click **Cancel** → the timer keeps its accumulated time (nothing logged).
- [ ] Click **Log & Reset…** again, type a note, confirm → the timer resets to `00:00`.
- [ ] Confirm a row was written: `tail -n 3 ~/Library/Mobile\ Documents/com~apple~CloudDocs/tiktimer-log.csv` shows the header (if new) and your entry with the note, correct `HH:MM:SS`, and decimal hours.
- [ ] Reset an untouched (`00:00`) timer → no dialog, no new CSV row.

Quit the manual binary (Ctrl-C in its terminal, or the menu Quit) and `rm /tmp/tiktimer-manual`.

- [ ] **Step 6: Commit**

```bash
git add main.go
git commit -m "feat: add Log & Reset menu action writing entries to CSV"
```

---

## Self-Review

**Spec coverage** — every spec section maps to a task:
- Behavior (rename, snapshot, `<1s` skip, note prompt, cancel, guarded reset) → Task 5.
- CSV format (columns, header, RFC3339, `HH:MM:SS`, `%.2f`, escaping) → Tasks 1 & 3.
- `logPath` override + round-trip → Task 4 (config), consumed in Task 5.
- Default-path / iCloud `MkdirAll` asymmetry and "iCloud not found" Alert → Task 5 (handler check) + Task 3 (`MkdirAll` parent only).
- Zero-byte-file header → Task 3.
- Error handling surfaces write/flush/close errors and skips reset on failure → Task 3 (`appendLogRow` return) + Task 5 (Alert + no reset).
- Cross-machine append-only sync → satisfied by append semantics in Task 3; no code beyond that (out-of-scope items intentionally absent).
- Testing (formatEntryFields cases incl. ≥100h, resolveLogPath, appendLogRow header/zero-byte/escaping) → Tasks 1–3.

**Placeholder scan:** no TBD/TODO; every code step contains complete code.

**Type consistency:** `formatEntryFields`, `resolveLogPath`, `defaultLogDir`, `defaultLogPath`, `appendLogRow`, `logAndReset`, and `App.logPath` / `saveData.LogPath` are named identically everywhere they appear across tasks.
