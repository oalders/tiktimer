package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/caseymrm/menuet"
)

type Timer struct {
	Name        string        `json:"name"`
	Accumulated time.Duration `json:"accumulated"`
}

type App struct {
	mu       sync.Mutex
	timers   []*Timer
	active   int  // index of active timer, -1 if none
	running  bool // whether the active timer is ticking
	lastTick time.Time
}

type saveData struct {
	Timers []Timer `json:"timers"`
	Active int     `json:"active"`
}

func newApp() *App {
	return &App{active: -1}
}

func (a *App) savePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".tiktimer.json")
}

func (a *App) save() {
	a.mu.Lock()
	data := saveData{Active: a.active}
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

	var title string
	if a.active >= 0 && a.active < len(a.timers) {
		t := a.timers[a.active]
		acc := t.Accumulated
		if a.running {
			acc += time.Since(a.lastTick)
		}
		title = fmt.Sprintf("%s %s", t.Name, formatDuration(acc, true))
	} else {
		title = "TikTimer"
	}

	menuet.App().SetMenuState(&menuet.MenuState{
		Title:    title,
		FontSize: 11,
	})
}

func (a *App) tick() {
	for {
		time.Sleep(100 * time.Millisecond)
		a.updateTitle()
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
		// Switch timers: pause current, start new
		a.flushActive()
		a.running = false
		a.active = index
		a.lastTick = time.Now()
		a.running = true
	}

	go a.save()
}

func (a *App) resetActive() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.active >= 0 && a.active < len(a.timers) {
		a.timers[a.active].Accumulated = 0
		a.lastTick = time.Now()
	}

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
			Text: "Reset Current Timer",
			Clicked: func() {
				a.resetActive()
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
	app.updateTitle()
	menuet.App().RunApplication()
}
