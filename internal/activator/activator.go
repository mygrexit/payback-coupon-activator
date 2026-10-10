// Package activator opens the payback.de coupon page in headless Chromium
// with the user's exported session cookies and activates every pending
// coupon by clicking its button.
//
// It never logs in by itself and never tries to get past CAPTCHAs or bot
// checks: if the session is gone or a challenge is shown, it stops and
// reports a typed error.
package activator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"

	"github.com/mygrexit/payback-coupon-activator/internal/cookies"
)

var (
	// ErrSessionExpired means there is no usable session: the cookie file
	// is missing/invalid or Payback showed the login page.
	ErrSessionExpired = errors.New("payback session missing or expired")
	// ErrBotCheck means Payback/CDN showed a bot challenge.
	ErrBotCheck = errors.New("payback showed a bot check")
	// ErrPageChanged means the page could not be recognized.
	ErrPageChanged = errors.New("payback coupon page not recognized (layout changed?)")
)

// Options configures a run.
type Options struct {
	CouponsURL  string
	CookiesFile string
	ChromePath  string // empty: autodetect
	UserAgent   string // empty: Chromium default
	NoSandbox   bool

	// DebugScreenshot, if set, is the PNG file a screenshot is written to
	// after the first page load. It contains personal data.
	DebugScreenshot string

	Logger *slog.Logger

	// Tunables; zero values use the defaults below.
	RunTimeout     time.Duration // default 30m (plus 1m reserved for saving cookies)
	PageTimeout    time.Duration // default 45s: wait for a recognizable page
	ClickDelay     time.Duration // default 700ms: pause between clicks
	ConfirmTimeout time.Duration // default 8s: wait for a click to take effect
	SettleDelay    time.Duration // default 2s: wait for lazy content once recognized
	MaxPasses      int           // default 3
	PollInterval   time.Duration // default 250ms
}

func (o *Options) defaults() {
	set := func(d *time.Duration, v time.Duration) {
		if *d == 0 {
			*d = v
		}
	}
	set(&o.RunTimeout, 30*time.Minute)
	set(&o.PageTimeout, 45*time.Second)
	set(&o.ClickDelay, 700*time.Millisecond)
	set(&o.ConfirmTimeout, 8*time.Second)
	set(&o.SettleDelay, 2*time.Second)
	set(&o.PollInterval, 250*time.Millisecond)
	if o.MaxPasses == 0 {
		o.MaxPasses = 3
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
}

// Summary describes a run. It is meaningful even when Run returns an
// error after the first page load (partial results).
type Summary struct {
	Pending      int  // pending coupons found on the first page load
	Activated    int  // distinct coupons confirmed as activated
	Failed       int  // distinct coupons clicked but never confirmed
	Remaining    int  // pending coupons still shown after the final reload
	CookiesSaved bool // refreshed cookies were written back
}

type pageInfo struct {
	State   string `json:"state"`
	Pending int    `json:"pending"`
}

// tracker records per-coupon outcomes across passes, keyed by coupon ID,
// so a coupon clicked in several passes is counted once.
type tracker struct {
	activated map[string]bool
	failed    map[string]bool
}

func (t *tracker) summary(sum *Summary) {
	sum.Activated = len(t.activated)
	sum.Failed = 0
	for l := range t.failed {
		if !t.activated[l] {
			sum.Failed++
		}
	}
}

// Run performs one activation cycle.
func Run(ctx context.Context, opts Options) (Summary, error) {
	opts.defaults()
	log := opts.Logger
	var sum Summary

	target, err := url.Parse(opts.CouponsURL)
	if err != nil {
		return sum, fmt.Errorf("parse coupons URL: %w", err)
	}

	all, err := cookies.Load(opts.CookiesFile)
	switch {
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, cookies.ErrFormat):
		// Same remedy as an expired session: export the cookies again.
		return sum, fmt.Errorf("%w: %v", ErrSessionExpired, err)
	case err != nil:
		return sum, err // e.g. permission denied: not a session problem
	}
	jar := cookies.ForHost(all, target.Hostname(), time.Now())
	if len(jar) == 0 {
		return sum, fmt.Errorf("%w: cookie file contains no unexpired cookies for %s", ErrSessionExpired, target.Hostname())
	}

	// The browser lives a minute longer than the work, so refreshed cookies
	// can still be saved when the run hits its time limit.
	browserCtx, cancel := context.WithTimeout(ctx, opts.RunTimeout+time.Minute)
	defer cancel()
	browserCtx, cancel = newBrowser(browserCtx, opts)
	defer cancel()
	if err := chromedp.Do(browserCtx); err != nil { // starts the browser
		return sum, fmt.Errorf("start browser: %w", err)
	}
	ctx, cancel = context.WithTimeout(browserCtx, opts.RunTimeout)
	defer cancel()

	if err := setCookies(ctx, jar, target.Scheme, log); err != nil {
		return sum, err
	}

	tr := tracker{activated: map[string]bool{}, failed: map[string]bool{}}
	progress := true
	sessionOK := false
	for pass := 1; ; pass++ {
		info, err := load(ctx, opts)
		if pass == 1 && opts.DebugScreenshot != "" {
			writeScreenshot(ctx, opts.DebugScreenshot, log)
		}
		if err != nil {
			// After a successful first load the session was valid, so keep
			// whatever refreshed cookies the browser received.
			if sessionOK && !errors.Is(err, ErrSessionExpired) && browserCtx.Err() == nil {
				sum.CookiesSaved = saveCookies(browserCtx, opts.CookiesFile, target.Hostname(), log)
			}
			return sum, timeLimit(err)
		}
		sessionOK = true
		if pass == 1 {
			sum.Pending = info.Pending
			log.Info(fmt.Sprintf("Found %d pending coupons", info.Pending))
		}
		// Stop when nothing is left, after MaxPasses click passes, or when
		// the previous pass could not activate anything (e.g. coupons that
		// need a code instead of a click).
		if info.Pending == 0 || pass > opts.MaxPasses || !progress {
			sum.Remaining = info.Pending
			break
		}
		if pass > 1 {
			log.Info(fmt.Sprintf("Pass %d: %d coupons still pending", pass, info.Pending))
		}
		before := len(tr.activated)
		err = clickAll(ctx, opts, info.Pending, &tr)
		tr.summary(&sum)
		if err != nil {
			if browserCtx.Err() == nil {
				sum.CookiesSaved = saveCookies(browserCtx, opts.CookiesFile, target.Hostname(), log)
			}
			return sum, timeLimit(err)
		}
		progress = len(tr.activated) > before
	}

	sum.CookiesSaved = saveCookies(ctx, opts.CookiesFile, target.Hostname(), log)
	return sum, nil
}

func newBrowser(ctx context.Context, opts Options) (context.Context, context.CancelFunc) {
	allocOpts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	allocOpts = append(allocOpts, chromedp.WindowSize(1366, 2000))
	if opts.ChromePath != "" {
		allocOpts = append(allocOpts, chromedp.ExecPath(opts.ChromePath))
	}
	if opts.UserAgent != "" {
		allocOpts = append(allocOpts, chromedp.UserAgent(opts.UserAgent))
	}
	if opts.NoSandbox {
		allocOpts = append(allocOpts, chromedp.NoSandbox)
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, allocOpts...)
	// chromedp's own logging can include page/protocol data; keep it at debug.
	logf := func(format string, args ...any) { opts.Logger.Debug("chromedp: " + fmt.Sprintf(format, args...)) }
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx, chromedp.WithErrorf(logf))
	return browserCtx, func() { cancelBrowser(); cancelAlloc() }
}

// setCookies sets cookies one at a time so that a single cookie Chrome
// rejects (e.g. SameSite=None without Secure) does not break the run.
// Cookie names are logged, values never.
func setCookies(ctx context.Context, jar []cookies.Cookie, scheme string, log *slog.Logger) error {
	ok := 0
	for _, p := range cookieParams(jar, scheme) {
		_, err := chromedp.Call(ctx, network.SetCookies, network.SetCookiesParams{Cookies: []*network.CookieParam{p}})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Warn("Skipping cookie rejected by the browser", "cookie", p.Name, "error", err)
			continue
		}
		ok++
	}
	if ok == 0 {
		return fmt.Errorf("%w: the browser accepted none of the exported cookies", ErrSessionExpired)
	}
	log.Debug("Cookies set", "count", ok)
	return nil
}

// load navigates to the coupon page and waits until it is recognized.
func load(ctx context.Context, opts Options) (pageInfo, error) {
	navCtx, cancel := context.WithTimeout(ctx, opts.PageTimeout)
	err := chromedp.Do(navCtx, chromedp.Navigate(opts.CouponsURL))
	cancel()
	if ctx.Err() != nil {
		return pageInfo{}, ctx.Err()
	}
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return pageInfo{}, fmt.Errorf("load coupon page: %w", err)
	}

	// Wait for the client-rendered page to become recognizable. A
	// challenge page is given the full timeout because some challenges
	// resolve on their own in a normal browser.
	deadline := time.Now().Add(opts.PageTimeout)
	var info pageInfo
	for {
		info, err = chromedp.Run(ctx, chromedp.Evaluate[pageInfo](inspectJS))
		if ctx.Err() != nil {
			return info, ctx.Err()
		}
		if err == nil && (info.State == "coupons" || info.State == "login") {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		if err := sleep(ctx, opts.PollInterval); err != nil {
			return info, err
		}
	}

	if info.State == "coupons" {
		// Coupons may still be streaming in, or a client-side auth check
		// may still redirect to the login page: classify once more.
		if err := sleep(ctx, opts.SettleDelay); err != nil {
			return info, err
		}
		info, err = chromedp.Run(ctx, chromedp.Evaluate[pageInfo](inspectJS))
		if ctx.Err() != nil {
			return info, ctx.Err()
		}
		if err != nil {
			return info, fmt.Errorf("%w: %v", ErrPageChanged, err)
		}
	}

	switch info.State {
	case "coupons":
		return info, nil
	case "login":
		return info, ErrSessionExpired
	case "challenge":
		return info, ErrBotCheck
	default:
		return info, ErrPageChanged
	}
}

// clickAll clicks every pending button once, one at a time, and waits for
// each click to take effect. The number of clicks is bounded so a page
// that keeps re-rendering its buttons cannot cause an endless loop.
func clickAll(ctx context.Context, opts Options, pending int, tr *tracker) error {
	maxClicks := pending + 10
	tried := []string{}
	for n := 1; n <= maxClicks; n++ {
		if n > 1 {
			if err := sleep(ctx, opts.ClickDelay); err != nil {
				return err
			}
		}
		next, err := chromedp.Run(ctx, chromedp.Evaluate[clicked](call(clickNextJS, tried)))
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			opts.Logger.Warn("Could not click coupon", "error", err)
			return nil // page in unknown state; the next reload re-checks
		}
		if next.ID == "" {
			return nil
		}
		tried = append(tried, next.ID)
		opts.Logger.Debug("Clicked coupon", "id", next.ID)

		switch state := waitConfirmed(ctx, opts, next.ID); state {
		case "done":
			if !tr.activated[next.ID] {
				opts.Logger.Info("Activated coupon: " + next.Label)
			}
			tr.activated[next.ID] = true
		case "gone":
			// The click navigated away from the coupon list. Not an
			// activation; reload and stop this pass.
			tr.failed[next.ID] = true
			opts.Logger.Warn("Clicking coupon left the coupon page: " + next.Label)
			return nil
		default:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !tr.failed[next.ID] {
				opts.Logger.Warn("Coupon not confirmed as activated: " + next.Label)
			}
			tr.failed[next.ID] = true
		}
	}
	opts.Logger.Warn("Click limit reached for this pass", "limit", maxClicks)
	return nil
}

type clicked struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// waitConfirmed polls until the clicked coupon left the pending section
// ("done"), the coupon page disappeared ("gone"), or ConfirmTimeout
// passed ("pending").
func waitConfirmed(ctx context.Context, opts Options, id string) string {
	deadline := time.Now().Add(opts.ConfirmTimeout)
	for {
		state, err := chromedp.Run(ctx, chromedp.Evaluate[string](call(confirmJS, id)))
		if err == nil && state != "pending" {
			return state
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return "pending"
		}
		if sleep(ctx, opts.PollInterval) != nil {
			return "pending"
		}
	}
}

// timeLimit turns the run's own deadline into a readable error.
func timeLimit(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("run time limit reached before all coupons were processed; the rest follow in the next run")
	}
	return err
}

// call renders a JS function call with JSON-encoded arguments.
func call(fn string, args ...any) string {
	enc, _ := json.Marshal(args) // ints and strings always encode
	return fmt.Sprintf("%s(...%s)", fn, enc)
}

// saveCookies writes the browser's current cookies for the site back to
// the cookie file. Failures are logged and reported via the return value.
func saveCookies(ctx context.Context, path, host string, log *slog.Logger) bool {
	res, err := chromedp.Call(ctx, storage.GetCookies, storage.GetCookiesParams{})
	if err == nil {
		var cs []cookies.Cookie
		for _, c := range res.Cookies {
			cc := cookies.Cookie{
				Name:     c.Name,
				Value:    c.Value,
				Domain:   c.Domain,
				Path:     c.Path,
				Secure:   c.Secure,
				HTTPOnly: c.HTTPOnly,
				SameSite: string(c.SameSite),
			}
			if !c.Session && c.Expires > 0 {
				cc.Expires = time.Unix(int64(c.Expires), 0)
			}
			cs = append(cs, cc)
		}
		cs = cookies.ForHost(cs, host, time.Now())
		if len(cs) == 0 {
			err = errors.New("browser returned no cookies")
		} else {
			err = cookies.Save(path, cs)
		}
	}
	if err != nil {
		log.Warn("Could not save refreshed cookies", "error", err)
		return false
	}
	return true
}

func cookieParams(cs []cookies.Cookie, scheme string) []*network.CookieParam {
	out := make([]*network.CookieParam, 0, len(cs))
	for _, c := range cs {
		p := &network.CookieParam{
			Name:     c.Name,
			Value:    c.Value,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
			SameSite: network.CookieSameSite(c.SameSite),
		}
		if c.HostOnly() {
			// Host-only cookies must be set via URL; a Domain makes them
			// domain cookies.
			p.URL = scheme + "://" + c.Domain + c.Path
		} else {
			p.Domain = c.Domain
		}
		if !c.Expires.IsZero() {
			p.Expires = cdp.TimeSinceEpoch(float64(c.Expires.Unix()))
		}
		out = append(out, p)
	}
	return out
}

func writeScreenshot(ctx context.Context, path string, log *slog.Logger) {
	buf, err := chromedp.Run(ctx, chromedp.FullScreenshot(100)) // 100 = PNG
	if err == nil {
		err = os.WriteFile(path, buf, 0o600)
	}
	if err != nil {
		log.Warn("Could not write debug screenshot", "error", err)
		return
	}
	log.Info("Debug screenshot written (contains personal data – delete after use)", "path", path)
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
