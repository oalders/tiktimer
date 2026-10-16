package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// defaultDailyTarget applies when dailyTargetHours is unset in the save file.
const defaultDailyTarget = 8 * time.Hour

// sfDataless is the BSD file flag macOS sets on an iCloud file whose contents
// have been evicted. Opening such a file blocks while it downloads, which
// would freeze the menu (menuItems runs on the main thread).
const sfDataless = 0x40000000

// logFilesToRead lists the CSVs the Today summary sums. A custom logPath is
// read alone; the default reads every per-machine CSV in the shared iCloud
// folder, so time logged on the other Mac counts too.
func logFilesToRead(configPath string) []string {
	if configPath != "" {
		return []string{configPath}
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(defaultLogPath()), "*.csv"))
	return matches
}

// todayLog is the logged time read from the CSVs for one local day.
type todayLog struct {
	logged map[string]time.Duration
	counts map[string]int
	// order lists project names in order of first appearance.
	order []string
	// unreadable is set when a file exists but could not be read; dataless when
	// one was skipped because iCloud has not downloaded it.
	unreadable bool
	dataless   bool
}

// dayBounds returns the half-open local-day window [start, end) containing
// now. AddDate (not +24h) keeps DST days correct.
func dayBounds(now time.Time) (time.Time, time.Time) {
	y, m, d := now.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	return start, start.AddDate(0, 0, 1)
}

// readTodayLogged sums each project's logged duration for now's local day
// across paths. Missing files count as zero. Rows are read by position with a
// variable field count, because files predating the tracked column have a
// 5-column header followed by 6-column rows. Malformed rows are skipped.
func readTodayLogged(paths []string, now time.Time) todayLog {
	tl := todayLog{logged: map[string]time.Duration{}, counts: map[string]int{}}
	start, end := dayBounds(now)
	for _, p := range paths {
		fi, err := os.Stat(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			tl.unreadable = true
			continue
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Flags&sfDataless != 0 {
			tl.dataless = true
			continue
		}
		if err := tl.readFile(p, start, end, now.Location()); err != nil {
			tl.unreadable = true
		}
	}
	return tl
}

func (tl *todayLog) readFile(path string, start, end time.Time, loc *time.Location) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	for {
		rec, err := r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			// A quoting error leaves the reader past the bad record; keep going.
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				continue
			}
			return err
		}
		if len(rec) < 3 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, rec[0])
		if err != nil {
			continue // header row or malformed timestamp
		}
		ts = ts.In(loc)
		if ts.Before(start) || !ts.Before(end) {
			continue
		}
		d, err := parseHMS(rec[2])
		if err != nil {
			continue
		}
		name := strings.TrimSpace(rec[1])
		if _, seen := tl.counts[name]; !seen {
			tl.order = append(tl.order, name)
		}
		tl.logged[name] += d
		tl.counts[name]++
	}
}

// projectTotal is one row of the Today summary.
type projectTotal struct {
	Name     string
	Logged   time.Duration
	Unlogged time.Duration
	Entries  int
}

func (p projectTotal) Total() time.Duration { return p.Logged + p.Unlogged }

// timerTime is a timer's name and its current (not yet logged) elapsed time.
type timerTime struct {
	Name    string
	Elapsed time.Duration
}

// buildDaySummary merges timer time with today's logged time, keyed by name.
// Timer order comes first, then CSV-only names (renamed or removed timers) in
// order of first appearance, so each project's palette color stays stable
// through the day. Projects with nothing logged and under a minute on the
// timer are omitted.
func buildDaySummary(timers []timerTime, tl todayLog) []projectTotal {
	byName := map[string]*projectTotal{}
	var ordered []*projectTotal
	get := func(name string) *projectTotal {
		if p, ok := byName[name]; ok {
			return p
		}
		p := &projectTotal{Name: name}
		byName[name] = p
		ordered = append(ordered, p)
		return p
	}
	for _, t := range timers {
		p := get(strings.TrimSpace(t.Name))
		if t.Elapsed > 0 {
			p.Unlogged += t.Elapsed
		}
	}
	for _, name := range tl.order {
		p := get(name)
		p.Logged = tl.logged[name]
		p.Entries = tl.counts[name]
	}
	var out []projectTotal
	for _, p := range ordered {
		if p.Entries > 0 || p.Unlogged >= time.Minute {
			out = append(out, *p)
		}
	}
	return out
}

// formatHM renders d as "7h 08m", truncated to whole minutes.
func formatHM(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	m := int(d / time.Minute)
	return fmt.Sprintf("%dh %02dm", m/60, m%60)
}

// formatCompact renders d without zero parts, truncated to minutes: "8h",
// "52m", "7h 30m" (and "0m" for under a minute).
func formatCompact(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	m := int(d / time.Minute)
	switch {
	case m < 60:
		return fmt.Sprintf("%dm", m)
	case m%60 == 0:
		return fmt.Sprintf("%dh", m/60)
	}
	return fmt.Sprintf("%dh %02dm", m/60, m%60)
}

// headlineText is the Today summary's first line. The remaining/over time is
// derived from the minute-truncated total, so the two numbers always agree.
func headlineText(total, target time.Duration, tl todayLog) string {
	total = total.Truncate(time.Minute)
	s := fmt.Sprintf("Today  %s of %s", formatHM(total), formatCompact(target))
	switch diff := target - total; {
	case diff < time.Minute && diff > -time.Minute:
		s += " · target reached"
	case diff > 0:
		s += fmt.Sprintf(" · %s to go", formatCompact(diff))
	default:
		s += fmt.Sprintf(" · %s over", formatCompact(-diff))
	}
	if tl.unreadable {
		s += " · log unreadable"
	}
	if tl.dataless {
		s += " · some logs not downloaded"
	}
	return s
}

// rowText is a project row: "Acme  4h 50m  (5 entries, 23m unlogged)". The
// total includes the unlogged timer time.
func rowText(p projectTotal) string {
	var parts []string
	switch {
	case p.Entries == 1:
		parts = append(parts, "1 entry")
	case p.Entries > 1:
		parts = append(parts, fmt.Sprintf("%d entries", p.Entries))
	}
	// Under a minute of timer time is noise next to hours of logged work.
	if p.Unlogged >= time.Minute {
		parts = append(parts, formatCompact(p.Unlogged)+" unlogged")
	}
	s := fmt.Sprintf("%s  %s", p.Name, formatHM(p.Total()))
	if len(parts) > 0 {
		s += "  (" + strings.Join(parts, ", ") + ")"
	}
	return s
}

// parseTargetHours parses the Daily Target dialog input: decimal hours, with
// either "." or "," as the decimal separator, in (0, 24].
func parseTargetHours(s string) (time.Duration, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	h, err := strconv.ParseFloat(s, 64)
	if err != nil || h <= 0 || h > 24 {
		return 0, fmt.Errorf("enter hours between 0 and 24, e.g. 8 or 7.5 (got %q)", s)
	}
	return time.Duration(h * float64(time.Hour)).Round(time.Minute), nil
}
