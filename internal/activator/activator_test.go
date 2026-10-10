package activator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mygrexit/payback-coupon-activator/internal/cookies"
)

func findChrome(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("CHROME_PATH"); p != "" {
		return p
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	t.Skip("no Chromium/Chrome binary found; skipping browser tests")
	return ""
}

type fakeCoupon struct {
	ID      int    `json:"id"`
	Partner string `json:"partner"`
	Title   string `json:"title"`
	Code    bool   `json:"code"`    // needs a code: clicking does nothing
	Broken  bool   `json:"broken"`  // server rejects the activation
	Details bool   `json:"details"` // card has a "Details" link-button first
	Soon    bool   `json:"soon"`    // not yet activatable: disabled button
}

// fakePayback imitates the coupon page: client-rendered, cookie-gated,
// activation via fetch, and one coupon that only appears after the first
// page load (lazy loading).
type fakePayback struct {
	mu        sync.Mutex
	loads     int
	activated map[int]bool
	delay     time.Duration // latency of /activate
}

func (f *fakePayback) handler() http.Handler {
	coupons := []fakeCoupon{
		{ID: 1, Partner: "dm", Title: "10fach Punkte", Details: true},
		{ID: 2, Partner: "ARAL", Title: "5fach Punkte"},
		{ID: 3, Partner: "REWE", Title: "3fach Punkte"},
		{ID: 4, Partner: "Otto", Title: "Code eingeben", Code: true},
		{ID: 6, Partner: "Lidl", Title: "ab morgen", Soon: true},
		{ID: 7, Partner: "Penny", Title: "Serverfehler", Broken: true},
	}
	late := fakeCoupon{ID: 5, Partner: "Amazon", Title: "2fach Punkte"}

	mux := http.NewServeMux()
	mux.HandleFunc("/coupons", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("sess"); err != nil || c.Value != "valid" {
			if r.URL.Query().Has("teaser") {
				// Logged-out page that first renders coupon teasers and then
				// redirects to the login page client-side.
				io.WriteString(w, `<html><body><div data-testid="not-activated-coupons-section"></div>
<script>setTimeout(() => { location.href = "/login"; }, 150);</script></body></html>`)
				return
			}
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		f.mu.Lock()
		f.loads++
		list := append([]fakeCoupon(nil), coupons...)
		if f.loads > 1 {
			list = append(list, late)
		}
		var pending, done []fakeCoupon
		for _, c := range list {
			if f.activated[c.ID] {
				done = append(done, c)
			} else {
				pending = append(pending, c)
			}
		}
		f.mu.Unlock()

		http.SetCookie(w, &http.Cookie{Name: "refreshed", Value: "yes", Path: "/", MaxAge: 3600})
		p, _ := json.Marshal(pending)
		d, _ := json.Marshal(done)
		fmt.Fprintf(w, couponPageHTML, p, d)
	})
	mux.HandleFunc("/activate", func(w http.ResponseWriter, r *http.Request) {
		var id int
		fmt.Sscan(r.URL.Query().Get("id"), &id)
		f.mu.Lock()
		delay := f.delay
		f.mu.Unlock()
		time.Sleep(delay)
		if r.URL.Query().Has("broken") {
			http.Error(w, "nope", http.StatusInternalServerError)
			return
		}
		f.mu.Lock()
		f.activated[id] = true
		f.mu.Unlock()
	})
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<html><body><form><input name="u"><input type="password" name="p"><button>Login</button></form></body></html>`)
	})
	mux.HandleFunc("/challenge", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<html><head><title>Just a moment...</title></head><body><div id="challenge-form"></div></body></html>`)
	})
	mux.HandleFunc("/unknown", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<html><body><h1>Wartungsarbeiten</h1></body></html>`)
	})
	return mux
}

// couponPageHTML mirrors the structure of the real page (2026-10-10):
// test IDs carry the coupon ID, activated coupons leave the
// not-activated section, code coupons have no "-not_activated" button.
const couponPageHTML = `<!doctype html><html><body><header><a href="/logout">Abmelden</a></header><main id="app">loading…</main><script>
const pending = %s || [];
const done = %s || [];
setTimeout(() => {
  const app = document.getElementById("app");
  app.innerHTML = '<div data-testid="not-activated-coupons-section"><p data-testid="not-activated-coupons-headline">Nicht aktiviert</p></div>' +
                  '<div data-testid="activated-coupons-section"></div>';
  const open = app.children[0], active = app.children[1];
  const card = (c, prefix) => {
    const item = document.createElement("div");
    item.setAttribute("data-testid", prefix + c.id);
    item.innerHTML =
      '<div data-testid="coupon-' + c.id + '">' +
      '<div data-testid="coupon-partner-logo-' + c.id + '"><img alt="' + c.partner + '"></div>' +
      '<div data-testid="coupon-headline-' + c.id + '">' + c.title + '</div>' +
      '<p data-testid="coupon-headline-description-' + c.id + '">auf den Einkauf!*</p>' +
      '<div data-testid="coupon-button-list-' + c.id + '"></div></div>';
    return item;
  };
  for (const c of done) active.appendChild(card(c, "activated-coupon-item-"));
  for (const c of pending) {
    const item = card(c, "not-activated-coupon-item-");
    const list = item.querySelector('[data-testid^="coupon-button-list-"]');
    if (c.details) {
      const d = document.createElement("button");
      d.textContent = "Details";
      d.onclick = () => { location.href = "/details"; };
      list.appendChild(d);
    }
    const b = document.createElement("button");
    if (c.code) {
      b.textContent = "Zur Code-Eingabe";
      b.setAttribute("data-testid", "coupon-button-" + c.id + "-code");
    } else {
      b.textContent = c.soon ? "Bald verfügbar" : "Jetzt aktivieren";
      b.setAttribute("data-testid", "coupon-button-" + c.id + "-not_activated");
    }
    b.disabled = !!c.soon;
    b.onclick = () => {
      if (c.code) return;
      b.disabled = true; // in flight
      fetch("/activate?id=" + c.id + (c.broken ? "&broken" : ""), {method: "POST"}).then((r) => {
        if (!r.ok) { b.disabled = false; return; }
        item.remove();
        active.appendChild(card(c, "activated-coupon-item-"));
      });
    };
    list.appendChild(b);
    open.appendChild(item);
  }
}, 200);
</script></body></html>`

func setup(t *testing.T, cookieValue string) (*fakePayback, *httptest.Server, string) {
	t.Helper()
	f := &fakePayback{activated: map[int]bool{}}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)

	path := filepath.Join(t.TempDir(), "cookies.json")
	err := cookies.Save(path, []cookies.Cookie{
		{Name: "sess", Value: cookieValue, Domain: "127.0.0.1", Path: "/", Expires: time.Now().Add(time.Hour)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return f, srv, path
}

func testOpts(t *testing.T, url, cookieFile string) Options {
	return Options{
		CouponsURL:   url,
		CookiesFile:  cookieFile,
		ChromePath:   findChrome(t),
		NoSandbox:    os.Getenv("CI") != "",
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		RunTimeout:     90 * time.Second,
		PageTimeout:    3 * time.Second,
		ClickDelay:     150 * time.Millisecond,
		ConfirmTimeout: 2 * time.Second,
		SettleDelay:    300 * time.Millisecond,
		PollInterval:   50 * time.Millisecond,
	}
}

func TestRunActivatesAllPendingCoupons(t *testing.T) {
	f, srv, cookieFile := setup(t, "valid")
	sum, err := Run(context.Background(), testOpts(t, srv.URL+"/coupons", cookieFile))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Pending excludes the disabled "ab morgen" coupon and the code-only
	// coupon; the server-rejected coupon stays pending; the late coupon is
	// picked up in the second pass; "Details" buttons are never clicked.
	want := Summary{Pending: 4, Activated: 4, Failed: 1, Remaining: 1, CookiesSaved: true}
	if sum != want {
		t.Errorf("summary = %+v, want %+v", sum, want)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range []int{1, 2, 3, 5} {
		if !f.activated[id] {
			t.Errorf("coupon %d not activated on server", id)
		}
	}

	saved, err := cookies.Load(cookieFile)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range saved {
		names = append(names, c.Name)
	}
	if got := strings.Join(names, ","); !strings.Contains(got, "sess") || !strings.Contains(got, "refreshed") {
		t.Errorf("saved cookies = %s, want sess and refreshed", got)
	}
}

// Activation slower than the click delay must still be confirmed.
func TestRunSlowActivation(t *testing.T) {
	f, srv, cookieFile := setup(t, "valid")
	f.delay = 600 * time.Millisecond
	sum, err := Run(context.Background(), testOpts(t, srv.URL+"/coupons", cookieFile))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := Summary{Pending: 4, Activated: 4, Failed: 1, Remaining: 1, CookiesSaved: true}
	if sum != want {
		t.Errorf("summary = %+v, want %+v", sum, want)
	}
}

// Hitting the run time limit keeps partial results and still saves the
// refreshed cookies.
func TestRunTimeLimit(t *testing.T) {
	f, srv, cookieFile := setup(t, "valid")
	f.delay = 400 * time.Millisecond
	opts := testOpts(t, srv.URL+"/coupons", cookieFile)
	opts.RunTimeout = 2 * time.Second
	sum, err := Run(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "time limit") {
		t.Fatalf("err = %v, want time limit error", err)
	}
	if sum.Activated == 0 || !sum.CookiesSaved {
		t.Errorf("summary = %+v, want partial activations and saved cookies", sum)
	}
}

func TestRunNothingPending(t *testing.T) {
	f, srv, cookieFile := setup(t, "valid")
	for id := 1; id <= 7; id++ {
		f.activated[id] = true
	}
	sum, err := Run(context.Background(), testOpts(t, srv.URL+"/coupons", cookieFile))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum != (Summary{CookiesSaved: true}) {
		t.Errorf("summary = %+v, want only CookiesSaved", sum)
	}
}

func TestRunErrors(t *testing.T) {
	tests := []struct {
		name   string
		cookie string
		path   string
		want   error
	}{
		{"session expired", "stale", "/coupons", ErrSessionExpired},
		{"login after coupon teaser", "stale", "/coupons?teaser=1", ErrSessionExpired},
		{"bot check", "valid", "/challenge", ErrBotCheck},
		{"unknown page", "valid", "/unknown", ErrPageChanged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, srv, cookieFile := setup(t, tt.cookie)
			before, _ := os.ReadFile(cookieFile)

			_, err := Run(context.Background(), testOpts(t, srv.URL+tt.path, cookieFile))
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			after, _ := os.ReadFile(cookieFile)
			if string(before) != string(after) {
				t.Error("cookie file must not be rewritten after a failed run")
			}
		})
	}
}

func TestRunWithoutMatchingCookies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.json")
	if err := cookies.Save(path, []cookies.Cookie{{Name: "x", Value: "y", Domain: ".example.com", Path: "/"}}); err != nil {
		t.Fatal(err)
	}
	// No browser needed: fails before Chromium starts.
	_, err := Run(context.Background(), Options{CouponsURL: "https://www.payback.de/coupons", CookiesFile: path})
	if !errors.Is(err, ErrSessionExpired) {
		t.Errorf("err = %v, want ErrSessionExpired", err)
	}
}
