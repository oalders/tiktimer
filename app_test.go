package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oalders/menuet"
)

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		name       string
		d          time.Duration
		showTenths bool
		want       string
	}{
		{"sub-hour-plain", 23*time.Minute + 45*time.Second, false, "23:45"},
		{"sub-hour-tenths", 23*time.Minute + 45*time.Second + 300*time.Millisecond, true, "23:45.3"},
		{"multi-hour-plain", time.Hour + 2*time.Minute + 3*time.Second, false, "1:02:03"},
		{"multi-hour-tenths", time.Hour + 2*time.Minute + 3*time.Second + 900*time.Millisecond, true, "1:02:03.9"},
		{"truncates-to-tenth", 5*time.Second + 199*time.Millisecond, true, "00:05.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatDuration(c.d, c.showTenths); got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

// newTestApp returns a stopped app with n named timers and no active timer.
// Its state file is isolated to a per-test temp dir so the async `go a.save()`
// calls fired by mutators never touch the real home directory or collide with
// other tests.
func newTestApp(t *testing.T, n int) *App {
	a := newApp()
	a.home = t.TempDir()
	// Wait for any fire-and-forget saves to finish before t.TempDir tears the
	// directory down, so a late write cannot fail cleanup.
	t.Cleanup(a.saveWG.Wait)
	for i := 0; i < n; i++ {
		a.timers = append(a.timers, &Timer{Name: "T"})
	}
	return a
}

func TestAddTimer(t *testing.T) {
	a := newApp()
	a.addTimer("Job")
	if len(a.timers) != 1 || a.timers[0].Name != "Job" {
		t.Fatalf("addTimer did not append a named timer: %+v", a.timers)
	}
}

func TestStatusDot(t *testing.T) {
	if got := statusDot(true); got != dotRunning {
		t.Errorf("running: got %q want %q", got, dotRunning)
	}
	if got := statusDot(false); got != dotPaused {
		t.Errorf("paused: got %q want %q", got, dotPaused)
	}
	if dotRunning == dotPaused {
		t.Error("running and paused dots must be visually distinct")
	}
}

func TestSoundForRunning(t *testing.T) {
	if got := soundForRunning(true); got != soundStart {
		t.Errorf("start: got %q want %q", got, soundStart)
	}
	if got := soundForRunning(false); got != soundStop {
		t.Errorf("stop: got %q want %q", got, soundStop)
	}
	if soundStart == soundStop {
		t.Error("start and stop sounds must differ so a toggle is audible")
	}
}

func TestMenuItemsToggle(t *testing.T) {
	newMenuApp := func(t *testing.T) *App {
		a := newTestApp(t, 2)
		a.logPath = filepath.Join(t.TempDir(), "log.csv") // keep off the real iCloud log
		a.timers[1].Name = "Acme"
		return a
	}
	t.Run("leads with the active timer's start/stop item", func(t *testing.T) {
		a := newMenuApp(t)
		a.active = 1

		items := a.menuItems()
		if !strings.HasPrefix(items[0].Text, "Start Acme") || items[0].Clicked == nil {
			t.Fatalf("stopped: first item %q (clickable=%v) want a clickable \"Start Acme\"", items[0].Text, items[0].Clicked != nil)
		}
		if items[1].Type != menuet.Separator {
			t.Errorf("second item type %q want separator", items[1].Type)
		}

		a.running = true
		if got := a.menuItems()[0].Text; !strings.HasPrefix(got, "Stop Acme") {
			t.Errorf("running: first item %q want \"Stop Acme\"", got)
		}
	})
	for _, active := range []int{-1, 2} {
		t.Run(fmt.Sprintf("omitted when active is %d", active), func(t *testing.T) {
			a := newMenuApp(t)
			a.active = active
			for _, it := range a.menuItems() {
				if strings.HasPrefix(it.Text, "Start ") || strings.HasPrefix(it.Text, "Stop ") {
					t.Fatalf("unexpected toggle item %q with no active timer", it.Text)
				}
			}
		})
	}
}

func TestMenuItemsSettingsSubmenu(t *testing.T) {
	a := newTestApp(t, 1)
	a.logPath = filepath.Join(t.TempDir(), "log.csv") // keep off the real iCloud log
	items := a.menuItems()
	last := items[len(items)-1]
	if last.Text != "Settings" || last.Children == nil {
		t.Fatalf("last item %q (submenu %v) want the Settings submenu", last.Text, last.Children != nil)
	}
	if sep := items[len(items)-2]; sep.Type != menuet.Separator {
		t.Errorf("item before Settings has Type %q want Separator", sep.Type)
	}
	var texts []string
	for _, c := range last.Children() {
		texts = append(texts, c.Text)
	}
	want := []string{"Odometer Display", "Daily Target (8h)…", "Copy Log File Path", "Show Log in Finder"}
	if !slices.Equal(texts, want) {
		t.Errorf("Settings children %q want %q", texts, want)
	}
	for _, it := range items[:len(items)-1] {
		if slices.Contains(want, it.Text) {
			t.Errorf("%q is still a top-level item", it.Text)
		}
	}
	for _, c := range last.Children() {
		if c.Clicked == nil {
			t.Errorf("%q is not clickable", c.Text)
		}
	}

	odometer := func() menuet.MenuItem {
		items := a.menuItems()
		return items[len(items)-1].Children()[0]
	}
	a.display = DisplayText
	if odometer().State {
		t.Error("Odometer Display is checked in text mode")
	}
	odometer().Clicked()
	if a.display != DisplayOdometer {
		t.Fatalf("display %q after clicking Odometer Display want %q", a.display, DisplayOdometer)
	}
	if !odometer().State {
		t.Error("Odometer Display is unchecked in odometer mode")
	}
}

func TestMenuItemsTimerSubmenus(t *testing.T) {
	newMenuApp := func(t *testing.T) *App {
		a := newTestApp(t, 2)
		a.logPath = filepath.Join(t.TempDir(), "log.csv") // keep off the real iCloud log
		a.timers[0].Name = "Acme"
		a.timers[1].Name = "Beta"
		a.active = 0
		return a
	}
	row := func(t *testing.T, items []menuet.MenuItem, name string) menuet.MenuItem {
		t.Helper()
		for _, it := range items {
			if strings.HasPrefix(it.Text, name+"  ") {
				return it
			}
		}
		t.Fatalf("no timer row for %q", name)
		return menuet.MenuItem{}
	}
	child := func(t *testing.T, it menuet.MenuItem, text string) menuet.MenuItem {
		t.Helper()
		for _, c := range it.Children() {
			if c.Text == text {
				return c
			}
		}
		t.Fatalf("row %q has no child %q", it.Text, text)
		return menuet.MenuItem{}
	}

	t.Run("each row is a submenu of timer actions", func(t *testing.T) {
		items := newMenuApp(t).menuItems()
		for name, active := range map[string]bool{"Acme": true, "Beta": false} {
			it := row(t, items, name)
			if it.Children == nil || it.Clicked != nil {
				t.Fatalf("%s: Children set=%v Clicked set=%v want a submenu only", name, it.Children != nil, it.Clicked != nil)
			}
			if it.State != active {
				t.Errorf("%s: State %v want %v", name, it.State, active)
			}
			var texts []string
			for _, c := range it.Children() {
				texts = append(texts, c.Text)
				if c.Text == "" && c.Type != menuet.Separator {
					t.Errorf("%s: blank child Type %v want Separator", name, c.Type)
				}
			}
			want := []string{"Switch To", "Rename…", "Set Time…", "Log & Reset…", "Pause on Sleep", "Count Toward Today", "Colour", "", "Remove…"}
			if !slices.Equal(texts, want) {
				t.Errorf("%s: children %q want %q", name, texts, want)
			}
			for _, text := range []string{"Rename…", "Set Time…", "Remove…"} {
				if child(t, it, text).Clicked == nil {
					t.Errorf("%s: %s is not clickable", name, text)
				}
			}
		}
	})

	t.Run("Switch To is disabled for the active timer", func(t *testing.T) {
		a := newMenuApp(t)
		items := a.menuItems()
		if child(t, row(t, items, "Acme"), "Switch To").Clicked != nil {
			t.Error("Switch To is clickable on the active timer")
		}
		sw := child(t, row(t, items, "Beta"), "Switch To")
		if sw.Clicked == nil {
			t.Fatal("Switch To is disabled on an inactive timer")
		}
		sw.Clicked()
		if a.active != 1 {
			t.Errorf("active %d after Switch To want 1", a.active)
		}
	})

	for _, tc := range []struct {
		acc     time.Duration
		enabled bool
	}{
		{999 * time.Millisecond, false},
		{time.Second, true},
	} {
		t.Run(fmt.Sprintf("Log & Reset with %v", tc.acc), func(t *testing.T) {
			a := newMenuApp(t)
			a.timers[1].Accumulated = tc.acc
			got := child(t, row(t, a.menuItems(), "Beta"), "Log & Reset…").Clicked != nil
			if got != tc.enabled {
				t.Errorf("enabled %v want %v", got, tc.enabled)
			}
		})
	}

	t.Run("Log & Reset counts live time on the running timer", func(t *testing.T) {
		a := newMenuApp(t)
		a.timers[0].Accumulated = 500 * time.Millisecond
		a.running = true
		a.lastTick = time.Now().Add(-2 * time.Second)
		if child(t, row(t, a.menuItems(), "Acme"), "Log & Reset…").Clicked == nil {
			t.Error("Log & Reset is disabled though live time is over a second")
		}
	})

	t.Run("Pause on Sleep toggles only its own timer", func(t *testing.T) {
		a := newMenuApp(t)
		a.timers[1].PauseOnSleep = true
		ps := child(t, row(t, a.menuItems(), "Beta"), "Pause on Sleep")
		if !ps.State {
			t.Error("State false want true")
		}
		ps.Clicked()
		if a.timers[1].PauseOnSleep {
			t.Error("Beta's PauseOnSleep did not flip")
		}
		if a.timers[0].PauseOnSleep {
			t.Error("Acme's PauseOnSleep changed")
		}
	})

	t.Run("Count Toward Today toggles only its own timer", func(t *testing.T) {
		a := newMenuApp(t)
		ct := child(t, row(t, a.menuItems(), "Beta"), "Count Toward Today")
		if !ct.State {
			t.Error("State false want true by default")
		}
		ct.Clicked()
		if !a.timers[1].ExcludeFromToday || a.timers[0].ExcludeFromToday {
			t.Errorf("ExcludeFromToday %v, %v want false, true", a.timers[0].ExcludeFromToday, a.timers[1].ExcludeFromToday)
		}
		if child(t, row(t, a.menuItems(), "Beta"), "Count Toward Today").State {
			t.Error("checkmark still shown after excluding")
		}
	})

	t.Run("Colour sets only its own timer", func(t *testing.T) {
		a := newMenuApp(t)
		colours := func() menuet.MenuItem { return child(t, row(t, a.menuItems(), "Beta"), "Colour") }
		if !child(t, colours(), "Automatic").State {
			t.Error("Automatic is not checked on a timer with no colour")
		}
		child(t, colours(), "Blue").Clicked()
		if a.timers[1].Color != "Blue" || a.timers[0].Color != "" {
			t.Fatalf("colours %q, %q want \"\", \"Blue\"", a.timers[0].Color, a.timers[1].Color)
		}
		if !child(t, colours(), "Blue").State || child(t, colours(), "Automatic").State {
			t.Error("checkmark did not move to Blue")
		}
		child(t, colours(), "Automatic").Clicked()
		if a.timers[1].Color != "" {
			t.Errorf("colour %q after Automatic want \"\"", a.timers[1].Color)
		}
	})
}

func TestToggleActive(t *testing.T) {
	t.Run("starts and stops the active timer", func(t *testing.T) {
		a := newTestApp(t, 1)
		a.active = 0

		running, ok := a.toggleActive()
		if !ok || !running || !a.running {
			t.Fatalf("first toggle: running=%v ok=%v a.running=%v want true/true/true", running, ok, a.running)
		}
		running, ok = a.toggleActive()
		if !ok || running || a.running {
			t.Fatalf("second toggle: running=%v ok=%v a.running=%v want false/true/false", running, ok, a.running)
		}
	})
	t.Run("no active timer is a no-op", func(t *testing.T) {
		a := newTestApp(t, 1) // active stays -1
		running, ok := a.toggleActive()
		if ok || running {
			t.Errorf("got running=%v ok=%v want false/false", running, ok)
		}
	})
}

func TestSwitchToSelectsAndPauses(t *testing.T) {
	a := newTestApp(t, 2)
	a.active = 0
	a.running = true

	a.switchTo(1) // switching to a different timer should select it, paused
	if a.active != 1 {
		t.Errorf("active: got %d want 1", a.active)
	}
	if a.running {
		t.Error("switching timers should leave the new timer paused")
	}
}

func TestSwitchToSameTogglesRunning(t *testing.T) {
	a := newTestApp(t, 1)
	a.active = 0
	a.running = false

	a.switchTo(0)
	if !a.running {
		t.Error("switching to the active paused timer should start it")
	}
	a.switchTo(0)
	if a.running {
		t.Error("switching to the active running timer should stop it")
	}
}

func TestRemoveTimerAdjustsActive(t *testing.T) {
	t.Run("removing before active shifts index down", func(t *testing.T) {
		a := newTestApp(t, 3)
		a.active = 2
		a.removeTimer(a.timers[0])
		if a.active != 1 {
			t.Errorf("active: got %d want 1", a.active)
		}
	})
	t.Run("removing the active clears it", func(t *testing.T) {
		a := newTestApp(t, 3)
		a.active = 1
		a.running = true
		a.removeTimer(a.timers[1])
		if a.active != -1 || a.running {
			t.Errorf("active/running: got %d/%v want -1/false", a.active, a.running)
		}
	})
}

// The confirmation modal is open while indices can shift, so removal must hit
// the timer that was chosen, and a timer already gone must be left alone.
func TestRemoveTimerByPointer(t *testing.T) {
	a := newTestApp(t, 3)
	first, second, third := a.timers[0], a.timers[1], a.timers[2]

	a.removeTimer(first)
	a.removeTimer(second)
	if len(a.timers) != 1 || a.timers[0] != third {
		t.Fatalf("after removing first and second: got %v want only third", a.timers)
	}

	a.active, a.running = 0, true
	a.removeTimer(second)
	if len(a.timers) != 1 || a.timers[0] != third {
		t.Errorf("stale pointer removed a timer: got %v want only third", a.timers)
	}
	if a.active != 0 || !a.running {
		t.Errorf("stale pointer changed state: active=%d running=%v want 0 true", a.active, a.running)
	}
}

func TestConfirmRemove(t *testing.T) {
	cases := []struct {
		name        string
		accumulated time.Duration
		stale       bool
		answer      bool
		wantAsked   bool
		wantRemoved bool
	}{
		{"under a second skips prompt", 999 * time.Millisecond, false, false, false, true},
		{"exactly one second asks", time.Second, false, true, true, true},
		{"confirmed", 5 * time.Second, false, true, true, true},
		{"cancelled", 5 * time.Second, false, false, true, false},
		{"stale pointer", 5 * time.Second, true, true, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newTestApp(t, 2)
			target := a.timers[1]
			target.Name = "Job"
			target.Accumulated = c.accumulated
			if c.stale {
				a.removeTimer(target)
			}
			before := len(a.timers)

			var asked bool
			var gotName string
			var gotElapsed time.Duration
			a.confirmRemove(target, func(name string, elapsed time.Duration) bool {
				asked, gotName, gotElapsed = true, name, elapsed
				return c.answer
			})

			if asked != c.wantAsked {
				t.Errorf("asked: got %v want %v", asked, c.wantAsked)
			}
			if asked && (gotName != "Job" || gotElapsed != c.accumulated) {
				t.Errorf("ask args: got %q %v want %q %v", gotName, gotElapsed, "Job", c.accumulated)
			}
			want := before
			if c.wantRemoved {
				want--
			}
			if len(a.timers) != want {
				t.Errorf("timers: got %d want %d", len(a.timers), want)
			}
			wantPresent := !c.wantRemoved && !c.stale
			if got := slices.Contains(a.timers, target); got != wantPresent {
				t.Errorf("target present: got %v want %v", got, wantPresent)
			}
		})
	}

	t.Run("running timer does not add live time to an idle one", func(t *testing.T) {
		a := newTestApp(t, 2)
		keep, target := a.timers[0], a.timers[1]
		target.Accumulated = 0
		a.active, a.running = 0, true
		a.lastTick = time.Now().Add(-5 * time.Second)

		var asked bool
		a.confirmRemove(target, func(string, time.Duration) bool {
			asked = true
			return false
		})

		if asked {
			t.Error("asked: got true want false")
		}
		if len(a.timers) != 1 || a.timers[0] != keep {
			t.Errorf("timers: got %v want only the running timer", a.timers)
		}
	})

	t.Run("running active timer counts live time", func(t *testing.T) {
		a := newTestApp(t, 2)
		target := a.timers[1]
		a.active, a.running = 1, true
		a.lastTick = time.Now().Add(-5 * time.Second)

		var asked bool
		var gotElapsed time.Duration
		a.confirmRemove(target, func(_ string, elapsed time.Duration) bool {
			asked, gotElapsed = true, elapsed
			return false
		})

		if !asked {
			t.Fatal("asked: got false want true")
		}
		if gotElapsed < 5*time.Second {
			t.Errorf("elapsed: got %v want >= 5s", gotElapsed)
		}
		if len(a.timers) != 2 || !slices.Contains(a.timers, target) {
			t.Errorf("timers: got %v want both timers kept after cancel", a.timers)
		}
	})

	t.Run("confirming a running active timer stops it", func(t *testing.T) {
		a := newTestApp(t, 2)
		keep, target := a.timers[0], a.timers[1]
		a.active, a.running = 1, true
		a.lastTick = time.Now().Add(-5 * time.Second)

		var asked bool
		a.confirmRemove(target, func(string, time.Duration) bool {
			asked = true
			return true
		})

		if !asked {
			t.Fatal("asked: got false want true")
		}
		if len(a.timers) != 1 || a.timers[0] != keep {
			t.Errorf("timers: got %v want only the untouched timer", a.timers)
		}
		if a.active != -1 || a.running {
			t.Errorf("active/running: got %d/%v want -1/false", a.active, a.running)
		}
	})

	t.Run("paused active timer adds no live time", func(t *testing.T) {
		a := newTestApp(t, 2)
		keep, target := a.timers[0], a.timers[1]
		target.Accumulated = 0
		a.active, a.running = 1, false
		a.lastTick = time.Now().Add(-5 * time.Second)

		var asked bool
		a.confirmRemove(target, func(string, time.Duration) bool {
			asked = true
			return false
		})

		if asked {
			t.Error("asked: got true want false")
		}
		if len(a.timers) != 1 || a.timers[0] != keep {
			t.Errorf("timers: got %v want only the untouched timer", a.timers)
		}
	})

	t.Run("removed while the dialog is open", func(t *testing.T) {
		a := newTestApp(t, 3)
		keep0, target, keep2 := a.timers[0], a.timers[1], a.timers[2]
		target.Accumulated = 5 * time.Second

		asked := false
		a.confirmRemove(target, func(string, time.Duration) bool {
			asked = true
			a.removeTimer(target)
			return true
		})

		if !asked {
			t.Fatal("asked: got false want true")
		}
		if len(a.timers) != 2 || a.timers[0] != keep0 || a.timers[1] != keep2 {
			t.Errorf("timers: got %v want the two untouched timers", a.timers)
		}
	})
}

func TestRename(t *testing.T) {
	a := newTestApp(t, 2)
	target := a.timers[1]
	a.rename(target, "Renamed")
	if a.timers[1].Name != "Renamed" {
		t.Errorf("name: got %q want %q", a.timers[1].Name, "Renamed")
	}

	// Renaming by pointer must be robust to concurrent index changes: after the
	// timer before it is removed, the rename still targets the same timer.
	a.removeTimer(a.timers[0])
	a.rename(target, "Still Me")
	if a.timers[0].Name != "Still Me" {
		t.Errorf("name after shift: got %q want %q", a.timers[0].Name, "Still Me")
	}

	a.rename(&Timer{Name: "gone"}, "Ignored") // removed timer: must not panic
}

func TestToggleDisplay(t *testing.T) {
	a := newApp() // starts as DisplayText
	a.toggleDisplay()
	if a.display != DisplayOdometer {
		t.Errorf("first toggle: got %q want %q", a.display, DisplayOdometer)
	}
	a.toggleDisplay()
	if a.display != DisplayText {
		t.Errorf("second toggle: got %q want %q", a.display, DisplayText)
	}
}

func TestSnapshotTimer(t *testing.T) {
	t.Run("removed timer", func(t *testing.T) {
		a := newTestApp(t, 1)
		if _, ok := a.snapshotTimer(&Timer{Name: "gone"}); ok {
			t.Error("expected ok=false for a timer not in the list")
		}
	})
	t.Run("stopped timer reports accumulated time", func(t *testing.T) {
		a := newTestApp(t, 1)
		a.timers[0].Accumulated = 90 * time.Second
		snap, ok := a.snapshotTimer(a.timers[0])
		if !ok {
			t.Fatal("expected ok=true")
		}
		if snap.elapsed != 90*time.Second || snap.name != "T" || snap.ptr != a.timers[0] {
			t.Errorf("snapshot mismatch: %+v", snap)
		}
	})
	t.Run("running timer includes time since last tick", func(t *testing.T) {
		a := newTestApp(t, 1)
		a.active = 0
		a.timers[0].Accumulated = 10 * time.Second
		a.running = true
		a.lastTick = time.Now().Add(-2 * time.Second)
		before := time.Now()
		snap, _ := a.snapshotTimer(a.timers[0])
		if snap.elapsed < 12*time.Second || snap.elapsed > 13*time.Second {
			t.Errorf("elapsed: got %v want about 12s", snap.elapsed)
		}
		if snap.lastRun.Before(before) {
			t.Errorf("lastRun: got %v, want now since it is running", snap.lastRun)
		}
	})
	t.Run("inactive timer ignores the running timer's ticks", func(t *testing.T) {
		a := newTestApp(t, 2)
		a.active = 0
		a.running = true
		a.lastTick = time.Now().Add(-time.Hour)
		a.timers[1].Accumulated = time.Minute
		snap, _ := a.snapshotTimer(a.timers[1])
		if snap.elapsed != time.Minute {
			t.Errorf("elapsed: got %v want 1m", snap.elapsed)
		}
	})
}

func TestResetLogged(t *testing.T) {
	t.Run("logging an inactive timer leaves the running one alone", func(t *testing.T) {
		a := newTestApp(t, 2)
		a.active = 0
		a.running = true
		tick := time.Now().Add(-time.Minute)
		a.lastTick = tick
		a.timers[0].Accumulated = time.Hour
		a.timers[1].Accumulated = 30 * time.Minute

		snap, _ := a.snapshotTimer(a.timers[1])
		if !a.resetLogged(snap) {
			t.Fatal("expected reset to be applied")
		}
		if a.timers[1].Accumulated != 0 {
			t.Errorf("logged timer: got %v want 0", a.timers[1].Accumulated)
		}
		if a.active != 0 || !a.running || !a.lastTick.Equal(tick) || a.timers[0].Accumulated != time.Hour {
			t.Errorf("running timer disturbed: active=%d running=%v acc=%v", a.active, a.running, a.timers[0].Accumulated)
		}
	})
	t.Run("running timer keeps time gained while the dialog was open", func(t *testing.T) {
		a := newTestApp(t, 1)
		a.active = 0
		a.running = true
		a.timers[0].Accumulated = 10 * time.Minute
		a.lastTick = time.Now()
		snap, _ := a.snapshotTimer(a.timers[0])
		a.lastTick = a.lastTick.Add(-2 * time.Minute) // two minutes pass with the dialog open

		a.resetLogged(snap)
		if got := a.timers[0].Accumulated; got < 2*time.Minute || got > 3*time.Minute {
			t.Errorf("accumulated: got %v want about 2m", got)
		}
		if !a.running {
			t.Error("timer stopped")
		}
		if !a.timers[0].LastRun.After(snap.lastRun.Add(-time.Second)) {
			t.Errorf("LastRun = %v, want it moved to now by the flush", a.timers[0].LastRun)
		}
	})
	t.Run("switching timers during the dialog still resets", func(t *testing.T) {
		a := newTestApp(t, 2)
		a.active = 0
		a.timers[0].Accumulated = time.Minute
		snap, _ := a.snapshotTimer(a.timers[0])
		a.active = 1
		if !a.resetLogged(snap) || a.timers[0].Accumulated != 0 {
			t.Errorf("accumulated: got %v want 0", a.timers[0].Accumulated)
		}
	})
	t.Run("removed timer", func(t *testing.T) {
		a := newTestApp(t, 1)
		a.timers[0].Accumulated = time.Minute
		snap, _ := a.snapshotTimer(a.timers[0])
		a.timers = nil
		if a.resetLogged(snap) {
			t.Error("expected false for a removed timer")
		}
	})
}

func TestElapsedOfAndSetElapsed(t *testing.T) {
	a := newTestApp(t, 2)
	target := a.timers[1]
	target.Accumulated = 30 * time.Second

	name, elapsed, ok := a.elapsedOf(target)
	if !ok || name != "T" || elapsed != 30*time.Second {
		t.Fatalf("elapsedOf: got %q/%v/%v", name, elapsed, ok)
	}

	a.setElapsed(target, 5*time.Minute)
	if target.Accumulated != 5*time.Minute {
		t.Errorf("setElapsed: got %v want 5m", target.Accumulated)
	}

	// A pointer not in the slice yields ok=false and no panic on set.
	orphan := &Timer{Name: "gone"}
	if _, _, ok := a.elapsedOf(orphan); ok {
		t.Error("expected ok=false for a removed timer")
	}
	a.setElapsed(orphan, time.Hour) // must not panic
}

func TestPauseForSleepAndResume(t *testing.T) {
	t.Run("opted-in running timer pauses on sleep and resumes on wake", func(t *testing.T) {
		a := newTestApp(t, 1)
		a.active = 0
		a.timers[0].PauseOnSleep = true
		a.running = true
		a.lastTick = time.Now().Add(-3 * time.Second)

		a.pauseForSleep()
		if a.running || !a.autoPaused {
			t.Fatalf("after sleep: running=%v autoPaused=%v want false/true", a.running, a.autoPaused)
		}
		// Time up to sleep was banked; the sleep interval itself is not counted.
		banked := a.timers[0].Accumulated

		a.resumeAfterWake()
		if !a.running || a.autoPaused {
			t.Fatalf("after wake: running=%v autoPaused=%v want true/false", a.running, a.autoPaused)
		}
		if a.timers[0].Accumulated != banked {
			t.Errorf("accumulated changed across wake: got %v want %v", a.timers[0].Accumulated, banked)
		}
	})

	t.Run("opted-out timer keeps running through sleep", func(t *testing.T) {
		a := newTestApp(t, 1)
		a.active = 0
		a.timers[0].PauseOnSleep = false
		a.running = true

		a.pauseForSleep()
		if !a.running || a.autoPaused {
			t.Errorf("opted-out timer should be untouched: running=%v autoPaused=%v", a.running, a.autoPaused)
		}
	})

	t.Run("a user-paused timer is not resumed on wake", func(t *testing.T) {
		a := newTestApp(t, 1)
		a.active = 0
		a.timers[0].PauseOnSleep = true
		a.running = false // user already stopped it

		a.pauseForSleep()   // no-op: not running
		a.resumeAfterWake() // must not start it
		if a.running {
			t.Error("wake should not start a timer the user had stopped")
		}
	})

	t.Run("pause and wake are idempotent across screen+system notifications", func(t *testing.T) {
		a := newTestApp(t, 1)
		a.active = 0
		a.timers[0].PauseOnSleep = true
		a.running = true

		a.pauseForSleep()
		a.pauseForSleep() // second notification
		if a.running || !a.autoPaused {
			t.Fatalf("double sleep: running=%v autoPaused=%v", a.running, a.autoPaused)
		}
		a.resumeAfterWake()
		a.resumeAfterWake() // second notification
		if !a.running || a.autoPaused {
			t.Fatalf("double wake: running=%v autoPaused=%v", a.running, a.autoPaused)
		}
	})

	t.Run("a manual toggle while auto-paused cancels the auto-resume", func(t *testing.T) {
		a := newTestApp(t, 1)
		a.active = 0
		a.timers[0].PauseOnSleep = true
		a.running = true

		a.pauseForSleep() // autoPaused = true
		a.toggleActive()  // user manually starts it; clears autoPaused
		if !a.running || a.autoPaused {
			t.Fatalf("after manual toggle: running=%v autoPaused=%v want true/false", a.running, a.autoPaused)
		}
	})
}

func TestTogglePauseOnSleep(t *testing.T) {
	a := newTestApp(t, 2)
	target := a.timers[1]
	if target.PauseOnSleep {
		t.Fatal("PauseOnSleep should default to false")
	}
	a.togglePauseOnSleep(target)
	if !a.timers[1].PauseOnSleep {
		t.Error("toggle should enable PauseOnSleep")
	}
	a.togglePauseOnSleep(target)
	if a.timers[1].PauseOnSleep {
		t.Error("second toggle should disable PauseOnSleep")
	}
	a.togglePauseOnSleep(&Timer{Name: "gone"}) // unknown pointer: must not panic
}

func TestLogDirReady(t *testing.T) {
	// A configured path is always considered ready (its parent dir is created
	// on write); only the iCloud default path is gated on iCloud being present.
	if err := logDirReady("/some/configured/path.csv"); err != nil {
		t.Errorf("configured path should be ready, got %v", err)
	}
}

func TestLogSuccessMessage(t *testing.T) {
	got := logSuccessMessage("01:23:45", "1.40", "Job 1", "/x/log.csv")
	want := "Logged 01:23:45 (1.40) for \"Job 1\" to:\n\n/x/log.csv"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestCopyPathMessage(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		got := copyPathMessage("/x/log.csv", nil)
		want := "/x/log.csv\n\nCopied to clipboard."
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	})
	t.Run("failure includes path and error", func(t *testing.T) {
		got := copyPathMessage("/x/log.csv", errors.New("boom"))
		want := "/x/log.csv\n\n(Could not copy to clipboard: boom)"
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	})
}

func TestSaveIsOwnerOnly(t *testing.T) {
	a := newApp()
	a.home = t.TempDir()
	path := a.savePath()
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.save()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("save file mode = %#o, want 0600", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()

	a := newApp()
	a.home = dir
	a.timers = []*Timer{{Name: "A", Accumulated: time.Minute, PauseOnSleep: true}, {Name: "B", Color: "Teal", ExcludeFromToday: true}}
	a.active = 1
	a.display = DisplayOdometer
	a.logPath = "/custom/log.csv"
	a.save()

	loaded := newApp()
	loaded.home = dir
	loaded.load()
	if len(loaded.timers) != 2 || loaded.timers[0].Name != "A" || loaded.timers[0].Accumulated != time.Minute {
		t.Errorf("timers not restored: %+v", loaded.timers)
	}
	if !loaded.timers[0].PauseOnSleep || loaded.timers[1].PauseOnSleep {
		t.Errorf("PauseOnSleep not restored: got %v/%v want true/false", loaded.timers[0].PauseOnSleep, loaded.timers[1].PauseOnSleep)
	}
	if loaded.timers[0].Color != "" || loaded.timers[1].Color != "Teal" {
		t.Errorf("Color not restored: got %q/%q want \"\"/Teal", loaded.timers[0].Color, loaded.timers[1].Color)
	}
	if loaded.timers[0].ExcludeFromToday || !loaded.timers[1].ExcludeFromToday {
		t.Errorf("ExcludeFromToday not restored: got %v/%v want false/true", loaded.timers[0].ExcludeFromToday, loaded.timers[1].ExcludeFromToday)
	}
	if loaded.active != 1 || loaded.display != DisplayOdometer || loaded.logPath != "/custom/log.csv" {
		t.Errorf("scalar fields not restored: active=%d display=%q logPath=%q", loaded.active, loaded.display, loaded.logPath)
	}
}

func TestLoadDefaultsWhenNoFile(t *testing.T) {
	a := newApp()
	a.home = t.TempDir() // no .tiktimer.json present
	a.load()
	if len(a.timers) != 2 || a.timers[0].Name != "Job 1" || a.timers[1].Name != "Job 2" {
		t.Errorf("expected two default jobs, got %+v", a.timers)
	}
}
