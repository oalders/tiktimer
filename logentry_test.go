package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormatEntryFields(t *testing.T) {
	cases := []struct {
		name     string
		d        time.Duration
		duration string
		hours    string
	}{
		{"zero", 0, "00:00:00", "0.00"},
		{"sub-minute", 45 * time.Second, "00:00:45", "0.01"},
		{"sub-hour", 23*time.Minute + 45*time.Second, "00:23:45", "0.40"},
		{"multi-hour", time.Hour + 23*time.Minute + 45*time.Second, "01:23:45", "1.40"},
		{"three-digit-hours", 100*time.Hour + 5*time.Minute + 3*time.Second, "100:05:03", "100.08"},
		{"negative-clamped", -5 * time.Second, "00:00:00", "0.00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotDur, gotHours := formatEntryFields(c.d)
			if gotDur != c.duration {
				t.Errorf("duration: got %q want %q", gotDur, c.duration)
			}
			if gotHours != c.hours {
				t.Errorf("hours: got %q want %q", gotHours, c.hours)
			}
		})
	}
}

func TestResolveLogPath(t *testing.T) {
	if got := resolveLogPath("/tmp/custom.csv"); got != "/tmp/custom.csv" {
		t.Errorf("non-empty config: got %q want %q", got, "/tmp/custom.csv")
	}
	got := resolveLogPath("")
	want := defaultLogPath()
	if got != want {
		t.Errorf("empty config: got %q want %q", got, want)
	}
	// The default path is a per-machine file under a shared "tiktimer" folder.
	// Assert on structure rather than the exact machine name so the test is
	// host-independent (defaultLogPath shells out for LocalHostName).
	if filepath.Ext(want) != ".csv" {
		t.Errorf("default extension: got %q want .csv", filepath.Ext(want))
	}
	if base := filepath.Base(want); base == ".csv" || base == "" {
		t.Errorf("default filename missing machine name: got %q", base)
	}
	if dir := filepath.Base(filepath.Dir(want)); dir != "tiktimer" {
		t.Errorf("default subfolder: got %q want tiktimer", dir)
	}
	if filepath.Base(defaultLogDir()) != "com~apple~CloudDocs" {
		t.Errorf("default dir: got %q", filepath.Base(defaultLogDir()))
	}
}

func TestDeriveMachineName(t *testing.T) {
	tests := []struct {
		name          string
		localHostName string
		hostname      string
		want          string
	}{
		{"local host name wins", "iMac", "imac.lan", "iMac"},
		{"trailing newline trimmed", "iMac\n", "", "iMac"},
		{"internal hyphen preserved", "Olafs-MacBook-Pro", "", "Olafs-MacBook-Pro"},
		{"fall back to hostname", "", "imac.lan", "imac"},
		{"hostname domain stripped at first dot", "", "host.example.com", "host"},
		{"whitespace local falls through", "   ", "imac.lan", "imac"},
		{"punctuation collapses to dashes", "", "Olaf's iMac", "Olaf-s-iMac"},
		{"all-invalid falls through", "…", "imac", "imac"},
		{"dot-only falls through to unknown", ".", "..", "unknown"},
		{"both empty is unknown", "", "", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveMachineName(tt.localHostName, tt.hostname); got != tt.want {
				t.Errorf("deriveMachineName(%q, %q) = %q, want %q", tt.localHostName, tt.hostname, got, tt.want)
			}
		})
	}
}

func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return rows
}

func TestAppendLogRowCreatesHeaderThenAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.csv")

	if err := appendLogRow(path, "2026-07-23T14:30:00-04:00", "Acme", "01:23:45", "1.40", "first", "01:23:45"); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := appendLogRow(path, "2026-07-23T15:00:00-04:00", "Acme", "00:10:00", "0.17", "second", "00:09:12"); err != nil {
		t.Fatalf("second append: %v", err)
	}

	rows := readCSV(t, path)
	if len(rows) != 3 {
		t.Fatalf("want 3 rows (header + 2), got %d: %v", len(rows), rows)
	}
	wantHeader := []string{"timestamp", "name", "duration", "hours", "note", "tracked"}
	for i, h := range wantHeader {
		if rows[0][i] != h {
			t.Errorf("header col %d: got %q want %q", i, rows[0][i], h)
		}
	}
	if rows[1][4] != "first" || rows[2][4] != "second" {
		t.Errorf("data rows wrong: %v", rows)
	}
	if rows[2][5] != "00:09:12" {
		t.Errorf("tracked: got %q want %q", rows[2][5], "00:09:12")
	}
}

func TestAppendLogRowWritesHeaderForZeroByteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.csv")
	// Pre-create an empty file, simulating an iCloud placeholder.
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatalf("precreate: %v", err)
	}
	if err := appendLogRow(path, "2026-07-23T14:30:00-04:00", "Acme", "01:00:00", "1.00", "x", "00:57:00"); err != nil {
		t.Fatalf("append: %v", err)
	}
	rows := readCSV(t, path)
	if len(rows) != 2 || rows[0][0] != "timestamp" {
		t.Fatalf("expected header + 1 row, got %v", rows)
	}
}

func TestAppendLogRowEscapesNote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.csv")
	note := `fixed "login", again`
	if err := appendLogRow(path, "2026-07-23T14:30:00-04:00", "Acme", "01:00:00", "1.00", note, "01:00:00"); err != nil {
		t.Fatalf("append: %v", err)
	}
	rows := readCSV(t, path)
	if rows[1][4] != note {
		t.Errorf("note not round-tripped: got %q want %q", rows[1][4], note)
	}
}

func TestParseHMS(t *testing.T) {
	good := []struct {
		in   string
		want time.Duration
	}{
		{"00:00:00", 0},
		{"00:00:45", 45 * time.Second},
		{"01:23:45", time.Hour + 23*time.Minute + 45*time.Second},
		{"100:05:03", 100*time.Hour + 5*time.Minute + 3*time.Second},
		{"  01:00:00 ", time.Hour},
	}
	for _, c := range good {
		got, err := parseHMS(c.in)
		if err != nil {
			t.Errorf("parseHMS(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseHMS(%q) = %v, want %v", c.in, got, c.want)
		}
	}

	bad := []string{"", "1:2", "01:60:00", "01:00:60", "aa:00:00", "-1:00:00", "01:00:00:00"}
	for _, in := range bad {
		if _, err := parseHMS(in); err == nil {
			t.Errorf("parseHMS(%q): expected error, got nil", in)
		}
	}

	// Round-trips with formatEntryFields output.
	for _, d := range []time.Duration{0, 45 * time.Second, time.Hour + 23*time.Minute + 45*time.Second, 100*time.Hour + 5*time.Minute + 3*time.Second} {
		dur, _ := formatEntryFields(d)
		got, err := parseHMS(dur)
		if err != nil || got != d {
			t.Errorf("round-trip %v: format=%q parse=%v err=%v", d, dur, got, err)
		}
	}
}

func TestRoundUpTo(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
		inc  time.Duration
		want time.Duration
	}{
		{"zero stays zero", 0, 6 * time.Minute, 0},
		{"one second bumps to one increment", time.Second, 6 * time.Minute, 6 * time.Minute},
		{"partial increment rounds up", 13*time.Minute + 27*time.Second, 6 * time.Minute, 18 * time.Minute},
		{"exact multiple is unchanged", 12 * time.Minute, 6 * time.Minute, 12 * time.Minute},
		{"multi-hour", time.Hour + 31*time.Minute, 6 * time.Minute, time.Hour + 36*time.Minute},
		{"non-positive increment is a no-op", 13 * time.Minute, 0, 13 * time.Minute},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := roundUpTo(c.d, c.inc); got != c.want {
				t.Errorf("roundUpTo(%v, %v) = %v, want %v", c.d, c.inc, got, c.want)
			}
		})
	}
}

func TestRoundUpLabel(t *testing.T) {
	if got := roundUpLabel(6 * time.Minute); got != "Round Up to 6m" {
		t.Errorf("6m: got %q", got)
	}
	if got := roundUpLabel(15 * time.Minute); got != "Round Up to 15m" {
		t.Errorf("15m: got %q", got)
	}
}

func TestFormulaSafe(t *testing.T) {
	for in, want := range map[string]string{
		"=HYPERLINK(\"x\")": "'=HYPERLINK(\"x\")",
		"+1":                "'+1",
		"-fixed bug":        "'-fixed bug",
		"@SUM(A1)":          "'@SUM(A1)",
		"\tx":               "'\tx",
		"plain note":        "plain note",
		"":                  "",
		"'quoted":           "'quoted",
	} {
		got := formulaSafe(in)
		if got != want {
			t.Errorf("formulaSafe(%q) = %q, want %q", in, got, want)
		}
		if back := unformulaSafe(got); back != in {
			t.Errorf("unformulaSafe(%q) = %q, want %q", got, back, in)
		}
	}
}

func TestAppendLogRowGuardsFormulas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "iMac.csv")
	if err := appendLogRow(path, "2026-10-16T09:00:00-04:00", "-Acme", "01:00:00", "1.00", "=1+1", "01:00:00"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), ",'-Acme,") || !strings.Contains(string(raw), ",'=1+1,") {
		t.Errorf("name and note should be guarded:\n%s", raw)
	}
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	if tl := readTodayLogged([]string{path}, now); tl.logged["-Acme"] != time.Hour {
		t.Errorf("the summary should read the guarded name back as %q: %+v", "-Acme", tl.logged)
	}
}
