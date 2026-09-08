package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
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
	if filepath.Base(want) != "tiktimer-log.csv" {
		t.Errorf("default filename: got %q", filepath.Base(want))
	}
	if filepath.Base(defaultLogDir()) != "com~apple~CloudDocs" {
		t.Errorf("default dir: got %q", filepath.Base(defaultLogDir()))
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

	if err := appendLogRow(path, "2026-07-23T14:30:00-04:00", "Acme", "01:23:45", "1.40", "first"); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := appendLogRow(path, "2026-07-23T15:00:00-04:00", "Acme", "00:10:00", "0.17", "second"); err != nil {
		t.Fatalf("second append: %v", err)
	}

	rows := readCSV(t, path)
	if len(rows) != 3 {
		t.Fatalf("want 3 rows (header + 2), got %d: %v", len(rows), rows)
	}
	wantHeader := []string{"timestamp", "name", "duration", "hours", "note"}
	for i, h := range wantHeader {
		if rows[0][i] != h {
			t.Errorf("header col %d: got %q want %q", i, rows[0][i], h)
		}
	}
	if rows[1][4] != "first" || rows[2][4] != "second" {
		t.Errorf("data rows wrong: %v", rows)
	}
}

func TestAppendLogRowWritesHeaderForZeroByteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.csv")
	// Pre-create an empty file, simulating an iCloud placeholder.
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatalf("precreate: %v", err)
	}
	if err := appendLogRow(path, "2026-07-23T14:30:00-04:00", "Acme", "01:00:00", "1.00", "x"); err != nil {
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
	if err := appendLogRow(path, "2026-07-23T14:30:00-04:00", "Acme", "01:00:00", "1.00", note); err != nil {
		t.Fatalf("append: %v", err)
	}
	rows := readCSV(t, path)
	if rows[1][4] != note {
		t.Errorf("note not round-tripped: got %q want %q", rows[1][4], note)
	}
}
