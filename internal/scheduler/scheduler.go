// Package scheduler runs a job every day at one or more fixed wall-clock
// times.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Clock is a wall-clock time of day.
type Clock struct {
	Hour, Minute int
}

func (c Clock) String() string { return fmt.Sprintf("%02d:%02d", c.Hour, c.Minute) }

// NextRun returns the earliest instant strictly after now at any of the
// given wall-clock times in loc. It returns the zero time if times is
// empty.
func NextRun(now time.Time, times []Clock, loc *time.Location) time.Time {
	var next time.Time
	for _, c := range times {
		if t := nextAt(now, c, loc); next.IsZero() || t.Before(next) {
			next = t
		}
	}
	return next
}

// nextAt returns the next instant strictly after now at c in loc.
//
// The date is rebuilt with time.Date instead of adding 24h, so the run
// stays at the configured local time across DST changes. A time that
// does not exist on a spring-forward day (e.g. 02:30) is normalized by
// time.Date to the equivalent instant after the gap (03:30).
func nextAt(now time.Time, c Clock, loc *time.Location) time.Time {
	local := now.In(loc)
	y, m, d := local.Date()
	next := time.Date(y, m, d, c.Hour, c.Minute, 0, 0, loc)
	for !next.After(now) {
		d++
		next = time.Date(y, m, d, c.Hour, c.Minute, 0, 0, loc)
	}
	return next
}

// checkInterval bounds how long Run sleeps before re-reading the wall
// clock, so clock corrections (NTP sync on devices without a real-time
// clock), suspend/resume and similar jumps are noticed within a minute.
const checkInterval = time.Minute

// Run calls job every day at each of times in loc until ctx is cancelled.
// times must not be empty. Jobs run sequentially; the next run is computed
// after each job finishes, so a time that passes while a job is still
// running is skipped. A run missed during suspend is executed on wake-up.
func Run(ctx context.Context, times []Clock, loc *time.Location, log *slog.Logger, job func(context.Context)) {
	schedule := func() time.Time {
		next := NextRun(time.Now(), times, loc)
		log.Info("Next run scheduled", "at", next.Format(time.RFC3339))
		return next
	}
	next := schedule()
	for {
		if wait := min(time.Until(next), checkInterval); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		} else if ctx.Err() != nil {
			return
		}

		now := time.Now()
		switch {
		case !now.Before(next):
			job(ctx)
			next = schedule()
		case next.Sub(now) > 25*time.Hour:
			// The clock jumped backwards; recompute from the new time.
			next = schedule()
		}
	}
}
