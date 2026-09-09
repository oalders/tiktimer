package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/caseymrm/menuet"
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
	home, _ := os.UserHomeDir()
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

// flushActive saves elapsed time into the active timer. Caller must hold a.mu.
func (a *App) flushActive() {
	if a.running && a.active >= 0 && a.active < len(a.timers) {
		a.timers[a.active].Accumulated += time.Since(a.lastTick)
		a.lastTick = time.Now()
	}
}

func (a *App) updateTitle() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.active >= 0 && a.active < len(a.timers) {
		t := a.timers[a.active]
		acc := t.Accumulated
		if a.running {
			acc += time.Since(a.lastTick)
		}
		timeStr := formatDuration(acc, true)
		if a.display == DisplayOdometer {
			noTemplate := false
			menuet.App().SetMenuState(&menuet.MenuState{
				Title:         " " + t.Name,
				Image:         renderOdometer(timeStr),
				FontSize:      11,
				TemplateImage: &noTemplate,
			})
		} else {
			menuet.App().SetMenuState(&menuet.MenuState{
				Title:    fmt.Sprintf("%s %s", t.Name, timeStr),
				FontSize: 11,
			})
		}
	} else {
		menuet.App().SetMenuState(&menuet.MenuState{
			Title:    "TikTimer",
			FontSize: 11,
		})
	}
}

func (a *App) tick() {
	saveInterval := 0
	for {
		time.Sleep(100 * time.Millisecond)
		a.updateTitle()
		saveInterval++
		if saveInterval >= 300 { // every 30 seconds
			saveInterval = 0
			a.mu.Lock()
			if a.running {
				a.flushActive()
			}
			a.mu.Unlock()
			a.save()
		}
	}
}

func (a *App) toggleActive() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.active < 0 || a.active >= len(a.timers) {
		return
	}

	if a.running {
		a.flushActive()
		a.running = false
	} else {
		a.lastTick = time.Now()
		a.running = true
	}

	go a.save()
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

	go a.save()
}

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

	// Prefill the elapsed time so the user can correct an over- or under-count
	// before it is logged.
	prefill, _ := formatEntryFields(snapDur)
	response := menuet.App().Alert(menuet.Alert{
		MessageText:     "Log & Reset",
		InformativeText: "Adjust the time if needed, and note how it was spent.",
		Buttons:         []string{"Log & Reset", "Cancel"},
		Inputs:          []string{"Time (HH:MM:SS)", "Note"},
		InputValues:     []string{prefill, ""},
	})
	if response.Button != 0 {
		return // Cancel or dismissed.
	}
	logDur := snapDur
	if len(response.Inputs) > 0 {
		parsed, err := parseHMS(response.Inputs[0])
		if err != nil {
			menuet.App().Alert(menuet.Alert{
				MessageText:     "Invalid time",
				InformativeText: fmt.Sprintf("%v\n\nUse HH:MM:SS. Nothing was logged.", err),
				Buttons:         []string{"OK"},
			})
			return
		}
		logDur = parsed
	}
	note := ""
	if len(response.Inputs) > 1 {
		note = response.Inputs[1]
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

	durStr, hoursStr := formatEntryFields(logDur)
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

// resetActive zeros the active timer without logging it, after a confirmation
// prompt (the accumulated time is discarded, not recorded). The prompt is
// modal, so the mutex is released around it and the reset applies only if the
// snapshotted timer is still the active one, matching logAndReset.
func (a *App) resetActive() {
	a.mu.Lock()
	if a.active < 0 || a.active >= len(a.timers) {
		a.mu.Unlock()
		return
	}
	snapIdx := a.active
	snapPtr := a.timers[snapIdx]
	snapName := snapPtr.Name
	a.mu.Unlock()

	response := menuet.App().Alert(menuet.Alert{
		MessageText:     "Reset Timer",
		InformativeText: fmt.Sprintf("Discard the elapsed time on %q without logging it?", snapName),
		Buttons:         []string{"Reset", "Cancel"},
	})
	if response.Button != 0 {
		return // Cancel or dismissed.
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

func (a *App) addTimer(name string) {
	a.mu.Lock()
	a.timers = append(a.timers, &Timer{Name: name})
	a.mu.Unlock()

	go a.save()
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

	go a.save()
}

func (a *App) menuItems() []menuet.MenuItem {
	a.mu.Lock()
	defer a.mu.Unlock()

	var items []menuet.MenuItem

	for i, t := range a.timers {
		idx := i
		acc := t.Accumulated
		if i == a.active && a.running {
			acc += time.Since(a.lastTick)
		}

		items = append(items, menuet.MenuItem{
			Text:  fmt.Sprintf("%s  %s", t.Name, formatDuration(acc, false)),
			State: i == a.active,
			Clicked: func() {
				a.switchTo(idx)
			},
		})
	}

	items = append(items, menuet.MenuItem{Type: menuet.Separator})

	if a.active >= 0 && a.active < len(a.timers) {
		items = append(items, menuet.MenuItem{
			Text: "Log & Reset…",
			Clicked: func() {
				a.logAndReset()
			},
		})
		items = append(items, menuet.MenuItem{
			Text: "Reset Current Timer",
			Clicked: func() {
				a.resetActive()
			},
		})
		activeName := a.timers[a.active].Name
		items = append(items, menuet.MenuItem{
			Text: "Rename Current Timer...",
			Clicked: func() {
				response := menuet.App().Alert(menuet.Alert{
					MessageText:     "Rename Timer",
					InformativeText: "Enter a new name:",
					Buttons:         []string{"Rename", "Cancel"},
					Inputs:          []string{"Timer name"},
					InputValues:     []string{activeName},
				})
				if response.Button == 0 && len(response.Inputs) > 0 && response.Inputs[0] != "" {
					a.mu.Lock()
					if a.active >= 0 && a.active < len(a.timers) {
						a.timers[a.active].Name = response.Inputs[0]
					}
					a.mu.Unlock()
					go a.save()
				}
			},
		})
	}

	items = append(items, menuet.MenuItem{
		Text: "New Timer...",
		Clicked: func() {
			response := menuet.App().Alert(menuet.Alert{
				MessageText:     "New Timer",
				InformativeText: "Enter a name for the timer:",
				Buttons:         []string{"Add", "Cancel"},
				Inputs:          []string{"Timer name"},
			})
			if response.Button == 0 && len(response.Inputs) > 0 && response.Inputs[0] != "" {
				a.addTimer(response.Inputs[0])
			}
		},
	})

	odometerOn := a.display == DisplayOdometer
	items = append(items, menuet.MenuItem{
		Text:  "Odometer Display",
		State: odometerOn,
		Clicked: func() {
			a.mu.Lock()
			if a.display == DisplayOdometer {
				a.display = DisplayText
			} else {
				a.display = DisplayOdometer
			}
			a.mu.Unlock()
			go a.save()
		},
	})

	if len(a.timers) > 0 {
		children := make([]menuet.MenuItem, len(a.timers))
		for i, t := range a.timers {
			idx := i
			children[i] = menuet.MenuItem{
				Text: t.Name,
				Clicked: func() {
					a.removeTimer(idx)
				},
			}
		}
		items = append(items, menuet.MenuItem{
			Text: "Remove Timer",
			Children: func() []menuet.MenuItem {
				return children
			},
		})
	}

	return items
}

func main() {
	app := newApp()
	app.load()

	go app.tick()

	menuet.App().Label = "com.github.oalders.tiktimer"
	menuet.App().Children = app.menuItems
	menuet.App().StatusItemClicked = func() {
		app.toggleActive()
		app.updateTitle()
		go exec.Command("afplay", "/System/Library/Sounds/Pop.aiff").Run()
	}
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		app.mu.Lock()
		app.flushActive()
		app.running = false
		app.mu.Unlock()
		app.save()
		os.Exit(0)
	}()

	go func() {
		// Wait for the app to start before updating the UI
		time.Sleep(500 * time.Millisecond)
		app.updateTitle()
		app.tick()
	}()
	menuet.App().RunApplication()
}
