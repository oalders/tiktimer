package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

// sanitizeName trims surrounding whitespace, drops any DNS domain suffix
// (everything from the first dot, so "imac.lan" becomes "imac"), and reduces
// the rest to a filesystem-safe label: runs of anything outside [A-Za-z0-9_]
// collapse to a single "-", with leading and trailing dashes trimmed. Input
// that reduces to nothing usable (empty, whitespace, all-punctuation, "." or
// "..") returns "", letting the caller fall back to another source.
func sanitizeName(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.IndexByte(raw, '.'); i >= 0 {
		raw = raw[:i]
	}
	var b strings.Builder
	prevDash := false
	for _, r := range raw {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// deriveMachineName picks a filesystem-safe per-machine label, preferring the
// LocalHostName (macOS's DNS/Bonjour-safe host identifier) and falling back to
// the OS hostname, then to "unknown". Sanitization runs before the empty check
// so a value that is only whitespace or punctuation also falls through.
func deriveMachineName(localHostName, hostname string) string {
	if n := sanitizeName(localHostName); n != "" {
		return n
	}
	if n := sanitizeName(hostname); n != "" {
		return n
	}
	return "unknown"
}

var (
	machineNameOnce  sync.Once
	machineNameValue string
)

// machineName returns the sanitized per-machine label, computed once per
// process so the written file and any path shown to the user stay consistent
// even if the host is renamed mid-session. Note: renaming the machine's
// LocalHostName starts a fresh per-machine CSV rather than continuing the old
// one — expected, not data loss.
func machineName() string {
	machineNameOnce.Do(func() {
		var localHostName string
		if out, err := exec.Command("scutil", "--get", "LocalHostName").Output(); err == nil {
			localHostName = string(out)
		}
		hostname, _ := os.Hostname()
		machineNameValue = deriveMachineName(localHostName, hostname)
	})
	return machineNameValue
}

// defaultLogPath returns the default CSV location inside iCloud Drive: a
// per-machine file under a shared "tiktimer" folder (e.g. tiktimer/iMac.csv).
// Each machine writes only its own file, so iCloud never has to merge
// concurrent appends, while everything stays in one folder for recovery.
func defaultLogPath() string {
	return filepath.Join(defaultLogDir(), "tiktimer", machineName()+".csv")
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
