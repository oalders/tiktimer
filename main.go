package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/caseymrm/menuet"
)

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
		dot := statusDot(a.running)
		if a.display == DisplayOdometer {
			noTemplate := false
			menuet.App().SetMenuState(&menuet.MenuState{
				Title:         dot + " " + t.Name,
				Image:         renderOdometer(timeStr),
				FontSize:      11,
				TemplateImage: &noTemplate,
			})
		} else {
			menuet.App().SetMenuState(&menuet.MenuState{
				Title:    fmt.Sprintf("%s %s %s", dot, t.Name, timeStr),
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

// logAndReset snapshots the active timer's elapsed time, asks the user how the
// time was spent, appends a CSV row for invoicing, and then zeros the timer.
// The note prompt is modal, so the mutex is released around it (matching the
// rename handler). The reset applies only if the snapshotted timer is still the
// active one, because menuet dispatches other click handlers on their own
// goroutines while this Alert is open.
func (a *App) logAndReset() {
	snap, ok := a.snapshotActive()
	if !ok {
		return
	}

	// Skip zero, sub-second, and clock-skew-negative durations entirely.
	if snap.elapsed < time.Second {
		return
	}

	// Prefill the elapsed time so the user can correct an over- or under-count
	// before it is logged.
	prefill, _ := formatEntryFields(snap.elapsed)
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
	logDur := snap.elapsed
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

	if err := logDirReady(a.logPath); err != nil {
		menuet.App().Alert(menuet.Alert{
			MessageText:     "iCloud Drive not found",
			InformativeText: "Enable iCloud Drive or set \"logPath\" in ~/.tiktimer.json. Nothing was logged.",
			Buttons:         []string{"OK"},
		})
		return
	}

	durStr, hoursStr := formatEntryFields(logDur)
	timestamp := time.Now().Format(time.RFC3339)
	logPath := resolveLogPath(a.logPath)
	if err := appendLogRow(logPath, timestamp, snap.name, durStr, hoursStr, note); err != nil {
		menuet.App().Alert(menuet.Alert{
			MessageText:     "Could not write log",
			InformativeText: fmt.Sprintf("%v\n\nThe timer was not reset.", err),
			Buttons:         []string{"OK"},
		})
		return
	}

	a.resetIfActive(snap)
	a.saveAsync()

	menuet.App().Alert(menuet.Alert{
		MessageText:     "Logged",
		InformativeText: logSuccessMessage(durStr, hoursStr, snap.name, logPath),
		Buttons:         []string{"OK"},
	})
}

// copyLogPath puts the resolved log file path on the clipboard (via pbcopy) so
// the user can find the CSV without having to log an entry first. The path is
// shown either way, so a pbcopy failure still leaves the user able to copy it
// by hand from the alert.
func (a *App) copyLogPath() {
	logPath := resolveLogPath(a.logPath)
	err := copyToClipboard(logPath)

	menuet.App().Alert(menuet.Alert{
		MessageText:     "Log File Path",
		InformativeText: copyPathMessage(logPath, err),
		Buttons:         []string{"OK"},
	})
}

// setTimerTime edits a timer's accumulated time to an arbitrary value. It works
// on any timer, running or paused, without switching to it (setting 00:00:00 is
// how a timer is reset). The edit prompt is modal, so the mutex is released
// around it; the snapshotted timer is identified by pointer, and the new value
// is applied only if that timer still exists (it may have been removed while
// the dialog was open).
func (a *App) setTimerTime(target *Timer) {
	name, elapsed, ok := a.elapsedOf(target)
	if !ok {
		return
	}

	prefill, _ := formatEntryFields(elapsed)
	response := menuet.App().Alert(menuet.Alert{
		MessageText:     "Set Time",
		InformativeText: fmt.Sprintf("Set the elapsed time for %q (HH:MM:SS):", name),
		Buttons:         []string{"Set", "Cancel"},
		Inputs:          []string{"Time (HH:MM:SS)"},
		InputValues:     []string{prefill},
	})
	if response.Button != 0 || len(response.Inputs) == 0 {
		return // Cancel, dismissed, or no input returned.
	}
	parsed, err := parseHMS(response.Inputs[0])
	if err != nil {
		menuet.App().Alert(menuet.Alert{
			MessageText:     "Invalid time",
			InformativeText: fmt.Sprintf("%v\n\nUse HH:MM:SS. Nothing was changed.", err),
			Buttons:         []string{"OK"},
		})
		return
	}

	a.setElapsed(target, parsed)
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
		activeTimer := a.timers[a.active]
		activeName := activeTimer.Name
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
					a.rename(activeTimer, response.Inputs[0])
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

	items = append(items, menuet.MenuItem{
		Text: "Copy Log File Path",
		Clicked: func() {
			a.copyLogPath()
		},
	})

	items = append(items, menuet.MenuItem{
		Text:  "Odometer Display",
		State: a.display == DisplayOdometer,
		Clicked: func() {
			a.toggleDisplay()
		},
	})

	if len(a.timers) > 0 {
		setChildren := make([]menuet.MenuItem, len(a.timers))
		for i, t := range a.timers {
			tp := t
			acc := t.Accumulated
			if i == a.active && a.running {
				acc += time.Since(a.lastTick)
			}
			setChildren[i] = menuet.MenuItem{
				Text: fmt.Sprintf("%s  %s", t.Name, formatDuration(acc, false)),
				Clicked: func() {
					a.setTimerTime(tp)
				},
			}
		}
		items = append(items, menuet.MenuItem{
			Text: "Set Time",
			Children: func() []menuet.MenuItem {
				return setChildren
			},
		})

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
		running, ok := app.toggleActive()
		app.updateTitle()
		if ok {
			go exec.Command("afplay", soundForRunning(running)).Run()
		}
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
