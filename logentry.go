package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// formatEntryFields renders a duration as a uniform HH:MM:SS string (hours,
// minutes, and seconds each zero-padded to at least two digits) and as decimal
// hours with two places. Negative durations are treated as zero.
func formatEntryFields(d time.Duration) (string, string) {
	if d < 0 {
		d = 0
	}
	total := int(d / time.Second)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	duration := fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	hours := fmt.Sprintf("%.2f", d.Hours())
	return duration, hours
}

// defaultLogDir returns the macOS iCloud Drive base directory.
func defaultLogDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Mobile Documents", "com~apple~CloudDocs")
}

// defaultLogPath returns the default CSV location inside iCloud Drive.
func defaultLogPath() string {
	return filepath.Join(defaultLogDir(), "tiktimer-log.csv")
}

// resolveLogPath returns configPath if set, otherwise the iCloud default.
func resolveLogPath(configPath string) string {
	if configPath != "" {
		return configPath
	}
	return defaultLogPath()
}

// parseHMS parses an "HH:MM:SS" duration string of the form produced by
// formatEntryFields (hours may be any number of digits; minutes and seconds
// must be 0-59). Surrounding whitespace is ignored. It returns an error rather
// than a best-effort value so a mistyped edit is rejected before anything is
// logged.
func parseHMS(s string) (time.Duration, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("expected HH:MM:SS, got %q", s)
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || h < 0 {
		return 0, fmt.Errorf("invalid hours in %q", s)
	}
	m, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("invalid minutes in %q", s)
	}
	sec, err := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err != nil || sec < 0 || sec > 59 {
		return 0, fmt.Errorf("invalid seconds in %q", s)
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second, nil
}

// appendLogRow appends one time-entry row to the CSV at path, creating the
// parent directory and a header row when needed. It returns the first error
// encountered while writing, flushing, or closing the file so callers can
// avoid resetting a timer whose time was never persisted.
func appendLogRow(path, timestamp, name, duration, hours, note string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	needHeader := true
	if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
		needHeader = false
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	w := csv.NewWriter(f)
	if needHeader {
		if err := w.Write([]string{"timestamp", "name", "duration", "hours", "note"}); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.Write([]string{timestamp, name, duration, hours, note}); err != nil {
		f.Close()
		return err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
