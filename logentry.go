package main

import (
	"fmt"
	"os"
	"path/filepath"
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
