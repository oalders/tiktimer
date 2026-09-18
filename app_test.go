package main

import (
	"errors"
	"testing"
	"time"
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
		a.removeTimer(0)
		if a.active != 1 {
			t.Errorf("active: got %d want 1", a.active)
		}
	})
	t.Run("removing the active clears it", func(t *testing.T) {
		a := newTestApp(t, 3)
		a.active = 1
		a.running = true
		a.removeTimer(1)
		if a.active != -1 || a.running {
			t.Errorf("active/running: got %d/%v want -1/false", a.active, a.running)
		}
	})
	t.Run("out-of-range index is a no-op", func(t *testing.T) {
		a := newTestApp(t, 2)
		a.removeTimer(5)
		if len(a.timers) != 2 {
			t.Errorf("timers len: got %d want 2", len(a.timers))
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
	a.removeTimer(0)
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

func TestSnapshotActive(t *testing.T) {
	t.Run("no active timer", func(t *testing.T) {
		a := newTestApp(t, 1) // active stays -1
		if _, ok := a.snapshotActive(); ok {
			t.Error("expected ok=false when no timer is active")
		}
	})
	t.Run("paused timer reports accumulated time", func(t *testing.T) {
		a := newTestApp(t, 1)
		a.active = 0
		a.timers[0].Accumulated = 90 * time.Second
		snap, ok := a.snapshotActive()
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
		snap, _ := a.snapshotActive()
		if snap.elapsed < 12*time.Second {
			t.Errorf("elapsed: got %v want >= 12s", snap.elapsed)
		}
	})
}

func TestResetIfActive(t *testing.T) {
	t.Run("resets when still active and unchanged", func(t *testing.T) {
		a := newTestApp(t, 1)
		a.active = 0
		a.timers[0].Accumulated = time.Minute
		snap, _ := a.snapshotActive()
		if !a.resetIfActive(snap) {
			t.Fatal("expected reset to be applied")
		}
		if a.timers[0].Accumulated != 0 {
			t.Errorf("accumulated: got %v want 0", a.timers[0].Accumulated)
		}
	})
	t.Run("does not reset when active changed since snapshot", func(t *testing.T) {
		a := newTestApp(t, 2)
		a.active = 0
		a.timers[0].Accumulated = time.Minute
		snap, _ := a.snapshotActive()
		a.active = 1 // user switched while the modal was open
		if a.resetIfActive(snap) {
			t.Error("expected reset to be skipped after active changed")
		}
		if a.timers[0].Accumulated != time.Minute {
			t.Errorf("accumulated: got %v want 1m", a.timers[0].Accumulated)
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

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()

	a := newApp()
	a.home = dir
	a.timers = []*Timer{{Name: "A", Accumulated: time.Minute}, {Name: "B"}}
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
