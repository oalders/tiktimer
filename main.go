package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/oalders/menuet"
)

// menuet drives AppKit, which requires all UI work to happen on the process's
// main OS thread. Go may otherwise migrate the main goroutine to another
// thread, so RunApplication's NSStatusBar setup aborts with "should only be
// invalidated on the Main Thread". Pinning here in init (which runs on the main
// thread before any goroutine starts) keeps main — and thus RunApplication — on
// thread 0.
func init() {
	runtime.LockOSThread()
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
		timeStr := formatDuration(acc, false)
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
		time.Sleep(time.Second)
		a.updateTitle()
		saveInterval++
		if saveInterval >= 30 { // every 30 seconds
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

// logAndReset snapshots target's elapsed time, asks the user how the time was
// spent, appends a CSV row for invoicing, and then takes that time off the
// timer. Any timer can be logged; the active one keeps running.
// Each "Round Up" button reopens the dialog with the time rounded up to the
// next multiple of its billingIncrements entry; the row's tracked column keeps
// the unrounded time.
// The note prompt is modal, so the mutex is released around it (matching the
// rename handler); menuet dispatches other click handlers on their own
// goroutines while this Alert is open, so the reset finds the timer by pointer.
func (a *App) logAndReset(target *Timer) {
	snap, ok := a.snapshotTimer(target)
	if !ok {
		return
	}

	// Skip zero, sub-second, and clock-skew-negative durations entirely.
	if snap.elapsed < time.Second {
		return
	}

	// Prefill the elapsed time so the user can correct an over- or under-count
	// before it is logged.
	tracked, _ := formatEntryFields(snap.elapsed)
	values := []string{tracked, ""}
	// Buttons: Log & Reset, Log & Reset as Yesterday (unless the time is already
	// from an earlier day and will be dated then), one Round Up per billing
	// increment, then Cancel.
	info := "Adjust the time if needed, and note how it was spent."
	buttons := []string{"Log & Reset"}
	yesterdayBtn := -1
	// The dating is decided against when the dialog opened, so the hint shown
	// and the date written agree even if the dialog stays open past 4 am.
	opened := time.Now()
	if workedBeforeToday(snap.lastRun, opened) {
		info += "\n\n" + backdatedNote(snap.lastRun)
	} else {
		yesterdayBtn = len(buttons)
		buttons = append(buttons, yesterdayButton)
		info += "\n\n" + yesterdayHint(yesterdayTimestamp(snap.lastRun, opened))
	}
	roundBase := len(buttons)
	for _, inc := range billingIncrements {
		buttons = append(buttons, roundUpLabel(inc))
	}
	buttons = append(buttons, "Cancel")
	var logDur time.Duration
	asYesterday := false
	for {
		response := menuet.App().Alert(menuet.Alert{
			MessageText:     "Log & Reset",
			InformativeText: info,
			Buttons:         buttons,
			Inputs:          []string{"Time (HH:MM:SS)", "Note"},
			InputValues:     values,
		})
		if response.Button < 0 || response.Button >= roundBase+len(billingIncrements) {
			return // Cancel or dismissed.
		}
		if len(response.Inputs) < 2 {
			return
		}
		parsed, err := parseHMS(response.Inputs[0])
		if err != nil {
			menuet.App().Alert(menuet.Alert{
				MessageText:     "Invalid time",
				InformativeText: fmt.Sprintf("%v\n\nUse HH:MM:SS.", err),
				Buttons:         []string{"OK"},
			})
			values = response.Inputs // Reopen with what was typed, note included.
			continue
		}
		if response.Button < roundBase {
			logDur = parsed
			values = response.Inputs
			asYesterday = response.Button == yesterdayBtn
			break
		}
		// Every NSAlert button dismisses the alert, so "Round Up" reopens it
		// with the rounded time and the note the user had typed so far.
		inc := billingIncrements[response.Button-roundBase]
		rounded, _ := formatEntryFields(roundUpTo(parsed, inc))
		values = []string{rounded, response.Inputs[1]}
	}
	note := values[1]

	if err := logDirReady(a.logPath); err != nil {
		menuet.App().Alert(menuet.Alert{
			MessageText:     "iCloud Drive not found",
			InformativeText: "Enable iCloud Drive or set \"logPath\" in ~/.tiktimer.json. Nothing was logged.",
			Buttons:         []string{"OK"},
		})
		return
	}

	durStr, hoursStr := formatEntryFields(logDur)
	now := time.Now()
	when := logTimestamp(snap.lastRun, opened, now, asYesterday)
	// Log under the timer's current name, in case it was renamed while the
	// dialog was open.
	if name, _, ok := a.elapsedOf(snap.ptr); ok {
		snap.name = name
	}
	timestamp := when.Format(time.RFC3339)
	logPath := resolveLogPath(a.logPath)
	if err := appendLogRow(logPath, timestamp, snap.name, durStr, hoursStr, note, tracked); err != nil {
		menuet.App().Alert(menuet.Alert{
			MessageText:     "Could not write log",
			InformativeText: fmt.Sprintf("%v\n\nThe timer was not reset.", err),
			Buttons:         []string{"OK"},
		})
		return
	}

	// Warnings go first: a banner truncates long text, and the path is the
	// least important part.
	var parts []string
	if !when.Equal(now) {
		parts = append(parts, backdatedNote(when))
	}
	if !a.resetLogged(snap) {
		parts = append(parts, fmt.Sprintf("%q was removed while the dialog was open, so there was nothing to reset.", snap.name))
	}
	msg := strings.Join(append(parts, logSuccessMessage(durStr, hoursStr, snap.name, logPath)), "\n\n")
	a.saveAsync()

	// A notification rather than an alert: success needs no dismissing.
	menuet.App().Notification(menuet.Notification{
		Title:   "Logged",
		Message: msg,
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

// askRemove confirms removing a timer with unlogged time. Cancel comes first so
// Return picks the non-destructive choice (Apple HIG).
func askRemove(name string, elapsed time.Duration) bool {
	response := menuet.App().Alert(menuet.Alert{
		MessageText:     fmt.Sprintf("Remove “%s”?", name),
		InformativeText: fmt.Sprintf("%s of unlogged time will be lost.", formatDuration(elapsed, false)),
		Buttons:         []string{"Cancel", "Remove"},
	})
	return response.Button == 1
}

// colorItems is a timer's Colour submenu: Automatic, then the palette with a
// swatch per color. current is the timer's saved color name.
func colorItems(a *App, tp *Timer, current string) []menuet.MenuItem {
	items := []menuet.MenuItem{
		{Text: "Automatic", State: current == "", Clicked: func() { a.setColor(tp, "") }},
		{Type: menuet.Separator},
	}
	for i, p := range projectPalette {
		items = append(items, menuet.MenuItem{
			Text:    p.Name,
			Image:   renderSwatch(i),
			State:   current == p.Name,
			Clicked: func() { a.setColor(tp, p.Name) },
		})
	}
	return items
}

// todayItems builds the Today summary at the top of the menu: a headline,
// the stacked day bar, and one row per project with a legend dot. Log files
// are read without holding the mutex. The rows are information only; their
// click does nothing, but without it AppKit greys them out as disabled.
func (a *App) todayItems() []menuet.MenuItem {
	timers, logPath, target := a.todaySnapshot()
	tl := readTodayLogged(logFilesToRead(logPath), time.Now())
	projects := buildDaySummary(timers, tl)

	var total time.Duration
	for _, p := range projects {
		total += p.Total()
	}
	items := []menuet.MenuItem{
		{Text: headlineText(total, target, tl), FontWeight: menuet.WeightBold, Clicked: func() {}},
		{Image: renderDayBar(projects, target), Clicked: func() {}},
	}
	for _, p := range projects {
		if !p.ShowRow() {
			continue
		}
		items = append(items, menuet.MenuItem{
			Text:    rowText(p),
			Image:   renderSwatch(p.Color),
			Clicked: func() {},
		})
	}
	return items
}

// revealLog shows the log in Finder: the file itself for a custom logPath,
// otherwise the shared iCloud folder holding every machine's CSV.
func revealLog(logPath string) {
	target := filepath.Dir(defaultLogPath())
	reveal := false
	if logPath != "" {
		target = filepath.Dir(logPath)
		if _, err := os.Stat(logPath); err == nil {
			target, reveal = logPath, true
		}
	}
	// An absolute path always starts with "/", so a logPath beginning with
	// "-" can never be parsed as an option to open.
	target, err := filepath.Abs(target)
	if err != nil {
		return
	}
	if reveal {
		go exec.Command("open", "-R", target).Run()
		return
	}
	go exec.Command("open", target).Run()
}

// setDailyTargetPrompt asks for the Today summary's daily target in hours.
func (a *App) setDailyTargetPrompt() {
	current := a.dailyTargetValue()
	response := menuet.App().Alert(menuet.Alert{
		MessageText:     "Daily Target",
		InformativeText: "Hours you aim to work per day, across all timers (e.g. 8 or 7.5):",
		Buttons:         []string{"Set", "Cancel"},
		Inputs:          []string{"Hours"},
		InputValues:     []string{strconv.FormatFloat(current.Hours(), 'f', -1, 64)},
	})
	if response.Button != 0 || len(response.Inputs) == 0 {
		return
	}
	d, err := parseTargetHours(response.Inputs[0])
	if err != nil {
		menuet.App().Alert(menuet.Alert{
			MessageText:     "Invalid target",
			InformativeText: fmt.Sprintf("%v\n\nNothing was changed.", err),
			Buttons:         []string{"OK"},
		})
		return
	}
	a.setDailyTarget(d)
}

// toggleWithSound starts or stops the active timer and plays the matching cue.
func (a *App) toggleWithSound() {
	running, ok := a.toggleActive()
	a.updateTitle()
	if ok {
		go exec.Command("afplay", soundForRunning(running)).Run()
	}
}

func (a *App) menuItems() []menuet.MenuItem {
	// Built before locking: it snapshots state under the mutex itself and then
	// reads the log files unlocked.
	today := a.todayItems()

	a.mu.Lock()
	defer a.mu.Unlock()

	var items []menuet.MenuItem
	if a.active >= 0 && a.active < len(a.timers) {
		items = append(items,
			menuet.MenuItem{
				Text:       toggleItemText(a.timers[a.active].Name, a.running),
				FontWeight: menuet.WeightBold,
				Clicked:    a.toggleWithSound,
			},
			menuet.MenuItem{Type: menuet.Separator},
		)
	}
	items = append(items, today...)
	items = append(items, menuet.MenuItem{Type: menuet.Separator})

	for i, t := range a.timers {
		idx := i
		tp := t
		name := t.Name
		acc := t.Accumulated
		if i == a.active && a.running {
			acc += time.Since(a.lastTick)
		}

		// Switching to the active timer would toggle it; the bold top item does that.
		switchItem := menuet.MenuItem{Text: "Switch To"}
		if i != a.active {
			switchItem.Clicked = func() { a.switchTo(idx) }
		}
		// Log & Reset never pauses or switches away from the timer in progress.
		// A timer with nothing to log is left disabled.
		logItem := menuet.MenuItem{Text: "Log & Reset…"}
		if acc >= time.Second {
			logItem.Clicked = func() { a.logAndReset(tp) }
		}
		children := []menuet.MenuItem{
			switchItem,
			{
				Text: "Rename…",
				Clicked: func() {
					response := menuet.App().Alert(menuet.Alert{
						MessageText:     "Rename Timer",
						InformativeText: "Enter a new name:",
						Buttons:         []string{"Rename", "Cancel"},
						Inputs:          []string{"Timer name"},
						InputValues:     []string{name},
					})
					if response.Button == 0 && len(response.Inputs) > 0 && response.Inputs[0] != "" {
						a.rename(tp, response.Inputs[0])
					}
				},
			},
			{Text: "Set Time…", Clicked: func() { a.setTimerTime(tp) }},
			logItem,
			{Text: "Pause on Sleep", State: t.PauseOnSleep, Clicked: func() { a.togglePauseOnSleep(tp) }},
			{Text: "Colour", Children: func() []menuet.MenuItem { return colorItems(a, tp, t.Color) }},
			{Type: menuet.Separator},
			{Text: "Remove…", Clicked: func() { a.confirmRemove(tp, askRemove) }},
		}

		items = append(items, menuet.MenuItem{
			Text:     fmt.Sprintf("%s  %s", t.Name, formatDuration(acc, false)),
			State:    i == a.active,
			Children: func() []menuet.MenuItem { return children },
		})
	}

	items = append(items, menuet.MenuItem{Type: menuet.Separator})

	items = append(items, menuet.MenuItem{
		Text: "New Timer…",
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

	// Settings are grouped apart from the everyday actions; menuet adds Start at
	// Login and Quit right after this.
	settings := []menuet.MenuItem{
		{Text: "Odometer Display", State: a.display == DisplayOdometer, Clicked: a.toggleDisplay},
		{Text: fmt.Sprintf("Daily Target (%s)…", formatCompact(a.targetLocked())), Clicked: a.setDailyTargetPrompt},
		{Text: "Copy Log File Path", Clicked: a.copyLogPath},
		{Text: "Show Log in Finder", Clicked: func() { revealLog(a.logPath) }},
	}
	items = append(items,
		menuet.MenuItem{Type: menuet.Separator},
		menuet.MenuItem{Text: "Settings", Children: func() []menuet.MenuItem { return settings }},
	)

	return items
}

func main() {
	app := newApp()
	app.load()

	watchSleep(app)

	go app.tick()

	menuet.App().Label = "com.github.oalders.tiktimer"
	menuet.App().Children = app.menuItems
	// Click opens the menu; Option+click toggles the timer. Toggling takes a
	// deliberate gesture so a stray click can't stop a billing timer unnoticed.
	menuet.App().StatusItemOptionClicked = app.toggleWithSound
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
	}()
	menuet.App().RunApplication()
}
