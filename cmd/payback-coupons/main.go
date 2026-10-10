// Command payback-coupons activates all pending Payback coupons once per
// day using an exported payback.de browser session.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mygrexit/payback-coupon-activator/internal/activator"
	"github.com/mygrexit/payback-coupon-activator/internal/config"
	"github.com/mygrexit/payback-coupon-activator/internal/notify"
	"github.com/mygrexit/payback-coupon-activator/internal/scheduler"
)

// Set via -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	os.Exit(run())
}

func run() int {
	once := flag.Bool("once", false, "run one activation cycle and exit (exit code 1 on problems)")
	showVersion := flag.Bool("version", false, "print version and exit")
	testNotify := flag.Bool("test-notify", false, "send a test notification and exit")
	debugSnapshot := flag.Bool("debug-snapshot", false, "save a screenshot of the coupon page next to the cookie file (contains personal data)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("payback-coupons %s (%s)\n", version, commit)
		return 0
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Error(err.Error())
		return 2
	}
	log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	notifier, err := notify.New(cfg.NotifyURL)
	if err != nil {
		log.Error("Invalid NOTIFY_URL", "error", err)
		return 2
	}
	if *testNotify {
		return sendTestNotification(notifier, log)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := activator.Options{
		CouponsURL:  cfg.CouponsURL,
		CookiesFile: cfg.CookiesFile,
		ChromePath:  cfg.ChromePath,
		UserAgent:   cfg.UserAgent,
		NoSandbox:   cfg.NoSandbox,
		Logger:      log,
	}
	if *debugSnapshot {
		opts.DebugScreenshot = filepath.Join(filepath.Dir(cfg.CookiesFile), "debug.png")
	}
	job := func(ctx context.Context) bool { return activate(ctx, opts, cfg, notifier, log) }

	if *once {
		if job(ctx) {
			return 0
		}
		return 1
	}

	log.Info("payback-coupons started", "version", version, "config", cfg)
	if _, err := os.Stat(cfg.CookiesFile); err != nil {
		log.Warn("Cookie file not found yet – export your payback.de cookies before the next run", "path", cfg.CookiesFile)
	}
	if cfg.RunOnStart {
		job(ctx)
	}
	scheduler.Run(ctx, cfg.RunHour, cfg.RunMinute, cfg.Location, log, func(ctx context.Context) { job(ctx) })
	log.Info("Shutting down")
	return 0
}

// activate runs one cycle, logs the result and sends notifications.
// It reports whether the run was free of problems.
func activate(ctx context.Context, opts activator.Options, cfg config.Config, n *notify.Notifier, log *slog.Logger) bool {
	log.Info("Starting Payback coupon activation")
	start := time.Now()
	sum, err := activator.Run(ctx, opts)

	if err != nil {
		if ctx.Err() != nil {
			log.Info("Run cancelled")
			return false
		}
		log.Error("Activation run failed", "error", err, "activated_before_failure", sum.Activated)
		msg := failureMessage(err, cfg.CookiesFile)
		if sum.Activated > 0 {
			msg += fmt.Sprintf(" (%d coupons were activated before this happened.)", sum.Activated)
		}
		send(n, log, msg)
		return false
	}

	msg := fmt.Sprintf("Activation finished: %d activated, %d failed, %d still pending", sum.Activated, sum.Failed, sum.Remaining)
	log.Info(msg, "duration", time.Since(start).Round(time.Second))

	problem := sum.Failed > 0 || sum.Remaining > 0 || !sum.CookiesSaved
	if !sum.CookiesSaved {
		msg += fmt.Sprintf(". Refreshed cookies could not be saved to %s – check that the directory is writable, or the session will expire", cfg.CookiesFile)
	}
	if problem || cfg.NotifyOnSuccess {
		prefix := "✅ Payback coupons: "
		if problem {
			prefix = "⚠️ Payback coupons: "
		}
		send(n, log, prefix+msg)
	}
	return !problem
}

func failureMessage(err error, cookiesFile string) string {
	switch {
	case errors.Is(err, activator.ErrSessionExpired):
		return fmt.Sprintf("🔑 Payback session expired or cookies missing. Log in on payback.de on your PC, export the cookies again and save them as %s.", cookiesFile)
	case errors.Is(err, activator.ErrBotCheck):
		return "🤖 Payback showed a bot check, the run was skipped. It will retry at the next scheduled time; if this repeats, export fresh cookies."
	case errors.Is(err, activator.ErrPageChanged):
		return "🧩 Payback coupon page was not recognized – the website layout probably changed. Run with --debug-snapshot and update selectors.go."
	default:
		return "❌ Payback coupon run failed: " + err.Error()
	}
}

func send(n *notify.Notifier, log *slog.Logger, msg string) {
	if !n.Enabled() {
		return
	}
	if err := n.Send(msg); err != nil {
		log.Warn("Could not send notification", "error", err)
	}
}

func sendTestNotification(n *notify.Notifier, log *slog.Logger) int {
	if !n.Enabled() {
		log.Error("NOTIFY_URL is not set")
		return 1
	}
	if err := n.Send("🔔 Payback coupons: test notification"); err != nil {
		log.Error("Test notification failed", "error", err)
		return 1
	}
	log.Info("Test notification sent")
	return 0
}
