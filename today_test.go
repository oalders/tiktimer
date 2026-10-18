package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestReadTodayLogged(t *testing.T) {
	loc := time.FixedZone("EDT", -4*3600)
	now := time.Date(2026, 10, 14, 15, 0, 0, 0, loc)
	dir := t.TempDir()

	// Pre-tracked file: 5-column header followed by both 5- and 6-column
	// rows, which a default csv.Reader would reject.
	mixed := filepath.Join(dir, "iMac.csv")
	writeFile(t, mixed, "timestamp,name,duration,hours,note\n"+
		"2026-10-14T03:59:59-04:00,Acme,05:00:00,5.00,before 4am is yesterday\n"+
		"2026-10-14T04:00:00-04:00,Acme,01:00:00,1.00,4am counts\n"+
		"2026-10-14T09:00:00-04:00,Acme,01:30:00,1.50,\"note, with comma\nand newline\",01:29:10\n"+
		"2026-10-14T10:00:00-04:00,Globex,00:45:00,0.75,old-format row\n"+
		"not-a-time,Acme,01:00:00,1.00,bad\n"+
		"2026-10-14T11:00:00-04:00,Acme,garbage,1.00,bad duration\n")

	// Another machine's file, written in UTC: 07:30Z is 03:30 local, before
	// the 4 am day start; 14:00Z is 10:00 local today.
	other := filepath.Join(dir, "MacBook.csv")
	writeFile(t, other, "timestamp,name,duration,hours,note,tracked\n"+
		"2026-10-14T07:30:00Z,Initech,02:00:00,2.00,,02:00:00\n"+
		"2026-10-14T14:00:00Z, Globex ,00:15:00,0.25,,00:15:00\n"+
		"2026-10-15T04:00:00-04:00,Acme,03:00:00,3.00,tomorrow,03:00:00\n")

	tl := readTodayLogged([]string{mixed, other, filepath.Join(dir, "missing.csv")}, now)

	if tl.unreadable || tl.dataless {
		t.Errorf("unexpected flags: unreadable=%v dataless=%v", tl.unreadable, tl.dataless)
	}
	if got := tl.logged["Acme"]; got != 2*time.Hour+30*time.Minute {
		t.Errorf("Acme logged = %v, want 2h30m", got)
	}
	if got := tl.counts["Acme"]; got != 2 {
		t.Errorf("Acme count = %d, want 2", got)
	}
	if got := tl.logged["Globex"]; got != time.Hour {
		t.Errorf("Globex logged = %v, want 1h (name trimmed, both files)", got)
	}
	if _, ok := tl.logged["Initech"]; ok {
		t.Errorf("Initech is before the 4 am day start and should be excluded")
	}
	if strings.Join(tl.order, ",") != "Acme,Globex" {
		t.Errorf("order = %v, want [Acme Globex]", tl.order)
	}
}

func TestReadTodayLoggedUnreadable(t *testing.T) {
	dir := t.TempDir()
	// A directory exists but cannot be read as a CSV.
	tl := readTodayLogged([]string{dir}, time.Now())
	if !tl.unreadable {
		t.Errorf("expected unreadable for a path that is not a readable file")
	}
}

func TestReadTodayLoggedMissingIsZero(t *testing.T) {
	tl := readTodayLogged([]string{filepath.Join(t.TempDir(), "nope.csv")}, time.Now())
	if tl.unreadable || len(tl.logged) != 0 {
		t.Errorf("missing file should count as zero without a warning: %+v", tl)
	}
	tl = readTodayLogged(nil, time.Now())
	if tl.unreadable || len(tl.logged) != 0 {
		t.Errorf("no files should count as zero without a warning: %+v", tl)
	}
}

func TestDayBoundsStartsAt4am(t *testing.T) {
	loc := time.FixedZone("EDT", -4*3600)
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"afternoon", time.Date(2026, 10, 14, 15, 0, 0, 0, loc), time.Date(2026, 10, 14, 4, 0, 0, 0, loc)},
		{"exactly 4am", time.Date(2026, 10, 14, 4, 0, 0, 0, loc), time.Date(2026, 10, 14, 4, 0, 0, 0, loc)},
		{"late night belongs to previous day", time.Date(2026, 10, 15, 1, 30, 0, 0, loc), time.Date(2026, 10, 14, 4, 0, 0, 0, loc)},
		{"midnight belongs to previous day", time.Date(2026, 10, 15, 0, 0, 0, 0, loc), time.Date(2026, 10, 14, 4, 0, 0, 0, loc)},
		{"just before 4am", time.Date(2026, 10, 15, 3, 59, 59, 0, loc), time.Date(2026, 10, 14, 4, 0, 0, 0, loc)},
		{"new year rolls back a year", time.Date(2027, 1, 1, 2, 0, 0, 0, loc), time.Date(2026, 12, 31, 4, 0, 0, 0, loc)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, end := dayBounds(c.now)
			if !start.Equal(c.want) || !end.Equal(c.want.AddDate(0, 0, 1)) {
				t.Errorf("got [%v, %v), want start %v", start, end, c.want)
			}
		})
	}
}

func TestDayBoundsDST(t *testing.T) {
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	// Clocks fall back at 2 am on 2026-11-01, so at 3 am that morning the work
	// day is 4 am Oct 31 to 4 am Nov 1, which is 25 hours long.
	start, end := dayBounds(time.Date(2026, 11, 1, 3, 0, 0, 0, loc))
	if got := end.Sub(start); got != 25*time.Hour {
		t.Errorf("fall-back day length = %v, want 25h", got)
	}
	if !start.Equal(time.Date(2026, 10, 31, 4, 0, 0, 0, loc)) || !end.Equal(time.Date(2026, 11, 1, 4, 0, 0, 0, loc)) {
		t.Errorf("got [%v, %v), want 4 am Oct 31 to 4 am Nov 1", start, end)
	}
}

func TestMachineLogsIn(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"iMac.csv", "Olafs-MacBook-Pro.csv", "iMac 2.csv", "export (1).csv", "notes.txt"} {
		writeFile(t, filepath.Join(dir, n), "")
	}
	got := machineLogsIn(dir)
	var names []string
	for _, p := range got {
		names = append(names, filepath.Base(p))
	}
	if strings.Join(names, ",") != "Olafs-MacBook-Pro.csv,iMac.csv" {
		t.Errorf("got %v, want only the per-machine CSVs (no conflict copies or exports)", names)
	}
}

func TestReadTodayLoggedFlagsBadQuote(t *testing.T) {
	now := time.Date(2026, 10, 14, 15, 0, 0, 0, time.Local)
	p := filepath.Join(t.TempDir(), "iMac.csv")
	writeFile(t, p, "timestamp,name,duration,hours,note\n"+
		"2026-10-14T09:00:00"+now.Format("-07:00")+",Acme,01:00:00,1.00,ok\n"+
		"2026-10-14T10:00:00"+now.Format("-07:00")+",Acme,01:00:00,1.00,bad \"quote\n")
	tl := readTodayLogged([]string{p}, now)
	if !tl.unreadable {
		t.Errorf("a quoting error can swallow rows, so it should flag the log")
	}
	if tl.logged["Acme"] != time.Hour {
		t.Errorf("rows before the bad quote should still count, got %v", tl.logged["Acme"])
	}
}

func TestBuildDaySummary(t *testing.T) {
	timers := []timerTime{
		{Name: "Idle", Elapsed: 0},
		{Name: "Globex", Elapsed: 23 * time.Minute},
		{Name: "Acme", Elapsed: 0},
	}
	tl := todayLog{
		logged: map[string]time.Duration{"Acme": 2 * time.Hour, "Old Name": time.Hour, "Globex": time.Hour},
		counts: map[string]int{"Acme": 2, "Old Name": 1, "Globex": 3},
		order:  []string{"Old Name", "Acme", "Globex"},
	}
	got := buildDaySummary(timers, tl)
	// Color is the position among all timers and names, so the idle timer
	// still reserves palette slot 0 and the others keep their colors when it
	// starts.
	want := []projectTotal{
		{Name: "Globex", Logged: time.Hour, Unlogged: 23 * time.Minute, Entries: 3, Color: 1},
		{Name: "Acme", Logged: 2 * time.Hour, Entries: 2, Color: 2},
		{Name: "Old Name", Logged: time.Hour, Entries: 1, Color: 3},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestBuildDaySummaryTimerOnly(t *testing.T) {
	timers := []timerTime{{Name: "Acme", Elapsed: time.Minute}, {Name: "Blip", Elapsed: 59 * time.Second}}
	got := buildDaySummary(timers, readTodayLogged(nil, time.Now()))
	// Blip stays in the summary so its seconds count toward the total and the
	// bar, but it gets no row of its own.
	if len(got) != 2 || !got[0].ShowRow() || got[1].ShowRow() {
		t.Errorf("want Acme shown and Blip hidden, got %+v", got)
	}
}

func TestFormatHMAndCompact(t *testing.T) {
	if got := formatHM(7*time.Hour + 8*time.Minute + 59*time.Second); got != "7h 08m" {
		t.Errorf("formatHM = %q", got)
	}
	if got := formatHM(-time.Minute); got != "0h 00m" {
		t.Errorf("formatHM negative = %q", got)
	}
	for d, want := range map[time.Duration]string{
		8 * time.Hour:                   "8h",
		7*time.Hour + 30*time.Minute:    "7h 30m",
		52*time.Minute + 59*time.Second: "52m",
		30 * time.Second:                "0m",
	} {
		if got := formatCompact(d); got != want {
			t.Errorf("formatCompact(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestHeadlineText(t *testing.T) {
	target := 8 * time.Hour
	cases := []struct {
		name  string
		total time.Duration
		tl    todayLog
		want  string
	}{
		// 7h08m59s truncates to 7h08m, so 52m to go (never 51m).
		{"under", 7*time.Hour + 8*time.Minute + 59*time.Second, todayLog{}, "Today  7h 08m of 8h · 52m to go"},
		{"just under", 7*time.Hour + 59*time.Minute + 30*time.Second, todayLog{}, "Today  7h 59m of 8h · 1m to go"},
		{"exact", target, todayLog{}, "Today  8h 00m of 8h · target reached"},
		{"over", 9*time.Hour + 15*time.Minute, todayLog{}, "Today  9h 15m of 8h · 1h 15m over"},
		{"flags", 0, todayLog{unreadable: true, dataless: true}, "Today  0h 00m of 8h · 8h to go · log unreadable · some logs not downloaded"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := headlineText(c.total, target, c.tl); got != c.want {
				t.Errorf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestRowText(t *testing.T) {
	cases := []struct {
		p    projectTotal
		want string
	}{
		{projectTotal{Name: "Acme", Logged: 4*time.Hour + 27*time.Minute, Unlogged: 23 * time.Minute, Entries: 5}, "Acme  4h 50m  (5 entries, 23m unlogged)"},
		{projectTotal{Name: "Globex", Logged: 30 * time.Minute, Entries: 1}, "Globex  0h 30m  (1 entry)"},
		{projectTotal{Name: "New", Unlogged: 90 * time.Minute}, "New  1h 30m  (1h 30m unlogged)"},
		{projectTotal{Name: "Acme", Logged: time.Hour, Unlogged: 10 * time.Second, Entries: 2}, "Acme  1h 00m  (2 entries)"},
	}
	for _, c := range cases {
		if got := rowText(c.p); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

func TestParseTargetHours(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"8":     8 * time.Hour,
		" 7.5 ": 7*time.Hour + 30*time.Minute,
		"7,5":   7*time.Hour + 30*time.Minute,
		"24":    24 * time.Hour,
	} {
		got, err := parseTargetHours(in)
		if err != nil || got != want {
			t.Errorf("parseTargetHours(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "-1", "25", "eight", "nan", "NaN", "inf", "0.004"} {
		if _, err := parseTargetHours(in); err == nil {
			t.Errorf("parseTargetHours(%q) should fail", in)
		}
	}
}

func decodeDataURI(t *testing.T, uri string) ([]byte, image.Image) {
	t.Helper()
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(uri, prefix) {
		t.Fatalf("not a PNG data URI: %.40q", uri)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, prefix))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode with pHYs chunk: %v", err)
	}
	return raw, img
}

func TestRenderDayBarUnderTarget(t *testing.T) {
	projects := []projectTotal{
		{Name: "Acme", Logged: 2 * time.Hour, Unlogged: 2 * time.Hour},
	}
	raw, img := decodeDataURI(t, renderDayBar(projects, 8*time.Hour))
	if !bytes.Contains(raw, []byte("pHYs")) {
		t.Errorf("missing pHYs chunk")
	}
	if b := img.Bounds(); b.Dx() != barW || b.Dy() != barH {
		t.Errorf("size = %v, want %dx%d", b, barW, barH)
	}
	midY := (barTop + barBot) / 2
	span := barRight - barLeft
	at := func(frac float64) (r, g, b, a uint32) {
		return img.At(barLeft+int(frac*float64(span)), midY).RGBA()
	}
	// 0–25%: solid logged Acme (blue); 25–50%: translucent unlogged; rest: track.
	if r, g, b, a := at(0.10); a != 0xFFFF || b <= r || b <= g {
		t.Errorf("logged segment pixel = %x %x %x %x, want opaque blue", r, g, b, a)
	}
	if _, _, _, a := at(0.40); a == 0 || a == 0xFFFF {
		t.Errorf("unlogged segment alpha = %x, want translucent", a)
	}
	_, _, _, trackA := at(0.80)
	if want := uint32(trackColor.A) * 0x101; trackA != want {
		t.Errorf("track alpha = %x, want %x", trackA, want)
	}
	// Above the pill is transparent (no target tick when under target).
	if _, _, _, a := img.At(barW/2, 1).RGBA(); a != 0 {
		t.Errorf("expected transparent above bar, alpha %x", a)
	}
}

func TestRenderDayBarOverTargetDrawsTick(t *testing.T) {
	projects := []projectTotal{{Name: "Acme", Logged: 10 * time.Hour}}
	_, img := decodeDataURI(t, renderDayBar(projects, 8*time.Hour))
	span := barRight - barLeft
	tx := barLeft + int(0.8*float64(span))
	if _, _, _, a := img.At(tx, barTop-3).RGBA(); a != 0xFFFF {
		t.Errorf("expected target tick above the bar at x=%d", tx)
	}
}

func TestRenderSwatch(t *testing.T) {
	_, img := decodeDataURI(t, renderSwatch(1))
	if b := img.Bounds(); b.Dx() != swatchSize || b.Dy() != swatchSize {
		t.Errorf("size = %v", b)
	}
	r, g, b, a := img.At(swatchSize/2, swatchSize/2).RGBA()
	c := paletteColor(1)
	if a != 0xFFFF || r>>8 != uint32(c.R) || g>>8 != uint32(c.G) || b>>8 != uint32(c.B) {
		t.Errorf("center = %x %x %x %x, want %v", r, g, b, a, c)
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Errorf("corner should be transparent")
	}
}

func TestDailyTargetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	a := newApp()
	a.home = dir
	if got := a.dailyTargetValue(); got != defaultDailyTarget {
		t.Errorf("default = %v, want %v", got, defaultDailyTarget)
	}
	a.setDailyTarget(7*time.Hour + 30*time.Minute)
	a.saveWG.Wait()

	loaded := newApp()
	loaded.home = dir
	loaded.load()
	if got := loaded.dailyTargetValue(); got != 7*time.Hour+30*time.Minute {
		t.Errorf("loaded target = %v, want 7h30m", got)
	}
}

func TestTodaySnapshot(t *testing.T) {
	a := newTestApp(t, 2)
	a.timers[0].Accumulated = time.Hour
	a.timers[1].Accumulated = 10 * time.Minute
	a.active = 1
	a.lastTick = time.Now().Add(-time.Minute)

	// Stopped: the active timer reports exactly its accumulated time.
	timers, _, target := a.todaySnapshot()
	if len(timers) != 2 || timers[0].Elapsed != time.Hour || timers[1].Elapsed != 10*time.Minute {
		t.Fatalf("stopped: timers = %+v", timers)
	}
	if target != defaultDailyTarget {
		t.Errorf("target = %v", target)
	}

	// Running: only the active timer gains in-flight time.
	a.running = true
	timers, _, _ = a.todaySnapshot()
	if timers[0].Elapsed != time.Hour {
		t.Errorf("inactive timer changed: %v", timers[0].Elapsed)
	}
	if got := timers[1].Elapsed; got < 11*time.Minute || got >= 12*time.Minute {
		t.Errorf("running timer = %v, want 10m plus about a minute in flight", got)
	}
}

func TestLoadRejectsOutOfRangeTarget(t *testing.T) {
	for _, raw := range []string{`1e18`, `-3`, `25`} {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".tiktimer.json"), `{"timers":[],"active":-1,"dailyTargetHours":`+raw+`}`)
		a := newApp()
		a.home = dir
		a.load()
		if got := a.dailyTargetValue(); got != defaultDailyTarget {
			t.Errorf("dailyTargetHours %s: target = %v, want default", raw, got)
		}
	}
}

func TestReadTodayLoggedBefore4amCountsPreviousEvening(t *testing.T) {
	loc := time.FixedZone("EDT", -4*3600)
	now := time.Date(2026, 10, 16, 1, 30, 0, 0, loc)
	p := filepath.Join(t.TempDir(), "iMac.csv")
	writeFile(t, p, "timestamp,name,duration,hours,note,tracked\n"+
		"2026-10-15T03:59:00-04:00,Acme,01:00:00,1.00,before the day,\n"+
		"2026-10-15T23:00:00-04:00,Acme,02:00:00,2.00,evening,\n"+
		"2026-10-16T00:09:00-04:00,Acme,00:30:00,0.50,after midnight,\n")
	tl := readTodayLogged([]string{p}, now)
	if got := tl.logged["Acme"]; got != 150*time.Minute || tl.counts["Acme"] != 2 {
		t.Errorf("got %v in %d entries, want 2h30m in 2 (evening and after-midnight rows)", got, tl.counts["Acme"])
	}
}

func TestLogTimestampBackdatesLeftovers(t *testing.T) {
	loc := time.FixedZone("EDT", -4*3600)
	now := time.Date(2026, 10, 16, 7, 0, 0, 0, loc)
	lastNight := time.Date(2026, 10, 15, 23, 40, 0, 0, loc)
	afterMidnight := time.Date(2026, 10, 16, 1, 15, 0, 0, loc)
	thisMorning := time.Date(2026, 10, 16, 6, 30, 0, 0, loc)
	cases := []struct {
		name    string
		lastRun time.Time
		want    time.Time
	}{
		{"last night", lastNight, lastNight},
		{"after midnight is still yesterday", afterMidnight, afterMidnight},
		{"ran this morning", thisMorning, now},
		{"never recorded", time.Time{}, now},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := logTimestamp(c.lastRun, now, now, false); !got.Equal(c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestTodaySnapshotLeavesOutLeftovers(t *testing.T) {
	a := newTestApp(t, 3)
	yesterday := time.Now().AddDate(0, 0, -1).Add(-5 * time.Hour)
	a.timers[0].Accumulated, a.timers[0].LastRun = 2*time.Hour, yesterday
	a.timers[1].Accumulated, a.timers[1].LastRun = 30*time.Minute, time.Now()
	a.timers[2].Accumulated, a.timers[2].LastRun = time.Hour, yesterday
	a.active = 2
	a.running = true
	a.lastTick = time.Now()

	timers, _, _ := a.todaySnapshot()
	if len(timers) != 3 {
		t.Fatalf("timers = %+v, want all three kept for stable colors", timers)
	}
	if timers[0].Elapsed != 0 {
		t.Errorf("stopped leftover = %v, want 0", timers[0].Elapsed)
	}
	if timers[1].Elapsed != 30*time.Minute {
		t.Errorf("today's timer = %v, want 30m", timers[1].Elapsed)
	}
	if timers[2].Elapsed < time.Hour {
		t.Errorf("running timer = %v, want its full time, since it is running today", timers[2].Elapsed)
	}
}

func TestLastRunTracksAccrual(t *testing.T) {
	a := newTestApp(t, 1)
	a.active = 0
	a.running = true
	a.lastTick = time.Now().Add(-time.Minute)
	before := time.Now()
	a.flushActive()
	if a.timers[0].LastRun.Before(before) {
		t.Errorf("flushActive left LastRun at %v", a.timers[0].LastRun)
	}

	old := time.Now().AddDate(0, 0, -2)
	a.running = false
	a.timers[0].LastRun = old
	a.setElapsed(a.timers[0], 0)
	a.saveWG.Wait()
	if !a.timers[0].LastRun.Equal(old) {
		t.Errorf("lowering the time moved LastRun to %v", a.timers[0].LastRun)
	}
	a.setElapsed(a.timers[0], time.Hour)
	a.saveWG.Wait()
	if a.timers[0].LastRun.Equal(old) {
		t.Errorf("adding time by hand should count as worked now")
	}

	snap, _ := a.snapshotTimer(a.timers[0])
	if !snap.lastRun.Equal(a.timers[0].LastRun) {
		t.Errorf("stopped snapshot lastRun = %v, want %v", snap.lastRun, a.timers[0].LastRun)
	}
}

func TestLastRunRoundTrip(t *testing.T) {
	dir := t.TempDir()
	a := newApp()
	a.home = dir
	when := time.Date(2026, 10, 15, 23, 40, 0, 0, time.Local)
	a.timers = []*Timer{{Name: "Acme", Accumulated: time.Hour, LastRun: when}, {Name: "Fresh"}}
	a.save()

	raw, err := os.ReadFile(filepath.Join(dir, ".tiktimer.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved struct{ Timers []map[string]any }
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Timers) != 2 || saved.Timers[0]["lastRun"] == nil || saved.Timers[1]["lastRun"] != nil {
		t.Errorf("want lastRun saved only for the timer that ran:\n%s", raw)
	}
	loaded := newApp()
	loaded.home = dir
	loaded.load()
	if len(loaded.timers) != 2 || !loaded.timers[0].LastRun.Equal(when) || !loaded.timers[1].LastRun.IsZero() {
		t.Errorf("loaded timers = %+v", loaded.timers)
	}
}

func TestYesterdayTimestamp(t *testing.T) {
	loc := time.FixedZone("EDT", -4*3600)
	lastNight := time.Date(2026, 10, 15, 23, 40, 0, 0, loc)
	cases := []struct {
		name         string
		lastRun, now time.Time
		want         time.Time
	}{
		{"leftover keeps when it ran", lastNight, time.Date(2026, 10, 16, 9, 0, 0, 0, loc), lastNight},
		{"ran a few seconds this morning", time.Date(2026, 10, 16, 8, 59, 50, 0, loc), time.Date(2026, 10, 16, 9, 0, 0, 0, loc), time.Date(2026, 10, 15, 23, 59, 0, 0, loc)},
		{"never ran", time.Time{}, time.Date(2026, 10, 16, 9, 0, 0, 0, loc), time.Date(2026, 10, 15, 23, 59, 0, 0, loc)},
		{"before 4am the previous work day is two dates back", time.Date(2026, 10, 16, 1, 0, 0, 0, loc), time.Date(2026, 10, 16, 2, 0, 0, 0, loc), time.Date(2026, 10, 14, 23, 59, 0, 0, loc)},
		{"month boundary", time.Time{}, time.Date(2026, 11, 1, 9, 0, 0, 0, loc), time.Date(2026, 10, 31, 23, 59, 0, 0, loc)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := yesterdayTimestamp(c.lastRun, c.now)
			if !got.Equal(c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
			if !workedBeforeToday(got, c.now) {
				t.Errorf("%v is not before now's work day", got)
			}
		})
	}
}

func TestYesterdayHint(t *testing.T) {
	when := time.Date(2026, 10, 15, 23, 59, 0, 0, time.UTC)
	got := yesterdayHint(when)
	if !strings.Contains(got, yesterdayButton) || !strings.Contains(got, "Oct 15 23:59") {
		t.Errorf("hint %q should name the button and the date it gives", got)
	}
}

func TestLogTimestampJudgesDayWhenDialogOpened(t *testing.T) {
	loc := time.FixedZone("EDT", -4*3600)
	lastRun := time.Date(2026, 10, 16, 1, 0, 0, 0, loc) // part of Oct 15's work day
	opened := time.Date(2026, 10, 16, 3, 58, 0, 0, loc)
	now := time.Date(2026, 10, 16, 4, 2, 0, 0, loc) // dialog closed after 4 am
	if got := logTimestamp(lastRun, opened, now, false); !got.Equal(now) {
		t.Errorf("got %v; at %v the time was today's, so it should be dated now", got, opened)
	}
	if got := logTimestamp(lastRun, opened, now, true); !got.Equal(time.Date(2026, 10, 14, 23, 59, 0, 0, loc)) {
		t.Errorf("as yesterday: got %v, want the work day before the one open at %v", got, opened)
	}
	if got := logTimestamp(time.Date(2026, 10, 15, 20, 0, 0, 0, loc), now, now, true); !got.Equal(time.Date(2026, 10, 15, 20, 0, 0, 0, loc)) {
		t.Errorf("leftover with as-yesterday: got %v, want when it last ran", got)
	}
}

func TestTodayItemsAreEnabled(t *testing.T) {
	a := newTestApp(t, 1)
	a.logPath = filepath.Join(t.TempDir(), "log.csv") // keep off the real iCloud log
	a.timers[0].Accumulated = time.Hour               // enough for a project row

	items := a.todayItems()
	// Headline, bar and one project row.
	if len(items) != 3 {
		t.Fatalf("got %d items want 3: %+v", len(items), items)
	}
	// A nil Clicked would make AppKit grey the item out (#18).
	for _, it := range items {
		if it.Clicked == nil {
			t.Errorf("summary item %q has no action, so it renders dimmed", it.Text)
		}
	}
}
