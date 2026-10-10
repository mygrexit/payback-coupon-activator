package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func cookieFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cookies.json")
	if err := os.WriteFile(p, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(envFrom(map[string]string{"COOKIES_FILE": cookieFile(t)}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Location.String() != "Europe/Berlin" || cfg.RunHour != 4 || cfg.RunMinute != 0 {
		t.Errorf("unexpected schedule defaults: %v %d:%d", cfg.Location, cfg.RunHour, cfg.RunMinute)
	}
	if !cfg.RunOnStart || cfg.NotifyOnSuccess || cfg.LogLevel != slog.LevelInfo {
		t.Errorf("unexpected flag defaults: %+v", cfg)
	}
	if cfg.CouponsURL != DefaultCouponsURL {
		t.Errorf("CouponsURL = %q", cfg.CouponsURL)
	}
}

func TestLoadCustom(t *testing.T) {
	cfg, err := Load(envFrom(map[string]string{
		"COOKIES_FILE":      cookieFile(t),
		"TZ":                "UTC",
		"RUN_AT":            "23:45",
		"RUN_ON_START":      "false",
		"NOTIFY_ON_SUCCESS": "true",
		"LOG_LEVEL":         "debug",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RunHour != 23 || cfg.RunMinute != 45 || cfg.RunOnStart || !cfg.NotifyOnSuccess || cfg.LogLevel != slog.LevelDebug {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

func TestLoadReportsAllErrors(t *testing.T) {
	_, err := Load(envFrom(map[string]string{
		"TZ":           "Mars/Olympus",
		"RUN_AT":       "25:00",
		"RUN_ON_START": "maybe",
		"LOG_LEVEL":    "loud",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"TZ", "RUN_AT", "RUN_ON_START", "LOG_LEVEL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}

func TestErrorsAndLogValueDoNotLeakSecrets(t *testing.T) {
	const secret = "telegram://123456:SUPERSECRET@telegram?chats=1"
	_, err := Load(envFrom(map[string]string{"NOTIFY_URL": secret, "RUN_AT": "x"}))
	if err == nil || strings.Contains(err.Error(), "SUPERSECRET") {
		t.Fatalf("error leaks secret or is nil: %v", err)
	}

	cfg, err := Load(envFrom(map[string]string{"COOKIES_FILE": cookieFile(t), "NOTIFY_URL": secret}))
	if err != nil {
		t.Fatal(err)
	}
	if s := cfg.LogValue().String(); strings.Contains(s, "SUPERSECRET") {
		t.Errorf("LogValue leaks secret: %s", s)
	}
}

func TestParseClock(t *testing.T) {
	for in, ok := range map[string]bool{"00:00": true, "4:00": true, "04:00": true, "23:59": true, "24:00": false, "12:60": false, "": false} {
		_, _, err := ParseClock(in)
		if (err == nil) != ok {
			t.Errorf("ParseClock(%q) err=%v, want ok=%v", in, err, ok)
		}
	}
}
