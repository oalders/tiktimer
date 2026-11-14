package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
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

// readDay reads paths for now's work day alone, with the file-level flags.
func readDay(paths []string, now time.Time) todayLog {
	start, end := dayBounds(now)
	days, flags := readLogged(paths, start, end)
	tl := days.day(start)
	tl.logFlags = flags
	return tl
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

	tl := readDay([]string{mixed, other, filepath.Join(dir, "missing.csv")}, now)

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
	tl := readDay([]string{dir}, time.Now())
	if !tl.unreadable {
		t.Errorf("expected unreadable for a path that is not a readable file")
	}
}

func TestReadTodayLoggedMissingIsZero(t *testing.T) {
	tl := readDay([]string{filepath.Join(t.TempDir(), "nope.csv")}, time.Now())
	if tl.unreadable || len(tl.logged) != 0 {
		t.Errorf("missing file should count as zero without a warning: %+v", tl)
	}
	tl = readDay(nil, time.Now())
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
	tl := readDay([]string{p}, now)
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

func TestBuildDaySummaryChosenColor(t *testing.T) {
	timers := []timerTime{
		{Name: "Acme", Elapsed: time.Hour},
		{Name: "Muevelo", Elapsed: time.Hour, Color: "Blue"},
		{Name: "Globex", Elapsed: time.Hour, Color: "no such colour"},
	}
	got := buildDaySummary(timers, todayLog{})
	// Muevelo's Blue (slot 0) wins over its slot 1; the others keep their positions.
	if len(got) != 3 || got[0].Color != 0 || got[1].Color != 0 || got[2].Color != 2 {
		t.Errorf("got %+v", got)
	}
}

func TestBuildDaySummaryExcluded(t *testing.T) {
	timers := []timerTime{{Name: "Acme", Elapsed: time.Hour}, {Name: "Blog", Elapsed: 30 * time.Minute, Excluded: true}}
	tl := todayLog{
		logged: map[string]time.Duration{"Blog": time.Hour, "Old Name": 2 * time.Hour},
		counts: map[string]int{"Blog": 1, "Old Name": 1},
		order:  []string{"Blog", "Old Name"},
	}
	got := buildDaySummary(timers, tl)
	if len(got) != 3 {
		t.Fatalf("got %d projects want 3: %+v", len(got), got)
	}
	if !got[1].Excluded || got[1].Total() != 90*time.Minute {
		t.Errorf("Blog = %+v, want excluded with its logged time, 1h30m", got[1])
	}
	if got[2].Excluded {
		t.Errorf("CSV-only Old Name is excluded; it has no setting and should count")
	}
	if counted, excluded := dayTotals(got); counted != 3*time.Hour || excluded != 90*time.Minute {
		t.Errorf("dayTotals = %v, %v want 3h, 1h30m", counted, excluded)
	}

	// Same-named timers merge into one project; either's exclusion wins.
	for _, firstExcluded := range []bool{true, false} {
		dup := buildDaySummary([]timerTime{
			{Name: "Blog", Elapsed: time.Hour, Excluded: firstExcluded},
			{Name: "Blog", Elapsed: time.Hour, Excluded: !firstExcluded},
		}, todayLog{})
		if len(dup) != 1 || !dup[0].Excluded {
			t.Errorf("first excluded %v: merged Blog = %+v, want one excluded project", firstExcluded, dup)
		}
	}
}

func TestBuildDaySummaryTimerOnly(t *testing.T) {
	timers := []timerTime{{Name: "Acme", Elapsed: time.Minute}, {Name: "Blip", Elapsed: 59 * time.Second}}
	got := buildDaySummary(timers, todayLog{})
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
		flags logFlags
		want  string
	}{
		// 7h08m59s truncates to 7h08m, so 52m to go (never 51m).
		{"under", 7*time.Hour + 8*time.Minute + 59*time.Second, logFlags{}, "Today  7h 08m of 8h · 52m to go"},
		{"just under", 7*time.Hour + 59*time.Minute + 30*time.Second, logFlags{}, "Today  7h 59m of 8h · 1m to go"},
		{"exact", target, logFlags{}, "Today  8h 00m of 8h · target reached"},
		{"over", 9*time.Hour + 15*time.Minute, logFlags{}, "Today  9h 15m of 8h · 1h 15m over"},
		{"flags", 0, logFlags{unreadable: true, dataless: true}, "Today  0h 00m of 8h · 8h to go · log unreadable · some logs not downloaded"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := headlineText("Today", c.total, target, "to go", c.flags); got != c.want {
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
		{projectTotal{Name: "Blog", Unlogged: 30 * time.Minute, Excluded: true}, "Blog  0h 30m  (30m unlogged, not counted)"},
		{projectTotal{Name: "Blog", Logged: 30 * time.Second, Entries: 1, Excluded: true}, "Blog  0h 00m  (1 entry, not counted)"},
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
		got, err := parseTargetHours(in, maxDailyHours)
		if err != nil || got != want {
			t.Errorf("parseTargetHours(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "-1", "25", "eight", "nan", "NaN", "inf", "0.004"} {
		if _, err := parseTargetHours(in, maxDailyHours); err == nil {
			t.Errorf("parseTargetHours(%q) should fail", in)
		}
	}
	if got, err := parseTargetHours("168", maxWeeklyHours); err != nil || got != 168*time.Hour {
		t.Errorf("weekly 168 = %v, %v", got, err)
	}
	if _, err := parseTargetHours("169", maxWeeklyHours); err == nil || !strings.Contains(err.Error(), "168") {
		t.Errorf("weekly 169: err %v, want one naming the 168 limit", err)
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

func TestRenderDayBarExcludedPastTarget(t *testing.T) {
	cases := []struct {
		name     string
		projects []projectTotal
		// Fractions of the span: the target tick, a pixel of plain counted
		// color, a track pixel (0 for none) and a pixel in the excluded segment.
		tick, counted, track, excluded float64
	}{
		// Scale 8h + 2h: Acme 0–20%, track to the target at 80%, Blog 80–100%.
		{"under target", []projectTotal{
			{Name: "Blog", Logged: 2 * time.Hour, Color: 1, Excluded: true},
			{Name: "Acme", Logged: 2 * time.Hour},
		}, 0.80, 0.10, 0.50, 0.90},
		// Scale 10h + 2h: Acme runs past the tick at 8/12 to 10/12, then Blog.
		{"over target", []projectTotal{
			{Name: "Acme", Logged: 10 * time.Hour},
			{Name: "Blog", Logged: 2 * time.Hour, Color: 1, Excluded: true},
		}, 8.0 / 12, 9.0 / 12, 0, 11.0 / 12},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, img := decodeDataURI(t, renderDayBar(c.projects, 8*time.Hour))
			midY := (barTop + barBot) / 2
			span := barRight - barLeft
			x := func(frac float64) int { return barLeft + int(frac*float64(span)) }
			at := func(x int) color.NRGBA { return color.NRGBAModel.Convert(img.At(x, midY)).(color.NRGBA) }
			// Eight pixels cover a stripe and a gap, so none may be hatched.
			for px := x(c.counted); px < x(c.counted)+8; px++ {
				if got := at(px); got != paletteColor(0) {
					t.Fatalf("counted pixel at x=%d = %v, want %v", px, got, paletteColor(0))
				}
			}
			if c.track > 0 && at(x(c.track)) != trackColor {
				t.Errorf("pixel at x=%d = %v, want track before the target", x(c.track), at(x(c.track)))
			}
			// Scan a few pixels for a colored stripe, then for a track-colored gap.
			stripe := x(c.excluded)
			for end := stripe + 8; hatched(stripe, midY); stripe++ {
				if stripe == end {
					t.Fatal("no colored stripe found in the excluded segment")
				}
			}
			if got := at(stripe); got != paletteColor(1) {
				t.Errorf("excluded stripe at x=%d = %v, want %v", stripe, got, paletteColor(1))
			}
			for end := stripe + 8; !hatched(stripe, midY); stripe++ {
				if stripe == end {
					t.Fatal("no hatch gap found in the excluded segment")
				}
			}
			if got := at(stripe); got != trackColor {
				t.Errorf("hatch gap at x=%d = %v, want track", stripe, got)
			}
			if _, _, _, a := img.At(x(c.tick), barTop-3).RGBA(); a != 0xFFFF {
				t.Errorf("expected target tick above the bar at x=%d", x(c.tick))
			}
		})
	}
}

func TestRenderDayBarNegativeLoggedDoesNotPanic(t *testing.T) {
	// Summing many bounded rows under one name can overflow Logged negative.
	projects := []projectTotal{
		{Name: "Acme", Logged: math.MinInt64 / 2, Unlogged: time.Hour},
	}
	decodeDataURI(t, renderDayBar(projects, 8*time.Hour))
}

func TestRenderSwatch(t *testing.T) {
	_, img := decodeDataURI(t, renderSwatch(1, false))
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

func TestRenderSwatchHatched(t *testing.T) {
	_, img := decodeDataURI(t, renderSwatch(1, true))
	at := func(x, y int) color.NRGBA { return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA) }
	// Both pixels are well inside the dot, so fully covered: the center is
	// off the hatching, 4px below it is on a stripe.
	mid := swatchSize / 2
	if !hatched(mid, mid+4) || hatched(mid, mid) {
		t.Fatal("test pixels no longer straddle a stripe")
	}
	if got := at(mid, mid+4); got != trackColor {
		t.Errorf("stripe pixel = %v, want trackColor %v", got, trackColor)
	}
	if got := at(mid, mid); got != paletteColor(1) {
		t.Errorf("non-stripe pixel = %v, want %v", got, paletteColor(1))
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
	a.timers[1].Color = "Teal"
	a.timers[1].ExcludeFromToday = true
	a.active = 1
	now := time.Now()
	a.lastTick = now.Add(-time.Minute)

	// Stopped: the active timer reports exactly its accumulated time.
	timers, _, target, _ := a.todaySnapshot(now)
	if len(timers) != 2 || timers[0].Elapsed != time.Hour || timers[1].Elapsed != 10*time.Minute {
		t.Fatalf("stopped: timers = %+v", timers)
	}
	if timers[1].Color != "Teal" || !timers[1].Excluded || timers[0].Excluded {
		t.Errorf("Color or Excluded not carried into snapshot: %+v", timers)
	}
	if target != defaultDailyTarget {
		t.Errorf("target = %v", target)
	}

	// Running: only the active timer gains in-flight time.
	a.running = true
	timers, _, _, _ = a.todaySnapshot(now)
	if timers[0].Elapsed != time.Hour {
		t.Errorf("inactive timer changed: %v", timers[0].Elapsed)
	}
	if got := timers[1].Elapsed; got != 11*time.Minute {
		t.Errorf("running timer = %v, want 10m plus the minute in flight up to now", got)
	}
}

func TestLoadRejectsOutOfRangeTarget(t *testing.T) {
	for _, raw := range []string{`1e18`, `-3`, `25`} {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".tiktimer.json"), `{"timers":[],"active":-1,"dailyTargetHours":`+raw+`,"weeklyTargetHours":`+raw+`0}`)
		a := newApp()
		a.home = dir
		a.load()
		if got := a.dailyTargetValue(); got != defaultDailyTarget {
			t.Errorf("dailyTargetHours %s: target = %v, want default", raw, got)
		}
		if a.weeklyTarget != 0 {
			t.Errorf("weeklyTargetHours %s0: target = %v, want default", raw, a.weeklyTarget)
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
	tl := readDay([]string{p}, now)
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

func TestTodaySnapshotDatesLeftoversToLastRunDay(t *testing.T) {
	a := newTestApp(t, 3)
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1).Add(-5 * time.Hour)
	a.timers[0].Accumulated, a.timers[0].LastRun = 2*time.Hour, yesterday
	a.timers[1].Accumulated, a.timers[1].LastRun = 30*time.Minute, now
	a.timers[2].Accumulated, a.timers[2].LastRun = time.Hour, yesterday
	a.active = 2
	a.running = true
	a.lastTick = now

	timers, _, _, _ := a.todaySnapshot(now)
	if len(timers) != 3 {
		t.Fatalf("timers = %+v, want all three kept for stable colors", timers)
	}
	today, _ := dayBounds(now)
	if lastDay, _ := dayBounds(yesterday); timers[0].Elapsed != 2*time.Hour || !timers[0].Day.Equal(lastDay) {
		t.Errorf("stopped leftover = %v on %v, want 2h on its LastRun day %v", timers[0].Elapsed, timers[0].Day, lastDay)
	}
	if got := onDay(timers, today); got[0].Elapsed != 0 || got[1].Elapsed != 30*time.Minute {
		t.Errorf("Today's timers = %+v, want the leftover zeroed and today's 30m kept", got)
	}
	if !timers[2].Day.Equal(today) || timers[2].Elapsed < time.Hour {
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

	items := a.todayItems(time.Now())
	yesterday, week := items[len(items)-2], items[len(items)-1]
	if !strings.HasPrefix(yesterday.Text, "Yesterday") || !strings.HasPrefix(week.Text, "This Week") {
		t.Fatalf("last two items %q, %q want Yesterday then This Week", yesterday.Text, week.Text)
	}
	if week.Children == nil {
		t.Fatalf("This Week %q has no per-day submenu", week.Text)
	}
	// A nil Clicked would make AppKit grey the item out (#18). This Week has a
	// submenu instead, like the timer rows; submenu parents stay enabled.
	for _, it := range slices.Concat(items[:len(items)-1], week.Children()) {
		if it.Clicked == nil {
			t.Errorf("summary item %q has no action, so it renders dimmed", it.Text)
		}
	}
}

func TestTodayItemsHeadlineLeavesOutExcluded(t *testing.T) {
	a := newTestApp(t, 2)
	a.logPath = filepath.Join(t.TempDir(), "log.csv") // keep off the real iCloud log
	a.timers[0].Name, a.timers[1].Name = "Acme", "Blog"
	a.timers[0].Accumulated = time.Hour
	a.timers[1].Accumulated = 2 * time.Hour
	a.timers[1].ExcludeFromToday = true

	now := time.Now()
	items := a.todayItems(now)
	// Headline and bar, then the project rows, then Yesterday and This Week.
	if rows := items[2 : len(items)-2]; len(rows) != 2 {
		t.Fatalf("got project rows %+v want Acme and Blog", rows)
	}
	if want := headlineText("Today", time.Hour, a.dailyTargetValue(), "to go", logFlags{}); items[0].Text != want {
		t.Errorf("headline %q want %q", items[0].Text, want)
	}
	if !strings.Contains(items[3].Text, "not counted") {
		t.Errorf("excluded row %q does not say \"not counted\"", items[3].Text)
	}
	// The bar and legend dots are drawn from the same summary; each dot is
	// hatched only for the excluded project.
	timers, _, target, _ := a.todaySnapshot(now)
	projects := buildDaySummary(timers, todayLog{})
	if items[1].Image != renderDayBar(projects, target) {
		t.Error("bar is not drawn from the summary's projects")
	}
	if items[2].Image != renderSwatch(projects[0].Color, false) {
		t.Error("counted row's swatch is not the plain dot")
	}
	if items[3].Image != renderSwatch(projects[1].Color, true) {
		t.Error("excluded row's swatch is not hatched")
	}
}

func TestWeekBoundsDST(t *testing.T) {
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	// Clocks fall back on Sunday 2026-11-01. At 2 am Monday it is still
	// Sunday's work day, so the week is the one starting Monday Oct 26.
	cases := []struct {
		name     string
		now      time.Time
		start    time.Time
		daysLeft int
	}{
		{"Monday before 4am", time.Date(2026, 11, 2, 2, 0, 0, 0, loc), time.Date(2026, 10, 26, 4, 0, 0, 0, loc), 1},
		{"Monday after 4am", time.Date(2026, 11, 2, 5, 0, 0, 0, loc), time.Date(2026, 11, 2, 4, 0, 0, 0, loc), 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, daysLeft := weekBounds(c.now)
			if !start.Equal(c.start) || daysLeft != c.daysLeft {
				t.Errorf("got %v with %d days left, want %v with %d", start, daysLeft, c.start, c.daysLeft)
			}
		})
	}
}

// summaryLog writes rows dated by the given local times (one hour of Acme
// each) and returns an App reading only that log.
func summaryLog(t *testing.T, rows ...time.Time) *App {
	t.Helper()
	a := newTestApp(t, 1)
	a.logPath = filepath.Join(t.TempDir(), "log.csv")
	csv := "timestamp,name,duration,hours,note,tracked\n"
	for _, r := range rows {
		csv += r.Format(time.RFC3339) + ",Acme,01:00:00,1.00,,\n"
	}
	writeFile(t, a.logPath, csv)
	return a
}

func TestTodayItemsMondayYesterdayIsPreviousWeek(t *testing.T) {
	loc := time.FixedZone("EST", -5*3600)
	sat := time.Date(2026, 10, 31, 12, 0, 0, 0, loc)
	sun := time.Date(2026, 11, 1, 12, 0, 0, 0, loc)
	a := summaryLog(t, sat, sun, sun)
	cases := []struct {
		name            string
		now             time.Time
		yesterday, week time.Duration
		daysLeft        int
	}{
		{"before 4am yesterday is Saturday", time.Date(2026, 11, 2, 2, 0, 0, 0, loc), time.Hour, 3 * time.Hour, 1},
		{"after 4am yesterday is Sunday", time.Date(2026, 11, 2, 5, 0, 0, 0, loc), 2 * time.Hour, 0, 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			items := a.todayItems(c.now)
			n := len(items)
			if want := headlineText("Yesterday", c.yesterday, 8*time.Hour, "short", logFlags{}); items[n-2].Text != want {
				t.Errorf("got %q want %q", items[n-2].Text, want)
			}
			if want := weekText(c.week, 40*time.Hour, c.daysLeft); items[n-1].Text != want {
				t.Errorf("got %q want %q", items[n-1].Text, want)
			}
		})
	}
}

func TestReadLoggedBucketsByWorkDay(t *testing.T) {
	loc := time.FixedZone("EST", -5*3600)
	mon := time.Date(2026, 11, 9, 4, 0, 0, 0, loc)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "iMac.csv"), "timestamp,name,duration,hours,note,tracked\n"+
		"2026-11-09T03:59:00-05:00,Acme,04:00:00,4.00,before the window,\n"+
		"2026-11-09T10:00:00-05:00,Globex,02:00:00,2.00,,\n"+
		"2026-11-11T01:00:00-05:00,Globex,00:30:00,0.50,late Tuesday,\n"+
		"2026-11-11T09:00:00-05:00,Acme,01:00:00,1.00,,\n")
	writeFile(t, filepath.Join(dir, "MacBook.csv"), "timestamp,name,duration,hours,note,tracked\n"+
		"2026-11-09T15:00:00Z,Acme,01:15:00,1.25,,\n"+
		"2026-11-11T14:00:00Z,Acme,00:45:00,0.75,,\n"+
		"2026-11-12T04:00:00-05:00,Acme,08:00:00,8.00,at the window's end,\n")
	days, _ := readLogged(machineLogsIn(dir), mon, mon.AddDate(0, 0, 3))
	if len(days) != 3 {
		t.Errorf("got %d days want Mon to Wed only; rows outside [start, end) must be dropped", len(days))
	}
	monLog, tueLog, wedLog := days.day(mon), days.day(mon.AddDate(0, 0, 1)), days.day(mon.AddDate(0, 0, 2))
	if monLog.logged["Globex"] != 2*time.Hour || monLog.logged["Acme"] != 75*time.Minute {
		t.Errorf("Monday = %v", monLog.logged)
	}
	if tueLog.logged["Globex"] != 30*time.Minute {
		t.Errorf("Tuesday = %v, want the 1 am row", tueLog.logged)
	}
	// Today keeps its own order, so earlier rows cannot shift its colors.
	if wedLog.logged["Acme"] != 105*time.Minute || strings.Join(wedLog.order, ",") != "Acme" {
		t.Errorf("Wednesday = %v in order %v, want Acme alone", wedLog.logged, wedLog.order)
	}
}

func TestTodayItemsWeekRowsSumToWeek(t *testing.T) {
	loc := time.FixedZone("EST", -5*3600)
	mon := time.Date(2026, 11, 9, 10, 0, 0, 0, loc)
	a := summaryLog(t, mon, mon, mon.AddDate(0, 0, 2))
	items := a.todayItems(time.Date(2026, 11, 11, 15, 0, 0, 0, loc)) // Wednesday
	week := items[len(items)-1]
	if want := weekText(3*time.Hour, 40*time.Hour, 5); week.Text != want {
		t.Errorf("week %q want %q", week.Text, want)
	}
	var texts []string
	for _, it := range week.Children() {
		texts = append(texts, it.Text)
	}
	if got, want := strings.Join(texts, "|"), "Mon  2h 00m|Tue  0h 00m|Wed  1h 00m"; got != want {
		t.Errorf("week rows %q want %q", got, want)
	}
}

func TestWeekText(t *testing.T) {
	target := 40 * time.Hour
	cases := []struct {
		name     string
		total    time.Duration
		daysLeft int
		want     string
	}{
		{"Monday", 0, 7, "This Week  0h 00m of 40h · 40h to go · 5h 43m/day left"},
		{"per day rounds up a minute", 23*time.Hour + 10*time.Minute + 59*time.Second, 3, "This Week  23h 10m of 40h · 16h 50m to go · 5h 37m/day left"},
		{"Sunday", 35 * time.Hour, 1, "This Week  35h 00m of 40h · 5h to go · 5h/day left"},
		{"met", target, 2, "This Week  40h 00m of 40h · target reached"},
		{"over", 42*time.Hour + 30*time.Minute, 2, "This Week  42h 30m of 40h · 2h 30m over"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := weekText(c.total, target, c.daysLeft); got != c.want {
				t.Errorf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestTodayItemsTimerTimeOnLastRunDay(t *testing.T) {
	loc := time.FixedZone("EST", -5*3600)
	cases := []struct {
		name                         string
		now                          time.Time
		today, yesterday, week, rows string
	}{
		{
			"Wednesday", time.Date(2026, 11, 11, 15, 0, 0, 0, loc),
			"Today  1h 00m of 8h · 7h to go",
			"Yesterday  2h 00m of 8h · 6h short",
			"This Week  3h 00m of 40h · 37h to go · 7h 24m/day left",
			"Mon  0h 00m|Tue  2h 00m|Wed  1h 00m",
		},
		{
			"Monday after 4am", time.Date(2026, 11, 16, 9, 0, 0, 0, loc),
			"Today  1h 00m of 8h · 7h to go",
			"Yesterday  2h 00m of 8h · 6h short",
			"This Week  1h 00m of 40h · 39h to go · 5h 35m/day left",
			"Mon  1h 00m",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			yesterday := c.now.AddDate(0, 0, -1).Add(-time.Hour)
			a := newTestApp(t, 5)
			a.logPath = filepath.Join(t.TempDir(), "log.csv") // keep off the real iCloud log
			a.timers[0].Name, a.timers[1].Name, a.timers[2].Name, a.timers[3].Name, a.timers[4].Name = "Acme", "Old", "Blog", "Side", "Live"
			a.timers[0].Accumulated, a.timers[0].LastRun = 2*time.Hour, yesterday
			a.timers[1].Accumulated, a.timers[1].LastRun = 3*time.Hour, c.now.AddDate(0, 0, -8) // before the window
			a.timers[2].Accumulated, a.timers[2].LastRun = 4*time.Hour, yesterday
			a.timers[2].ExcludeFromToday = true
			// Excluded, left over from Monday Nov 9: in the Wednesday week but not counted.
			a.timers[3].Accumulated, a.timers[3].LastRun = 5*time.Hour, time.Date(2026, 11, 9, 10, 0, 0, 0, loc)
			a.timers[3].ExcludeFromToday = true
			// Running: 30m stored plus 30m in flight, all on today.
			a.timers[4].Accumulated, a.timers[4].LastRun = 30*time.Minute, yesterday
			a.active, a.running, a.lastTick = 4, true, c.now.Add(-30*time.Minute)

			items := a.todayItems(c.now)
			n := len(items)
			if items[0].Text != c.today {
				t.Errorf("Today %q want %q", items[0].Text, c.today)
			}
			if items[n-2].Text != c.yesterday {
				t.Errorf("Yesterday %q want %q, without the excluded or out-of-window time", items[n-2].Text, c.yesterday)
			}
			if items[n-1].Text != c.week {
				t.Errorf("This Week %q want %q", items[n-1].Text, c.week)
			}
			var texts []string
			for _, it := range items[n-1].Children() {
				texts = append(texts, it.Text)
			}
			if got := strings.Join(texts, "|"); got != c.rows {
				t.Errorf("week rows %q want %q", got, c.rows)
			}
		})
	}
}

func TestTodayItemsFlagsUnreadableOnTodayOnly(t *testing.T) {
	a := newTestApp(t, 1)
	a.logPath = filepath.Join(t.TempDir(), "log.csv")
	writeFile(t, a.logPath, "timestamp,name,duration,hours,note,tracked\n"+
		"2026-11-11T09:00:00-05:00,Acme,01:00:00,1.00,ok,\n"+
		"2026-11-11T10:00:00-05:00,Acme,01:00:00,1.00,bad \"quote,\n")
	items := a.todayItems(time.Date(2026, 11, 11, 15, 0, 0, 0, time.FixedZone("EST", -5*3600)))
	if want := "Today  1h 00m of 8h · 7h to go · log unreadable"; items[0].Text != want {
		t.Errorf("Today %q want %q", items[0].Text, want)
	}
	for _, it := range items[len(items)-2:] {
		if strings.Contains(it.Text, "unreadable") {
			t.Errorf("%q carries the warning; it belongs on the Today headline only", it.Text)
		}
	}
}

func TestTodayItemsWeekAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	// Clocks fall back at 2 am Sunday Nov 1; at 3 am Monday it is still
	// Sunday's work day. The 30s row and 30s timer fall on different days, so
	// each day truncates them away and the week line must too.
	a := newTestApp(t, 1)
	a.logPath = filepath.Join(t.TempDir(), "log.csv")
	csv := "timestamp,name,duration,hours,note,tracked\n"
	for _, r := range []struct {
		when time.Time
		dur  string
	}{
		{time.Date(2026, 10, 26, 10, 0, 0, 0, loc), "01:00:00"},
		{time.Date(2026, 10, 31, 23, 0, 0, 0, loc), "00:00:30"},
		{time.Date(2026, 11, 1, 10, 0, 0, 0, loc), "01:00:00"},
		{time.Date(2026, 11, 2, 1, 30, 0, 0, loc), "01:00:00"},
	} {
		csv += r.when.Format(time.RFC3339) + ",Acme," + r.dur + ",,,\n"
	}
	writeFile(t, a.logPath, csv)
	a.timers[0].Accumulated, a.timers[0].LastRun = 30*time.Second, time.Date(2026, 11, 1, 12, 0, 0, 0, loc)

	items := a.todayItems(time.Date(2026, 11, 2, 3, 0, 0, 0, loc))
	week := items[len(items)-1]
	if want := "This Week  3h 00m of 40h · 37h to go · 37h/day left"; week.Text != want {
		t.Errorf("week %q want %q", week.Text, want)
	}
	var texts []string
	for _, it := range week.Children() {
		texts = append(texts, it.Text)
	}
	if got, want := strings.Join(texts, "|"), "Mon  1h 00m|Tue  0h 00m|Wed  0h 00m|Thu  0h 00m|Fri  0h 00m|Sat  0h 00m|Sun  2h 00m"; got != want {
		t.Errorf("week rows %q want %q", got, want)
	}
}

func TestWeeklyTarget(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, 1)
	a.home = dir
	a.logPath = filepath.Join(dir, "log.csv") // keep off the real iCloud log
	weekly := func() string {
		items := a.menuItems()
		return items[len(items)-1].Children()[2].Text
	}
	if _, _, _, got := a.todaySnapshot(time.Now()); got != 40*time.Hour || weekly() != "Weekly Target (40h, auto)…" {
		t.Errorf("default = %v, %q want 40h auto", got, weekly())
	}
	a.setDailyTarget(7 * time.Hour)
	if _, _, _, got := a.todaySnapshot(time.Now()); got != 35*time.Hour {
		t.Errorf("default after daily 7h = %v, want 35h", got)
	}
	a.setWeeklyTarget(30 * time.Hour)
	a.saveWG.Wait()
	if weekly() != "Weekly Target (30h)…" {
		t.Errorf("set label %q", weekly())
	}
	loaded := newApp()
	loaded.home = dir
	loaded.load()
	if _, _, _, got := loaded.todaySnapshot(time.Now()); got != 30*time.Hour {
		t.Errorf("loaded = %v, want 30h", got)
	}
	a.setWeeklyTarget(0)
	if _, _, _, got := a.todaySnapshot(time.Now()); got != 35*time.Hour {
		t.Errorf("after reset = %v, want the daily target × 5", got)
	}
	a.saveWG.Wait()
}
