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

// maxLogBytes caps how much of each log file the Today summary reads.
const maxLogBytes = 32 << 20

// logFilesToRead lists the CSVs the Today summary sums. A custom logPath is
// read alone; the default reads every per-machine CSV in the shared iCloud
// folder, so time logged on the other Mac counts too.
func logFilesToRead(configPath string) []string {
	if configPath != "" {
		return []string{configPath}
	}
	return machineLogsIn(filepath.Dir(defaultLogPath()))
}

// machineLogsIn lists the per-machine CSVs in dir: files whose base name is
// already a sanitized machine name (see defaultLogPath). That skips iCloud
// conflict copies ("iMac 2.csv") and stray exports, which would otherwise
// count the same rows twice.
func machineLogsIn(dir string) []string {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.csv"))
	var out []string
	for _, m := range matches {
		base := strings.TrimSuffix(filepath.Base(m), ".csv")
		if base != "" && sanitizeName(base) == base {
			out = append(out, m)
		}
	}
	return out
}

// todayLog is the logged time read from the CSVs for one local day.
type todayLog struct {
	logged map[string]time.Duration
	counts map[string]int
	// order lists project names in order of first appearance.
	order []string
	logFlags
}

// logFlags warn that the logs read may be incomplete: unreadable when a file
// exists but could not be read, dataless when one was skipped because iCloud
// has not downloaded it.
type logFlags struct {
	unreadable bool
	dataless   bool
}

// dayStartHour is when a work day begins. Starting at 4 am rather than
// midnight keeps a late-night session on the day it belongs to: work logged
// at 1 am still counts toward the previous evening.
const dayStartHour = 4

// dayBounds returns the half-open work-day window [start, end) containing
// now: from dayStartHour local time to the same time the next day. AddDate
// (not +24h) keeps DST days correct.
func dayBounds(now time.Time) (time.Time, time.Time) {
	y, m, d := now.Date()
	start := time.Date(y, m, d, dayStartHour, 0, 0, 0, now.Location())
	if now.Before(start) {
		start = start.AddDate(0, 0, -1)
	}
	return start, start.AddDate(0, 0, 1)
}

// workedBeforeToday reports whether time that last accrued at lastRun belongs
// to an earlier work day than now's. A zero lastRun counts as today.
func workedBeforeToday(lastRun, now time.Time) bool {
	start, _ := dayBounds(now)
	return !lastRun.IsZero() && lastRun.Before(start)
}

// weekBounds returns the start of the work week containing now's work day,
// Monday at dayStartHour. Judging by the work day keeps Monday before 4 am in
// the previous week.
func weekBounds(now time.Time) time.Time {
	day, _ := dayBounds(now)
	offset := (int(day.Weekday()) + 6) % 7 // days since Monday
	return day.AddDate(0, 0, -offset)
}

// logTimestamp is the time a Log & Reset row is dated: when the timer last
// ran if all its time is from an earlier work day (so clearing out
// yesterday's timers in the morning files that work under yesterday), the
// previous work day if the user chose Log & Reset as Yesterday, otherwise
// now. The day is judged at opened, when the dialog appeared, so the dialog's
// hint and the row agree even if it stays open past dayStartHour. A timer that
// ran on both days is dated now.
func logTimestamp(lastRun, opened, now time.Time, asYesterday bool) time.Time {
	switch {
	case workedBeforeToday(lastRun, opened):
		return lastRun
	case asYesterday:
		return yesterdayTimestamp(lastRun, opened)
	}
	return now
}

// yesterdayTimestamp dates a row the user chose to log as the previous work
// day's (the yesterdayButton), for when a few seconds of accidental
// running today would otherwise date it now: when the timer last ran, if that
// was before today's work day, otherwise 23:59 on the previous work day.
func yesterdayTimestamp(lastRun, now time.Time) time.Time {
	if workedBeforeToday(lastRun, now) {
		return lastRun
	}
	start, _ := dayBounds(now)
	y, m, d := start.AddDate(0, 0, -1).Date()
	return time.Date(y, m, d, 23, 59, 0, 0, now.Location())
}

// yesterdayButton is the Log & Reset dialog button that dates the row on the
// previous work day; named as a variant of Log & Reset so it is clearly an
// alternative to it, not an extra step.
const yesterdayButton = "Log & Reset as Yesterday"

// yesterdayHint explains, in the dialog, the date each Log & Reset button gives.
func yesterdayHint(when time.Time) string {
	return fmt.Sprintf("Log & Reset dates it now; %s dates it %s.", yesterdayButton, when.Format("Mon Jan 2 15:04"))
}

// backdatedNote tells the user a Log & Reset row is dated on an earlier work
// day rather than now.
func backdatedNote(when time.Time) string {
	return "Dated " + when.Format("Mon Jan 2 15:04") + ", an earlier work day."
}

// loggedDays holds one todayLog per work day, keyed by the day's start in Unix
// seconds (time.Time keys would also compare locations), so each day keeps its
// own order and palette colors.
type loggedDays map[int64]*todayLog

// day returns the logged time for the work day starting at start.
func (ld loggedDays) day(start time.Time) todayLog {
	if tl := ld[start.Unix()]; tl != nil {
		return *tl
	}
	return todayLog{}
}

// readLogged sums each project's logged duration per work day in [start, end)
// across paths, reading each file once. The flags are file-level, so they are
// returned apart from any day. Missing files count as zero. Rows are read by
// position with a variable field count, because files predating the tracked
// column have a 5-column header followed by 6-column rows. Malformed rows are
// skipped.
func readLogged(paths []string, start, end time.Time) (days loggedDays, flags logFlags) {
	days = loggedDays{}
	for _, p := range paths {
		fi, err := os.Stat(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			flags.unreadable = true
			continue
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Flags&sfDataless != 0 {
			flags.dataless = true
			continue
		}
		if err := readFile(p, start, end, days); err != nil {
			flags.unreadable = true
		}
	}
	return days, flags
}

// readFile adds path's rows in [start, end) to days. A quoting error skips
// the bad record but is still returned, since it can swallow later rows.
func readFile(path string, start, end time.Time, days loggedDays) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Cap each file so a huge or corrupt file in the synced folder cannot hang
	// the menu; a real log is a few kilobytes per day.
	r := csv.NewReader(io.LimitReader(f, maxLogBytes))
	r.FieldsPerRecord = -1
	var parseErr error
	for {
		rec, err := r.Read()
		if err == io.EOF {
			return parseErr
		}
		if err != nil {
			// A quoting error leaves the reader past the bad record; keep going.
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				parseErr = err
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
		ts = ts.In(start.Location())
		if ts.Before(start) || !ts.Before(end) {
			continue
		}
		d, err := parseHMS(rec[2])
		if err != nil {
			continue
		}
		name := strings.TrimSpace(unformulaSafe(rec[1]))
		ds, _ := dayBounds(ts)
		day := days[ds.Unix()]
		if day == nil {
			day = &todayLog{logged: map[string]time.Duration{}, counts: map[string]int{}}
			days[ds.Unix()] = day
		}
		if _, seen := day.counts[name]; !seen {
			day.order = append(day.order, name)
		}
		day.logged[name] += d
		day.counts[name]++
	}
}

// projectTotal is one row of the Today summary.
type projectTotal struct {
	Name     string
	Logged   time.Duration
	Unlogged time.Duration
	Entries  int
	// Color is the project's palette index: the timer's chosen color, else
	// fixed by its position among all timers and logged names, so it does
	// not shift as other projects come and go from the summary.
	Color int
	// Excluded marks a timer set not to count toward the totals.
	Excluded bool
}

func (p projectTotal) Total() time.Duration { return p.Logged + p.Unlogged }

// dayTotals splits the projects' time into what counts toward the totals
// and what is excluded from them.
func dayTotals(projects []projectTotal) (counted, excluded time.Duration) {
	for _, p := range projects {
		if p.Excluded {
			excluded += p.Total()
		} else {
			counted += p.Total()
		}
	}
	return counted, excluded
}

// ShowRow reports whether the project gets its own row. Under a minute of
// timer time with nothing logged is noise; it still adds to the totals and
// the bar.
func (p projectTotal) ShowRow() bool { return p.Entries > 0 || p.Unlogged >= time.Minute }

// timerTime is a timer's name, its current (not yet logged) elapsed time, its
// chosen palette color name, if any, whether it is left out of the totals, and
// the start of the work day its time counts on.
type timerTime struct {
	Name     string
	Elapsed  time.Duration
	Color    string
	Excluded bool
	Day      time.Time
}

// onDay returns timers with no time on those whose time counts on another
// work day; they stay listed so colors and exclusions still apply.
func onDay(timers []timerTime, day time.Time) []timerTime {
	out := make([]timerTime, len(timers))
	for i, t := range timers {
		if !t.Day.Equal(day) {
			t.Elapsed = 0
		}
		out[i] = t
	}
	return out
}

// buildDaySummary merges timer time with today's logged time, keyed by name.
// Timer order comes first, then CSV-only names (renamed or removed timers) in
// order of first appearance, so each project's palette color stays stable
// through the day. A timer's chosen color overrides its position, and its
// exclusion covers its logged time too (any same-named timer's exclusion
// wins); CSV-only names always count. Projects with no time are omitted.
func buildDaySummary(timers []timerTime, tl todayLog) []projectTotal {
	byName := map[string]*projectTotal{}
	var ordered []*projectTotal
	get := func(name string) *projectTotal {
		if p, ok := byName[name]; ok {
			return p
		}
		p := &projectTotal{Name: name, Color: len(ordered)}
		byName[name] = p
		ordered = append(ordered, p)
		return p
	}
	for _, t := range timers {
		p := get(strings.TrimSpace(t.Name))
		if c := paletteIndex(t.Color); c >= 0 {
			p.Color = c
		}
		p.Excluded = p.Excluded || t.Excluded
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
		if p.Total() > 0 || p.Entries > 0 {
			out = append(out, *p)
		}
	}
	return out
}

// formatHM renders d as "07:08", truncated to whole minutes.
func formatHM(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	m := int(d / time.Minute)
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
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
		s += fmt.Sprintf(" · %s to go", formatHM(diff))
	default:
		s += fmt.Sprintf(" · %s over", formatHM(-diff))
	}
	if tl.unreadable {
		s += " · log unreadable"
	}
	if tl.dataless {
		s += " · some logs not downloaded"
	}
	return s
}

// rowText is a project row: "Acme  04:50  (5 entries, 00:23 unlogged)". The
// total includes the unlogged timer time; an excluded project says so last.
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
		parts = append(parts, formatHM(p.Unlogged)+" unlogged")
	}
	if p.Excluded {
		parts = append(parts, "not counted")
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
	// Written as !(in range) so NaN, which fails every comparison, is rejected.
	if err != nil || !(h > 0 && h <= 24) {
		return 0, fmt.Errorf("enter hours between 0 and 24, e.g. 8 or 7.5 (got %q)", s)
	}
	d := time.Duration(h * float64(time.Hour)).Round(time.Minute)
	if d < time.Minute {
		return 0, fmt.Errorf("enter at least one minute (got %q)", s)
	}
	return d, nil
}
