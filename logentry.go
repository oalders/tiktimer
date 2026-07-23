package main

import (
	"fmt"
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
