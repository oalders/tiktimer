package main

import (
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
