package scheduler

import (
	"testing"
	"time"
)

func TestNextRun(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	at := func(s string) time.Time {
		t.Helper()
		v, err := time.ParseInLocation("2006-01-02 15:04", s, berlin)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	tests := []struct {
		name      string
		now       time.Time
		hour, min int
		want      string // RFC3339 in Berlin
	}{
		{"before run time", at("2026-06-10 03:00"), 4, 0, "2026-06-10T04:00:00+02:00"},
		{"exactly at run time", at("2026-06-10 04:00"), 4, 0, "2026-06-11T04:00:00+02:00"},
		{"after run time", at("2026-06-10 04:01"), 4, 0, "2026-06-11T04:00:00+02:00"},
		{"month rollover", at("2026-06-30 05:00"), 4, 0, "2026-07-01T04:00:00+02:00"},
		{"year rollover", at("2026-12-31 05:00"), 4, 0, "2027-01-01T04:00:00+01:00"},
		// DST starts 2026-03-29 02:00 CET -> 03:00 CEST.
		{"spring forward keeps 04:00", at("2026-03-28 05:00"), 4, 0, "2026-03-29T04:00:00+02:00"},
		{"spring forward gap normalized", at("2026-03-28 05:00"), 2, 30, "2026-03-29T03:30:00+02:00"},
		// DST ends 2026-10-25 03:00 CEST -> 02:00 CET.
		{"fall back keeps 04:00", at("2026-10-24 05:00"), 4, 0, "2026-10-25T04:00:00+01:00"},
		{"day after fall back", at("2026-10-25 05:00"), 4, 0, "2026-10-26T04:00:00+01:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextRun(tt.now, []Clock{{tt.hour, tt.min}}, berlin)
			if s := got.In(berlin).Format(time.RFC3339); s != tt.want {
				t.Errorf("NextRun = %s, want %s", s, tt.want)
			}
			if !got.After(tt.now) {
				t.Errorf("NextRun %v not after now %v", got, tt.now)
			}
		})
	}
}

func TestNextRunFallBackDuration(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	// From 04:00 on the day before the fall-back to 04:00 after it is 25h.
	start := time.Date(2026, 10, 24, 4, 0, 0, 0, berlin)
	if d := NextRun(start, []Clock{{4, 0}}, berlin).Sub(start); d != 25*time.Hour {
		t.Errorf("duration = %v, want 25h", d)
	}
	// Spring forward: 23h.
	start = time.Date(2026, 3, 28, 4, 0, 0, 0, berlin)
	if d := NextRun(start, []Clock{{4, 0}}, berlin).Sub(start); d != 23*time.Hour {
		t.Errorf("duration = %v, want 23h", d)
	}
}

func TestNextRunMultipleTimes(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	times := []Clock{{4, 0}, {12, 0}, {19, 0}}

	tests := []struct {
		now  time.Time
		want string
	}{
		{time.Date(2026, 6, 10, 3, 0, 0, 0, berlin), "2026-06-10T04:00:00+02:00"},
		{time.Date(2026, 6, 10, 4, 0, 0, 0, berlin), "2026-06-10T12:00:00+02:00"},
		{time.Date(2026, 6, 10, 12, 30, 0, 0, berlin), "2026-06-10T19:00:00+02:00"},
		{time.Date(2026, 6, 10, 19, 0, 0, 0, berlin), "2026-06-11T04:00:00+02:00"},
	}
	for _, tt := range tests {
		if got := NextRun(tt.now, times, berlin).Format(time.RFC3339); got != tt.want {
			t.Errorf("NextRun(%s) = %s, want %s", tt.now.Format(time.RFC3339), got, tt.want)
		}
	}

	// The order of times does not matter.
	now := time.Date(2026, 6, 10, 13, 0, 0, 0, berlin)
	if got := NextRun(now, []Clock{{19, 0}, {4, 0}, {12, 0}}, berlin).Format(time.RFC3339); got != "2026-06-10T19:00:00+02:00" {
		t.Errorf("unsorted times: NextRun = %s", got)
	}

	if got := NextRun(now, nil, berlin); !got.IsZero() {
		t.Errorf("no times: NextRun = %v, want zero", got)
	}
}
