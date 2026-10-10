// Package scheduler runs a job once per day at a fixed wall-clock time.
package scheduler

import (
	"context"
	"log/slog"
	"time"
)

// NextRun returns the next instant strictly after now at hour:minute
// wall-clock time in loc.
//
// The date is rebuilt with time.Date instead of adding 24h, so the run
// stays at the configured local time across DST changes. A time that
// does not exist on a spring-forward day (e.g. 02:30) is normalized by
// time.Date to the equivalent instant after the gap (03:30).
func NextRun(now time.Time, hour, minute int, loc *time.Location) time.Time {
	local := now.In(loc)
	y, m, d := local.Date()
	next := time.Date(y, m, d, hour, minute, 0, 0, loc)
	for !next.After(now) {
		d++
		next = time.Date(y, m, d, hour, minute, 0, 0, loc)
	}
	return next
}

// checkInterval bounds how long Run sleeps before re-reading the wall
// clock, so clock corrections (NTP sync on devices without a real-time
// clock), suspend/resume and similar jumps are noticed within a minute.
const checkInterval = time.Minute

// Run calls job every day at hour:minute in loc until ctx is cancelled.
// Jobs run sequentially; the next run is computed after each job finishes.
// A run missed during suspend is executed on wake-up.
func Run(ctx context.Context, hour, minute int, loc *time.Location, log *slog.Logger, job func(context.Context)) {
	schedule := func() time.Time {
		next := NextRun(time.Now(), hour, minute, loc)
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
