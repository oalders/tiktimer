package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Timer struct {
	Name        string        `json:"name"`
	Accumulated time.Duration `json:"accumulated"`
}

type DisplayMode string

const (
	DisplayText     DisplayMode = "text"
	DisplayOdometer DisplayMode = "odometer"
)

type App struct {
	mu       sync.Mutex
	timers   []*Timer
	active   int  // index of active timer, -1 if none
	running  bool // whether the active timer is ticking
	lastTick time.Time
	display  DisplayMode
	logPath  string // CSV destination; empty means the iCloud default
	home     string // base dir for the state file; empty means the OS home dir
	saveWG   sync.WaitGroup
}

type saveData struct {
	Timers  []Timer     `json:"timers"`
	Active  int         `json:"active"`
	Display DisplayMode `json:"display,omitempty"`
	LogPath string      `json:"logPath,omitempty"`
}

func newApp() *App {
	return &App{active: -1, display: DisplayText}
}

func (a *App) savePath() string {
	home := a.home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, ".tiktimer.json")
}

func (a *App) save() {
	a.mu.Lock()
	data := saveData{Active: a.active, Display: a.display, LogPath: a.logPath}
	for _, t := range a.timers {
		data.Timers = append(data.Timers, *t)
	}
	a.mu.Unlock()

	b, _ := json.MarshalIndent(data, "", "  ")
	os.WriteFile(a.savePath(), b, 0644)
}

// saveAsync persists state off the caller's goroutine so UI click handlers do
// not block on disk I/O. The WaitGroup lets tests await in-flight saves before
// their temp dirs are torn down.
func (a *App) saveAsync() {
	a.saveWG.Add(1)
	go func() {
		defer a.saveWG.Done()
		a.save()
	}()
}

func (a *App) load() {
	b, err := os.ReadFile(a.savePath())
	if err != nil {
		a.timers = []*Timer{
			{Name: "Job 1"},
			{Name: "Job 2"},
		}
		return
	}
	var data saveData
	if err := json.Unmarshal(b, &data); err != nil {
		return
	}
	for i := range data.Timers {
		a.timers = append(a.timers, &data.Timers[i])
	}
	a.active = data.Active
	if data.Display != "" {
		a.display = data.Display
	}
	a.logPath = data.LogPath
}

func formatDuration(d time.Duration, showTenths bool) string {
	d = d.Truncate(100 * time.Millisecond)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	t := int(d.Milliseconds()/100) % 10
	if h > 0 {
		if showTenths {
			return fmt.Sprintf("%d:%02d:%02d.%d", h, m, s, t)
		}
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	if showTenths {
		return fmt.Sprintf("%02d:%02d.%d", m, s, t)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

const (
	dotRunning = "🟢" // active timer is ticking
	dotPaused  = "⚪" // a timer is selected but stopped
)

// statusDot returns the colored menu-bar indicator for the active timer's run
// state, so the running/paused state is legible at a glance without reading the
// changing digits.
func statusDot(running bool) string {
	if running {
		return dotRunning
	}
	return dotPaused
}

const (
	// Distinct start/stop cues so an accidental toggle is audible even when the
	// menu bar is not being watched. Tink is a bright, high "on"; Bottle is a
	// muted, lower "off".
	soundStart = "/System/Library/Sounds/Tink.aiff"
	soundStop  = "/System/Library/Sounds/Bottle.aiff"
)

// soundForRunning returns the system sound to play for a run-state transition:
// the start cue when the timer just began running, the stop cue otherwise.
func soundForRunning(running bool) string {
	if running {
		return soundStart
	}
	return soundStop
}

// flushActive saves elapsed time into the active timer. Caller must hold a.mu.
func (a *App) flushActive() {
	if a.running && a.active >= 0 && a.active < len(a.timers) {
		a.timers[a.active].Accumulated += time.Since(a.lastTick)
		a.lastTick = time.Now()
	}
}

// toggleActive starts or stops the active timer. It returns the resulting run
// state and ok=false when there is no active timer to toggle, so the caller can
// play the matching start/stop cue only when a real transition happened.
func (a *App) toggleActive() (running, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.active < 0 || a.active >= len(a.timers) {
		return false, false
	}

	if a.running {
		a.flushActive()
		a.running = false
	} else {
		a.lastTick = time.Now()
		a.running = true
	}

	a.saveAsync()
	return a.running, true
}

func (a *App) switchTo(index int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if index == a.active {
		// Toggle start/stop
		if a.running {
			a.flushActive()
			a.running = false
		} else {
			a.lastTick = time.Now()
			a.running = true
		}
	} else {
		// Switch timers: pause current, select new (paused)
		a.flushActive()
		a.running = false
		a.active = index
	}

	a.saveAsync()
}

func (a *App) addTimer(name string) {
	a.mu.Lock()
	a.timers = append(a.timers, &Timer{Name: name})
	a.mu.Unlock()

	a.saveAsync()
}

func (a *App) removeTimer(index int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if index < 0 || index >= len(a.timers) {
		return
	}

	a.timers = append(a.timers[:index], a.timers[index+1:]...)

	if index == a.active {
		a.active = -1
		a.running = false
	} else if index < a.active {
		a.active--
	}

	a.saveAsync()
}

// rename sets target's name if it still exists. Like the other snapshot
// mutators it identifies the timer by pointer rather than index, so a
// concurrent switchTo/removeTimer while the rename modal is open cannot cause
// the wrong timer to be renamed.
func (a *App) rename(target *Timer, name string) {
	a.mu.Lock()
	for _, t := range a.timers {
		if t == target {
			t.Name = name
			break
		}
	}
	a.mu.Unlock()

	a.saveAsync()
}

func (a *App) toggleDisplay() {
	a.mu.Lock()
	if a.display == DisplayOdometer {
		a.display = DisplayText
	} else {
		a.display = DisplayOdometer
	}
	a.mu.Unlock()

	a.saveAsync()
}

// timerSnapshot is a copy of one timer's current elapsed time, taken while the
// mutex was held, so the UI can prompt the user without holding the lock.
type timerSnapshot struct {
	idx     int
	ptr     *Timer
	name    string
	elapsed time.Duration
}

// snapshotActive returns a snapshot of the active timer's elapsed time,
// including any time accrued since the last tick if it is running. ok is false
// when there is no active timer.
func (a *App) snapshotActive() (snap timerSnapshot, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.active < 0 || a.active >= len(a.timers) {
		return timerSnapshot{}, false
	}
	ptr := a.timers[a.active]
	elapsed := ptr.Accumulated
	if a.running {
		elapsed += time.Since(a.lastTick)
	}
	return timerSnapshot{idx: a.active, ptr: ptr, name: ptr.Name, elapsed: elapsed}, true
}

// resetIfActive zeros the snapshotted timer, but only if it is still the active
// one and unchanged since the snapshot (menuet may dispatch other handlers
// while a modal is open). It reports whether the reset was applied.
func (a *App) resetIfActive(snap timerSnapshot) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.active == snap.idx && snap.idx < len(a.timers) && a.timers[snap.idx] == snap.ptr {
		a.timers[snap.idx].Accumulated = 0
		a.lastTick = time.Now()
		return true
	}
	return false
}

// elapsedOf returns target's current elapsed time, accounting for live ticking
// if it is the active running timer. ok is false if target no longer exists.
func (a *App) elapsedOf(target *Timer) (name string, elapsed time.Duration, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for i, t := range a.timers {
		if t == target {
			elapsed = t.Accumulated
			if i == a.active && a.running {
				elapsed += time.Since(a.lastTick)
			}
			return t.Name, elapsed, true
		}
	}
	return "", 0, false
}

// setElapsed sets target's accumulated time if it still exists. If target is
// the active running timer, lastTick is reset so ticking continues from the new
// base without double-counting.
func (a *App) setElapsed(target *Timer, d time.Duration) {
	a.mu.Lock()
	for i, t := range a.timers {
		if t == target {
			t.Accumulated = d
			if i == a.active && a.running {
				a.lastTick = time.Now()
			}
			break
		}
	}
	a.mu.Unlock()

	a.saveAsync()
}

// logDirReady reports an error when the log destination is not usable. For the
// default (empty) path it requires iCloud Drive to be present, since otherwise
// MkdirAll would create a plain local folder that never syncs.
func logDirReady(configPath string) error {
	if configPath == "" {
		if _, err := os.Stat(defaultLogDir()); err != nil {
			return err
		}
	}
	return nil
}

// logSuccessMessage builds the confirmation text shown after a row is written.
func logSuccessMessage(durStr, hoursStr, name, path string) string {
	return fmt.Sprintf("Logged %s (%s) for %q to:\n\n%s", durStr, hoursStr, name, path)
}

// copyPathMessage builds the text shown by the Copy Log File Path item. The
// path is always included so the user can copy it by hand if pbcopy failed.
func copyPathMessage(path string, copyErr error) string {
	if copyErr != nil {
		return fmt.Sprintf("%s\n\n(Could not copy to clipboard: %v)", path, copyErr)
	}
	return fmt.Sprintf("%s\n\nCopied to clipboard.", path)
}

// copyToClipboard writes s to the macOS clipboard via pbcopy.
func copyToClipboard(s string) error {
	c := exec.Command("pbcopy")
	c.Stdin = strings.NewReader(s)
	return c.Run()
}
