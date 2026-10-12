// Package config loads and validates the runtime configuration from
// environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "time/tzdata" // embed zone data so TZ works in minimal images

	"github.com/mygrexit/payback-coupon-activator/internal/scheduler"
)

const (
	DefaultCookiesFile = "/data/cookies.json"
	DefaultCouponsURL  = "https://www.payback.de/coupons"
	DefaultTZ          = "Europe/Berlin"
	DefaultRunAt       = "04:00"
)

// Config is the validated runtime configuration.
type Config struct {
	CookiesFile     string
	NotifyURL       string // secret: may contain tokens
	NotifyOnSuccess bool
	Location        *time.Location
	RunAt           []scheduler.Clock // sorted, without duplicates, never empty
	RunOnStart      bool
	LogLevel        slog.Level
	ChromePath      string
	NoSandbox       bool
	UserAgent       string
	CouponsURL      string
}

// LogValue implements slog.LogValuer and never exposes secrets.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("cookies_file", c.CookiesFile),
		slog.Bool("notify_configured", c.NotifyURL != ""),
		slog.Bool("notify_on_success", c.NotifyOnSuccess),
		slog.String("tz", c.Location.String()),
		slog.String("run_at", formatClocks(c.RunAt)),
		slog.Bool("run_on_start", c.RunOnStart),
		slog.String("log_level", c.LogLevel.String()),
	)
}

// Load reads the configuration using getenv (os.Getenv in production).
// All problems are reported together. Error messages name variables,
// never their values.
func Load(getenv func(string) string) (Config, error) {
	var errs []error
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	parseBool := func(key string, def bool) bool {
		raw := get(key, strconv.FormatBool(def))
		v, err := strconv.ParseBool(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s must be true or false", key))
		}
		return v
	}

	cfg := Config{
		CookiesFile:     get("COOKIES_FILE", DefaultCookiesFile),
		NotifyURL:       get("NOTIFY_URL", ""),
		NotifyOnSuccess: parseBool("NOTIFY_ON_SUCCESS", false),
		RunOnStart:      parseBool("RUN_ON_START", true),
		ChromePath:      get("CHROME_PATH", ""),
		NoSandbox:       parseBool("CHROME_NO_SANDBOX", false),
		UserAgent:       get("BROWSER_USER_AGENT", ""),
		CouponsURL:      get("PAYBACK_COUPONS_URL", DefaultCouponsURL),
	}

	loc, err := time.LoadLocation(get("TZ", DefaultTZ))
	if err != nil {
		errs = append(errs, errors.New("TZ is not a valid IANA time zone (e.g. Europe/Berlin)"))
		loc = time.UTC
	}
	cfg.Location = loc

	cfg.RunAt, err = ParseRunAt(get("RUN_AT", DefaultRunAt))
	if err != nil {
		errs = append(errs, fmt.Errorf("RUN_AT: %w", err))
	}

	if err := cfg.LogLevel.UnmarshalText([]byte(get("LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, errors.New("LOG_LEVEL must be one of debug, info, warn, error"))
	}

	if u, err := url.Parse(cfg.CouponsURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		errs = append(errs, errors.New("PAYBACK_COUPONS_URL must be an absolute http(s) URL"))
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return cfg, nil
}

// ParseRunAt parses a comma-separated list of "HH:MM" times, e.g.
// "04:00,12:00,19:00". The result is sorted and free of duplicates.
func ParseRunAt(s string) ([]scheduler.Clock, error) {
	var times []scheduler.Clock
	for part := range strings.SplitSeq(s, ",") {
		h, m, err := ParseClock(strings.TrimSpace(part))
		if err != nil {
			return nil, errors.New("must be one or more HH:MM times in 24h format, separated by commas, e.g. 04:00 or 04:00,12:00,19:00")
		}
		times = append(times, scheduler.Clock{Hour: h, Minute: m})
	}
	slices.SortFunc(times, func(a, b scheduler.Clock) int {
		return (a.Hour*60 + a.Minute) - (b.Hour*60 + b.Minute)
	})
	return slices.Compact(times), nil
}

func formatClocks(times []scheduler.Clock) string {
	parts := make([]string, len(times))
	for i, c := range times {
		parts[i] = c.String()
	}
	return strings.Join(parts, ",")
}

// ParseClock parses "HH:MM" (24h).
func ParseClock(s string) (hour, minute int, err error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, errors.New("must be HH:MM in 24h format, e.g. 04:00")
	}
	return t.Hour(), t.Minute(), nil
}
